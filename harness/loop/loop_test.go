package loop

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/flarexio/stoa/llm"
)

type testIntent struct {
	Action string
}

type fakeEngine struct {
	results []llm.ReasoningOutput[testIntent]
	inputs  []llm.ReasoningInput
}

func (e *fakeEngine) Predict(ctx context.Context, input llm.ReasoningInput) (llm.ReasoningOutput[testIntent], error) {
	e.inputs = append(e.inputs, input)
	if err := ctx.Err(); err != nil {
		return llm.ReasoningOutput[testIntent]{}, err
	}
	if len(e.results) == 0 {
		return llm.ReasoningOutput[testIntent]{}, errors.New("no result")
	}
	result := e.results[0]
	e.results = e.results[1:]
	return result, nil
}

func intentOutput(action, rationale string) llm.ReasoningOutput[testIntent] {
	return llm.IntentOutput(testIntent{Action: action}, nil, rationale)
}

func toolCallsOutput(rationale string, calls ...llm.ToolCall) llm.ReasoningOutput[testIntent] {
	return llm.ToolCallsOutput[testIntent](calls, nil, rationale)
}

func TestRunnerExecutesValidatedIntent(t *testing.T) {
	engine := &fakeEngine{
		results: []llm.ReasoningOutput[testIntent]{
			intentOutput("continue", "action is supported"),
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
		results: []llm.ReasoningOutput[testIntent]{
			intentOutput("delete", "try invalid action"),
			intentOutput("continue", "correct unsupported action"),
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
		results: []llm.ReasoningOutput[testIntent]{
			intentOutput("delete", "try invalid action"),
			intentOutput("continue", "correct unsupported action"),
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
		results: []llm.ReasoningOutput[testIntent]{
			intentOutput("continue", "first try"),
			intentOutput("continue", "retry after executor feedback"),
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
		results: []llm.ReasoningOutput[testIntent]{
			intentOutput("delete", "invalid"),
			intentOutput("delete", "still invalid"),
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

type recordingSink struct {
	events []llm.CycleEvent
}

func (s *recordingSink) Emit(_ context.Context, event llm.CycleEvent) error {
	s.events = append(s.events, event)
	return nil
}

func TestEventSinkReceivesEventsInOrder(t *testing.T) {
	engine := &fakeEngine{
		results: []llm.ReasoningOutput[testIntent]{
			intentOutput("continue", "action is supported"),
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
		results: []llm.ReasoningOutput[testIntent]{
			intentOutput("delete", "try invalid"),
			intentOutput("continue", "corrected"),
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
	// turn 1: model_output + validation_error; turn 2: model_output + observation
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
		results: []llm.ReasoningOutput[testIntent]{
			intentOutput("continue", "first try"),
			intentOutput("continue", "retry"),
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
	// turn 1: model_output + execution_error; turn 2: model_output + observation
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
		results: []llm.ReasoningOutput[testIntent]{
			intentOutput("delete", "try invalid"),
			intentOutput("continue", "corrected"),
		},
	}

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
		results: []llm.ReasoningOutput[testIntent]{
			intentOutput("delete", "try invalid"),
			intentOutput("continue", "corrected"),
		},
	}

	sink := &recordingSink{}
	runner := Runner[testIntent]{
		Engine: engine,
		Validator: ValidatorFunc[testIntent](func(_ context.Context, intent testIntent) error {
			if intent.Action == "delete" {
				cancel()
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

func TestRunnerRunsToolThenIntent(t *testing.T) {
	engine := &fakeEngine{
		results: []llm.ReasoningOutput[testIntent]{
			toolCallsOutput("need a lookup first", llm.ToolCall{Name: "echo", Args: json.RawMessage(`{"q":"hello"}`)}),
			intentOutput("continue", "now I can act"),
		},
	}

	var toolArgs string
	runner := Runner[testIntent]{
		Engine: engine,
		Tools: map[string]Tool{
			"echo": {
				Spec: llm.ToolSpec{
					Name:        "echo",
					Description: "echo back its args",
					ArgsSchema:  json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}`),
				},
				Handler: func(_ context.Context, args json.RawMessage) (string, error) {
					toolArgs = string(args)
					return "echoed: " + string(args), nil
				},
			},
		},
		Validator: ValidatorFunc[testIntent](func(context.Context, testIntent) error { return nil }),
		Executor: ExecutorFunc[testIntent](func(context.Context, testIntent) (llm.Observation, error) {
			return llm.Observation{Summary: "done"}, nil
		}),
	}

	result, err := runner.Run(context.Background(), llm.ReasoningInput{Task: "go"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Turns != 2 {
		t.Fatalf("turns = %d, want 2 (one tool round + one intent)", result.Turns)
	}
	if toolArgs != `{"q":"hello"}` {
		t.Errorf("tool received args %q, want the raw JSON the model supplied", toolArgs)
	}
	if len(engine.inputs) != 2 {
		t.Fatalf("engine called %d times, want 2", len(engine.inputs))
	}
	specs := engine.inputs[0].Tools
	if len(specs) != 1 || specs[0].Name != "echo" {
		t.Fatalf("first turn tool specs = %+v, want one [echo]", specs)
	}
	var sawToolResult bool
	for _, ev := range engine.inputs[1].Events {
		if ev.Kind == llm.EventToolResult && strings.Contains(ev.Content, "echoed:") {
			sawToolResult = true
		}
	}
	if !sawToolResult {
		t.Error("second turn did not receive the tool result as an event")
	}
	if result.Observation.Summary != "done" {
		t.Errorf("observation = %q, want done", result.Observation.Summary)
	}
}

func TestRunnerUnknownToolFeedsBackAndContinues(t *testing.T) {
	// no Tools registered: unknown tool call must feed back, not abort.
	engine := &fakeEngine{
		results: []llm.ReasoningOutput[testIntent]{
			toolCallsOutput("try a tool", llm.ToolCall{Name: "nope"}),
			intentOutput("continue", "fall back to acting"),
		},
	}
	runner := Runner[testIntent]{
		Engine:    engine,
		Validator: ValidatorFunc[testIntent](func(context.Context, testIntent) error { return nil }),
		Executor: ExecutorFunc[testIntent](func(context.Context, testIntent) (llm.Observation, error) {
			return llm.Observation{Summary: "done"}, nil
		}),
	}

	result, err := runner.Run(context.Background(), llm.ReasoningInput{Task: "go"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Turns != 2 {
		t.Fatalf("turns = %d, want 2", result.Turns)
	}
	var sawNotAvailable bool
	for _, ev := range engine.inputs[1].Events {
		if ev.Kind == llm.EventToolResult && strings.Contains(ev.Content, "not available") {
			sawNotAvailable = true
		}
	}
	if !sawNotAvailable {
		t.Error("unknown tool should feed back a 'not available' tool_result event")
	}
}
