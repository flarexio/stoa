package bookkeeping

import (
	"context"
	"fmt"
	"slices"

	"github.com/flarexio/stoa/accounting"
)

// intentRoute is the Registry's entry for one IntentKind: how to
// validate and how to execute that variant. Each closure pulls the
// payload Kind selects out of the union and rejects a Intent whose
// payload is absent.
type intentRoute struct {
	validate func(ctx context.Context, intent Intent) error
	execute  func(ctx context.Context, intent Intent) (accounting.JournalEntry, error)
}

// Registry routes a Intent to the use case registered for its Kind. It is
// deliberately dumb dispatch -- a map from IntentKind to a validate /
// execute pair, with no behaviour of its own beyond the lookup.
//
// Registry.Validate and Registry.Execute have the shapes harness/loop's
// Validator and Executor expect over Intent, so the agent hands the whole
// Registry to the loop and the loop routes to many use cases while staying
// generic over a single intent type. Adding a use case is one more route
// in NewBookkeepingRegistry; the agent and the loop need no change.
type Registry struct {
	routes map[IntentKind]intentRoute
}

// NewBookkeepingRegistry builds the Registry for the bookkeeping agent,
// wiring every accounting use case against the shared ledger repository
// and event publisher. The kinds it routes must match Intents(), the
// vocabulary the prompt is built from; a registry test enforces that.
func NewBookkeepingRegistry(repo accounting.LedgerRepository, pub EventPublisher, clock Clock, subject string) Registry {
	post := PostJournal{Repo: repo, Publisher: pub, Clock: clock, Subject: subject}
	reverse := ReverseJournal{Repo: repo, Publisher: pub, Clock: clock, Subject: subject}

	return Registry{routes: map[IntentKind]intentRoute{
		IntentPostJournal: {
			validate: func(ctx context.Context, intent Intent) error {
				if intent.Post == nil {
					return missingPayloadErr(IntentPostJournal)
				}
				return post.Validate(ctx, *intent.Post)
			},
			execute: func(ctx context.Context, intent Intent) (accounting.JournalEntry, error) {
				if intent.Post == nil {
					return accounting.JournalEntry{}, missingPayloadErr(IntentPostJournal)
				}
				return post.Execute(ctx, *intent.Post)
			},
		},
		IntentReverseJournal: {
			validate: func(ctx context.Context, intent Intent) error {
				if intent.Reverse == nil {
					return missingPayloadErr(IntentReverseJournal)
				}
				return reverse.Validate(ctx, *intent.Reverse)
			},
			execute: func(ctx context.Context, intent Intent) (accounting.JournalEntry, error) {
				if intent.Reverse == nil {
					return accounting.JournalEntry{}, missingPayloadErr(IntentReverseJournal)
				}
				return reverse.Execute(ctx, *intent.Reverse)
			},
		},
	}}
}

// Validate routes intent to its use case's validation step. An unrecognised
// Kind is itself a validation failure, not a panic: the harness loop feeds
// it back to the model as correctable feedback and the model retries with
// a known kind.
func (r Registry) Validate(ctx context.Context, intent Intent) error {
	route, ok := r.routes[intent.Kind]
	if !ok {
		return r.unknownKindErr(intent.Kind)
	}
	return route.validate(ctx, intent)
}

// Execute routes an already-validated intent to its use case's execution step
// and returns the posted entry. Both bookkeeping use cases produce a
// JournalEntry -- a reversal is itself an entry -- so the result type is
// uniform across the union.
func (r Registry) Execute(ctx context.Context, intent Intent) (accounting.JournalEntry, error) {
	route, ok := r.routes[intent.Kind]
	if !ok {
		return accounting.JournalEntry{}, r.unknownKindErr(intent.Kind)
	}
	return route.execute(ctx, intent)
}

// Kinds returns the sorted set of IntentKinds the Registry routes. A test
// can assert they match Intents(), and the agent can report its vocabulary
// without reaching into the route table.
func (r Registry) Kinds() []IntentKind {
	out := make([]IntentKind, 0, len(r.routes))
	for kind := range r.routes {
		out = append(out, kind)
	}
	slices.Sort(out)
	return out
}

func (r Registry) unknownKindErr(kind IntentKind) error {
	return fmt.Errorf("bookkeeping: unknown intent kind %q; expected one of %v", kind, r.Kinds())
}

// missingPayloadErr reports a Intent whose Kind selects a payload the
// model did not fill -- e.g. kind "post_journal" with no post_journal
// object. The payload field carries the same name as the kind.
func missingPayloadErr(kind IntentKind) error {
	return fmt.Errorf("bookkeeping: intent kind %q is missing its %q payload object", kind, kind)
}
