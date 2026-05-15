package runtime

import (
	"context"
	"sort"

	"github.com/flarexio/stoa/accounting"
	"github.com/flarexio/stoa/llm"
)

// ScriptedBookEngine is a deterministic, offline llm.ReasoningEngine. It
// first proposes a JournalIntent that the accounting Validator will reject
// (credits short of debits), then — once validation feedback appears in
// the cycle events — proposes a balanced intent derived from the seeded
// repository.
type ScriptedBookEngine struct {
	repo     accounting.LedgerRepository
	amount   int64
	currency string
}

func NewScriptedBookEngine(repo accounting.LedgerRepository, amount int64, currency string) *ScriptedBookEngine {
	return &ScriptedBookEngine{repo: repo, amount: amount, currency: currency}
}

func (e *ScriptedBookEngine) Predict(ctx context.Context, input llm.ReasoningInput) (llm.ReasoningResult[accounting.JournalIntent], error) {
	if HasValidationFeedback(input.Events) {
		intent, rationale, err := e.recover(ctx)
		if err != nil {
			return llm.ReasoningResult[accounting.JournalIntent]{}, err
		}
		return llm.ReasoningResult[accounting.JournalIntent]{
			Evidence: []llm.EvidenceRef{
				{Source: "validator", Fact: "previous intent failed validation; rebalancing credit to match debit"},
			},
			Rationale: rationale,
			Intent:    intent,
		}, nil
	}

	intent, rationale, err := e.firstAttempt(ctx)
	if err != nil {
		return llm.ReasoningResult[accounting.JournalIntent]{}, err
	}
	return llm.ReasoningResult[accounting.JournalIntent]{
		Evidence: []llm.EvidenceRef{
			{Source: "scenario", Fact: "first attempt: drafted from the bookkeeping request"},
		},
		Rationale: rationale,
		Intent:    intent,
	}, nil
}

func (e *ScriptedBookEngine) firstAttempt(ctx context.Context) (accounting.JournalIntent, string, error) {
	intent, err := e.balancedIntent(ctx)
	if err != nil {
		return accounting.JournalIntent{}, "", err
	}
	if len(intent.Lines) >= 2 {
		intent.Lines[1].Amount = intent.Lines[0].Amount - intent.Lines[0].Amount/10
	}
	return intent, "first attempt: misread the bill; credit short of debit", nil
}

func (e *ScriptedBookEngine) recover(ctx context.Context) (accounting.JournalIntent, string, error) {
	intent, err := e.balancedIntent(ctx)
	if err != nil {
		return accounting.JournalIntent{}, "", err
	}
	return intent, "recover: rebalance credit to match debit", nil
}

func (e *ScriptedBookEngine) balancedIntent(ctx context.Context) (accounting.JournalIntent, error) {
	period, err := FirstOpenPeriod(ctx, e.repo)
	if err != nil {
		return accounting.JournalIntent{}, err
	}
	expense, err := FirstActiveAccount(ctx, e.repo, accounting.AccountExpense)
	if err != nil {
		return accounting.JournalIntent{}, err
	}
	liability, err := FirstActiveAccount(ctx, e.repo, accounting.AccountLiability)
	if err != nil {
		return accounting.JournalIntent{}, err
	}

	dims := accounting.Dimensions{}
	branch, err := FirstBranch(ctx, e.repo)
	if err != nil {
		return accounting.JournalIntent{}, err
	}
	if branch != "" {
		dims.BranchID = branch
	}

	return accounting.JournalIntent{
		Date:        period.Start,
		PeriodID:    period.ID,
		Currency:    e.currency,
		Description: "Demo bookkeeping post",
		Lines: []accounting.JournalLine{
			{
				AccountCode: expense,
				Side:        accounting.SideDebit,
				Amount:      e.amount,
				Memo:        "Demo expense",
				Dimensions:  dims,
			},
			{
				AccountCode: liability,
				Side:        accounting.SideCredit,
				Amount:      e.amount,
				Memo:        "Demo liability",
				Dimensions:  dims,
			},
		},
	}, nil
}

// FirstActiveAccount returns the first active account code of the given
// type, or an empty string if none is found.
func FirstActiveAccount(ctx context.Context, repo accounting.LedgerRepository, t accounting.AccountType) (string, error) {
	accounts, err := repo.Accounts(ctx)
	if err != nil {
		return "", err
	}
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].Code < accounts[j].Code })
	for _, a := range accounts {
		if a.Type == t && a.Active {
			return a.Code, nil
		}
	}
	return "", nil
}

// FirstOpenPeriod returns the first open accounting period, or a zero
// Period if none is open.
func FirstOpenPeriod(ctx context.Context, repo accounting.LedgerRepository) (accounting.Period, error) {
	periods, err := repo.Periods(ctx)
	if err != nil {
		return accounting.Period{}, err
	}
	sort.Slice(periods, func(i, j int) bool { return periods[i].ID < periods[j].ID })
	for _, p := range periods {
		if p.Status == accounting.PeriodOpen {
			return p, nil
		}
	}
	return accounting.Period{}, nil
}

// FirstBranch returns the first branch ID, or an empty string if none
// exist.
func FirstBranch(ctx context.Context, repo accounting.LedgerRepository) (string, error) {
	branches, err := repo.Branches(ctx)
	if err != nil {
		return "", err
	}
	sort.Slice(branches, func(i, j int) bool { return branches[i].ID < branches[j].ID })
	if len(branches) > 0 {
		return branches[0].ID, nil
	}
	return "", nil
}
