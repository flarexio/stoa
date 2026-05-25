package openai

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
	t.Setenv("OPENAI_API_KEY", "")

	_, err := NewAdapter(Config[testIntent]{})
	if err == nil {
		t.Fatal("expected missing API key error")
	}
}

func TestNewAdapterRequiresModel(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")

	_, err := NewAdapter(Config[testIntent]{APIKey: "test-key"})
	if err == nil {
		t.Fatal("expected error when Model is empty")
	}
	if !strings.Contains(err.Error(), "model") {
		t.Fatalf("error should mention model, got %v", err)
	}
}

func TestNewAdapterAcceptsExplicitModel(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")

	adapter, err := NewAdapter(Config[testIntent]{APIKey: "test-key", Model: "gpt-5.4-mini"})
	if err != nil {
		t.Fatalf("NewAdapter returned error: %v", err)
	}
	if adapter.model != "gpt-5.4-mini" {
		t.Fatalf("model = %q, want gpt-5.4-mini", adapter.model)
	}
}

func TestRenderReasoningInputDoesNotDictateShape(t *testing.T) {
	// With structured outputs / native tools, the prompt no longer prescribes
	// the JSON envelope; the provider enforces it.
	rendered := llm.RenderReasoningInput(llm.ReasoningInput{
		Task:         "Choose the next step.",
		Instructions: "Only use validated facts.",
	})

	for _, want := range []string{
		"Task:",
		"Choose the next step.",
		"Feature instructions:",
		"Only use validated facts.",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered input missing %q:\n%s", want, rendered)
		}
	}
	for _, banned := range []string{`"evidence"`, `"rationale"`, `"intent"`} {
		if strings.Contains(rendered, banned) {
			t.Fatalf("rendered input must not embed the response shape %q anymore:\n%s", banned, rendered)
		}
	}
}

func TestMessagesMapEnvironmentFeedbackToUserContext(t *testing.T) {
	adapter, err := NewAdapter(Config[testIntent]{
		APIKey: "test-key",
		Model:  "gpt-5.4-mini",
		Renderer: llm.DefaultPromptRenderer{
			SystemPrompt: "system",
		},
	})
	if err != nil {
		t.Fatalf("NewAdapter returned error: %v", err)
	}

	messages, err := adapter.messages(llm.ReasoningInput{
		Task: "Pick an action.",
		Events: []llm.CycleEvent{
			{
				Role:    llm.EventRoleEnvironment,
				Kind:    llm.EventValidationError,
				Content: "amount must be positive",
			},
		},
	}, "")
	if err != nil {
		t.Fatalf("messages returned error: %v", err)
	}

	raw, err := json.Marshal(messages)
	if err != nil {
		t.Fatalf("marshal messages: %v", err)
	}
	encoded := string(raw)

	for _, want := range []string{
		`"role":"system"`,
		`"role":"user"`,
		"validation_error",
		"amount must be positive",
	} {
		if !strings.Contains(encoded, want) {
			t.Fatalf("messages missing %q:\n%s", want, encoded)
		}
	}
}

func TestNewAdapterAPIKeyFromEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "env-api-key")
	t.Setenv("OPENAI_BASE_URL", "")

	adapter, err := NewAdapter(Config[testIntent]{Model: "gpt-5.4-mini"})
	if err != nil {
		t.Fatalf("NewAdapter returned error: %v", err)
	}
	if adapter.model != "gpt-5.4-mini" {
		t.Fatalf("model = %q, want gpt-5.4-mini", adapter.model)
	}
}

func TestNewAdapterAPIKeyExplicitOverridesEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "env-key")

	adapter, err := NewAdapter(Config[testIntent]{APIKey: "explicit-key", Model: "gpt-5.4-mini"})
	if err != nil {
		t.Fatalf("NewAdapter returned error: %v", err)
	}
	if adapter.model != "gpt-5.4-mini" {
		t.Fatalf("model = %q, want gpt-5.4-mini", adapter.model)
	}
}

func TestNewAdapterBaseURLExplicit(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_BASE_URL", "")

	adapter, err := NewAdapter(Config[testIntent]{
		APIKey:  "test-key",
		Model:   "gpt-5.4-mini",
		BaseURL: "https://api.openai.com/v1",
	})
	if err != nil {
		t.Fatalf("NewAdapter returned error: %v", err)
	}
	if adapter.model != "gpt-5.4-mini" {
		t.Fatalf("model = %q, want gpt-5.4-mini", adapter.model)
	}
}

func TestNewAdapterBaseURLFromEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_BASE_URL", "https://custom-llm.example.com/v1")

	adapter, err := NewAdapter(Config[testIntent]{Model: "gpt-5.4-mini"})
	if err != nil {
		t.Fatalf("NewAdapter returned error: %v", err)
	}
	if adapter.model != "gpt-5.4-mini" {
		t.Fatalf("model = %q, want gpt-5.4-mini", adapter.model)
	}
}

func TestNewAdapterBaseURLExplicitOverridesEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_BASE_URL", "https://ignored.example.com")

	_, err := NewAdapter(Config[testIntent]{
		Model:   "gpt-5.4-mini",
		BaseURL: "https://explicit.example.com/v1",
	})
	if err != nil {
		t.Fatalf("NewAdapter returned error: %v", err)
	}
}

func TestNewAdapterDefaultsToJSONObjectWithoutSchema(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")

	adapter, err := NewAdapter(Config[testIntent]{Model: "gpt-5.4-mini"})
	if err != nil {
		t.Fatalf("NewAdapter returned error: %v", err)
	}
	if adapter.outputFormat != OutputFormatJSONObject {
		t.Fatalf("outputFormat = %q, want json_object (back-compat default)", adapter.outputFormat)
	}
}

func TestNewAdapterSwitchesToJSONSchemaWhenIntentSchemaProvided(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")

	adapter, err := NewAdapter(Config[testIntent]{
		Model:        "gpt-5.4-mini",
		IntentSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["action"],"properties":{"action":{"type":"string"}}}`),
	})
	if err != nil {
		t.Fatalf("NewAdapter returned error: %v", err)
	}
	if adapter.outputFormat != OutputFormatJSONSchema {
		t.Fatalf("outputFormat = %q, want json_schema", adapter.outputFormat)
	}

	envelope, ok := adapter.envelope.(map[string]any)
	if !ok {
		t.Fatalf("envelope is %T, want map[string]any", adapter.envelope)
	}
	required, ok := envelope["required"].([]string)
	if !ok || len(required) != 3 {
		t.Fatalf("envelope.required = %v, want [evidence rationale intent]", envelope["required"])
	}
	props, ok := envelope["properties"].(map[string]any)
	if !ok {
		t.Fatalf("envelope.properties is %T", envelope["properties"])
	}
	if _, ok := props["intent"]; !ok {
		t.Fatal("envelope.properties.intent missing")
	}
}

func TestNewAdapterRejectsJSONSchemaWithoutIntentSchema(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")

	_, err := NewAdapter(Config[testIntent]{
		Model:        "gpt-5.4-mini",
		OutputFormat: OutputFormatJSONSchema,
	})
	if err == nil {
		t.Fatal("expected error when json_schema is requested without IntentSchema")
	}
}

func TestCustomRendererAndDecoderDisableDefaultJSONMode(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")

	adapter, err := NewAdapter(Config[testIntent]{
		APIKey: "test-key",
		Model:  "gpt-5.4-mini",
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

	if adapter.outputFormat != "" {
		t.Fatalf("outputFormat = %q, want empty default for custom decoder", adapter.outputFormat)
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

func TestEffectiveOutputFormatDowngradesWithToolsWhenFlagSet(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")

	adapter, err := NewAdapter(Config[testIntent]{
		Model:                        "gpt-5.4-mini",
		IntentSchema:                 json.RawMessage(`{"type":"object","additionalProperties":false,"required":["action"],"properties":{"action":{"type":"string"}}}`),
		DisableStrictSchemaWithTools: true,
	})
	if err != nil {
		t.Fatalf("NewAdapter returned error: %v", err)
	}

	if got := adapter.effectiveOutputFormat(true); got != OutputFormatJSONObject {
		t.Fatalf("effectiveOutputFormat(hasTools=true) = %q, want json_object", got)
	}
	if got := adapter.effectiveOutputFormat(false); got != OutputFormatJSONSchema {
		t.Fatalf("effectiveOutputFormat(hasTools=false) = %q, want json_schema (no tool path that turn)", got)
	}
}

func TestEffectiveOutputFormatKeepsSchemaWithToolsWhenFlagUnset(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")

	adapter, err := NewAdapter(Config[testIntent]{
		Model:        "gpt-5.4-mini",
		IntentSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["action"],"properties":{"action":{"type":"string"}}}`),
	})
	if err != nil {
		t.Fatalf("NewAdapter returned error: %v", err)
	}

	if got := adapter.effectiveOutputFormat(true); got != OutputFormatJSONSchema {
		t.Fatalf("effectiveOutputFormat(hasTools=true, flag=false) = %q, want json_schema (default zero-regression behavior)", got)
	}
}

func TestNewAdapterBuildsEnvelopeHintWhenDowngradeEnabled(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")

	adapter, err := NewAdapter(Config[testIntent]{
		Model:                        "gpt-5.4-mini",
		IntentSchema:                 json.RawMessage(`{"type":"object","additionalProperties":false,"required":["action"],"properties":{"action":{"type":"string"}}}`),
		DisableStrictSchemaWithTools: true,
	})
	if err != nil {
		t.Fatalf("NewAdapter returned error: %v", err)
	}

	for _, want := range []string{
		`"evidence"`,
		`"rationale"`,
		`"intent"`,
		`"action"`, // the inlined IntentSchema field
		"No prose",
	} {
		if !strings.Contains(adapter.envelopeHint, want) {
			t.Fatalf("envelopeHint missing %q:\n%s", want, adapter.envelopeHint)
		}
	}
}

func TestNewAdapterNoEnvelopeHintWithoutFlag(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")

	adapter, err := NewAdapter(Config[testIntent]{
		Model:        "gpt-5.4-mini",
		IntentSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["action"],"properties":{"action":{"type":"string"}}}`),
	})
	if err != nil {
		t.Fatalf("NewAdapter returned error: %v", err)
	}
	if adapter.envelopeHint != "" {
		t.Fatalf("envelopeHint should be empty when downgrade cannot fire, got:\n%s", adapter.envelopeHint)
	}
}

func TestHintForReturnsHintOnlyOnDowngradeTurn(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")

	adapter, err := NewAdapter(Config[testIntent]{
		Model:                        "gpt-5.4-mini",
		IntentSchema:                 json.RawMessage(`{"type":"object","additionalProperties":false,"required":["action"],"properties":{"action":{"type":"string"}}}`),
		DisableStrictSchemaWithTools: true,
	})
	if err != nil {
		t.Fatalf("NewAdapter returned error: %v", err)
	}

	if hint := adapter.hintFor(true); hint == "" {
		t.Fatal("hintFor(hasTools=true) should return the envelope hint")
	}
	if hint := adapter.hintFor(false); hint != "" {
		t.Fatalf("hintFor(hasTools=false) should be empty (strict schema still fires), got:\n%s", hint)
	}
}

func TestHintForEmptyWithoutFlag(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")

	adapter, err := NewAdapter(Config[testIntent]{
		Model:        "gpt-5.4-mini",
		IntentSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["action"],"properties":{"action":{"type":"string"}}}`),
	})
	if err != nil {
		t.Fatalf("NewAdapter returned error: %v", err)
	}

	if hint := adapter.hintFor(true); hint != "" {
		t.Fatalf("flag off: hintFor must be empty, got:\n%s", hint)
	}
}

func TestMergeEnvelopeHintFoldsIntoExistingSystem(t *testing.T) {
	in := []llm.Message{
		{Role: llm.MessageRoleSystem, Content: "you are a reasoning engine"},
		{Role: llm.MessageRoleUser, Content: "task body"},
	}
	out := mergeEnvelopeHint(in, "envelope shape teaching")

	if len(out) != len(in) {
		t.Fatalf("len(out) = %d, want %d (merge, not insert)", len(out), len(in))
	}
	systems := 0
	for _, m := range out {
		if m.Role == llm.MessageRoleSystem {
			systems++
		}
	}
	if systems != 1 {
		t.Fatalf("got %d system messages, want exactly 1 (Qwen templates reject multiple)", systems)
	}
	if !strings.Contains(out[0].Content, "you are a reasoning engine") {
		t.Fatalf("original system body lost: %s", out[0].Content)
	}
	if !strings.Contains(out[0].Content, "envelope shape teaching") {
		t.Fatalf("hint not appended: %s", out[0].Content)
	}
}

func TestMergeEnvelopeHintPrependsWhenNoSystem(t *testing.T) {
	in := []llm.Message{
		{Role: llm.MessageRoleUser, Content: "task body"},
	}
	out := mergeEnvelopeHint(in, "hint")

	if len(out) != len(in)+1 {
		t.Fatalf("len(out) = %d, want %d (prepended)", len(out), len(in)+1)
	}
	if out[0].Role != llm.MessageRoleSystem || out[0].Content != "hint" {
		t.Fatalf("first message = %+v, want a system message carrying just the hint", out[0])
	}
}

func TestMessagesEmitOneSystemMessageOnDowngradeTurn(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")

	adapter, err := NewAdapter(Config[testIntent]{
		Model:                        "gpt-5.4-mini",
		IntentSchema:                 json.RawMessage(`{"type":"object","additionalProperties":false,"required":["action"],"properties":{"action":{"type":"string"}}}`),
		DisableStrictSchemaWithTools: true,
		Renderer:                     llm.DefaultPromptRenderer{SystemPrompt: "you are a reasoning engine"},
	})
	if err != nil {
		t.Fatalf("NewAdapter returned error: %v", err)
	}

	messages, err := adapter.messages(llm.ReasoningInput{Task: "task body"}, adapter.hintFor(true))
	if err != nil {
		t.Fatalf("messages returned error: %v", err)
	}

	raw, _ := json.Marshal(messages)
	encoded := string(raw)
	if got := strings.Count(encoded, `"role":"system"`); got != 1 {
		t.Fatalf("downgrade turn produced %d system messages, want exactly 1 (Qwen-style templates reject multiple):\n%s", got, encoded)
	}
	if !strings.Contains(encoded, "you are a reasoning engine") {
		t.Fatalf("renderer's system content lost:\n%s", encoded)
	}
	if !strings.Contains(encoded, "evidence") {
		t.Fatalf("envelope hint missing:\n%s", encoded)
	}
}

func TestTranslateToolsBuildsFunctionParams(t *testing.T) {
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
	fn := tools[0].Function
	if fn.Name != "find_accounts" {
		t.Fatalf("name = %q, want find_accounts", fn.Name)
	}
	if fn.Description.Value != "look up account codes" {
		t.Fatalf("description = %q, want the spec description", fn.Description.Value)
	}
	if fn.Parameters["type"] != "object" {
		t.Fatalf("parameters.type = %v, want object", fn.Parameters["type"])
	}
	if !fn.Strict.Value {
		t.Fatal("Strict should be set to true when ArgsSchema is provided")
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
