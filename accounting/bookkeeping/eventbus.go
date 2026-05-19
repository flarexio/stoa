package bookkeeping

import (
	"context"
	"io"

	"github.com/flarexio/stoa/accounting"
)

// EventPublisher publishes a JournalPosted through a transport, which assigns
// Subject and Sequence. Callers use the returned event, not the value they
// passed in, when they need the broker-assigned identifiers.
//
// It is a use-case port, not a domain one: publishing is orchestration, so
// transport adapters implement it and cmd/stoa wires the adapter at boot.
type EventPublisher interface {
	Publish(ctx context.Context, evt accounting.JournalPosted, expect accounting.ExpectedSequence) (accounting.JournalPosted, error)
}

// EventHandler consumes a JournalPosted, typically projecting it into an
// accounting.LedgerRepository.
type EventHandler interface {
	Handle(ctx context.Context, evt accounting.JournalPosted) error
}

// EventHandlerFunc adapts an ordinary function to EventHandler.
type EventHandlerFunc func(ctx context.Context, evt accounting.JournalPosted) error

func (f EventHandlerFunc) Handle(ctx context.Context, evt accounting.JournalPosted) error {
	return f(ctx, evt)
}

// EventSubscriber registers a handler with a transport, which owns
// per-message context, ack/nak, and concurrency.
type EventSubscriber interface {
	Subscribe(handler EventHandler) error
}

// EventBus is the bidirectional transport contract for the bookkeeping flow:
// publish events out, subscribe handlers, and close the transport.
type EventBus interface {
	EventPublisher
	EventSubscriber
	io.Closer
}
