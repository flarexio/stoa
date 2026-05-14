// Package bookkeeper is the bookkeeping use-case package. It wires the
// accounting domain through the harness loop: the LLM proposes an
// accounting.JournalIntent, the accounting Validator enforces ledger
// invariants against a LedgerRepository, and the bookkeeper publishes a
// JournalPosted event through an EventPublisher. A subscribed
// EventHandler applies the event to the projection. The bookkeeper never
// writes to the repository itself, so the publish path is the single
// authoritative place a posted entry comes into being.
package bookkeeper

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/flarexio/stoa/accounting"
	"github.com/flarexio/stoa/harness/loop"
	"github.com/flarexio/stoa/llm"
)

// SubjectLedger is the default subject the bookkeeping agent publishes
// JournalPosted events on for optimistic-concurrency scoping. Override
// it via Agent.Subject when multiple ledgers share a transport.
const SubjectLedger = "accounting.journal"

// Clock returns the time a posted journal entry is stamped with. Default
// is time.Now().UTC(); tests inject a deterministic clock through
// Agent.Clock.
type Clock func() time.Time

// Agent runs one bookkeeping decision: a natural-language request is
// turned into a typed JournalIntent, validated against the
// LedgerRepository, and published as a JournalPosted event. Producers
// never call repo.Apply; that is the consumer's job and runs inside the
// EventHandler subscribed to the publisher.
type Agent struct {
	Engine    llm.ReasoningEngine[accounting.JournalIntent]
	Repo      accounting.LedgerRepository
	Publisher EventPublisher
	Subject   string
	Clock     Clock
	MaxTurns  int
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
func (a Agent) Book(ctx context.Context, request string) (Result, error) {
	if a.Engine == nil {
		return Result{}, errors.New("bookkeeper: agent has no reasoning engine")
	}
	if a.Repo == nil {
		return Result{}, errors.New("bookkeeper: agent has no repository")
	}
	if a.Publisher == nil {
		return Result{}, errors.New("bookkeeper: agent has no event publisher")
	}

	subject := a.Subject
	if subject == "" {
		subject = SubjectLedger
	}
	clock := a.Clock
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}

	validator := accounting.Validator{Repo: a.Repo}

	var posted accounting.JournalEntry
	executor := loop.ExecutorFunc[accounting.JournalIntent](func(ctx context.Context, intent accounting.JournalIntent) (llm.Observation, error) {
		// lastSeq is read here both as the broker's optimistic-concurrency
		// expectation and as the dense counter for the entry's identity:
		// because Apply writes the entry row and bumps subject_offsets in
		// the same transaction, MAX(sequence) and last_sequence are
		// guaranteed equal, and lastSeq+1 is the sequence the broker
		// will assign on a successful publish. The agent therefore picks
		// the entry's ID right here, before publishing, and the
		// transport carries the ID through the wire unchanged. If
		// another producer wins the race the broker rejects this
		// publish with accounting.ErrConcurrentUpdate, the loop retries
		// with a freshly read lastSeq, and a new ID is assigned -- no
		// duplicate entry can take this ID because the publish failed.
		lastSeq, err := a.Repo.LastSequence(ctx, subject)
		if err != nil {
			return llm.Observation{}, fmt.Errorf("bookkeeper: read last sequence: %w", err)
		}

		entry := accounting.JournalEntry{
			ID:          accounting.FormatEntryID(lastSeq + 1),
			Date:        intent.Date,
			PeriodID:    intent.PeriodID,
			Currency:    intent.Currency,
			Description: intent.Description,
			Lines:       intent.Lines,
			PostedAt:    clock(),
		}

		dispatched, err := a.Publisher.Publish(ctx, accounting.JournalPosted{Entry: entry}, accounting.ExpectedSequence{
			Subject: subject,
			LastSeq: lastSeq,
		})
		if err != nil {
			return llm.Observation{}, fmt.Errorf("bookkeeper: publish: %w", err)
		}

		posted = dispatched.Entry
		return llm.Observation{
			Summary: fmt.Sprintf("Posted journal entry %s for %s with %d line(s).",
				posted.ID, posted.Description, len(posted.Lines)),
			Fields: map[string]string{
				"entry_id":  posted.ID,
				"period_id": posted.PeriodID,
				"currency":  posted.Currency,
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
