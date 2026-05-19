package loop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/flarexio/stoa/llm"
)

const defaultMaxTurns = 3

var (
	ErrMissingEngine    = errors.New("loop: missing reasoning engine")
	ErrMissingValidator = errors.New("loop: missing validator")
	ErrMissingExecutor  = errors.New("loop: missing executor")
	ErrMaxTurnsExceeded = errors.New("loop: max turns exceeded")
)

// Validator is supplied by a feature or plugin. The loop owns when validation
// runs; the feature owns the domain-specific rules.
type Validator[TIntent any] interface {
	Validate(ctx context.Context, intent TIntent) error
}

type ValidatorFunc[TIntent any] func(ctx context.Context, intent TIntent) error

func (f ValidatorFunc[TIntent]) Validate(ctx context.Context, intent TIntent) error {
	return f(ctx, intent)
}

// Executor performs a validated intent. It is a port owned by the use case and
// implemented by adapters or infrastructure.
type Executor[TIntent any] interface {
	Execute(ctx context.Context, intent TIntent) (llm.Observation, error)
}

type ExecutorFunc[TIntent any] func(ctx context.Context, intent TIntent) (llm.Observation, error)

func (f ExecutorFunc[TIntent]) Execute(ctx context.Context, intent TIntent) (llm.Observation, error) {
	return f(ctx, intent)
}

// ToolHandler answers one tool call. args is the raw JSON the model supplied
// for the call; the handler decodes it into its own typed parameters and
// returns a result string for the model to read on the next turn. A handler
// is owned by a feature, never by the loop -- the loop only routes a call to
// its handler by name.
type ToolHandler func(ctx context.Context, args json.RawMessage) (string, error)

// EventSink receives per-turn cycle events as they happen. A caller that
// wants to observe the reason -> validate -> execute cycle incrementally
// (e.g. a TUI) implements this interface and sets it on Runner. When Sink
// is nil the existing blocking Run(ctx, input) API is unchanged.
type EventSink interface {
	Emit(ctx context.Context, event llm.CycleEvent) error
}

type Runner[TIntent any] struct {
	Engine              llm.ReasoningEngine[TIntent]
	Validator           Validator[TIntent]
	Executor            Executor[TIntent]
	Tools               map[string]ToolHandler // optional; keyed by tool name
	ValidationFormatter FeedbackFormatter
	ExecutionFormatter  FeedbackFormatter
	MaxTurns            int
	Sink                EventSink
}

type FeedbackFormatter func(error) string

type Result[TIntent any] struct {
	Reasoning   llm.ReasoningResult[TIntent]
	Observation llm.Observation
	Events      []llm.CycleEvent
	Turns       int
}

func (r Runner[TIntent]) Run(ctx context.Context, input llm.ReasoningInput) (Result[TIntent], error) {
	var result Result[TIntent]
	if err := r.validate(); err != nil {
		return result, err
	}

	maxTurns := r.MaxTurns
	if maxTurns <= 0 {
		maxTurns = defaultMaxTurns
	}

	events := append([]llm.CycleEvent(nil), input.Events...)
	for turn := 1; turn <= maxTurns; turn++ {
		cycleInput := input
		cycleInput.Events = append([]llm.CycleEvent(nil), events...)

		reasoning, err := r.Engine.Predict(ctx, cycleInput)
		if err != nil {
			result.Events = events
			result.Turns = turn
			return result, fmt.Errorf("loop: predict turn %d: %w", turn, err)
		}

		mo := modelOutputEvent(reasoning)
		events = append(events, mo)
		if err := r.emit(ctx, mo); err != nil {
			result.Events = events
			result.Turns = turn
			return result, fmt.Errorf("loop: event sink (model output): %w", err)
		}

		// A turn that asks for tools yields no intent: run each tool,
		// feed the results back as events, and re-predict.
		if len(reasoning.ToolCalls) > 0 {
			for _, call := range reasoning.ToolCalls {
				te := r.runTool(ctx, call)
				events = append(events, te)
				if emitErr := r.emit(ctx, te); emitErr != nil {
					result.Events = events
					result.Turns = turn
					return result, fmt.Errorf("loop: event sink (tool result): %w", emitErr)
				}
			}
			continue
		}

		if err := r.Validator.Validate(ctx, reasoning.Intent); err != nil {
			ve := validationErrorEvent(err, r.validationFormatter())
			events = append(events, ve)
			if emitErr := r.emit(ctx, ve); emitErr != nil {
				result.Events = events
				result.Turns = turn
				return result, fmt.Errorf("loop: event sink (validation error): %w", emitErr)
			}
			continue
		}

		observation, err := r.Executor.Execute(ctx, reasoning.Intent)
		if err != nil {
			ee := executionErrorEvent(err, r.executionFormatter())
			events = append(events, ee)
			if emitErr := r.emit(ctx, ee); emitErr != nil {
				result.Events = events
				result.Turns = turn
				return result, fmt.Errorf("loop: event sink (execution error): %w", emitErr)
			}
			continue
		}

		oe := observationEvent(observation)
		events = append(events, oe)
		if emitErr := r.emit(ctx, oe); emitErr != nil {
			result.Events = events
			result.Turns = turn
			return result, fmt.Errorf("loop: event sink (observation): %w", emitErr)
		}

		return Result[TIntent]{
			Reasoning:   reasoning,
			Observation: observation,
			Events:      events,
			Turns:       turn,
		}, nil
	}

	result.Events = events
	result.Turns = maxTurns
	return result, ErrMaxTurnsExceeded
}

func (r Runner[TIntent]) emit(ctx context.Context, event llm.CycleEvent) error {
	if r.Sink == nil {
		return nil
	}
	return r.Sink.Emit(ctx, event)
}

// runTool routes one tool call to its handler and wraps the outcome as a
// tool-result event. An unknown tool name or a handler error becomes
// feedback content the model can recover from on the next turn; it never
// aborts the loop.
func (r Runner[TIntent]) runTool(ctx context.Context, call llm.ToolCall) llm.CycleEvent {
	handler, ok := r.Tools[call.Name]
	if !ok {
		return toolResultEvent(call.Name, fmt.Sprintf("tool %q is not available", call.Name))
	}
	out, err := handler(ctx, call.Args)
	if err != nil {
		return toolResultEvent(call.Name, fmt.Sprintf("tool %q failed: %v", call.Name, err))
	}
	return toolResultEvent(call.Name, out)
}

func (r Runner[TIntent]) validate() error {
	if r.Engine == nil {
		return ErrMissingEngine
	}
	if r.Validator == nil {
		return ErrMissingValidator
	}
	if r.Executor == nil {
		return ErrMissingExecutor
	}
	return nil
}

func (r Runner[TIntent]) validationFormatter() FeedbackFormatter {
	if r.ValidationFormatter != nil {
		return r.ValidationFormatter
	}
	return defaultValidationFormatter
}

func (r Runner[TIntent]) executionFormatter() FeedbackFormatter {
	if r.ExecutionFormatter != nil {
		return r.ExecutionFormatter
	}
	return defaultExecutionFormatter
}

func modelOutputEvent[TIntent any](reasoning llm.ReasoningResult[TIntent]) llm.CycleEvent {
	detail := "intent: " + formatIntent(reasoning.Intent)
	if len(reasoning.ToolCalls) > 0 {
		calls := make([]string, len(reasoning.ToolCalls))
		for i, c := range reasoning.ToolCalls {
			calls[i] = strings.TrimSpace(c.Name + " " + string(c.Args))
		}
		detail = "tool calls:\n  " + strings.Join(calls, "\n  ")
	}
	return llm.CycleEvent{
		Role:    llm.EventRoleAssistant,
		Kind:    llm.EventModelOutput,
		Content: fmt.Sprintf("rationale: %s\n%s", reasoning.Rationale, detail),
	}
}

// formatIntent renders a proposed intent for a model_output event. JSON
// keeps the rendering deterministic and readable across intent types --
// including a discriminated-union intent whose %#v would expose
// non-deterministic pointer addresses. It falls back to %#v only for an
// intent that cannot be marshalled.
func formatIntent[TIntent any](intent TIntent) string {
	if b, err := json.Marshal(intent); err == nil {
		return string(b)
	}
	return fmt.Sprintf("%#v", intent)
}

func validationErrorEvent(err error, format FeedbackFormatter) llm.CycleEvent {
	return llm.CycleEvent{
		Role:    llm.EventRoleEnvironment,
		Kind:    llm.EventValidationError,
		Content: format(err),
	}
}

func executionErrorEvent(err error, format FeedbackFormatter) llm.CycleEvent {
	return llm.CycleEvent{
		Role:    llm.EventRoleEnvironment,
		Kind:    llm.EventExecutionError,
		Content: format(err),
	}
}

func observationEvent(observation llm.Observation) llm.CycleEvent {
	return llm.CycleEvent{
		Role:    llm.EventRoleEnvironment,
		Kind:    llm.EventObservation,
		Content: observation.Summary,
	}
}

func toolResultEvent(name, content string) llm.CycleEvent {
	return llm.CycleEvent{
		Role:    llm.EventRoleEnvironment,
		Kind:    llm.EventToolResult,
		Content: fmt.Sprintf("[%s]\n%s", name, content),
	}
}

func defaultValidationFormatter(err error) string {
	return fmt.Sprintf("Validation failed: %v. Please correct the intent and try again.", err)
}

func defaultExecutionFormatter(err error) string {
	return fmt.Sprintf("Execution failed: %v. Please correct the intent and try again.", err)
}
