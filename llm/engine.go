package llm

import (
	"context"
	"encoding/json"
)

// EngineKind names a reasoning engine implementation; empty defaults to EngineScripted.
type EngineKind string

const (
	EngineScripted EngineKind = "scripted"
	EngineOpenAI   EngineKind = "openai"
)

// ReasoningEngine is the port use cases call to ask a model for a typed Intent.
// Concrete providers live in adapters, not in domain or use-case code.
type ReasoningEngine[TIntent any] interface {
	Predict(ctx context.Context, input ReasoningInput) (ReasoningResult[TIntent], error)
}

// ReasoningInput is the complete context for one reasoning cycle.
type ReasoningInput struct {
	Task         string
	Instructions string
	Events       []CycleEvent
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

// ReasoningResult is the structured output of one reasoning step. When
// ToolCalls is non-empty the model is asking for information before it can
// propose a final Intent.
type ReasoningResult[TIntent any] struct {
	Evidence  []EvidenceRef `json:"evidence"`
	Rationale string        `json:"rationale"`
	Intent    TIntent       `json:"intent"`
	ToolCalls []ToolCall    `json:"tool_calls,omitempty"`
}

// ToolCall is the model's request to invoke a named tool. Args is the raw JSON
// the model supplied; the tool handler decodes it.
type ToolCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

// Decoder turns a provider's raw model output into a typed ReasoningResult.
type Decoder[TIntent any] interface {
	Decode(content string) (ReasoningResult[TIntent], error)
}

// DecoderFunc adapts a function to Decoder.
type DecoderFunc[TIntent any] func(content string) (ReasoningResult[TIntent], error)

func (f DecoderFunc[TIntent]) Decode(content string) (ReasoningResult[TIntent], error) {
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
