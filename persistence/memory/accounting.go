package memory

import (
	"context"
	"sync"

	"github.com/flarexio/stoa/accounting"
)

// Repository is the in-memory accounting.LedgerRepository.
//
// All operations are safe for concurrent use. Stored entries and Lines
// slices are cloned on the way in and on the way out so callers cannot
// mutate the repository's state through any returned value.
type Repository struct {
	mu       sync.RWMutex
	accounts map[string]accounting.Account
	branches map[string]accounting.Branch
	periods  map[string]accounting.Period
	entries  []accounting.JournalEntry
	entryIdx map[string]int
	lastSeq  map[string]uint64
}

// New returns an empty in-memory repository.
func New() *Repository {
	return &Repository{
		accounts: make(map[string]accounting.Account),
		branches: make(map[string]accounting.Branch),
		periods:  make(map[string]accounting.Period),
		entryIdx: make(map[string]int),
		lastSeq:  make(map[string]uint64),
	}
}

// NewAccountingRepository returns an in-memory accounting.LedgerRepository.
func NewAccountingRepository() accounting.LedgerRepository {
	return New()
}

// --- point reads ---

func (r *Repository) Account(_ context.Context, code string) (accounting.Account, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.accounts[code]
	return a, ok, nil
}

func (r *Repository) Period(_ context.Context, id string) (accounting.Period, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.periods[id]
	return p, ok, nil
}

func (r *Repository) Branch(_ context.Context, id string) (accounting.Branch, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	b, ok := r.branches[id]
	return b, ok, nil
}

func (r *Repository) Entry(_ context.Context, id string) (accounting.JournalEntry, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	idx, ok := r.entryIdx[id]
	if !ok {
		return accounting.JournalEntry{}, false, nil
	}
	return cloneEntry(r.entries[idx]), true, nil
}

// --- listings ---

func (r *Repository) Accounts(_ context.Context) ([]accounting.Account, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]accounting.Account, 0, len(r.accounts))
	for _, a := range r.accounts {
		out = append(out, a)
	}
	return out, nil
}

func (r *Repository) Periods(_ context.Context) ([]accounting.Period, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]accounting.Period, 0, len(r.periods))
	for _, p := range r.periods {
		out = append(out, p)
	}
	return out, nil
}

func (r *Repository) Branches(_ context.Context) ([]accounting.Branch, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]accounting.Branch, 0, len(r.branches))
	for _, b := range r.branches {
		out = append(out, b)
	}
	return out, nil
}

func (r *Repository) Entries(_ context.Context) ([]accounting.JournalEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]accounting.JournalEntry, len(r.entries))
	for i, e := range r.entries {
		out[i] = cloneEntry(e)
	}
	return out, nil
}

// --- seed ---

func (r *Repository) PutAccount(_ context.Context, a accounting.Account) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.accounts[a.Code] = a
	return nil
}

func (r *Repository) PutPeriod(_ context.Context, p accounting.Period) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.periods[p.ID] = p
	return nil
}

func (r *Repository) PutBranch(_ context.Context, b accounting.Branch) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.branches[b.ID] = b
	return nil
}

// --- apply / last sequence ---

func (r *Repository) Apply(_ context.Context, evt accounting.JournalPosted) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored := cloneEntry(evt.Entry)
	r.entries = append(r.entries, stored)
	r.entryIdx[stored.ID] = len(r.entries) - 1
	if evt.Subject != "" && evt.Sequence > r.lastSeq[evt.Subject] {
		r.lastSeq[evt.Subject] = evt.Sequence
	}
	return nil
}

func (r *Repository) LastSequence(_ context.Context, subject string) (uint64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastSeq[subject], nil
}

// --- internal cloning ---

func cloneEntry(e accounting.JournalEntry) accounting.JournalEntry {
	out := e
	out.Lines = cloneLines(e.Lines)
	return out
}

func cloneLines(in []accounting.JournalLine) []accounting.JournalLine {
	if in == nil {
		return nil
	}
	out := make([]accounting.JournalLine, len(in))
	for i, l := range in {
		out[i] = l
		if l.Dimensions.Tags != nil {
			tags := make(map[string]string, len(l.Dimensions.Tags))
			for k, v := range l.Dimensions.Tags {
				tags[k] = v
			}
			out[i].Dimensions.Tags = tags
		}
	}
	return out
}
