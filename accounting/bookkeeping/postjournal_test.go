package bookkeeping_test

import (
	"context"
	"testing"
	"time"

	"github.com/flarexio/stoa/accounting"
	"github.com/flarexio/stoa/accounting/bookkeeping"
	"github.com/flarexio/stoa/messaging/inproc"
	"github.com/flarexio/stoa/persistence/memory"
)

// ledgerScenario is a minimal in-code fixture: two accounts and one open
// period, enough to post a single balanced entry. It is built in Go
// rather than loaded from testdata so the use-case test states its own
// preconditions and carries no dependency on a fixture file's layout.
func ledgerScenario() accounting.Scenario {
	return accounting.Scenario{
		Company: accounting.Company{ID: "acme", Name: "Acme Co."},
		Accounts: []accounting.Account{
			{Code: "5200", Name: "Cloud Infrastructure", Type: accounting.AccountExpense, Active: true},
			{Code: "2100", Name: "Credit Card Payable", Type: accounting.AccountLiability, Active: true},
		},
		Periods: []accounting.Period{
			{
				ID:     "2026-05",
				Start:  time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
				End:    time.Date(2026, 5, 31, 23, 59, 59, 0, time.UTC),
				Status: accounting.PeriodOpen,
			},
		},
	}
}

// seededLedger returns a memory repository seeded with ledgerScenario and
// an inproc bus whose published events are applied back into that
// repository's projection -- the same publish -> handler -> Apply path the
// CLI wires, with no LLM and no agent in the picture.
func seededLedger(t *testing.T) (accounting.LedgerRepository, bookkeeping.EventBus) {
	t.Helper()
	repo := memory.NewAccountingRepository()
	if err := ledgerScenario().Seed(context.Background(), repo); err != nil {
		t.Fatalf("seed: %v", err)
	}
	bus := inproc.NewAccountingBus()
	apply := bookkeeping.EventHandlerFunc(func(ctx context.Context, evt accounting.JournalPosted) error {
		return repo.Apply(ctx, evt)
	})
	if err := bus.Subscribe(apply); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	return repo, bus
}

func balancedIntent() accounting.JournalIntent {
	return accounting.JournalIntent{
		Date:        time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC),
		PeriodID:    "2026-05",
		Currency:    "USD",
		Description: "Paid cloud bill on company credit card",
		Lines: []accounting.JournalLine{
			{AccountCode: "5200", Side: accounting.SideDebit, Amount: 10000},
			{AccountCode: "2100", Side: accounting.SideCredit, Amount: 10000},
		},
	}
}

func fixedClock() time.Time {
	return time.Date(2026, 5, 12, 9, 0, 0, 0, time.UTC)
}

// TestPostJournal_HandlePostsWithoutLLM is the point of the use-case
// layer: a caller with no reasoning engine -- here the test itself, but
// equally a REST handler or a batch job -- posts a journal entry in one
// Handle call and the entry lands in the projection.
func TestPostJournal_HandlePostsWithoutLLM(t *testing.T) {
	ctx := context.Background()
	repo, bus := seededLedger(t)

	uc := bookkeeping.PostJournal{Repo: repo, Publisher: bus, Clock: fixedClock}
	entry, err := uc.Handle(ctx, balancedIntent())
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if entry.ID == "" {
		t.Fatal("expected a posted entry with an ID")
	}
	if !entry.PostedAt.Equal(fixedClock()) {
		t.Fatalf("expected PostedAt stamped from the injected clock, got %s", entry.PostedAt)
	}

	stored, err := repo.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].ID != entry.ID {
		t.Fatalf("expected one stored entry matching the returned ID, got %+v", stored)
	}
}

// TestPostJournal_HandleRejectsInvalidIntent shows Handle runs Validate
// first: an unbalanced intent is rejected and nothing reaches the
// projection.
func TestPostJournal_HandleRejectsInvalidIntent(t *testing.T) {
	ctx := context.Background()
	repo, bus := seededLedger(t)

	intent := balancedIntent()
	intent.Lines[1].Amount = 9000 // credit no longer equals debit

	uc := bookkeeping.PostJournal{Repo: repo, Publisher: bus, Clock: fixedClock}
	if _, err := uc.Handle(ctx, intent); err == nil {
		t.Fatal("expected Handle to reject an unbalanced intent")
	}

	stored, err := repo.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 0 {
		t.Fatalf("expected no entry posted for an invalid intent, got %d", len(stored))
	}
}

// TestPostJournal_ValidateAndExecuteAreSeparable exercises the two-step
// path the harness loop drives: Validate is a side-effect-free check, and
// Execute posts an already-validated intent. Calling them in sequence
// must match what Handle does in one call.
func TestPostJournal_ValidateAndExecuteAreSeparable(t *testing.T) {
	ctx := context.Background()
	repo, bus := seededLedger(t)

	uc := bookkeeping.PostJournal{Repo: repo, Publisher: bus, Clock: fixedClock}
	intent := balancedIntent()

	if err := uc.Validate(ctx, intent); err != nil {
		t.Fatalf("validate: %v", err)
	}
	// Validate ran no side effect: the projection is still empty.
	if stored, _ := repo.Entries(ctx); len(stored) != 0 {
		t.Fatalf("expected Validate to post nothing, got %d entries", len(stored))
	}

	entry, err := uc.Execute(ctx, intent)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if stored, _ := repo.Entries(ctx); len(stored) != 1 || stored[0].ID != entry.ID {
		t.Fatalf("expected Execute to post the validated entry, got %+v", stored)
	}
}
