// Package accounting is the bookkeeping domain for Stoa.
// It owns the core ledger model -- company, chart of accounts, accounting
// periods, branches as reporting dimensions, and the journal entry value
// types -- along with the validation rules an agent's proposed JournalIntent
// must satisfy before posting.
//
// The package has no dependency on LLM SDKs, provider adapters, the harness,
// or CLI code, so it can be imported by offline tools, batch validators, or
// other agents without pulling in any AI infrastructure.
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
type JournalIntent struct {
	Date        time.Time     `json:"date"`
	PeriodID    string        `json:"period_id"`
	Currency    string        `json:"currency"`
	Description string        `json:"description"`
	Lines       []JournalLine `json:"lines"`
}

// JournalEntry is a posted (validated, sealed) accounting entry. The fields
// are exported for serialization, but the ledger only exposes copies so
// posted entries cannot be mutated through the ledger surface.
type JournalEntry struct {
	ID          string        `json:"id"`
	Date        time.Time     `json:"date"`
	PeriodID    string        `json:"period_id"`
	Currency    string        `json:"currency"`
	Description string        `json:"description"`
	Lines       []JournalLine `json:"lines"`
	PostedAt    time.Time     `json:"posted_at"`
}

// cloneLines returns an independent copy of lines so callers cannot mutate
// the ledger's stored state through a returned entry.
func cloneLines(in []JournalLine) []JournalLine {
	if in == nil {
		return nil
	}
	out := make([]JournalLine, len(in))
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
