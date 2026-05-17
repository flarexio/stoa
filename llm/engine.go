package llm

import (
	"context"
	"encoding/json"
)

// ReasoningEngine is the port used by use cases to ask a model for a typed
// intent. Concrete providers live in adapters, not in domain or use case code.
type ReasoningEngine[TIntent any] interface {
	Predict(ctx context.Context, input ReasoningInput) (ReasoningResult[TIntent], error)
}

// ReasoningInput is the complete context for one reasoning cycle.
type ReasoningInput struct {
	Task         string
	Instructions string
	Events       []CycleEvent
}

// PromptRenderer turns Stoa's typed reasoning input into provider-neutral
// messages. Provider adapters translate these messages into SDK-specific types.
type PromptRenderer interface {
	Render(input ReasoningInput) ([]Message, error)
}

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

// ReasoningResult is the structured output expected from an agent reasoning
// step: evidence first, then an auditable rationale, then either a typed
// intent or a set of tool calls. A turn yields one or the other -- when
// ToolCalls is non-empty the model is asking for information before it can
// propose a final Intent.
type ReasoningResult[TIntent any] struct {
	Evidence  []EvidenceRef `json:"evidence"`
	Rationale string        `json:"rationale"`
	Intent    TIntent       `json:"intent"`
	ToolCalls []ToolCall    `json:"tool_calls,omitempty"`
}

// ToolCall is the model's request to invoke a named tool mid-reasoning.
// Args is the raw JSON the model supplied; the tool handler decodes it into
// its own typed parameters -- the same pattern Decoder uses for an intent,
// so a tool argument stays a typed contract rather than free-form text. The
// harness loop never inspects Args or a tool's result, which keeps the tool
// mechanism generic across features.
type ToolCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

// Decoder turns a provider's raw model output into Stoa's typed result.
// JSON is one possible implementation, not an architectural requirement.
type Decoder[TIntent any] interface {
	Decode(content string) (ReasoningResult[TIntent], error)
}

type DecoderFunc[TIntent any] func(content string) (ReasoningResult[TIntent], error)

func (f DecoderFunc[TIntent]) Decode(content string) (ReasoningResult[TIntent], error) {
	return f(content)
}

// EvidenceRef names a supplied fact the model used to justify its intent.
type EvidenceRef struct {
	Source string `json:"source"`
	Fact   string `json:"fact"`
}

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

// Observation is the typed result returned by executors after a valid intent is
// acted on. Use cases can feed it back into the next cycle as an event.
type Observation struct {
	Summary string            `json:"summary"`
	Fields  map[string]string `json:"fields,omitempty"`
}

// ModelInfo provides metadata about the underlying model.
type ModelInfo struct {
	Name        string
	MaxTokens   int
	Temperature float32
}
