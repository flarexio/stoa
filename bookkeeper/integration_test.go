package bookkeeper_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/flarexio/stoa/accounting"
	"github.com/flarexio/stoa/bookkeeper"
	"github.com/flarexio/stoa/llm/openai"
)

// TestAgent_OpenAI exercises the full bookkeeping loop against the real
// OpenAI API. It is gated by STOA_RUN_OPENAI_TESTS so that a plain
// `go test ./...` -- even with OPENAI_API_KEY in the environment -- never
// silently spends API tokens. Both the flag and the API key must be set.
func TestAgent_OpenAI(t *testing.T) {
	if os.Getenv("STOA_RUN_OPENAI_TESTS") == "" {
		t.Skip("set STOA_RUN_OPENAI_TESTS=1 to run OpenAI integration tests")
	}
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Fatal("STOA_RUN_OPENAI_TESTS is set but OPENAI_API_KEY is empty")
	}

	scenario, repo := awsBillScenario(t)
	bus := wireBus(t, repo)

	renderer, err := bookkeeper.NewPromptRenderer(context.Background(), scenario.Company, repo)
	if err != nil {
		t.Fatalf("new renderer: %v", err)
	}

	engine, err := openai.NewAdapter(openai.Config[accounting.JournalIntent]{
		APIKey:       apiKey,
		Model:        "gpt-5.4-mini",
		OutputFormat: openai.OutputFormatJSONObject,
		Renderer:     renderer,
	})
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}

	agent := bookkeeper.Agent{
		Engine:    engine,
		Repo:      repo,
		Publisher: bus,
		Clock:     func() time.Time { return time.Date(2026, 5, 12, 9, 0, 0, 0, time.UTC) },
		MaxTurns:  3,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	res, err := agent.Book(ctx, "Paid AWS bill 100 USD using company credit card on 12 May 2026.")
	if err != nil {
		t.Fatalf("bookkeeping run failed: %v", err)
	}
	if res.Entry.ID == "" {
		t.Fatal("expected a posted entry")
	}
	if res.Entry.PeriodID != "2026-05" {
		t.Errorf("expected entry posted to open May 2026 period, got %q", res.Entry.PeriodID)
	}

	var debit, credit int64
	for _, line := range res.Entry.Lines {
		switch line.Side {
		case accounting.SideDebit:
			debit += line.Amount
		case accounting.SideCredit:
			credit += line.Amount
		}
	}
	if debit == 0 || credit == 0 || debit != credit {
		t.Errorf("posted entry should be balanced, got debit=%d credit=%d", debit, credit)
	}

	t.Logf("turns=%d entry=%s currency=%s debit=%d credit=%d", res.Turns, res.Entry.ID, res.Entry.Currency, debit, credit)
	for _, line := range res.Entry.Lines {
		t.Logf("  %s %s %d (%s) memo=%q", line.AccountCode, line.Side, line.Amount, line.Dimensions.BranchID, line.Memo)
	}
}
