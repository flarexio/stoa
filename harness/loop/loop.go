// Package loop is the generic reason -> validate -> execute harness loop that
// drives a feature's domain through an llm.ReasoningEngine.
package loop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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

// Validator runs a feature's domain rules; the loop owns when validation runs.
type Validator[TIntent any] interface {
	Validate(ctx context.Context, intent TIntent) error
}

type ValidatorFunc[TIntent any] func(ctx context.Context, intent TIntent) error

func (f ValidatorFunc[TIntent]) Validate(ctx context.Context, intent TIntent) error {
	return f(ctx, intent)
}

// Executor performs a validated intent.
type Executor[TIntent any] interface {
	Execute(ctx context.Context, intent TIntent) (llm.Observation, error)
}

type ExecutorFunc[TIntent any] func(ctx context.Context, intent TIntent) (llm.Observation, error)

func (f ExecutorFunc[TIntent]) Execute(ctx context.Context, intent TIntent) (llm.Observation, error) {
	return f(ctx, intent)
}

// FinalIntent makes a Run multi-action: when the intent type implements it, the
// loop keeps executing intents until one whose IsFinal is true — the model's
// "this is my last action" marker — which is executed and then ends the run.
// Types that don't implement it run single-action (return after the first
// executed intent).
type FinalIntent interface {
	IsFinal() bool
}

func isFinal[TIntent any](intent TIntent) bool {
	t, ok := any(intent).(FinalIntent)
	return ok && t.IsFinal()
}

// ToolHandler answers one tool call: it decodes args (the raw JSON the model
// supplied) into its own typed parameters and returns a result string for the
// model's next turn.
type ToolHandler func(ctx context.Context, args json.RawMessage) (string, error)

// Tool pairs a llm.ToolSpec (what the model sees) with the handler that runs
// when the model invokes it. The loop owns the registry; adapters translate
// the spec list into provider-native tool definitions.
type Tool struct {
	Spec    llm.ToolSpec
	Handler ToolHandler
}

// EventSink receives per-turn cycle events as they happen, so a caller can
// observe the loop incrementally (e.g. a TUI). When Sink is nil, Run is a
// plain blocking call.
type EventSink interface {
	Emit(ctx context.Context, event llm.CycleEvent) error
}

// Runner is the harness loop for one feature's typed Intent.
type Runner[TIntent any] struct {
	Engine              llm.ReasoningEngine[TIntent]
	Validator           Validator[TIntent]
	Executor            Executor[TIntent]
	Tools               map[string]Tool // optional; keyed by tool name
	ValidationFormatter FeedbackFormatter
	ExecutionFormatter  FeedbackFormatter
	MaxTurns            int
	Sink                EventSink
}

// FeedbackFormatter formats a validation/execution error into prompt feedback.
type FeedbackFormatter func(error) string

// Result is the outcome of one Run. Steps holds each executed intent in order;
// Reasoning and Observation mirror the last one.
type Result[TIntent any] struct {
	Reasoning   llm.ReasoningOutput[TIntent]
	Observation llm.Observation
	Events      []llm.CycleEvent
	Turns       int
	Steps       []Step[TIntent]
}

// Step is one executed intent and the observation it produced.
type Step[TIntent any] struct {
	Reasoning   llm.ReasoningOutput[TIntent]
	Observation llm.Observation
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

	specs := r.toolSpecs()
	events := append([]llm.CycleEvent(nil), input.Events...)
	var steps []Step[TIntent]
	var zero TIntent
	_, multiAction := any(zero).(FinalIntent) // intents that can mark a final action run multi-action
	for turn := 1; turn <= maxTurns; turn++ {
		cycleInput := input
		cycleInput.Events = append([]llm.CycleEvent(nil), events...)
		cycleInput.Tools = specs

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

		switch reasoning.Kind {
		case llm.ReasoningToolCalls:
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
		case llm.ReasoningIntent:
			// fall through
		default:
			return result, fmt.Errorf("loop: unknown reasoning kind %q", reasoning.Kind)
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

		steps = append(steps, Step[TIntent]{Reasoning: reasoning, Observation: observation})
		// Single-action stops after the first intent; multi-action stops once the
		// model marks an executed intent as final.
		if !multiAction || isFinal(reasoning.Intent) {
			return Result[TIntent]{
				Reasoning:   reasoning,
				Observation: observation,
				Events:      events,
				Turns:       turn,
				Steps:       steps,
			}, nil
		}
	}

	// MaxTurns can be partial completion in multi-action mode; report what ran.
	result.Events = events
	result.Turns = maxTurns
	result.Steps = steps
	if n := len(steps); n > 0 {
		result.Reasoning = steps[n-1].Reasoning
		result.Observation = steps[n-1].Observation
	}
	return result, ErrMaxTurnsExceeded
}

func (r Runner[TIntent]) emit(ctx context.Context, event llm.CycleEvent) error {
	if r.Sink == nil {
		return nil
	}
	return r.Sink.Emit(ctx, event)
}

// toolSpecs returns the registered tools' specs, sorted by name for a stable
// prompt ordering across turns.
func (r Runner[TIntent]) toolSpecs() []llm.ToolSpec {
	if len(r.Tools) == 0 {
		return nil
	}
	names := make([]string, 0, len(r.Tools))
	for name := range r.Tools {
		names = append(names, name)
	}
	sort.Strings(names)
	specs := make([]llm.ToolSpec, 0, len(names))
	for _, name := range names {
		specs = append(specs, r.Tools[name].Spec)
	}
	return specs
}

// runTool routes one tool call to its handler. An unknown name or handler
// error becomes feedback the model can recover from; it never aborts the loop.
func (r Runner[TIntent]) runTool(ctx context.Context, call llm.ToolCall) llm.CycleEvent {
	tool, ok := r.Tools[call.Name]
	if !ok || tool.Handler == nil {
		return toolResultEvent(call.Name, fmt.Sprintf("tool %q is not available", call.Name))
	}
	out, err := tool.Handler(ctx, call.Args)
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

func modelOutputEvent[TIntent any](reasoning llm.ReasoningOutput[TIntent]) llm.CycleEvent {
	var detail string
	switch reasoning.Kind {
	case llm.ReasoningToolCalls:
		calls := make([]string, len(reasoning.ToolCalls))
		for i, c := range reasoning.ToolCalls {
			calls[i] = strings.TrimSpace(c.Name + " " + string(c.Args))
		}
		detail = "tool calls:\n  " + strings.Join(calls, "\n  ")
	default:
		detail = "intent: " + formatIntent(reasoning.Intent)
	}
	return llm.CycleEvent{
		Role:    llm.EventRoleAssistant,
		Kind:    llm.EventModelOutput,
		Content: fmt.Sprintf("rationale: %s\n%s", reasoning.Rationale, detail),
	}
}

// JSON keeps formatIntent deterministic; %#v on a union with pointers would
// expose non-deterministic addresses.
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
