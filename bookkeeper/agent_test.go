package bookkeeper_test

import (
	"context"
	"testing"
	"time"

	"github.com/flarexio/stoa/accounting"
	"github.com/flarexio/stoa/bookkeeper"
	"github.com/flarexio/stoa/llm"
)

type fakeEngineFunc func(ctx context.Context, input llm.ReasoningInput) (llm.ReasoningResult[accounting.JournalIntent], error)

func (f fakeEngineFunc) Predict(ctx context.Context, input llm.ReasoningInput) (llm.ReasoningResult[accounting.JournalIntent], error) {
	return f(ctx, input)
}

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

func balancedAWSIntent() accounting.JournalIntent {
	return accounting.JournalIntent{
		Date:        time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC),
		PeriodID:    "2026-05",
		Currency:    "USD",
		Description: "Paid AWS bill on company credit card",
		Lines: []accounting.JournalLine{
			{AccountCode: "5200", Side: accounting.SideDebit, Amount: 10000, Dimensions: accounting.Dimensions{BranchID: "hq"}},
			{AccountCode: "2100", Side: accounting.SideCredit, Amount: 10000, Dimensions: accounting.Dimensions{BranchID: "hq"}},
		},
	}
}

func TestAgent_PostsBalancedJournal(t *testing.T) {
	ledger := awsBillLedger(t)
	engine := fakeEngineFunc(func(_ context.Context, _ llm.ReasoningInput) (llm.ReasoningResult[accounting.JournalIntent], error) {
		return llm.ReasoningResult[accounting.JournalIntent]{
			Rationale: "AWS invoice paid on credit card; expense debit, liability credit",
			Intent:    balancedAWSIntent(),
		}, nil
	})

	agent := bookkeeper.Agent{Engine: engine, Ledger: ledger, MaxTurns: 3}
	res, err := agent.Book(context.Background(), "Paid AWS bill 100 USD using company credit card")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if res.Turns != 1 {
		t.Fatalf("expected 1 turn, got %d", res.Turns)
	}
	if res.Entry.ID == "" {
		t.Fatal("expected posted entry to be returned")
	}
	if got := ledger.Entries(); len(got) != 1 {
		t.Fatalf("expected one posted entry, got %d", len(got))
	}
}

func TestAgent_CorrectsAfterValidationFeedback(t *testing.T) {
	ledger := awsBillLedger(t)

	calls := 0
	engine := fakeEngineFunc(func(_ context.Context, input llm.ReasoningInput) (llm.ReasoningResult[accounting.JournalIntent], error) {
		calls++
		switch calls {
		case 1:
			// First attempt: agent misreads the bill and lists the wrong credit
			// amount, so debits and credits do not balance.
			intent := balancedAWSIntent()
			intent.Lines[1].Amount = 9000
			return llm.ReasoningResult[accounting.JournalIntent]{
				Rationale: "first pass: assume $90 surcharge waived",
				Intent:    intent,
			}, nil
		default:
			sawValidationErr := false
			for _, e := range input.Events {
				if e.Kind == llm.EventValidationError {
					sawValidationErr = true
				}
			}
			if !sawValidationErr {
				t.Errorf("expected validation_error event on retry, got events %+v", input.Events)
			}
			return llm.ReasoningResult[accounting.JournalIntent]{
				Rationale: "corrected: rebalance credit to match $100 debit",
				Intent:    balancedAWSIntent(),
			}, nil
		}
	})

	agent := bookkeeper.Agent{Engine: engine, Ledger: ledger, MaxTurns: 3}
	res, err := agent.Book(context.Background(), "Paid AWS bill 100 USD using company credit card")
	if err != nil {
		t.Fatalf("expected success after correction, got %v", err)
	}
	if res.Turns != 2 {
		t.Fatalf("expected 2 turns, got %d", res.Turns)
	}
	if calls != 2 {
		t.Fatalf("expected engine called twice, got %d", calls)
	}
	if got := ledger.Entries(); len(got) != 1 {
		t.Fatalf("expected exactly one entry posted after correction, got %d", len(got))
	}
}

func TestAgent_RejectsClosedPeriodIntent(t *testing.T) {
	ledger := awsBillLedger(t)

	calls := 0
	engine := fakeEngineFunc(func(_ context.Context, _ llm.ReasoningInput) (llm.ReasoningResult[accounting.JournalIntent], error) {
		calls++
		intent := balancedAWSIntent()
		if calls == 1 {
			// First attempt targets the closed April period.
			intent.PeriodID = "2026-04"
		}
		return llm.ReasoningResult[accounting.JournalIntent]{
			Rationale: "best guess",
			Intent:    intent,
		}, nil
	})

	agent := bookkeeper.Agent{Engine: engine, Ledger: ledger, MaxTurns: 3}
	res, err := agent.Book(context.Background(), "Record April AWS bill late")
	if err != nil {
		t.Fatalf("expected success after correcting to open period, got %v", err)
	}
	if res.Entry.PeriodID != "2026-05" {
		t.Fatalf("expected entry posted to open period, got %q", res.Entry.PeriodID)
	}
}

func TestAgent_MissingEngine(t *testing.T) {
	ledger := awsBillLedger(t)
	agent := bookkeeper.Agent{Ledger: ledger}
	if _, err := agent.Book(context.Background(), "x"); err == nil {
		t.Fatal("expected error for missing engine")
	}
}

func TestAgent_MissingLedger(t *testing.T) {
	engine := fakeEngineFunc(func(_ context.Context, _ llm.ReasoningInput) (llm.ReasoningResult[accounting.JournalIntent], error) {
		return llm.ReasoningResult[accounting.JournalIntent]{}, nil
	})
	agent := bookkeeper.Agent{Engine: engine}
	if _, err := agent.Book(context.Background(), "x"); err == nil {
		t.Fatal("expected error for missing ledger")
	}
}
