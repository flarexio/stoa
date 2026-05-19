// Package usecase holds the bookkeeping use cases -- the application-layer
// operations that validate and execute a typed command against the
// accounting domain. A use case carries no LLM dependency: the agent
// drives it through the harness loop, but a REST handler, a batch job, or
// a test can call Handle directly.
package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/flarexio/stoa/accounting"
)

// SubjectLedger is the default subject JournalPosted events are published
// on for optimistic-concurrency scoping. Override it via PostJournal.Subject
// when multiple ledgers share a transport.
const SubjectLedger = "accounting.journal"

// Clock returns the time a posted journal entry is stamped with. Default
// is time.Now().UTC(); tests inject a deterministic clock.
type Clock func() time.Time

// PostJournal is the "post a journal entry" use case. It validates a
// proposed JournalIntent against the ledger and, on success, publishes it
// as a JournalPosted event.
//
// It owns no reasoning engine. The bookkeeping agent drives Validate and
// Execute as the two separate steps of the harness loop (validate, feed
// failures back, execute once clean), while a non-LLM caller posts an
// entry in one call through Handle.
type PostJournal struct {
	Repo      accounting.LedgerRepository
	Publisher EventPublisher
	Clock     Clock
	Subject   string
}

// Validate reports whether intent satisfies every accounting invariant,
// delegating to the domain validator. It runs no side effect.
func (uc PostJournal) Validate(ctx context.Context, intent accounting.JournalIntent) error {
	return accounting.Validator{Repo: uc.Repo}.Validate(ctx, intent)
}

// Execute publishes an already-validated intent as a JournalPosted event
// and returns the posted entry. It does not re-validate -- a caller that
// has not validated first must use Handle.
func (uc PostJournal) Execute(ctx context.Context, intent accounting.JournalIntent) (accounting.JournalEntry, error) {
	if uc.Repo == nil {
		return accounting.JournalEntry{}, errors.New("usecase: post journal has no repository")
	}
	if uc.Publisher == nil {
		return accounting.JournalEntry{}, errors.New("usecase: post journal has no event publisher")
	}

	subject := uc.Subject
	if subject == "" {
		subject = SubjectLedger
	}
	clock := uc.Clock
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}

	// lastSeq is read here both as the broker's optimistic-concurrency
	// expectation and as the dense counter for the entry's identity:
	// because Apply writes the entry row and bumps subject_offsets in the
	// same transaction, MAX(sequence) and last_sequence are guaranteed
	// equal, and lastSeq+1 is the sequence the broker will assign on a
	// successful publish. The use case therefore picks the entry's ID
	// right here, before publishing, and the transport carries the ID
	// through the wire unchanged. If another producer wins the race the
	// broker rejects this publish with accounting.ErrConcurrentUpdate; the
	// caller (the harness loop) retries with a freshly read lastSeq and a
	// new ID is assigned -- no duplicate entry can take this ID because
	// the publish failed.
	lastSeq, err := uc.Repo.LastSequence(ctx, subject)
	if err != nil {
		return accounting.JournalEntry{}, fmt.Errorf("usecase: read last sequence: %w", err)
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
		return accounting.JournalEntry{}, fmt.Errorf("usecase: publish: %w", err)
	}
	return dispatched.Entry, nil
}

// Handle validates intent and, if it is clean, executes it -- the path a
// non-LLM caller (a REST handler, a batch job) uses to post a journal
// entry in a single call.
func (uc PostJournal) Handle(ctx context.Context, intent accounting.JournalIntent) (accounting.JournalEntry, error) {
	if err := uc.Validate(ctx, intent); err != nil {
		return accounting.JournalEntry{}, err
	}
	return uc.Execute(ctx, intent)
}
