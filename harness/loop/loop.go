package loop

import (
	"context"
	"errors"
	"fmt"

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

type Runner[TIntent any] struct {
	Engine              llm.ReasoningEngine[TIntent]
	Validator           Validator[TIntent]
	Executor            Executor[TIntent]
	ValidationFormatter FeedbackFormatter
	ExecutionFormatter  FeedbackFormatter
	MaxTurns            int
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

		events = append(events, modelOutputEvent(reasoning))
		if err := r.Validator.Validate(ctx, reasoning.Intent); err != nil {
			events = append(events, validationErrorEvent(err, r.validationFormatter()))
			continue
		}

		observation, err := r.Executor.Execute(ctx, reasoning.Intent)
		if err != nil {
			events = append(events, executionErrorEvent(err, r.executionFormatter()))
			continue
		}

		events = append(events, observationEvent(observation))
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
	return llm.CycleEvent{
		Role:    llm.EventRoleAssistant,
		Kind:    llm.EventModelOutput,
		Content: fmt.Sprintf("rationale: %s\nintent: %#v", reasoning.Rationale, reasoning.Intent),
	}
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

func defaultValidationFormatter(err error) string {
	return fmt.Sprintf("Validation failed: %v. Please correct the intent and try again.", err)
}

func defaultExecutionFormatter(err error) string {
	return fmt.Sprintf("Execution failed: %v. Please correct the intent and try again.", err)
}
