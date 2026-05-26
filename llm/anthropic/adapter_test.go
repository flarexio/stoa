package anthropic

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/flarexio/stoa/llm"
)

type testIntent struct {
	Action string `json:"action"`
}

func TestNewAdapterRequiresAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")

	_, err := NewAdapter(Config[testIntent]{})
	if err == nil {
		t.Fatal("expected missing API key error")
	}
}

func TestNewAdapterRequiresModel(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")

	_, err := NewAdapter(Config[testIntent]{APIKey: "test-key"})
	if err == nil {
		t.Fatal("expected error when Model is empty")
	}
	if !strings.Contains(err.Error(), "model") {
		t.Fatalf("error should mention model, got %v", err)
	}
}

func TestNewAdapterAcceptsExplicitModel(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")

	adapter, err := NewAdapter(Config[testIntent]{APIKey: "test-key", Model: "claude-opus-4-7"})
	if err != nil {
		t.Fatalf("NewAdapter returned error: %v", err)
	}
	if adapter.model != "claude-opus-4-7" {
		t.Fatalf("model = %q, want claude-opus-4-7", adapter.model)
	}
}

func TestNewAdapterAPIKeyFromEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "env-api-key")
	t.Setenv("ANTHROPIC_BASE_URL", "")

	adapter, err := NewAdapter(Config[testIntent]{Model: "claude-opus-4-7"})
	if err != nil {
		t.Fatalf("NewAdapter returned error: %v", err)
	}
	if adapter.model != "claude-opus-4-7" {
		t.Fatalf("model = %q, want claude-opus-4-7", adapter.model)
	}
}

func TestNewAdapterAPIKeyExplicitOverridesEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "env-key")

	adapter, err := NewAdapter(Config[testIntent]{APIKey: "explicit-key", Model: "claude-opus-4-7"})
	if err != nil {
		t.Fatalf("NewAdapter returned error: %v", err)
	}
	if adapter.model != "claude-opus-4-7" {
		t.Fatalf("model = %q, want claude-opus-4-7", adapter.model)
	}
}

func TestNewAdapterBaseURLFromEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", "https://custom-llm.example.com")

	adapter, err := NewAdapter(Config[testIntent]{Model: "claude-opus-4-7"})
	if err != nil {
		t.Fatalf("NewAdapter returned error: %v", err)
	}
	if adapter.model != "claude-opus-4-7" {
		t.Fatalf("model = %q, want claude-opus-4-7", adapter.model)
	}
}

func TestNewAdapterDefaultsMaxTokens(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	adapter, err := NewAdapter(Config[testIntent]{Model: "claude-opus-4-7"})
	if err != nil {
		t.Fatalf("NewAdapter returned error: %v", err)
	}
	if adapter.maxTokens != defaultMaxTokens {
		t.Fatalf("maxTokens = %d, want %d", adapter.maxTokens, defaultMaxTokens)
	}
}

func TestNewAdapterRespectsExplicitMaxTokens(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	adapter, err := NewAdapter(Config[testIntent]{Model: "claude-opus-4-7", MaxTokens: 8192})
	if err != nil {
		t.Fatalf("NewAdapter returned error: %v", err)
	}
	if adapter.maxTokens != 8192 {
		t.Fatalf("maxTokens = %d, want 8192", adapter.maxTokens)
	}
}

func TestNewAdapterBuildsEnvelopeWhenIntentSchemaProvided(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	adapter, err := NewAdapter(Config[testIntent]{
		Model:        "claude-opus-4-7",
		IntentSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["action"],"properties":{"action":{"type":"string"}}}`),
	})
	if err != nil {
		t.Fatalf("NewAdapter returned error: %v", err)
	}
	if adapter.envelope == nil {
		t.Fatal("envelope should be built when IntentSchema is set")
	}
	required, ok := adapter.envelope["required"].([]string)
	if !ok || len(required) != 3 {
		t.Fatalf("envelope.required = %v, want [evidence rationale intent]", adapter.envelope["required"])
	}
	props, ok := adapter.envelope["properties"].(map[string]any)
	if !ok {
		t.Fatalf("envelope.properties is %T", adapter.envelope["properties"])
	}
	if _, ok := props["intent"]; !ok {
		t.Fatal("envelope.properties.intent missing")
	}
}

func TestNewAdapterNoEnvelopeWithoutSchema(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	adapter, err := NewAdapter(Config[testIntent]{Model: "claude-opus-4-7"})
	if err != nil {
		t.Fatalf("NewAdapter returned error: %v", err)
	}
	if adapter.envelope != nil {
		t.Fatalf("envelope should be nil without IntentSchema, got %v", adapter.envelope)
	}
}

func TestMessagesLiftSystemMessagesToTopLevel(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	adapter, err := NewAdapter(Config[testIntent]{
		Model:    "claude-opus-4-7",
		Renderer: llm.DefaultPromptRenderer{SystemPrompt: "you are a reasoning engine"},
	})
	if err != nil {
		t.Fatalf("NewAdapter returned error: %v", err)
	}

	system, messages, err := adapter.messages(llm.ReasoningInput{Task: "Pick an action."})
	if err != nil {
		t.Fatalf("messages returned error: %v", err)
	}
	if len(system) != 1 {
		t.Fatalf("len(system) = %d, want 1 (anthropic has no system role on messages)", len(system))
	}
	if !strings.Contains(system[0].Text, "you are a reasoning engine") {
		t.Fatalf("system prompt missing renderer content: %s", system[0].Text)
	}
	if len(messages) == 0 {
		t.Fatal("expected at least one user/assistant message")
	}
	for _, m := range messages {
		if m.Role != "user" && m.Role != "assistant" {
			t.Fatalf("unexpected message role %q (system must be lifted out)", m.Role)
		}
	}
}

func TestMessagesMapEnvironmentFeedbackToUserContext(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	adapter, err := NewAdapter(Config[testIntent]{
		Model:    "claude-opus-4-7",
		Renderer: llm.DefaultPromptRenderer{SystemPrompt: "system"},
	})
	if err != nil {
		t.Fatalf("NewAdapter returned error: %v", err)
	}

	_, messages, err := adapter.messages(llm.ReasoningInput{
		Task: "Pick an action.",
		Events: []llm.CycleEvent{
			{
				Role:    llm.EventRoleEnvironment,
				Kind:    llm.EventValidationError,
				Content: "amount must be positive",
			},
		},
	})
	if err != nil {
		t.Fatalf("messages returned error: %v", err)
	}
	raw, err := json.Marshal(messages)
	if err != nil {
		t.Fatalf("marshal messages: %v", err)
	}
	encoded := string(raw)
	for _, want := range []string{`"role":"user"`, "validation_error", "amount must be positive"} {
		if !strings.Contains(encoded, want) {
			t.Fatalf("messages missing %q:\n%s", want, encoded)
		}
	}
}

func TestCustomRendererAndDecoder(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")

	adapter, err := NewAdapter(Config[testIntent]{
		APIKey: "test-key",
		Model:  "claude-opus-4-7",
		Renderer: llm.PromptRendererFunc(func(input llm.ReasoningInput) ([]llm.Message, error) {
			return []llm.Message{{Role: llm.MessageRoleUser, Content: input.Task}}, nil
		}),
		Decoder: llm.DecoderFunc[testIntent](func(content string) (llm.ReasoningOutput[testIntent], error) {
			return llm.IntentOutput(testIntent{Action: strings.TrimSpace(content)}, nil, ""), nil
		}),
	})
	if err != nil {
		t.Fatalf("NewAdapter returned error: %v", err)
	}

	decoded, err := adapter.decoder.Decode("handoff")
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if decoded.Kind != llm.ReasoningIntent {
		t.Fatalf("decoded kind = %q, want intent", decoded.Kind)
	}
	if decoded.Intent.Action != "handoff" {
		t.Fatalf("decoded action = %q, want handoff", decoded.Intent.Action)
	}
}

func TestTranslateToolsBuildsToolParams(t *testing.T) {
	specs := []llm.ToolSpec{
		{
			Name:        "find_accounts",
			Description: "look up account codes",
			ArgsSchema:  json.RawMessage(`{"type":"object","additionalProperties":false,"required":["query"],"properties":{"query":{"type":"string"}}}`),
		},
	}
	tools, err := translateTools(specs)
	if err != nil {
		t.Fatalf("translateTools returned error: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("len(tools) = %d, want 1", len(tools))
	}
	tool := tools[0].OfTool
	if tool == nil {
		t.Fatal("OfTool is nil")
	}
	if tool.Name != "find_accounts" {
		t.Fatalf("name = %q, want find_accounts", tool.Name)
	}
	if tool.Description.Value != "look up account codes" {
		t.Fatalf("description = %q, want the spec description", tool.Description.Value)
	}
	if !tool.Strict.Value {
		t.Fatal("Strict should be set to true when ArgsSchema is provided")
	}
	if len(tool.InputSchema.Required) != 1 || tool.InputSchema.Required[0] != "query" {
		t.Fatalf("InputSchema.Required = %v, want [query]", tool.InputSchema.Required)
	}
	if tool.InputSchema.ExtraFields["additionalProperties"] != false {
		t.Fatalf("additionalProperties not preserved in ExtraFields: %v", tool.InputSchema.ExtraFields)
	}
}

func TestTranslateToolsRejectsBadSchema(t *testing.T) {
	_, err := translateTools([]llm.ToolSpec{{
		Name:       "broken",
		ArgsSchema: json.RawMessage(`{not-json`),
	}})
	if err == nil {
		t.Fatal("expected error for invalid args schema")
	}
}

func TestTranslateToolsRejectsBlankName(t *testing.T) {
	_, err := translateTools([]llm.ToolSpec{{Name: "  "}})
	if err == nil {
		t.Fatal("expected error for blank tool name")
	}
}
