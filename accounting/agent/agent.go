// Package agent runs the bookkeeping agent. It wires the
// accounting domain through the harness loop: the LLM proposes an
// accounting.JournalIntent, the accounting Validator enforces ledger
// invariants against a LedgerRepository, and the bookkeeper publishes a
// JournalPosted event through an EventPublisher. A subscribed
// EventHandler applies the event to the projection. The bookkeeper never
// writes to the repository itself, so the publish path is the single
// authoritative place a posted entry comes into being.
package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/flarexio/stoa/accounting"
	"github.com/flarexio/stoa/accounting/usecase"
	"github.com/flarexio/stoa/harness/loop"
	"github.com/flarexio/stoa/llm"
)

// Bookkeeper runs one bookkeeping decision: a natural-language request is
// turned into a typed JournalIntent, validated against the
// LedgerRepository, and published as a JournalPosted event. Producers
// never call repo.Apply; that is the consumer's job and runs inside the
// EventHandler subscribed to the publisher.
type Bookkeeper struct {
	Engine    llm.ReasoningEngine[accounting.JournalIntent]
	Repo      accounting.LedgerRepository
	Publisher usecase.EventPublisher
	Subject   string
	Clock     usecase.Clock
	MaxTurns  int
	Sink      loop.EventSink
}

// Result is the outcome of one bookkeeping cycle.
type Result struct {
	Intent      accounting.JournalIntent
	Entry       accounting.JournalEntry
	Observation llm.Observation
	Turns       int
	Events      []llm.CycleEvent
}

// Book runs the reason -> validate -> publish loop for the given
// bookkeeping request.
func (a Bookkeeper) Book(ctx context.Context, request string) (Result, error) {
	if a.Engine == nil {
		return Result{}, errors.New("bookkeeper: agent has no reasoning engine")
	}
	if a.Repo == nil {
		return Result{}, errors.New("bookkeeper: agent has no repository")
	}
	if a.Publisher == nil {
		return Result{}, errors.New("bookkeeper: agent has no event publisher")
	}

	uc := usecase.PostJournal{
		Repo:      a.Repo,
		Publisher: a.Publisher,
		Clock:     a.Clock,
		Subject:   a.Subject,
	}

	// The use case owns validate + execute. The agent only adapts the
	// posted entry into the llm.Observation the harness loop feeds back
	// to the model; a non-LLM caller would call uc.Handle instead.
	var posted accounting.JournalEntry
	executor := loop.ExecutorFunc[accounting.JournalIntent](func(ctx context.Context, intent accounting.JournalIntent) (llm.Observation, error) {
		entry, err := uc.Execute(ctx, intent)
		if err != nil {
			return llm.Observation{}, err
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
		Validator: uc,
		Executor:  executor,
		Tools:     accountTools(a.Repo),
		MaxTurns:  a.MaxTurns,
		Sink:      a.Sink,
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
