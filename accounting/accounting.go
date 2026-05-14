// Package accounting is the bookkeeping domain for Stoa.
// It owns the core ledger model -- company, chart of accounts, accounting
// periods, branches as reporting dimensions, and the journal entry value
// types -- along with the validation rules an agent's proposed JournalIntent
// must satisfy before posting.
//
// The package has no dependency on LLM SDKs, provider adapters, the harness,
// or CLI code, so it can be imported by offline tools, batch validators, or
// other agents without pulling in any AI infrastructure.
//
// # Journal-entry immutability
//
// A JournalEntry, once posted, is immutable. Corrections do not edit or
// delete the original; they post a new entry that reverses it (and, if
// applicable, a second entry with the correct figures). This is a
// double-entry-bookkeeping invariant, not a project convention: SOX,
// GAAP, and IFRS all require a posted entry's audit trail to be
// preserved verbatim, so erasing or rewriting history is non-compliant.
//
// As a direct consequence, this package exposes a single domain event
// affecting entries: JournalPosted. There is intentionally no
// JournalEdited and no JournalDeleted. Code that needs to express "this
// entry was reversed by that one" must do so through entry-payload
// relationships (e.g. a future reversed_by reference field), not by
// mutating the original.
//
// This invariant also keeps the broker-sequence -> Entry.ID derivation
// (see FormatEntryID) safe to treat as a permanent aggregate identifier:
// an entry's ID never changes after creation because the entry itself
// never changes.
package accounting

import "time"

// LineSide names the two halves of a balanced double-entry posting.
type LineSide string

const (
	SideDebit  LineSide = "debit"
	SideCredit LineSide = "credit"
)

// AccountType is the high-level classification of a chart-of-accounts entry.
type AccountType string

const (
	AccountAsset     AccountType = "asset"
	AccountLiability AccountType = "liability"
	AccountEquity    AccountType = "equity"
	AccountRevenue   AccountType = "revenue"
	AccountExpense   AccountType = "expense"
)

// PeriodStatus controls whether a period accepts new postings.
type PeriodStatus string

const (
	PeriodOpen   PeriodStatus = "open"
	PeriodClosed PeriodStatus = "closed"
)

// Company is the legal entity that owns the ledger. Branches are reporting
// dimensions inside the same legal entity, not separate companies.
type Company struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Account is one row in the chart of accounts. Inactive accounts cannot be
// used in new postings.
type Account struct {
	Code   string      `json:"code"`
	Name   string      `json:"name"`
	Type   AccountType `json:"type"`
	Active bool        `json:"active"`
}

// Branch is a reporting dimension within the single ledger. Branches do not
// own their own books; they tag journal lines for reporting.
type Branch struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Period is an accounting period. Closed periods cannot accept postings, so
// closing a period seals the books for that range.
type Period struct {
	ID     string       `json:"id"`
	Start  time.Time    `json:"start"`
	End    time.Time    `json:"end"`
	Status PeriodStatus `json:"status"`
}

// Dimensions tag a journal line with reporting cuts. BranchID is the first
// dimension; Tags is intentionally open-ended so future dimensions (project,
// department, channel) can be added without changing the journal-line shape.
type Dimensions struct {
	BranchID string            `json:"branch_id,omitempty"`
	Tags     map[string]string `json:"tags,omitempty"`
}

// JournalLine is one debit or credit on a journal entry. Amount is in minor
// units of Currency (e.g. cents) so balanced checks are exact and never rely
// on floating point.
type JournalLine struct {
	AccountCode string     `json:"account_code"`
	Side        LineSide   `json:"side"`
	Amount      int64      `json:"amount"`
	Memo        string     `json:"memo,omitempty"`
	Dimensions  Dimensions `json:"dimensions"`
}

// JournalIntent is the typed output of the bookkeeping agent for one
// transaction. It is not yet a posted entry: it must clear the accounting
// Validator before Ledger.Post will accept it.
//
// Description is optional audit metadata for human reviewers; it is not
// enforced by Validator and may be empty.
type JournalIntent struct {
	Date        time.Time     `json:"date"`
	PeriodID    string        `json:"period_id"`
	Currency    string        `json:"currency"`
	Description string        `json:"description"`
	Lines       []JournalLine `json:"lines"`
}

// JournalEntry is a posted (validated, sealed) accounting entry. The fields
// are exported for serialization, but the ledger only exposes copies so
// posted entries cannot be mutated through the ledger surface. The
// stronger rule -- entries are immutable by accounting policy, not just
// by API surface -- is documented in the package overview; corrections
// go through new reversing entries, never through edits to this value.
type JournalEntry struct {
	ID          string        `json:"id"`
	Date        time.Time     `json:"date"`
	PeriodID    string        `json:"period_id"`
	Currency    string        `json:"currency"`
	Description string        `json:"description"`
	Lines       []JournalLine `json:"lines"`
	PostedAt    time.Time     `json:"posted_at"`
}
