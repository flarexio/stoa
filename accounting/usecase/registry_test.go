package usecase_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/flarexio/stoa/accounting/usecase"
)

// TestRegistry_RoutesPostJournal drives a post_journal Command through the
// registry and checks it reaches PostJournal: an entry lands in the ledger.
func TestRegistry_RoutesPostJournal(t *testing.T) {
	ctx := context.Background()
	repo, bus := seededLedger(t)
	reg := usecase.NewBookkeepingRegistry(repo, bus, fixedClock, "")

	intent := balancedIntent()
	cmd := usecase.Command{Kind: usecase.CommandPostJournal, Post: &intent}

	if err := reg.Validate(ctx, cmd); err != nil {
		t.Fatalf("validate: %v", err)
	}
	entry, err := reg.Execute(ctx, cmd)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if stored, _ := repo.Entries(ctx); len(stored) != 1 || stored[0].ID != entry.ID {
		t.Fatalf("expected the routed entry posted, got %+v", stored)
	}
}

// TestRegistry_RoutesReverseJournal posts an entry, then drives a
// reverse_journal Command through the same registry and checks it reaches
// ReverseJournal: a second, reversing entry lands in the ledger.
func TestRegistry_RoutesReverseJournal(t *testing.T) {
	ctx := context.Background()
	repo, bus := seededLedger(t)
	reg := usecase.NewBookkeepingRegistry(repo, bus, fixedClock, "")

	original := postOne(t, repo, bus)

	reverse := usecase.ReverseIntent{EntryID: original.ID, Reason: "wrong amount"}
	cmd := usecase.Command{Kind: usecase.CommandReverseJournal, Reverse: &reverse}

	if err := reg.Validate(ctx, cmd); err != nil {
		t.Fatalf("validate: %v", err)
	}
	reversal, err := reg.Execute(ctx, cmd)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if reversal.ID == original.ID {
		t.Fatal("expected a new reversing entry")
	}
	if stored, _ := repo.Entries(ctx); len(stored) != 2 {
		t.Fatalf("expected the original and the reversal stored, got %d", len(stored))
	}
}

// TestRegistry_RejectsUnknownKind shows an unrecognised Kind is a routed
// validation error -- correctable feedback for the loop, not a panic.
func TestRegistry_RejectsUnknownKind(t *testing.T) {
	repo, bus := seededLedger(t)
	reg := usecase.NewBookkeepingRegistry(repo, bus, fixedClock, "")

	err := reg.Validate(context.Background(), usecase.Command{Kind: "frobnicate"})
	if err == nil {
		t.Fatal("expected an unknown command kind to be rejected")
	}
	if !strings.Contains(err.Error(), "unknown command kind") {
		t.Fatalf("expected an unknown-kind error, got %v", err)
	}
}

// TestRegistry_RejectsMissingPayload shows a Command whose Kind selects a
// payload the model did not fill is rejected, not nil-dereferenced.
func TestRegistry_RejectsMissingPayload(t *testing.T) {
	repo, bus := seededLedger(t)
	reg := usecase.NewBookkeepingRegistry(repo, bus, fixedClock, "")

	if err := reg.Validate(context.Background(), usecase.Command{Kind: usecase.CommandPostJournal}); err == nil {
		t.Fatal("expected post_journal with no payload to be rejected")
	}
	if err := reg.Validate(context.Background(), usecase.Command{Kind: usecase.CommandReverseJournal}); err == nil {
		t.Fatal("expected reverse_journal with no payload to be rejected")
	}
}

// TestRegistry_KindsMatchCommands is the drift guard: the kinds the
// registry routes must be exactly the vocabulary Commands() describes for
// the prompt, so the model is never offered a command the registry cannot
// route, nor a routable command the prompt never mentions.
func TestRegistry_KindsMatchCommands(t *testing.T) {
	reg := usecase.NewBookkeepingRegistry(nil, nil, nil, "")

	want := make([]usecase.CommandKind, 0)
	for _, d := range usecase.Commands() {
		want = append(want, d.Kind)
	}
	slices.Sort(want)

	if got := reg.Kinds(); !slices.Equal(got, want) {
		t.Fatalf("registry routes %v but Commands() describes %v", got, want)
	}
}
