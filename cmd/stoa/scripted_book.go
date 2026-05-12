package main

import (
	"context"
	"sort"

	"github.com/flarexio/stoa/accounting"
	"github.com/flarexio/stoa/llm"
)

// scriptedBookEngine is a deterministic, offline llm.ReasoningEngine used by
// the book-run demo. It first proposes a JournalIntent that the accounting
// Validator will reject (credits short of debits), then -- once validation
// feedback appears in the cycle events -- proposes a balanced intent derived
// from the seeded ledger. This proves the reason -> validate -> post -> feedback
// loop end to end without needing an LLM provider.
type scriptedBookEngine struct {
	ledger   *accounting.Ledger
	amount   int64
	currency string
}

func newScriptedBookEngine(l *accounting.Ledger, amount int64, currency string) *scriptedBookEngine {
	return &scriptedBookEngine{ledger: l, amount: amount, currency: currency}
}

func (e *scriptedBookEngine) Predict(_ context.Context, input llm.ReasoningInput) (llm.ReasoningResult[accounting.JournalIntent], error) {
	if hasValidationFeedback(input.Events) {
		intent, rationale := e.recover()
		return llm.ReasoningResult[accounting.JournalIntent]{
			Evidence: []llm.EvidenceRef{
				{Source: "validator", Fact: "previous intent failed validation; rebalancing credit to match debit"},
			},
			Rationale: rationale,
			Intent:    intent,
		}, nil
	}

	intent, rationale := e.firstAttempt()
	return llm.ReasoningResult[accounting.JournalIntent]{
		Evidence: []llm.EvidenceRef{
			{Source: "scenario", Fact: "first attempt: drafted from the bookkeeping request"},
		},
		Rationale: rationale,
		Intent:    intent,
	}, nil
}

// firstAttempt proposes an intent the validator will reject: a balanced
// journal skeleton whose credit line is short, so total debits != total
// credits. This exercises the validation-feedback loop on every run.
func (e *scriptedBookEngine) firstAttempt() (accounting.JournalIntent, string) {
	intent := e.balancedIntent()
	if len(intent.Lines) >= 2 {
		intent.Lines[1].Amount = intent.Lines[0].Amount - 1000
	}
	return intent, "first attempt: misread the bill; credit short of debit"
}

// recover proposes a balanced intent derived from the scenario.
func (e *scriptedBookEngine) recover() (accounting.JournalIntent, string) {
	return e.balancedIntent(), "recover: rebalance credit to match debit"
}

// balancedIntent builds a debit-expense / credit-liability journal entry,
// dated to the start of the first open period and tagged with the first
// reporting branch if any. Accounts are picked in deterministic order so the
// demo output is stable across runs.
func (e *scriptedBookEngine) balancedIntent() accounting.JournalIntent {
	period := firstOpenPeriod(e.ledger)
	expense := firstActiveAccount(e.ledger, accounting.AccountExpense)
	liability := firstActiveAccount(e.ledger, accounting.AccountLiability)

	dims := accounting.Dimensions{}
	if branch := firstBranch(e.ledger); branch != "" {
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
	}
}

func firstActiveAccount(l *accounting.Ledger, t accounting.AccountType) string {
	codes := make([]string, 0, len(l.Accounts))
	for code := range l.Accounts {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	for _, code := range codes {
		a := l.Accounts[code]
		if a.Type == t && a.Active {
			return code
		}
	}
	return ""
}

func firstOpenPeriod(l *accounting.Ledger) accounting.Period {
	ids := make([]string, 0, len(l.Periods))
	for id := range l.Periods {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		p := l.Periods[id]
		if p.Status == accounting.PeriodOpen {
			return p
		}
	}
	return accounting.Period{}
}

func firstBranch(l *accounting.Ledger) string {
	ids := make([]string, 0, len(l.Branches))
	for id := range l.Branches {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) > 0 {
		return ids[0]
	}
	return ""
}
