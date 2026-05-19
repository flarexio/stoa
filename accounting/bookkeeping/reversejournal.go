package bookkeeping

import (
	"context"
	"errors"
	"fmt"

	"github.com/flarexio/stoa/accounting"
)

// ReverseJournal is the "reverse a posted entry" use case. It builds the
// mirror-image entry -- every line's debit and credit swapped -- and posts it
// through PostJournal into the original's period and date. The original entry
// is never touched: a reversal is a new, immutable entry that cancels the
// first, the only correction double-entry bookkeeping allows. If the period
// has since closed the domain validator rejects the reversal.
type ReverseJournal struct {
	Repo      accounting.LedgerRepository
	Publisher EventPublisher
	Clock     Clock
	Subject   string
}

// Validate reports whether intent names an existing entry and the resulting
// reversal satisfies every accounting invariant. It runs no side effect.
func (uc ReverseJournal) Validate(ctx context.Context, intent ReverseIntent) error {
	reversal, err := uc.reversalIntent(ctx, intent)
	if err != nil {
		return err
	}
	return uc.post().Validate(ctx, reversal)
}

// Execute posts the reversing entry for an already-validated intent and
// returns it. It does not re-validate -- an unvalidated caller must use Handle.
func (uc ReverseJournal) Execute(ctx context.Context, intent ReverseIntent) (accounting.JournalEntry, error) {
	reversal, err := uc.reversalIntent(ctx, intent)
	if err != nil {
		return accounting.JournalEntry{}, err
	}
	return uc.post().Execute(ctx, reversal)
}

// Handle validates intent and, if clean, executes it in a single call.
func (uc ReverseJournal) Handle(ctx context.Context, intent ReverseIntent) (accounting.JournalEntry, error) {
	if err := uc.Validate(ctx, intent); err != nil {
		return accounting.JournalEntry{}, err
	}
	return uc.Execute(ctx, intent)
}

// post returns the PostJournal use case ReverseJournal delegates to, so a
// reversal reaches the ledger through the same validated publish path.
func (uc ReverseJournal) post() PostJournal {
	return PostJournal{
		Repo:      uc.Repo,
		Publisher: uc.Publisher,
		Clock:     uc.Clock,
		Subject:   uc.Subject,
	}
}

// reversalIntent loads the target entry and builds the JournalIntent that
// mirrors it: same period, date and currency, every line's side flipped, and a
// description recording the reversal and the intent's reason.
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

// flipSide swaps a debit for a credit and vice versa. An unrecognised side is
// returned unchanged so the domain validator reports it.
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
