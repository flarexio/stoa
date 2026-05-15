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

func (e *fakeEngine) Predict(ctx context.Context, input llm.ReasoningInput) (llm.ReasoningResult[testIntent], error) {
	e.inputs = append(e.inputs, input)
	if err := ctx.Err(); err != nil {
		return llm.ReasoningResult[testIntent]{}, err
	}
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

// --- EventSink tests ---

type recordingSink struct {
	events []llm.CycleEvent
}

func (s *recordingSink) Emit(_ context.Context, event llm.CycleEvent) error {
	s.events = append(s.events, event)
	return nil
}

func TestEventSinkReceivesEventsInOrder(t *testing.T) {
	engine := &fakeEngine{
		results: []llm.ReasoningResult[testIntent]{
			{
				Rationale: "action is supported",
				Intent:    testIntent{Action: "continue"},
			},
		},
	}

	sink := &recordingSink{}
	runner := Runner[testIntent]{
		Engine: engine,
		Validator: ValidatorFunc[testIntent](func(_ context.Context, intent testIntent) error {
			return nil
		}),
		Executor: ExecutorFunc[testIntent](func(_ context.Context, intent testIntent) (llm.Observation, error) {
			return llm.Observation{Summary: "done"}, nil
		}),
		Sink: sink,
	}

	result, err := runner.Run(context.Background(), llm.ReasoningInput{Task: "move forward"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Turns != 1 {
		t.Fatalf("turns = %d, want 1", result.Turns)
	}
	if len(sink.events) != 2 {
		t.Fatalf("sink events = %d, want 2 (model_output + observation)", len(sink.events))
	}
	if sink.events[0].Kind != llm.EventModelOutput {
		t.Fatalf("first sink event kind = %q, want model_output", sink.events[0].Kind)
	}
	if sink.events[1].Kind != llm.EventObservation {
		t.Fatalf("second sink event kind = %q, want observation", sink.events[1].Kind)
	}
}

func TestEventSinkReceivesValidationErrors(t *testing.T) {
	engine := &fakeEngine{
		results: []llm.ReasoningResult[testIntent]{
			{Rationale: "try invalid", Intent: testIntent{Action: "delete"}},
			{Rationale: "corrected", Intent: testIntent{Action: "continue"}},
		},
	}

	sink := &recordingSink{}
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
		Sink:     sink,
	}

	result, err := runner.Run(context.Background(), llm.ReasoningInput{Task: "move forward"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Turns != 2 {
		t.Fatalf("turns = %d, want 2", result.Turns)
	}
	// Turn 1: model_output, validation_error
	// Turn 2: model_output, observation
	if len(sink.events) != 4 {
		t.Fatalf("sink events = %d, want 4", len(sink.events))
	}
	if sink.events[0].Kind != llm.EventModelOutput {
		t.Fatalf("event 0 kind = %q, want model_output", sink.events[0].Kind)
	}
	if sink.events[1].Kind != llm.EventValidationError {
		t.Fatalf("event 1 kind = %q, want validation_error", sink.events[1].Kind)
	}
	if sink.events[2].Kind != llm.EventModelOutput {
		t.Fatalf("event 2 kind = %q, want model_output", sink.events[2].Kind)
	}
	if sink.events[3].Kind != llm.EventObservation {
		t.Fatalf("event 3 kind = %q, want observation", sink.events[3].Kind)
	}
}

func TestEventSinkReceivesExecutionErrors(t *testing.T) {
	engine := &fakeEngine{
		results: []llm.ReasoningResult[testIntent]{
			{Rationale: "first try", Intent: testIntent{Action: "continue"}},
			{Rationale: "retry", Intent: testIntent{Action: "continue"}},
		},
	}
	executions := 0

	sink := &recordingSink{}
	runner := Runner[testIntent]{
		Engine:    engine,
		Validator: ValidatorFunc[testIntent](func(context.Context, testIntent) error { return nil }),
		Executor: ExecutorFunc[testIntent](func(_ context.Context, intent testIntent) (llm.Observation, error) {
			executions++
			if executions == 1 {
				return llm.Observation{}, errors.New("temporary failure")
			}
			return llm.Observation{Summary: "done"}, nil
		}),
		MaxTurns: 2,
		Sink:     sink,
	}

	result, err := runner.Run(context.Background(), llm.ReasoningInput{Task: "move forward"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Turns != 2 {
		t.Fatalf("turns = %d, want 2", result.Turns)
	}
	// Turn 1: model_output, execution_error
	// Turn 2: model_output, observation
	if len(sink.events) != 4 {
		t.Fatalf("sink events = %d, want 4", len(sink.events))
	}
	if sink.events[1].Kind != llm.EventExecutionError {
		t.Fatalf("event 1 kind = %q, want execution_error", sink.events[1].Kind)
	}
	if sink.events[3].Kind != llm.EventObservation {
		t.Fatalf("event 3 kind = %q, want observation", sink.events[3].Kind)
	}
}

type errorSink struct {
	errAfter int
	count    int
}

func (s *errorSink) Emit(_ context.Context, _ llm.CycleEvent) error {
	s.count++
	if s.count > s.errAfter {
		return errors.New("sink disconnected")
	}
	return nil
}

func TestEventSinkErrorPropagates(t *testing.T) {
	engine := &fakeEngine{
		results: []llm.ReasoningResult[testIntent]{
			{Rationale: "try invalid", Intent: testIntent{Action: "delete"}},
			{Rationale: "corrected", Intent: testIntent{Action: "continue"}},
		},
	}

	// Sink will fail after 1 event (the first model_output).
	sink := &errorSink{errAfter: 1}
	runner := Runner[testIntent]{
		Engine: engine,
		Validator: ValidatorFunc[testIntent](func(_ context.Context, intent testIntent) error {
			return errors.New("invalid")
		}),
		Executor: ExecutorFunc[testIntent](func(context.Context, testIntent) (llm.Observation, error) {
			t.Fatal("executor should not run")
			return llm.Observation{}, nil
		}),
		MaxTurns: 2,
		Sink:     sink,
	}

	_, err := runner.Run(context.Background(), llm.ReasoningInput{Task: "move forward"})
	if err == nil {
		t.Fatal("expected error from sink failure, got nil")
	}
	if !strings.Contains(err.Error(), "event sink") {
		t.Fatalf("error = %v, want event sink error", err)
	}
}

func TestContextCancellationAbortsLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	engine := &fakeEngine{
		results: []llm.ReasoningResult[testIntent]{
			{Rationale: "try invalid", Intent: testIntent{Action: "delete"}},
			{Rationale: "corrected", Intent: testIntent{Action: "continue"}},
		},
	}

	sink := &recordingSink{}
	runner := Runner[testIntent]{
		Engine: engine,
		Validator: ValidatorFunc[testIntent](func(_ context.Context, intent testIntent) error {
			if intent.Action == "delete" {
				cancel() // cancel context on first invalid intent
				return errors.New("unsupported action")
			}
			return nil
		}),
		Executor: ExecutorFunc[testIntent](func(_ context.Context, intent testIntent) (llm.Observation, error) {
			return llm.Observation{Summary: "done"}, nil
		}),
		MaxTurns: 2,
		Sink:     sink,
	}

	_, err := runner.Run(ctx, llm.ReasoningInput{Task: "move forward"})
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	// Should have emitted model_output and validation_error before the
	// cancellation took effect on the next Predict call.
	if len(sink.events) < 2 {
		t.Fatalf("sink events = %d, want at least 2", len(sink.events))
	}
	if sink.events[0].Kind != llm.EventModelOutput {
		t.Fatalf("event 0 kind = %q, want model_output", sink.events[0].Kind)
	}
	if sink.events[1].Kind != llm.EventValidationError {
		t.Fatalf("event 1 kind = %q, want validation_error", sink.events[1].Kind)
	}
}
