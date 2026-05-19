package bookkeeping

import (
	"context"
	"errors"
	"fmt"

	"github.com/flarexio/stoa/accounting"
)

// ReverseJournal is the "reverse a posted entry" use case. Given the ID of
// an existing JournalEntry it builds the mirror-image entry -- every line's
// debit and credit swapped -- and posts it through PostJournal. The
// original entry is never touched: a reversal is a new, immutable entry
// that cancels the first, which is the only correction double-entry
// bookkeeping allows (see the accounting package overview).
//
// The reversing entry is posted into the original entry's period and date.
// If that period has since closed the domain validator rejects it, and the
// harness loop surfaces that to the model as correctable feedback.
//
// Like PostJournal it owns no reasoning engine: the agent drives Validate
// and Execute as the two harness-loop steps, while a non-LLM caller
// reverses an entry in one call through Handle.
type ReverseJournal struct {
	Repo      accounting.LedgerRepository
	Publisher EventPublisher
	Clock     Clock
	Subject   string
}

// Validate reports whether intent names an existing entry and the
// resulting reversal satisfies every accounting invariant. It runs no
// side effect.
func (uc ReverseJournal) Validate(ctx context.Context, intent ReverseIntent) error {
	reversal, err := uc.reversalIntent(ctx, intent)
	if err != nil {
		return err
	}
	return uc.post().Validate(ctx, reversal)
}

// Execute posts the reversing entry for an already-validated intent and
// returns it. It does not re-validate -- a caller that has not validated
// first must use Handle.
func (uc ReverseJournal) Execute(ctx context.Context, intent ReverseIntent) (accounting.JournalEntry, error) {
	reversal, err := uc.reversalIntent(ctx, intent)
	if err != nil {
		return accounting.JournalEntry{}, err
	}
	return uc.post().Execute(ctx, reversal)
}

// Handle validates intent and, if it is clean, executes it -- the path a
// non-LLM caller uses to reverse an entry in a single call.
func (uc ReverseJournal) Handle(ctx context.Context, intent ReverseIntent) (accounting.JournalEntry, error) {
	if err := uc.Validate(ctx, intent); err != nil {
		return accounting.JournalEntry{}, err
	}
	return uc.Execute(ctx, intent)
}

// post returns a PostJournal wired against the same repo, publisher, and
// clock so a reversal reaches the ledger through the same publish path.
func (uc ReverseJournal) post() PostJournal {
	return PostJournal{
		Repo:      uc.Repo,
		Publisher: uc.Publisher,
		Clock:     uc.Clock,
		Subject:   uc.Subject,
	}
}

// reversalIntent loads the target entry and builds the JournalIntent that
// mirrors it: same period, date and currency, every line's side flipped,
// and a description that records the reversal and the intent's reason.
func (uc ReverseJournal) reversalIntent(ctx context.Context, intent ReverseIntent) (accounting.JournalIntent, error) {
	if uc.Repo == nil {
		return accounting.JournalIntent{}, errors.New("bookkeeping: reverse journal has no repository")
	}
	if intent.EntryID == "" {
		return accounting.JournalIntent{}, errors.New("bookkeeping: reverse journal needs an entry_id")
	}

	entry, ok, err := uc.Repo.Entry(ctx, intent.EntryID)
	if err != nil {
		return accounting.JournalIntent{}, fmt.Errorf("bookkeeping: load entry %q: %w", intent.EntryID, err)
	}
	if !ok {
		return accounting.JournalIntent{}, fmt.Errorf("bookkeeping: entry %q is not in the ledger", intent.EntryID)
	}

	lines := make([]accounting.JournalLine, len(entry.Lines))
	for i, line := range entry.Lines {
		line.Side = flipSide(line.Side)
		lines[i] = line
	}

	description := fmt.Sprintf("Reversal of %s", entry.ID)
	if intent.Reason != "" {
		description += ": " + intent.Reason
	}

	return accounting.JournalIntent{
		Date:        entry.Date,
		PeriodID:    entry.PeriodID,
		Currency:    entry.Currency,
		Description: description,
		Lines:       lines,
	}, nil
}

func flipSide(side accounting.LineSide) accounting.LineSide {
	switch side {
	case accounting.SideDebit:
		return accounting.SideCredit
	case accounting.SideCredit:
		return accounting.SideDebit
	default:
		return side
	}
}
