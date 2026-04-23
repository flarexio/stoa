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

	_, err := NewAdapter[testIntent](Config[testIntent]{})
	if err == nil {
		t.Fatal("expected missing API key error")
	}
}

func TestNewAdapterDefaultsProviderConfig(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")

	adapter, err := NewAdapter[testIntent](Config[testIntent]{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("NewAdapter returned error: %v", err)
	}

	if adapter.model != defaultModel {
		t.Fatalf("model = %q, want %q", adapter.model, defaultModel)
	}
}

func TestRenderReasoningInputIncludesContract(t *testing.T) {
	rendered := renderReasoningInput(llm.ReasoningInput{
		Task:         "Choose the next step.",
		Instructions: "Only use validated facts.",
	})

	for _, want := range []string{
		"Task:",
		"Choose the next step.",
		"Feature instructions:",
		"Only use validated facts.",
		`"evidence"`,
		`"rationale"`,
		`"intent"`,
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered input missing %q:\n%s", want, rendered)
		}
	}
}

func TestMessagesMapEnvironmentFeedbackToUserContext(t *testing.T) {
	adapter, err := NewAdapter[testIntent](Config[testIntent]{
		APIKey:       "test-key",
		SystemPrompt: "system",
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
	})
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

func TestCustomRendererAndDecoderDisableDefaultJSONMode(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")

	adapter, err := NewAdapter[testIntent](Config[testIntent]{
		APIKey: "test-key",
		Renderer: llm.PromptRendererFunc(func(input llm.ReasoningInput) ([]llm.Message, error) {
			return []llm.Message{{Role: llm.MessageRoleUser, Content: input.Task}}, nil
		}),
		Decoder: llm.DecoderFunc[testIntent](func(content string) (llm.ReasoningResult[testIntent], error) {
			return llm.ReasoningResult[testIntent]{
				Intent: testIntent{Action: strings.TrimSpace(content)},
			}, nil
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
	if decoded.Intent.Action != "handoff" {
		t.Fatalf("decoded action = %q, want handoff", decoded.Intent.Action)
	}
}
