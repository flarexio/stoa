// Package agent runs the bookkeeping agent. It wires the accounting use
// cases through the harness loop: the LLM proposes a typed intent, the
// use-case Registry validates and executes it, and the registry routes the
// intent to the matching bookkeeping operation. post_journal posts a new
// entry; reverse_journal reverses an existing one.
// Both reach the ledger by publishing a JournalPosted event through an
// EventPublisher, never by writing the repository directly, so the publish
// path stays the single authoritative place a posted entry comes into being.
package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/flarexio/stoa/accounting"
	"github.com/flarexio/stoa/accounting/bookkeeping"
	"github.com/flarexio/stoa/harness/loop"
	"github.com/flarexio/stoa/llm"
)

// Bookkeeper runs one bookkeeping decision: a natural-language request is
// turned into a typed intent, routed by the use-case Registry to the
// matching use case, validated, and executed. Producers never call
// repo.Apply; that is the consumer's job and runs inside the EventHandler
// subscribed to the publisher.
type Bookkeeper struct {
	Engine    llm.ReasoningEngine[bookkeeping.Intent]
	Repo      accounting.LedgerRepository
	Publisher bookkeeping.EventPublisher
	Subject   string
	Clock     bookkeeping.Clock
	MaxTurns  int
	Sink      loop.EventSink
}

type Result struct {
	Intent      bookkeeping.Intent
	Entry       accounting.JournalEntry
	Observation llm.Observation
	Turns       int
	Events      []llm.CycleEvent
}

// Book runs the reason -> validate -> execute loop for the given
// bookkeeping request, routing whichever intent the model proposes
// through the use-case Registry.
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

	registry := bookkeeping.NewBookkeepingRegistry(a.Repo, a.Publisher, a.Clock, a.Subject)

	// The registry owns validate + execute for every intent. The agent
	// only adapts the posted entry into the llm.Observation the harness
	// loop feeds back to the model; a non-LLM caller drives a use case's
	// Handle directly instead.
	var posted accounting.JournalEntry
	executor := loop.ExecutorFunc[bookkeeping.Intent](func(ctx context.Context, intent bookkeeping.Intent) (llm.Observation, error) {
		entry, err := registry.Execute(ctx, intent)
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

	runner := loop.Runner[bookkeeping.Intent]{
		Engine:    a.Engine,
		Validator: registry,
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

const bookkeeperInstructions = `You are a bookkeeping agent. Choose ONE intent for the requested task and return it as a typed intent:
- post_journal: post a new journal entry. Include at least two lines with one or more debits and one or more credits; total debit must equal total credit; use only active account codes; reference an open period; use one currency throughout.
- reverse_journal: reverse an existing posted entry. Give the entry's JE-id and a short reason; the mirror-image entry is built for you.
If validation feedback is present in the message history, fix only the problems it names and resubmit.
Output JSON only. No prose outside the JSON object.`
