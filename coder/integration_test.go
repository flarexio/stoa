package coder_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/flarexio/stoa/coder"
	"github.com/flarexio/stoa/icd"
	"github.com/flarexio/stoa/llm/openai"
)

// TestAgent_OpenAI exercises the full loop against the real OpenAI API.
// It is gated by STOA_RUN_OPENAI_TESTS so that a plain `go test ./...` --
// even with OPENAI_API_KEY in the environment -- never silently spends
// API tokens. Both the flag and the API key must be set.
func TestAgent_OpenAI(t *testing.T) {
	if os.Getenv("STOA_RUN_OPENAI_TESTS") == "" {
		t.Skip("set STOA_RUN_OPENAI_TESTS=1 to run OpenAI integration tests")
	}
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Fatal("STOA_RUN_OPENAI_TESTS is set but OPENAI_API_KEY is empty")
	}

	dict := icd.DefaultDictionary()
	engine, err := openai.NewAdapter(openai.Config[icd.Intent]{
		APIKey:       apiKey,
		Model:        "gpt-5.4-mini",
		OutputFormat: openai.OutputFormatJSONObject,
		Renderer:     coder.PromptRenderer{Dict: dict},
	})
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}

	agent := coder.Agent{
		Engine:   engine,
		Dict:     dict,
		Recorder: icd.NewInMemoryRecorder(),
		MaxTurns: 3,
	}

	note := icd.Note{
		ID: "demo-1",
		Text: "48F presents with worsening exertional chest pain for two weeks. " +
			"History of type 2 diabetes mellitus and essential hypertension. " +
			"BP 148/92 in clinic. No acute ECG changes. " +
			"Plan: stress test, continue metformin and lisinopril.",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	res, err := agent.Code(ctx, note)
	if err != nil {
		t.Fatalf("coding run failed: %v", err)
	}
	if len(res.Intent.Suggestions) == 0 {
		t.Fatal("expected at least one ICD-10 suggestion")
	}
	t.Logf("turns=%d, suggestions=%d", res.Turns, len(res.Intent.Suggestions))
	for _, s := range res.Intent.Suggestions {
		t.Logf("  %s (%.2f) [%s]: %q", s.Code, s.Confidence, s.Description, s.EvidenceSpan)
	}
}
