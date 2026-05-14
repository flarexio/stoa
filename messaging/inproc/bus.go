// Package inproc provides an in-process message bus. It is the test/dev
// counterpart of messaging/nats: same optimistic-concurrency semantics,
// no external infrastructure. Domain-specific encoding and port adaptation
// live in per-domain factory files (e.g. accounting.go).
//
// It is not suitable for multi-process production deployments because the
// broker state lives in process memory.
package inproc

import (
	"errors"
	"sync"
)

// Bus is an in-process message bus that assigns per-subject stream
// sequences with optimistic-concurrency checks. Domain-specific factories
// wrap it to dispatch typed events to their own handlers.
type Bus struct {
	mu        sync.Mutex
	streamSeq uint64
	lastSubj  map[string]uint64
}

// ErrConcurrentUpdate signals that a publish was rejected because the
// producer's expected last sequence does not match the broker's view.
var ErrConcurrentUpdate = errors.New("inproc: concurrent update on subject")

// New returns an empty in-process bus.
func New() *Bus {
	return &Bus{lastSubj: make(map[string]uint64)}
}

// AllocateSeq checks optimistic concurrency and allocates the next broker
// sequence under the bus's mutex so the check and assignment are atomic.
//
// If expectLastSeq is non-nil and subject is non-empty, the bus checks
// that the last sequence for subject matches *expectLastSeq before
// allocating. A mismatch returns ErrConcurrentUpdate.
func (b *Bus) AllocateSeq(subject string, expectLastSeq *uint64) (uint64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if expectLastSeq != nil && subject != "" {
		if b.lastSubj[subject] != *expectLastSeq {
			return 0, ErrConcurrentUpdate
		}
	}
	b.streamSeq++
	seq := b.streamSeq
	if subject != "" {
		b.lastSubj[subject] = seq
	}
	return seq, nil
}

// Close releases any resources the bus owns. The in-process bus owns
// nothing, so Close is a no-op.
func (b *Bus) Close() error {
	return nil
}
