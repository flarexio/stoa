package inproc_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/flarexio/stoa/accounting"
	"github.com/flarexio/stoa/messaging/inproc"
)

func sampleEvent() accounting.JournalPosted {
	return accounting.JournalPosted{
		Entry: accounting.JournalEntry{
			Date:        time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC),
			PeriodID:    "2026-05",
			Currency:    "USD",
			Description: "Demo",
			Lines: []accounting.JournalLine{
				{AccountCode: "5200", Side: accounting.SideDebit, Amount: 10000},
				{AccountCode: "2100", Side: accounting.SideCredit, Amount: 10000},
			},
		},
	}
}

func TestBus_PublishStampsSubjectSequenceAndID(t *testing.T) {
	ctx := context.Background()
	bus := inproc.New()

	var observed []accounting.JournalPosted
	bus.Subscribe(accounting.EventHandlerFunc(func(_ context.Context, evt accounting.JournalPosted) error {
		observed = append(observed, evt)
		return nil
	}))

	dispatched, err := bus.Publish(ctx, sampleEvent(), accounting.ExpectedSequence{Subject: "accounting.journal", LastSeq: 0})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	if dispatched.Subject != "accounting.journal" {
		t.Fatalf("expected Subject stamped, got %q", dispatched.Subject)
	}
	if dispatched.Sequence != 1 {
		t.Fatalf("expected Sequence=1, got %d", dispatched.Sequence)
	}
	if dispatched.Entry.ID != accounting.FormatEntryID(1) {
		t.Fatalf("expected Entry.ID=%q, got %q", accounting.FormatEntryID(1), dispatched.Entry.ID)
	}

	if len(observed) != 1 {
		t.Fatalf("expected one handler call, got %d", len(observed))
	}
	if observed[0].Entry.ID != dispatched.Entry.ID {
		t.Fatalf("handler saw different ID than dispatched: %q vs %q", observed[0].Entry.ID, dispatched.Entry.ID)
	}
}

func TestBus_RejectsStaleExpectedSequence(t *testing.T) {
	ctx := context.Background()
	bus := inproc.New()

	if _, err := bus.Publish(ctx, sampleEvent(), accounting.ExpectedSequence{Subject: "accounting.journal", LastSeq: 0}); err != nil {
		t.Fatalf("first publish: %v", err)
	}

	_, err := bus.Publish(ctx, sampleEvent(), accounting.ExpectedSequence{Subject: "accounting.journal", LastSeq: 0})
	if !errors.Is(err, accounting.ErrConcurrentUpdate) {
		t.Fatalf("expected ErrConcurrentUpdate, got %v", err)
	}
}

func TestBus_SkipsConcurrencyCheckWhenSubjectEmpty(t *testing.T) {
	ctx := context.Background()
	bus := inproc.New()

	if _, err := bus.Publish(ctx, sampleEvent(), accounting.ExpectedSequence{}); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	if _, err := bus.Publish(ctx, sampleEvent(), accounting.ExpectedSequence{}); err != nil {
		t.Fatalf("second publish: %v", err)
	}
}

func TestBus_DispatchIsSerializedAcrossSubscribers(t *testing.T) {
	ctx := context.Background()
	bus := inproc.New()

	var (
		mu    sync.Mutex
		order []string
	)
	bus.Subscribe(accounting.EventHandlerFunc(func(_ context.Context, evt accounting.JournalPosted) error {
		mu.Lock()
		order = append(order, "a:"+evt.Entry.ID)
		mu.Unlock()
		return nil
	}))
	bus.Subscribe(accounting.EventHandlerFunc(func(_ context.Context, evt accounting.JournalPosted) error {
		mu.Lock()
		order = append(order, "b:"+evt.Entry.ID)
		mu.Unlock()
		return nil
	}))

	if _, err := bus.Publish(ctx, sampleEvent(), accounting.ExpectedSequence{}); err != nil {
		t.Fatal(err)
	}

	if len(order) != 2 || order[0] != "a:"+accounting.FormatEntryID(1) || order[1] != "b:"+accounting.FormatEntryID(1) {
		t.Fatalf("unexpected dispatch order: %v", order)
	}
}

func TestBus_HandlerErrorPropagates(t *testing.T) {
	ctx := context.Background()
	bus := inproc.New()

	want := errors.New("boom")
	bus.Subscribe(accounting.EventHandlerFunc(func(_ context.Context, _ accounting.JournalPosted) error {
		return want
	}))

	_, err := bus.Publish(ctx, sampleEvent(), accounting.ExpectedSequence{})
	if !errors.Is(err, want) {
		t.Fatalf("expected handler error to propagate, got %v", err)
	}
}
