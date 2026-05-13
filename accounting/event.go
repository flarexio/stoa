package accounting

import (
	"context"
	"errors"
	"fmt"
)

// JournalPosted is the domain event emitted once the bookkeeper agent has
// validated a JournalIntent and the broker has accepted it. It is the
// canonical record of a posted entry: LedgerRepository.Apply turns it
// into projection state, and any other subscriber (notifications,
// reporting, downstream agent handoff) can react to it independently
// without coordinating with the projection writer.
//
// Subject and Sequence are routing/ordering metadata supplied by the
// transport when the event is dispatched to a handler. They are excluded
// from JSON because broker metadata, not the body, is authoritative:
// the inproc bus assigns them under its mutex, and the NATS adapter
// (planned for PR2) recovers them from PubAck and the consumer's message
// metadata. Entry.ID is derived from Sequence via FormatEntryID before
// any handler receives the event, so handlers can rely on Entry.ID being
// populated even though it is empty on the wire.
type JournalPosted struct {
	Subject  string       `json:"-"`
	Sequence uint64       `json:"-"`
	Entry    JournalEntry `json:"entry"`
}

// FormatEntryID maps a broker sequence to the canonical JournalEntry.ID
// used by the bookkeeper agent and stored in every projection. Producers
// and consumers apply this function independently so both sides converge
// on the same identifier without coordinating.
func FormatEntryID(seq uint64) string {
	return fmt.Sprintf("JE-%04d", seq)
}

// ExpectedSequence carries the optimistic-concurrency hint a producer
// passes to EventPublisher.Publish. Subject is the scope of mutual
// exclusion (typically one ledger; PR2 may introduce per-period subjects
// if write contention emerges); LastSeq is the producer's view of the
// last sequence already accepted on that Subject.
//
// The broker rejects the publish with ErrConcurrentUpdate when its view
// of the last sequence on Subject does not match LastSeq. A producer
// receiving that error should refresh its repository read, re-validate,
// and retry.
//
// A zero ExpectedSequence (empty Subject) skips the check; use it only
// for seed-time loads or genuinely single-writer flows where no other
// producer can interleave.
type ExpectedSequence struct {
	Subject string
	LastSeq uint64
}

// ErrConcurrentUpdate signals that EventPublisher.Publish was rejected
// because the producer's ExpectedSequence is stale relative to the
// broker's view. Producers should re-read repository state and retry.
var ErrConcurrentUpdate = errors.New("accounting: concurrent update on subject")

// EventPublisher publishes JournalPosted events through a transport. The
// transport is the single point at which Subject and Sequence are
// assigned, so the returned event carries those fields populated along
// with Entry.ID. Callers should use the returned event, not the value
// they passed in, when they need the broker-assigned identifiers.
type EventPublisher interface {
	Publish(ctx context.Context, evt JournalPosted, expect ExpectedSequence) (JournalPosted, error)
}

// EventHandler consumes a JournalPosted from a transport. Implementations
// typically project the event into a LedgerRepository, but any subscriber
// (notification, downstream agent handoff, metrics) implements the same
// interface.
type EventHandler interface {
	Handle(ctx context.Context, evt JournalPosted) error
}

// EventHandlerFunc adapts an ordinary function to the EventHandler
// interface so wiring code can subscribe small handlers without
// declaring a named type.
type EventHandlerFunc func(ctx context.Context, evt JournalPosted) error

// Handle satisfies EventHandler.
func (f EventHandlerFunc) Handle(ctx context.Context, evt JournalPosted) error {
	return f(ctx, evt)
}
