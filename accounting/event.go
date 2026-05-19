package accounting

import (
	"errors"
	"fmt"
)

// JournalPosted is the domain event emitted once a JournalIntent has been
// validated and the broker has accepted it -- the canonical record of a posted
// entry. LedgerRepository.Apply projects it into state; other subscribers
// (notifications, reporting, downstream agents) can react independently.
//
// Subject and Sequence are transport-assigned routing/ordering metadata and so
// are excluded from JSON. Entry.ID, by contrast, is producer-assigned before
// Publish and carried through the wire unchanged.
type JournalPosted struct {
	Subject  string       `json:"-"`
	Sequence uint64       `json:"-"`
	Entry    JournalEntry `json:"entry"`
}

// FormatEntryID formats a dense per-subject counter into the canonical
// JournalEntry.ID. PostJournal calls it with LastSequence(subject)+1, which
// optimistic concurrency guarantees equals the broker sequence on success.
func FormatEntryID(seq uint64) string {
	return fmt.Sprintf("JE-%04d", seq)
}

// ExpectedSequence is the optimistic-concurrency hint passed to
// EventPublisher.Publish: Subject is the scope of mutual exclusion and LastSeq
// is the producer's view of the last sequence accepted on it. The broker
// rejects the publish with ErrConcurrentUpdate on a mismatch. A zero value
// (empty Subject) skips the check -- use it only for single-writer seed loads.
type ExpectedSequence struct {
	Subject string
	LastSeq uint64
}

// ErrConcurrentUpdate signals that a publish was rejected because the
// producer's ExpectedSequence is stale. Producers should re-read and retry.
var ErrConcurrentUpdate = errors.New("accounting: concurrent update on subject")
