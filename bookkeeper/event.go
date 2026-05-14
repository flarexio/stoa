package bookkeeper

import (
	"context"

	"github.com/flarexio/stoa/accounting"
)

// EventPublisher publishes JournalPosted events through a transport. The
// transport is the single point at which Subject and Sequence are
// assigned, so the returned event carries those fields populated along
// with Entry.ID. Callers should use the returned event, not the value
// they passed in, when they need the broker-assigned identifiers.
//
// EventPublisher is a use-case port: the bookkeeper agent depends on
// it, transport adapters (messaging/inproc, messaging/nats) implement
// it, and the composition root in cmd/stoa wires which adapter the
// agent receives at boot. It does not live in the accounting domain
// package because publishing is orchestration, not a business rule.
type EventPublisher interface {
	Publish(ctx context.Context, evt accounting.JournalPosted, expect accounting.ExpectedSequence) (accounting.JournalPosted, error)
}

// EventHandler consumes a JournalPosted from a transport. Implementations
// typically project the event into an accounting.LedgerRepository, but
// any subscriber (notification, downstream agent handoff, metrics)
// implements the same interface.
type EventHandler interface {
	Handle(ctx context.Context, evt accounting.JournalPosted) error
}

// EventHandlerFunc adapts an ordinary function to the EventHandler
// interface so wiring code can subscribe small handlers without
// declaring a named type.
type EventHandlerFunc func(ctx context.Context, evt accounting.JournalPosted) error

// Handle satisfies EventHandler.
func (f EventHandlerFunc) Handle(ctx context.Context, evt accounting.JournalPosted) error {
	return f(ctx, evt)
}
