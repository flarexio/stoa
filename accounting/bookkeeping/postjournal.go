// Package bookkeeping holds the bookkeeping use cases: application-layer
// operations that validate and execute a typed intent against the accounting
// domain. A use case carries no LLM dependency -- the agent drives it through
// the harness loop, but a REST handler, batch job, or test can call Handle
// directly.
package bookkeeping

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/flarexio/stoa/accounting"
)

// SubjectLedger is the default subject JournalPosted events are published on
// for optimistic-concurrency scoping. Override it via PostJournal.Subject when
// multiple ledgers share a transport.
const SubjectLedger = "accounting.journal"

// Clock returns the time a posted entry is stamped with; tests inject a
// deterministic clock.
type Clock func() time.Time

// PostJournal is the "post a journal entry" use case: it validates a
// JournalIntent against the ledger and, on success, publishes it as a
// JournalPosted event. The agent drives Validate and Execute as the two
// harness-loop steps; a non-LLM caller posts in one call through Handle.
type PostJournal struct {
	Repo      accounting.LedgerRepository
	Publisher EventPublisher
	Clock     Clock
	Subject   string
}

// Validate reports whether intent satisfies every accounting invariant. It
// runs no side effect.
func (uc PostJournal) Validate(ctx context.Context, intent accounting.JournalIntent) error {
	return accounting.Validator{Repo: uc.Repo}.Validate(ctx, intent)
}

// Execute publishes an already-validated intent and returns the posted entry.
// It does not re-validate -- an unvalidated caller must use Handle.
func (uc PostJournal) Execute(ctx context.Context, intent accounting.JournalIntent) (accounting.JournalEntry, error) {
	if uc.Repo == nil {
		return accounting.JournalEntry{}, errors.New("bookkeeping: post journal has no repository")
	}
	if uc.Publisher == nil {
		return accounting.JournalEntry{}, errors.New("bookkeeping: post journal has no event publisher")
	}

	subject := uc.Subject
	if subject == "" {
		subject = SubjectLedger
	}
	clock := uc.Clock
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}

	// lastSeq is both the broker's optimistic-concurrency expectation and the
	// dense counter for the entry ID: Apply writes the entry and bumps the
	// subject offset in one transaction, so lastSeq+1 is the sequence a
	// successful publish assigns. On a lost race the broker returns
	// ErrConcurrentUpdate and the caller retries with a fresh lastSeq, so no
	// duplicate entry can take this ID.
	lastSeq, err := uc.Repo.LastSequence(ctx, subject)
	if err != nil {
		return accounting.JournalEntry{}, fmt.Errorf("bookkeeping: read last sequence: %w", err)
	}

	entry := accounting.JournalEntry{
		ID:          accounting.FormatEntryID(lastSeq + 1),
		Date:        intent.Date,
		PeriodID:    intent.PeriodID,
		Currency:    intent.Currency,
		Description: intent.Description,
		Lines:       intent.Lines,
		PostedAt:    clock(),
	}

	dispatched, err := uc.Publisher.Publish(ctx, accounting.JournalPosted{Entry: entry}, accounting.ExpectedSequence{
		Subject: subject,
		LastSeq: lastSeq,
	})
	if err != nil {
		return accounting.JournalEntry{}, fmt.Errorf("bookkeeping: publish: %w", err)
	}
	return dispatched.Entry, nil
}

// Handle validates intent and, if clean, executes it in a single call.
func (uc PostJournal) Handle(ctx context.Context, intent accounting.JournalIntent) (accounting.JournalEntry, error) {
	if err := uc.Validate(ctx, intent); err != nil {
		return accounting.JournalEntry{}, err
	}
	return uc.Execute(ctx, intent)
}
