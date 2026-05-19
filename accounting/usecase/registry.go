package usecase

import (
	"context"
	"fmt"
	"slices"

	"github.com/flarexio/stoa/accounting"
)

// commandRoute is the Registry's entry for one CommandKind: how to
// validate and how to execute that variant. Each closure pulls the
// payload Kind selects out of the union and rejects a Command whose
// payload is absent.
type commandRoute struct {
	validate func(ctx context.Context, cmd Command) error
	execute  func(ctx context.Context, cmd Command) (accounting.JournalEntry, error)
}

// Registry routes a Command to the use case registered for its Kind. It is
// deliberately dumb dispatch -- a map from CommandKind to a validate /
// execute pair, with no behaviour of its own beyond the lookup.
//
// Registry.Validate and Registry.Execute have the shapes harness/loop's
// Validator and Executor expect over Command, so the agent hands the whole
// Registry to the loop and the loop routes to many use cases while staying
// generic over a single intent type. Adding a use case is one more route
// in NewBookkeepingRegistry; the agent and the loop need no change.
type Registry struct {
	routes map[CommandKind]commandRoute
}

// NewBookkeepingRegistry builds the Registry for the bookkeeping agent,
// wiring every accounting use case against the shared ledger repository
// and event publisher. The kinds it routes must match Commands(), the
// vocabulary the prompt is built from; a registry test enforces that.
func NewBookkeepingRegistry(repo accounting.LedgerRepository, pub EventPublisher, clock Clock, subject string) Registry {
	post := PostJournal{Repo: repo, Publisher: pub, Clock: clock, Subject: subject}
	reverse := ReverseJournal{Repo: repo, Publisher: pub, Clock: clock, Subject: subject}

	return Registry{routes: map[CommandKind]commandRoute{
		CommandPostJournal: {
			validate: func(ctx context.Context, cmd Command) error {
				if cmd.Post == nil {
					return missingPayloadErr(CommandPostJournal)
				}
				return post.Validate(ctx, *cmd.Post)
			},
			execute: func(ctx context.Context, cmd Command) (accounting.JournalEntry, error) {
				if cmd.Post == nil {
					return accounting.JournalEntry{}, missingPayloadErr(CommandPostJournal)
				}
				return post.Execute(ctx, *cmd.Post)
			},
		},
		CommandReverseJournal: {
			validate: func(ctx context.Context, cmd Command) error {
				if cmd.Reverse == nil {
					return missingPayloadErr(CommandReverseJournal)
				}
				return reverse.Validate(ctx, *cmd.Reverse)
			},
			execute: func(ctx context.Context, cmd Command) (accounting.JournalEntry, error) {
				if cmd.Reverse == nil {
					return accounting.JournalEntry{}, missingPayloadErr(CommandReverseJournal)
				}
				return reverse.Execute(ctx, *cmd.Reverse)
			},
		},
	}}
}

// Validate routes cmd to its use case's validation step. An unrecognised
// Kind is itself a validation failure, not a panic: the harness loop feeds
// it back to the model as correctable feedback and the model retries with
// a known kind.
func (r Registry) Validate(ctx context.Context, cmd Command) error {
	route, ok := r.routes[cmd.Kind]
	if !ok {
		return r.unknownKindErr(cmd.Kind)
	}
	return route.validate(ctx, cmd)
}

// Execute routes an already-validated cmd to its use case's execution step
// and returns the posted entry. Both bookkeeping use cases produce a
// JournalEntry -- a reversal is itself an entry -- so the result type is
// uniform across the union.
func (r Registry) Execute(ctx context.Context, cmd Command) (accounting.JournalEntry, error) {
	route, ok := r.routes[cmd.Kind]
	if !ok {
		return accounting.JournalEntry{}, r.unknownKindErr(cmd.Kind)
	}
	return route.execute(ctx, cmd)
}

// Kinds returns the CommandKinds the Registry routes, sorted, so a test
// can assert they match Commands() and the agent can report its
// vocabulary without reaching into the route table.
func (r Registry) Kinds() []CommandKind {
	out := make([]CommandKind, 0, len(r.routes))
	for kind := range r.routes {
		out = append(out, kind)
	}
	slices.Sort(out)
	return out
}

func (r Registry) unknownKindErr(kind CommandKind) error {
	return fmt.Errorf("usecase: unknown command kind %q; expected one of %v", kind, r.Kinds())
}

// missingPayloadErr reports a Command whose Kind selects a payload the
// model did not fill -- e.g. kind "post_journal" with no post_journal
// object. The payload field carries the same name as the kind.
func missingPayloadErr(kind CommandKind) error {
	return fmt.Errorf("usecase: command kind %q is missing its %q payload object", kind, kind)
}
