package bookkeeper_test

import (
	"strings"
	"testing"

	"github.com/flarexio/stoa/bookkeeper"
	"github.com/flarexio/stoa/llm"
)

func TestPromptRenderer_IncludesActiveAccountsAndOpenPeriods(t *testing.T) {
	ledger := awsBillLedger(t)
	renderer := bookkeeper.PromptRenderer{Ledger: ledger}

	messages, err := renderer.Render(llm.ReasoningInput{
		Task: "Paid AWS bill 100 USD using company credit card",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected exactly system + user with no events, got %d messages", len(messages))
	}
	if messages[0].Role != llm.MessageRoleSystem {
		t.Errorf("first message should be system, got %q", messages[0].Role)
	}
	if !strings.Contains(messages[0].Content, "bookkeeping reasoning engine") {
		t.Errorf("system prompt missing identity line, got %q", messages[0].Content)
	}

	user := messages[1].Content
	for _, want := range []string{
		"Paid AWS bill 100 USD using company credit card", // task echoed
		"Acme Cloud Co.",      // company name from fixture
		"5200 Cloud Hosting",  // active expense account
		"2100 Credit Card",    // active liability account
		"2026-05",             // open period
		"hq",                  // branch
		"eu",                  // branch
		`"side":"debit"`,      // JSON schema example
		"minor currency units", // unit guidance
	} {
		if !strings.Contains(user, want) {
			t.Errorf("user prompt missing %q\n--- prompt ---\n%s", want, user)
		}
	}

	// Inactive accounts and closed periods must NOT leak into the prompt.
	for _, hide := range []string{
		"5900 Legacy Office Rent", // inactive
		"2026-04",                 // closed in fixture
	} {
		if strings.Contains(user, hide) {
			t.Errorf("user prompt should not include %q\n--- prompt ---\n%s", hide, user)
		}
	}
}

func TestPromptRenderer_AppendsValidationFeedbackAsMessages(t *testing.T) {
	ledger := awsBillLedger(t)
	renderer := bookkeeper.PromptRenderer{Ledger: ledger}

	messages, err := renderer.Render(llm.ReasoningInput{
		Task: "Paid AWS bill",
		Events: []llm.CycleEvent{
			{Role: llm.EventRoleAssistant, Kind: llm.EventModelOutput, Content: "first attempt"},
			{Role: llm.EventRoleEnvironment, Kind: llm.EventValidationError, Content: "debits != credits"},
		},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(messages) != 4 {
		t.Fatalf("expected system + user + 2 event messages, got %d", len(messages))
	}
	if messages[2].Role != llm.MessageRoleAssistant {
		t.Errorf("assistant event should map to assistant role, got %q", messages[2].Role)
	}
	if messages[3].Role != llm.MessageRoleUser {
		t.Errorf("environment event should map to user role, got %q", messages[3].Role)
	}
	if !strings.Contains(messages[3].Content, "debits != credits") {
		t.Errorf("validation_error event body missing from feedback message: %q", messages[3].Content)
	}
}

func TestPromptRenderer_NilLedger(t *testing.T) {
	if _, err := (bookkeeper.PromptRenderer{}).Render(llm.ReasoningInput{}); err == nil {
		t.Fatal("expected error when renderer has no ledger")
	}
}
