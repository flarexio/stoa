// Package bookkeeper is the bookkeeping use-case package. It wires the
// accounting domain (entities, validator, ledger) through the harness loop:
// the LLM proposes an accounting.JournalIntent, the accounting validator
// enforces ledger invariants, and the ledger only posts the entry after
// validation succeeds. Bookkeeping orchestration lives here; the accounting
// domain stays free of LLM and harness dependencies.
package bookkeeper

import (
	"context"
	"errors"
	"fmt"

	"github.com/flarexio/stoa/accounting"
	"github.com/flarexio/stoa/harness/loop"
	"github.com/flarexio/stoa/llm"
)

// Agent runs one bookkeeping decision: a natural-language request is turned
// into a typed JournalIntent, validated by the accounting domain, and only
// posted to the Ledger after validation succeeds.
type Agent struct {
	Engine   llm.ReasoningEngine[accounting.JournalIntent]
	Ledger   *accounting.Ledger
	MaxTurns int
}

// Result is the outcome of one bookkeeping cycle.
type Result struct {
	Intent      accounting.JournalIntent
	Entry       accounting.JournalEntry
	Observation llm.Observation
	Turns       int
	Events      []llm.CycleEvent
}

// Book runs the reason → validate → post loop for the given bookkeeping
// request. The request is forwarded to the LLM as the in-world task.
func (a Agent) Book(ctx context.Context, request string) (Result, error) {
	if a.Engine == nil {
		return Result{}, errors.New("bookkeeper: agent has no reasoning engine")
	}
	if a.Ledger == nil {
		return Result{}, errors.New("bookkeeper: agent has no ledger")
	}

	// Validator is run twice per successful turn: once by the harness loop
	// (so validation failures surface as EventValidationError and feed the
	// LLM correction cycle) and once inside Ledger.Post (the ledger's own
	// safety gate -- it must never trust its caller). Under the documented
	// concurrency model on Ledger, setup completes before any concurrent
	// Post, so the two runs see the same Accounts/Branches/Periods state
	// and the second run cannot reject what the first accepted.
	validator := accounting.Validator{Ledger: a.Ledger}

	var posted accounting.JournalEntry
	executor := loop.ExecutorFunc[accounting.JournalIntent](func(ctx context.Context, intent accounting.JournalIntent) (llm.Observation, error) {
		entry, err := a.Ledger.Post(ctx, intent)
		if err != nil {
			return llm.Observation{}, fmt.Errorf("bookkeeper: post: %w", err)
		}
		posted = entry
		return llm.Observation{
			Summary: fmt.Sprintf("Posted journal entry %s for %s with %d line(s).",
				entry.ID, entry.Description, len(entry.Lines)),
			Fields: map[string]string{
				"entry_id":  entry.ID,
				"period_id": entry.PeriodID,
				"currency":  entry.Currency,
			},
		}, nil
	})

	runner := loop.Runner[accounting.JournalIntent]{
		Engine:    a.Engine,
		Validator: validator,
		Executor:  executor,
		MaxTurns:  a.MaxTurns,
	}

	out, err := runner.Run(ctx, llm.ReasoningInput{
		Task:         request,
		Instructions: bookkeeperInstructions,
	})
	return Result{
		Intent:      out.Reasoning.Intent,
		Entry:       posted,
		Observation: out.Observation,
		Turns:       out.Turns,
		Events:      out.Events,
	}, err
}

const bookkeeperInstructions = `You are a bookkeeping agent. Propose a typed JournalIntent for the requested transaction:
- include at least two lines, one debit and one credit
- total debit must equal total credit
- use only account codes from the chart of accounts that are active
- reference an open accounting period
- use the same currency on the whole entry
If validation feedback is present in the message history, fix only the problems it names and resubmit.
Output JSON only. No prose outside the JSON object.`
