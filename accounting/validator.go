package accounting

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Validator enforces the accounting invariants on a proposed JournalIntent.
// It reads from Ledger but never mutates it; Ledger.Post is the only path
// that records an entry, and it runs Validator first.
type Validator struct {
	Ledger *Ledger
}

// Validate returns nil if intent satisfies every accounting invariant, or a
// joined error describing every violation so the bookkeeping agent can fix
// them all in one correction cycle.
func (v Validator) Validate(_ context.Context, intent JournalIntent) error {
	if v.Ledger == nil {
		return errors.New("accounting: validator has no ledger")
	}

	var errs []error

	if intent.Currency == "" {
		errs = append(errs, errors.New("currency is required"))
	}

	period, periodOK := v.Ledger.Periods[intent.PeriodID]
	switch {
	case intent.PeriodID == "":
		errs = append(errs, errors.New("period_id is required"))
	case !periodOK:
		errs = append(errs, fmt.Errorf("period %q does not exist", intent.PeriodID))
	case period.Status == PeriodClosed:
		errs = append(errs, fmt.Errorf("period %q is closed and cannot accept postings", intent.PeriodID))
	}

	switch {
	case intent.Date.IsZero():
		errs = append(errs, errors.New("date is required"))
	case periodOK && intent.Date.Before(period.Start):
		errs = append(errs, fmt.Errorf("date %s is before period %q starts (%s)", intent.Date.Format(time.RFC3339), intent.PeriodID, period.Start.Format(time.RFC3339)))
	case periodOK && intent.Date.After(period.End):
		errs = append(errs, fmt.Errorf("date %s is after period %q ends (%s)", intent.Date.Format(time.RFC3339), intent.PeriodID, period.End.Format(time.RFC3339)))
	}

	if len(intent.Lines) < 2 {
		errs = append(errs, fmt.Errorf("journal entry must have at least two lines, got %d", len(intent.Lines)))
	}

	var debits, credits int64
	for i, line := range intent.Lines {
		label := fmt.Sprintf("line[%d]", i)

		switch line.Side {
		case SideDebit:
			debits += line.Amount
		case SideCredit:
			credits += line.Amount
		default:
			errs = append(errs, fmt.Errorf("%s: side must be %q or %q, got %q", label, SideDebit, SideCredit, line.Side))
		}

		if line.Amount <= 0 {
			errs = append(errs, fmt.Errorf("%s: amount must be positive, got %d", label, line.Amount))
		}

		if line.AccountCode == "" {
			errs = append(errs, fmt.Errorf("%s: account_code is required", label))
		} else {
			acct, ok := v.Ledger.Accounts[line.AccountCode]
			switch {
			case !ok:
				errs = append(errs, fmt.Errorf("%s: account %q is not in the chart of accounts", label, line.AccountCode))
			case !acct.Active:
				errs = append(errs, fmt.Errorf("%s: account %q is inactive and cannot be used", label, line.AccountCode))
			}
		}

		if line.Dimensions.BranchID != "" {
			if _, ok := v.Ledger.Branches[line.Dimensions.BranchID]; !ok {
				errs = append(errs, fmt.Errorf("%s: branch %q is not a known reporting dimension", label, line.Dimensions.BranchID))
			}
		}
	}

	if len(intent.Lines) >= 2 && debits != credits {
		errs = append(errs, fmt.Errorf("debits (%d) must equal credits (%d)", debits, credits))
	}

	return errors.Join(errs...)
}
