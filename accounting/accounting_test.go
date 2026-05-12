package accounting_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/flarexio/stoa/accounting"
)

// awsBillLedger returns a deterministic two-branch ledger seeded from the
// testdata fixture: April 2026 is closed, May 2026 is open, one expense
// account is inactive to exercise the inactive-account rule.
func awsBillLedger(t *testing.T) *accounting.Ledger {
	t.Helper()
	scenario, err := accounting.LoadScenarioFile("../testdata/accounting/aws_bill.json")
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	l := scenario.BuildLedger()
	l.Clock = func() time.Time { return time.Date(2026, 5, 12, 9, 0, 0, 0, time.UTC) }
	return l
}

// balancedAWSIntent is the canonical AWS-bill journal: $100 to cloud hosting
// expense (debit), $100 to credit card payable (credit), in the open period.
func balancedAWSIntent() accounting.JournalIntent {
	return accounting.JournalIntent{
		Date:        time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC),
		PeriodID:    "2026-05",
		Currency:    "USD",
		Description: "Paid AWS bill on company credit card",
		Lines: []accounting.JournalLine{
			{
				AccountCode: "5200",
				Side:        accounting.SideDebit,
				Amount:      10000,
				Memo:        "AWS monthly invoice",
				Dimensions:  accounting.Dimensions{BranchID: "hq"},
			},
			{
				AccountCode: "2100",
				Side:        accounting.SideCredit,
				Amount:      10000,
				Memo:        "Charged to Visa",
				Dimensions:  accounting.Dimensions{BranchID: "hq"},
			},
		},
	}
}

func TestValidator_BalancedAWSBill(t *testing.T) {
	l := awsBillLedger(t)
	v := accounting.Validator{Ledger: l}
	if err := v.Validate(context.Background(), balancedAWSIntent()); err != nil {
		t.Fatalf("expected balanced AWS bill to pass, got %v", err)
	}
}

func TestValidator_RejectsUnbalanced(t *testing.T) {
	l := awsBillLedger(t)
	intent := balancedAWSIntent()
	intent.Lines[1].Amount = 9000 // credit no longer matches debit
	err := accounting.Validator{Ledger: l}.Validate(context.Background(), intent)
	if err == nil || !strings.Contains(err.Error(), "debits") {
		t.Fatalf("expected unbalanced error, got %v", err)
	}
}

func TestValidator_RejectsSingleLine(t *testing.T) {
	l := awsBillLedger(t)
	intent := balancedAWSIntent()
	intent.Lines = intent.Lines[:1]
	err := accounting.Validator{Ledger: l}.Validate(context.Background(), intent)
	if err == nil || !strings.Contains(err.Error(), "at least two lines") {
		t.Fatalf("expected at-least-two-lines error, got %v", err)
	}
}

func TestValidator_RejectsClosedPeriod(t *testing.T) {
	l := awsBillLedger(t)
	intent := balancedAWSIntent()
	intent.PeriodID = "2026-04" // closed in the fixture
	err := accounting.Validator{Ledger: l}.Validate(context.Background(), intent)
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("expected closed-period error, got %v", err)
	}
}

func TestValidator_RejectsUnknownPeriod(t *testing.T) {
	l := awsBillLedger(t)
	intent := balancedAWSIntent()
	intent.PeriodID = "1999-12"
	err := accounting.Validator{Ledger: l}.Validate(context.Background(), intent)
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("expected unknown-period error, got %v", err)
	}
}

func TestValidator_RejectsZeroDate(t *testing.T) {
	l := awsBillLedger(t)
	intent := balancedAWSIntent()
	intent.Date = time.Time{}
	err := accounting.Validator{Ledger: l}.Validate(context.Background(), intent)
	if err == nil || !strings.Contains(err.Error(), "date is required") {
		t.Fatalf("expected zero-date error, got %v", err)
	}
}

func TestValidator_RejectsDateBeforePeriod(t *testing.T) {
	l := awsBillLedger(t)
	intent := balancedAWSIntent()
	intent.Date = time.Date(2026, 4, 30, 23, 0, 0, 0, time.UTC) // before 2026-05 start
	err := accounting.Validator{Ledger: l}.Validate(context.Background(), intent)
	if err == nil || !strings.Contains(err.Error(), "before period") {
		t.Fatalf("expected date-before-period error, got %v", err)
	}
}

func TestValidator_RejectsDateAfterPeriod(t *testing.T) {
	l := awsBillLedger(t)
	intent := balancedAWSIntent()
	intent.Date = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC) // after 2026-05 end
	err := accounting.Validator{Ledger: l}.Validate(context.Background(), intent)
	if err == nil || !strings.Contains(err.Error(), "after period") {
		t.Fatalf("expected date-after-period error, got %v", err)
	}
}

func TestValidator_RejectsInactiveAccount(t *testing.T) {
	l := awsBillLedger(t)
	intent := balancedAWSIntent()
	intent.Lines[0].AccountCode = "5900" // inactive in the fixture
	err := accounting.Validator{Ledger: l}.Validate(context.Background(), intent)
	if err == nil || !strings.Contains(err.Error(), "inactive") {
		t.Fatalf("expected inactive-account error, got %v", err)
	}
}

func TestValidator_RejectsUnknownAccount(t *testing.T) {
	l := awsBillLedger(t)
	intent := balancedAWSIntent()
	intent.Lines[0].AccountCode = "9999"
	err := accounting.Validator{Ledger: l}.Validate(context.Background(), intent)
	if err == nil || !strings.Contains(err.Error(), "chart of accounts") {
		t.Fatalf("expected unknown-account error, got %v", err)
	}
}

func TestValidator_RejectsUnknownBranch(t *testing.T) {
	l := awsBillLedger(t)
	intent := balancedAWSIntent()
	intent.Lines[0].Dimensions.BranchID = "atlantis"
	err := accounting.Validator{Ledger: l}.Validate(context.Background(), intent)
	if err == nil || !strings.Contains(err.Error(), "branch") {
		t.Fatalf("expected unknown-branch error, got %v", err)
	}
}

func TestValidator_RejectsNonPositiveAmount(t *testing.T) {
	l := awsBillLedger(t)
	intent := balancedAWSIntent()
	intent.Lines[0].Amount = 0
	intent.Lines[1].Amount = 0
	err := accounting.Validator{Ledger: l}.Validate(context.Background(), intent)
	if err == nil || !strings.Contains(err.Error(), "positive") {
		t.Fatalf("expected positive-amount error, got %v", err)
	}
}

func TestValidator_RejectsInvalidSide(t *testing.T) {
	l := awsBillLedger(t)
	intent := balancedAWSIntent()
	intent.Lines[0].Side = "sideways"
	err := accounting.Validator{Ledger: l}.Validate(context.Background(), intent)
	if err == nil || !strings.Contains(err.Error(), "side") {
		t.Fatalf("expected invalid-side error, got %v", err)
	}
}

func TestValidator_RejectsMissingCurrency(t *testing.T) {
	l := awsBillLedger(t)
	intent := balancedAWSIntent()
	intent.Currency = ""
	err := accounting.Validator{Ledger: l}.Validate(context.Background(), intent)
	if err == nil || !strings.Contains(err.Error(), "currency") {
		t.Fatalf("expected currency error, got %v", err)
	}
}

func TestValidator_NilLedger(t *testing.T) {
	err := accounting.Validator{}.Validate(context.Background(), balancedAWSIntent())
	if err == nil {
		t.Fatal("expected error when validator has no ledger")
	}
}

func TestLedger_PostStoresValidEntry(t *testing.T) {
	l := awsBillLedger(t)
	entry, err := l.Post(context.Background(), balancedAWSIntent())
	if err != nil {
		t.Fatalf("expected post to succeed, got %v", err)
	}
	if entry.ID == "" {
		t.Fatal("expected non-empty entry ID")
	}
	if entry.PostedAt.IsZero() {
		t.Fatal("expected PostedAt to be stamped by clock")
	}
	if got := l.Entries(); len(got) != 1 || got[0].ID != entry.ID {
		t.Fatalf("expected one stored entry matching returned ID, got %+v", got)
	}
}

func TestLedger_PostRejectsInvalid(t *testing.T) {
	l := awsBillLedger(t)
	intent := balancedAWSIntent()
	intent.Lines[1].Amount = 1
	if _, err := l.Post(context.Background(), intent); err == nil {
		t.Fatal("expected unbalanced post to fail")
	}
	if entries := l.Entries(); len(entries) != 0 {
		t.Fatalf("expected no entries after failed post, got %d", len(entries))
	}
}

func TestLedger_PostedEntriesImmutable(t *testing.T) {
	l := awsBillLedger(t)
	entry, err := l.Post(context.Background(), balancedAWSIntent())
	if err != nil {
		t.Fatalf("post failed: %v", err)
	}

	// Mutate the returned entry's line slice.
	entry.Lines[0].Amount = 1
	entry.Lines[0].AccountCode = "tampered"

	// Stored copy must be unchanged.
	stored, ok := l.Entry(entry.ID)
	if !ok {
		t.Fatalf("expected stored entry %s", entry.ID)
	}
	if stored.Lines[0].Amount != 10000 || stored.Lines[0].AccountCode != "5200" {
		t.Fatalf("stored entry was mutated through returned value: %+v", stored.Lines[0])
	}

	// Mutate the slice returned by Entries().
	listed := l.Entries()
	listed[0].Lines[0].Amount = 1
	stored2, _ := l.Entry(entry.ID)
	if stored2.Lines[0].Amount != 10000 {
		t.Fatal("stored entry was mutated through Entries() snapshot")
	}
}

func TestLedger_PostedEntryNotAffectedByLaterIntentMutation(t *testing.T) {
	l := awsBillLedger(t)
	intent := balancedAWSIntent()
	entry, err := l.Post(context.Background(), intent)
	if err != nil {
		t.Fatalf("post failed: %v", err)
	}

	// Mutate the original intent's line slice after posting.
	intent.Lines[0].Amount = 1

	stored, _ := l.Entry(entry.ID)
	if stored.Lines[0].Amount != 10000 {
		t.Fatalf("posted entry was mutated through the originating intent: %+v", stored.Lines[0])
	}
}

func TestLedger_ClosePeriodPreventsFurtherPostings(t *testing.T) {
	l := awsBillLedger(t)
	if _, err := l.Post(context.Background(), balancedAWSIntent()); err != nil {
		t.Fatalf("first post should succeed: %v", err)
	}
	if !l.ClosePeriod("2026-05") {
		t.Fatal("expected ClosePeriod to succeed")
	}
	_, err := l.Post(context.Background(), balancedAWSIntent())
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("expected closed-period error after close, got %v", err)
	}
}

func TestLedger_SequentialIDs(t *testing.T) {
	l := awsBillLedger(t)
	e1, err := l.Post(context.Background(), balancedAWSIntent())
	if err != nil {
		t.Fatalf("post 1: %v", err)
	}
	e2, err := l.Post(context.Background(), balancedAWSIntent())
	if err != nil {
		t.Fatalf("post 2: %v", err)
	}
	if e1.ID == e2.ID {
		t.Fatalf("expected distinct IDs, got %s and %s", e1.ID, e2.ID)
	}
}

func TestScenarioLoader_BuildsLedger(t *testing.T) {
	scenario, err := accounting.LoadScenarioFile("../testdata/accounting/aws_bill.json")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if scenario.Company.ID != "acme" {
		t.Fatalf("unexpected company: %+v", scenario.Company)
	}
	l := scenario.BuildLedger()
	if _, ok := l.Accounts["5200"]; !ok {
		t.Fatal("expected cloud hosting account in chart of accounts")
	}
	if _, ok := l.Branches["hq"]; !ok {
		t.Fatal("expected hq branch")
	}
	if p, ok := l.Periods["2026-04"]; !ok || p.Status != accounting.PeriodClosed {
		t.Fatalf("expected closed April period, got %+v ok=%v", p, ok)
	}
}
