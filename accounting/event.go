package accounting

import (
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
// JournalPosted is intentionally the only entry-affecting event in this
// package; see the package overview for why the domain has no
// JournalEdited or JournalDeleted. New event types that introduce
// relationships between entries (reversals, links, annotations) must
// reference the target entry's ID in their payload rather than treating
// the broker sequence as the entry identifier -- the broker sequence
// belongs to the event, the Entry.ID belongs to the aggregate.
//
// Subject and Sequence are routing/ordering metadata supplied by the
// transport when the event is dispatched to a handler. They are excluded
// from JSON because broker metadata, not the body, is authoritative:
// the inproc bus assigns them under its mutex, and the NATS adapter
// recovers them from PubAck and the consumer's message metadata.
// Entry.ID, by contrast, is producer-assigned: the bookkeeper agent
// picks it as FormatEntryID(lastSeq+1) before Publish is called, the
// transport carries it through the wire unchanged, and the consumer
// reads the same identifier the producer wrote. Optimistic concurrency
// at the broker keeps the chosen ID race-safe -- a stale lastSeq is
// rejected as ErrConcurrentUpdate before any duplicate ID can land.
type JournalPosted struct {
	Subject  string       `json:"-"`
	Sequence uint64       `json:"-"`
	Entry    JournalEntry `json:"entry"`
}

// FormatEntryID formats a dense per-subject counter into the canonical
// JournalEntry.ID. The bookkeeper agent calls it with
// LastSequence(subject)+1 right before publishing, which equals the
// broker sequence the publish will receive on success (optimistic
// concurrency guarantees the prediction). The function itself stays
// purely formatting so it remains safe for any layer to reuse.
func FormatEntryID(seq uint64) string {
	return fmt.Sprintf("JE-%04d", seq)
}

// ExpectedSequence carries the optimistic-concurrency hint a producer
// passes to bookkeeper.EventPublisher.Publish. Subject is the scope of
// mutual exclusion (typically one ledger; later we may introduce
// per-period subjects if write contention emerges); LastSeq is the
// producer's view of the last sequence already accepted on that Subject.
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

// ErrConcurrentUpdate signals that a publish was rejected because the
// producer's ExpectedSequence is stale relative to the broker's view.
// Producers should re-read repository state and retry.
var ErrConcurrentUpdate = errors.New("accounting: concurrent update on subject")
