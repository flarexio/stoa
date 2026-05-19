package usecase_test

import (
	"context"
	"strings"
	"testing"

	"github.com/flarexio/stoa/accounting"
	"github.com/flarexio/stoa/accounting/usecase"
)

// postOne posts balancedIntent through PostJournal and returns the entry,
// the starting point for the reversal tests.
func postOne(t *testing.T, repo accounting.LedgerRepository, bus usecase.EventBus) accounting.JournalEntry {
	t.Helper()
	uc := usecase.PostJournal{Repo: repo, Publisher: bus, Clock: fixedClock}
	entry, err := uc.Handle(context.Background(), balancedIntent())
	if err != nil {
		t.Fatalf("seed posted entry: %v", err)
	}
	return entry
}

// TestReverseJournal_HandleReversesPostedEntry posts an entry, reverses it,
// and checks the reversal is a mirror image: same accounts, debit and
// credit swapped on every line.
func TestReverseJournal_HandleReversesPostedEntry(t *testing.T) {
	ctx := context.Background()
	repo, bus := seededLedger(t)
	original := postOne(t, repo, bus)

	uc := usecase.ReverseJournal{Repo: repo, Publisher: bus, Clock: fixedClock}
	reversal, err := uc.Handle(ctx, usecase.ReverseCommand{EntryID: original.ID, Reason: "duplicate posting"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}

	if reversal.ID == original.ID {
		t.Fatal("expected the reversal to be a new entry, not the original")
	}
	if !strings.HasPrefix(reversal.Description, "Reversal of "+original.ID) {
		t.Fatalf("expected description to record the reversal, got %q", reversal.Description)
	}
	if !strings.Contains(reversal.Description, "duplicate posting") {
		t.Fatalf("expected the reason in the description, got %q", reversal.Description)
	}
	if len(reversal.Lines) != len(original.Lines) {
		t.Fatalf("expected %d lines, got %d", len(original.Lines), len(reversal.Lines))
	}
	for i, line := range reversal.Lines {
		orig := original.Lines[i]
		if line.AccountCode != orig.AccountCode || line.Amount != orig.Amount {
			t.Fatalf("line %d: account/amount changed: %+v vs %+v", i, line, orig)
		}
		if line.Side == orig.Side {
			t.Fatalf("line %d: expected the side flipped, both are %q", i, line.Side)
		}
	}

	stored, err := repo.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 {
		t.Fatalf("expected the original and the reversal stored, got %d entries", len(stored))
	}
}

// TestReverseJournal_RejectsUnknownEntry shows Validate fails before any
// side effect when the entry_id names no posted entry.
func TestReverseJournal_RejectsUnknownEntry(t *testing.T) {
	ctx := context.Background()
	repo, bus := seededLedger(t)

	uc := usecase.ReverseJournal{Repo: repo, Publisher: bus, Clock: fixedClock}
	if err := uc.Validate(ctx, usecase.ReverseCommand{EntryID: "JE-9999"}); err == nil {
		t.Fatal("expected Validate to reject an unknown entry_id")
	}

	if _, err := uc.Handle(ctx, usecase.ReverseCommand{EntryID: "JE-9999"}); err == nil {
		t.Fatal("expected Handle to reject an unknown entry_id")
	}
	if stored, _ := repo.Entries(ctx); len(stored) != 0 {
		t.Fatalf("expected nothing posted for an unknown entry, got %d", len(stored))
	}
}

// TestReverseJournal_RejectsMissingEntryID shows an empty entry_id is
// rejected rather than treated as a lookup for the zero ID.
func TestReverseJournal_RejectsMissingEntryID(t *testing.T) {
	repo, bus := seededLedger(t)

	uc := usecase.ReverseJournal{Repo: repo, Publisher: bus, Clock: fixedClock}
	if err := uc.Validate(context.Background(), usecase.ReverseCommand{}); err == nil {
		t.Fatal("expected Validate to reject a missing entry_id")
	}
}
