package llm

import (
	"context"
	"encoding/json"
)

// EngineKind names a reasoning engine implementation; empty defaults to EngineScripted.
type EngineKind string

const (
	EngineScripted  EngineKind = "scripted"
	EngineOpenAI    EngineKind = "openai"
	EngineAnthropic EngineKind = "anthropic"
)

// ReasoningEngine is the port use cases call to ask a model for a typed Intent.
// Concrete providers live in adapters, not in domain or use-case code.
type ReasoningEngine[TIntent any] interface {
	Predict(ctx context.Context, input ReasoningInput) (ReasoningOutput[TIntent], error)
}

// ReasoningInput is the complete context for one reasoning cycle.
type ReasoningInput struct {
	Task         string
	Instructions string
	Events       []CycleEvent
	Tools        []ToolSpec
}

// PromptRenderer turns ReasoningInput into provider-neutral messages.
type PromptRenderer interface {
	Render(input ReasoningInput) ([]Message, error)
}

// PromptRendererFunc adapts a function to PromptRenderer.
type PromptRendererFunc func(input ReasoningInput) ([]Message, error)

func (f PromptRendererFunc) Render(input ReasoningInput) ([]Message, error) {
	return f(input)
}

type Message struct {
	Role    MessageRole
	Content string
}

type MessageRole string

const (
	MessageRoleSystem    MessageRole = "system"
	MessageRoleUser      MessageRole = "user"
	MessageRoleAssistant MessageRole = "assistant"
)

// ReasoningKind discriminates the two terminal shapes a reasoning turn can take.
type ReasoningKind string

const (
	ReasoningIntent    ReasoningKind = "intent"
	ReasoningToolCalls ReasoningKind = "tool_calls"
)

// ReasoningOutput is the structured result of one reasoning step. Kind selects
// which payload is meaningful: when ReasoningToolCalls, the model is asking for
// information before it can propose a final Intent; when ReasoningIntent, the
// model has committed to a typed Intent.
type ReasoningOutput[TIntent any] struct {
	Kind      ReasoningKind
	Evidence  []EvidenceRef
	Rationale string
	Intent    TIntent
	ToolCalls []ToolCall
}

// IntentOutput constructs a ReasoningOutput carrying a final typed intent.
func IntentOutput[TIntent any](intent TIntent, evidence []EvidenceRef, rationale string) ReasoningOutput[TIntent] {
	return ReasoningOutput[TIntent]{
		Kind:      ReasoningIntent,
		Evidence:  evidence,
		Rationale: rationale,
		Intent:    intent,
	}
}

// ToolCallsOutput constructs a ReasoningOutput carrying one or more tool calls.
func ToolCallsOutput[TIntent any](calls []ToolCall, evidence []EvidenceRef, rationale string) ReasoningOutput[TIntent] {
	return ReasoningOutput[TIntent]{
		Kind:      ReasoningToolCalls,
		Evidence:  evidence,
		Rationale: rationale,
		ToolCalls: calls,
	}
}

// ToolSpec advertises a callable tool to the model. Args is documented as a
// JSON Schema describing the argument shape.
type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	ArgsSchema  json.RawMessage `json:"args_schema,omitempty"`
}

// ToolCall is the model's request to invoke a named tool. Args is the raw JSON
// the model supplied; the tool handler decodes it.
type ToolCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

// Decoder turns a provider's raw model output into a typed ReasoningOutput.
type Decoder[TIntent any] interface {
	Decode(content string) (ReasoningOutput[TIntent], error)
}

// DecoderFunc adapts a function to Decoder.
type DecoderFunc[TIntent any] func(content string) (ReasoningOutput[TIntent], error)

func (f DecoderFunc[TIntent]) Decode(content string) (ReasoningOutput[TIntent], error) {
	return f(content)
}

// EvidenceRef names a supplied fact the model used to justify its intent.
type EvidenceRef struct {
	Source string `json:"source"`
	Fact   string `json:"fact"`
}

// CycleEvent is one entry in the conversation history fed back to the model.
type CycleEvent struct {
	Role    EventRole `json:"role"`
	Kind    EventKind `json:"kind"`
	Content string    `json:"content"`
}

type EventRole string

const (
	EventRoleUser        EventRole = "user"
	EventRoleAssistant   EventRole = "assistant"
	EventRoleEnvironment EventRole = "environment"
)

type EventKind string

const (
	EventTask            EventKind = "task"
	EventModelOutput     EventKind = "model_output"
	EventValidationError EventKind = "validation_error"
	EventExecutionError  EventKind = "execution_error"
	EventObservation     EventKind = "observation"
	EventToolResult      EventKind = "tool_result"
)

// Observation is the typed result executors return after a valid intent is
// acted on; use cases feed it back into the next cycle as an event.
type Observation struct {
	Summary string            `json:"summary"`
	Fields  map[string]string `json:"fields,omitempty"`
}
