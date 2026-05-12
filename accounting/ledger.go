package accounting

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Clock returns the current time used to stamp posted entries. The default
// uses time.Now; tests can supply a deterministic clock.
type Clock func() time.Time

// IDGenerator returns the next journal-entry ID. The default ledger uses an
// internal sequential counter; tests can supply a deterministic generator.
type IDGenerator func() string

// Ledger holds the single set of books for one Company. Accounts, branches,
// periods, and posted entries live here. Branches are reporting dimensions
// within this ledger, not separate ledgers.
//
// Posted entries are stored privately and only exposed through copies, so the
// "posted entries are immutable" invariant cannot be bypassed by mutating a
// returned slice or struct.
//
// Concurrency: only Post, Entries, and Entry are safe to call concurrently.
// The setup methods (AddAccount, AddBranch, AddPeriod, ClosePeriod) read and
// write the Accounts/Branches/Periods maps without holding the mutex, so
// they must complete before any concurrent Post begins. Live mutation of
// the chart of accounts or period status alongside in-flight postings is
// not supported by design.
type Ledger struct {
	Company  Company
	Accounts map[string]Account // keyed by Code
	Branches map[string]Branch  // keyed by ID
	Periods  map[string]Period  // keyed by ID

	Clock Clock
	NewID IDGenerator

	mu      sync.Mutex
	entries []JournalEntry
	seq     int
}

// NewLedger builds an empty ledger for company.
func NewLedger(company Company) *Ledger {
	return &Ledger{
		Company:  company,
		Accounts: make(map[string]Account),
		Branches: make(map[string]Branch),
		Periods:  make(map[string]Period),
	}
}

// AddAccount adds or replaces an account in the chart of accounts.
func (l *Ledger) AddAccount(a Account) {
	l.Accounts[a.Code] = a
}

// AddBranch adds or replaces a reporting branch.
func (l *Ledger) AddBranch(b Branch) {
	l.Branches[b.ID] = b
}

// AddPeriod adds or replaces an accounting period.
func (l *Ledger) AddPeriod(p Period) {
	l.Periods[p.ID] = p
}

// ClosePeriod marks a period closed so it will reject new postings.
// Returns false if no period with that ID exists.
func (l *Ledger) ClosePeriod(id string) bool {
	p, ok := l.Periods[id]
	if !ok {
		return false
	}
	p.Status = PeriodClosed
	l.Periods[id] = p
	return true
}

// Post validates intent and, on success, appends a sealed JournalEntry to
// the ledger and returns a copy of it. Callers that hold the returned entry
// cannot affect the ledger's stored state because the line slice is cloned.
//
// Post is the only path that mutates the ledger's entry log, so the
// invariant "posted journal entries are immutable" reduces to: never expose
// the internal slice or any element of it by reference.
func (l *Ledger) Post(ctx context.Context, intent JournalIntent) (JournalEntry, error) {
	if err := (Validator{Ledger: l}).Validate(ctx, intent); err != nil {
		return JournalEntry{}, err
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	id := l.nextID()
	entry := JournalEntry{
		ID:          id,
		Date:        intent.Date,
		PeriodID:    intent.PeriodID,
		Currency:    intent.Currency,
		Description: intent.Description,
		Lines:       cloneLines(intent.Lines),
		PostedAt:    l.now(),
	}
	l.entries = append(l.entries, entry)

	// Return a copy whose Lines slice is independent of the stored one.
	out := entry
	out.Lines = cloneLines(entry.Lines)
	return out, nil
}

// Entries returns a snapshot of all posted entries. Mutating the returned
// slice or its entries does not affect the ledger.
func (l *Ledger) Entries() []JournalEntry {
	l.mu.Lock()
	defer l.mu.Unlock()

	out := make([]JournalEntry, len(l.entries))
	for i, e := range l.entries {
		out[i] = e
		out[i].Lines = cloneLines(e.Lines)
	}
	return out
}

// Entry returns a snapshot of a posted entry by ID.
func (l *Ledger) Entry(id string) (JournalEntry, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, e := range l.entries {
		if e.ID == id {
			out := e
			out.Lines = cloneLines(e.Lines)
			return out, true
		}
	}
	return JournalEntry{}, false
}

func (l *Ledger) now() time.Time {
	if l.Clock != nil {
		return l.Clock()
	}
	return time.Now().UTC()
}

func (l *Ledger) nextID() string {
	if l.NewID != nil {
		return l.NewID()
	}
	l.seq++
	return fmt.Sprintf("JE-%04d", l.seq)
}
