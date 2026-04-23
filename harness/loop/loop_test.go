package loop

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/flarexio/stoa/llm"
)

type testIntent struct {
	Action string
}

type fakeEngine struct {
	results []llm.ReasoningResult[testIntent]
	inputs  []llm.ReasoningInput
}

func (e *fakeEngine) Predict(_ context.Context, input llm.ReasoningInput) (llm.ReasoningResult[testIntent], error) {
	e.inputs = append(e.inputs, input)
	if len(e.results) == 0 {
		return llm.ReasoningResult[testIntent]{}, errors.New("no result")
	}
	result := e.results[0]
	e.results = e.results[1:]
	return result, nil
}

func TestRunnerExecutesValidatedIntent(t *testing.T) {
	engine := &fakeEngine{
		results: []llm.ReasoningResult[testIntent]{
			{
				Rationale: "action is supported",
				Intent:    testIntent{Action: "continue"},
			},
		},
	}

	runner := Runner[testIntent]{
		Engine: engine,
		Validator: ValidatorFunc[testIntent](func(_ context.Context, intent testIntent) error {
			if intent.Action != "continue" {
				t.Fatalf("validated action = %q, want continue", intent.Action)
			}
			return nil
		}),
		Executor: ExecutorFunc[testIntent](func(_ context.Context, intent testIntent) (llm.Observation, error) {
			return llm.Observation{Summary: "done"}, nil
		}),
	}

	result, err := runner.Run(context.Background(), llm.ReasoningInput{Task: "move forward"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if result.Turns != 1 {
		t.Fatalf("turns = %d, want 1", result.Turns)
	}
	if result.Observation.Summary != "done" {
		t.Fatalf("observation = %q, want done", result.Observation.Summary)
	}
	if last := result.Events[len(result.Events)-1]; last.Kind != llm.EventObservation {
		t.Fatalf("last event kind = %q, want observation", last.Kind)
	}
}

func TestRunnerFeedsValidationErrorBackIntoNextTurn(t *testing.T) {
	engine := &fakeEngine{
		results: []llm.ReasoningResult[testIntent]{
			{
				Rationale: "try invalid action",
				Intent:    testIntent{Action: "delete"},
			},
			{
				Rationale: "correct unsupported action",
				Intent:    testIntent{Action: "continue"},
			},
		},
	}

	runner := Runner[testIntent]{
		Engine: engine,
		Validator: ValidatorFunc[testIntent](func(_ context.Context, intent testIntent) error {
			if intent.Action == "delete" {
				return errors.New("unsupported action")
			}
			return nil
		}),
		Executor: ExecutorFunc[testIntent](func(_ context.Context, intent testIntent) (llm.Observation, error) {
			return llm.Observation{Summary: "done"}, nil
		}),
		MaxTurns: 2,
	}

	result, err := runner.Run(context.Background(), llm.ReasoningInput{Task: "move forward"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if result.Turns != 2 {
		t.Fatalf("turns = %d, want 2", result.Turns)
	}
	if len(engine.inputs) != 2 {
		t.Fatalf("engine calls = %d, want 2", len(engine.inputs))
	}
	secondTurnEvents := engine.inputs[1].Events
	if got := secondTurnEvents[len(secondTurnEvents)-1]; got.Kind != llm.EventValidationError {
		t.Fatalf("feedback kind = %q, want validation_error", got.Kind)
	}
}

func TestRunnerUsesCustomValidationFormatter(t *testing.T) {
	engine := &fakeEngine{
		results: []llm.ReasoningResult[testIntent]{
			{
				Rationale: "try invalid action",
				Intent:    testIntent{Action: "delete"},
			},
			{
				Rationale: "correct unsupported action",
				Intent:    testIntent{Action: "continue"},
			},
		},
	}

	runner := Runner[testIntent]{
		Engine: engine,
		Validator: ValidatorFunc[testIntent](func(_ context.Context, intent testIntent) error {
			if intent.Action == "delete" {
				return errors.New("unsupported action")
			}
			return nil
		}),
		Executor: ExecutorFunc[testIntent](func(_ context.Context, intent testIntent) (llm.Observation, error) {
			return llm.Observation{Summary: "done"}, nil
		}),
		ValidationFormatter: func(err error) string {
			return "domain feedback: " + err.Error()
		},
		MaxTurns: 2,
	}

	_, err := runner.Run(context.Background(), llm.ReasoningInput{Task: "move forward"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	secondTurnEvents := engine.inputs[1].Events
	feedback := secondTurnEvents[len(secondTurnEvents)-1]
	if !strings.Contains(feedback.Content, "domain feedback: unsupported action") {
		t.Fatalf("feedback content = %q, want custom validation feedback", feedback.Content)
	}
}

func TestRunnerFeedsExecutionErrorBackIntoNextTurn(t *testing.T) {
	engine := &fakeEngine{
		results: []llm.ReasoningResult[testIntent]{
			{Rationale: "first try", Intent: testIntent{Action: "continue"}},
			{Rationale: "retry after executor feedback", Intent: testIntent{Action: "continue"}},
		},
	}
	executions := 0

	runner := Runner[testIntent]{
		Engine:    engine,
		Validator: ValidatorFunc[testIntent](func(context.Context, testIntent) error { return nil }),
		Executor: ExecutorFunc[testIntent](func(_ context.Context, intent testIntent) (llm.Observation, error) {
			executions++
			if executions == 1 {
				return llm.Observation{}, errors.New("temporary tool failure")
			}
			return llm.Observation{Summary: "done"}, nil
		}),
		MaxTurns: 2,
	}

	result, err := runner.Run(context.Background(), llm.ReasoningInput{Task: "move forward"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if result.Turns != 2 {
		t.Fatalf("turns = %d, want 2", result.Turns)
	}
	secondTurnEvents := engine.inputs[1].Events
	if got := secondTurnEvents[len(secondTurnEvents)-1]; got.Kind != llm.EventExecutionError {
		t.Fatalf("feedback kind = %q, want execution_error", got.Kind)
	}
}

func TestRunnerStopsAtMaxTurns(t *testing.T) {
	engine := &fakeEngine{
		results: []llm.ReasoningResult[testIntent]{
			{Rationale: "invalid", Intent: testIntent{Action: "delete"}},
			{Rationale: "still invalid", Intent: testIntent{Action: "delete"}},
		},
	}

	runner := Runner[testIntent]{
		Engine: engine,
		Validator: ValidatorFunc[testIntent](func(context.Context, testIntent) error {
			return errors.New("invalid")
		}),
		Executor: ExecutorFunc[testIntent](func(context.Context, testIntent) (llm.Observation, error) {
			t.Fatal("executor should not run for invalid intents")
			return llm.Observation{}, nil
		}),
		MaxTurns: 2,
	}

	result, err := runner.Run(context.Background(), llm.ReasoningInput{Task: "move forward"})
	if !errors.Is(err, ErrMaxTurnsExceeded) {
		t.Fatalf("Run error = %v, want ErrMaxTurnsExceeded", err)
	}
	if result.Turns != 2 {
		t.Fatalf("turns = %d, want 2", result.Turns)
	}
}

func TestRunnerRequiresPorts(t *testing.T) {
	_, err := Runner[testIntent]{}.Run(context.Background(), llm.ReasoningInput{})
	if !errors.Is(err, ErrMissingEngine) {
		t.Fatalf("Run error = %v, want ErrMissingEngine", err)
	}
}
