// Package inproc provides an in-process bookkeeper.EventBus. It is the
// test/dev counterpart of messaging/nats: same EventBus interface, same
// optimistic-concurrency semantics, no external infrastructure. It is
// not suitable for multi-process production deployments because the
// broker state lives in process memory.
package inproc

import (
	"context"
	"sync"

	"github.com/flarexio/stoa/accounting"
	"github.com/flarexio/stoa/bookkeeper"
)

// bus is an in-process bookkeeper.EventBus. It dispatches every
// published event synchronously to all subscribed handlers under a
// single mutex, so Publish returns only after every handler has
// finished. That makes the bus suitable for tests that assert
// projection state immediately after Publish without polling, and for
// single-process wiring that does not need cross-process fanout.
//
// Optimistic concurrency follows the same model as NATS JetStream's
// Nats-Expected-Last-Subject-Sequence header: a producer that sends an
// ExpectedSequence whose LastSeq does not match the broker's view is
// rejected with accounting.ErrConcurrentUpdate before any handler runs.
type bus struct {
	mu        sync.Mutex
	streamSeq uint64
	lastSubj  map[string]uint64
	handlers  []bookkeeper.EventHandler
}

// New returns an empty in-process bus exposed as a bookkeeper.EventBus.
// The concrete type stays unexported so callers depend only on the
// interface.
func New() bookkeeper.EventBus {
	return &bus{lastSubj: make(map[string]uint64)}
}

// Subscribe registers handler to receive every subsequent JournalPosted
// published through the bus. Handlers run in registration order under
// the calling goroutine; the bus has no fan-out concurrency. The error
// return exists to match bookkeeper.EventSubscriber; this transport
// never errors on registration.
func (b *bus) Subscribe(handler bookkeeper.EventHandler) error {
	b.mu.Lock()
	b.handlers = append(b.handlers, handler)
	b.mu.Unlock()
	return nil
}

// Close releases any resources the bus owns. The in-process bus owns
// nothing, so Close is a no-op that exists only to satisfy
// bookkeeper.EventBus.
func (b *bus) Close() error {
	return nil
}

// Publish assigns the next broker sequence under the bus's mutex (so the
// optimistic-concurrency check and the sequence assignment are atomic),
// stamps Subject, Sequence, and the derived Entry.ID into the event, and
// dispatches it to every subscribed handler. The returned event is the
// one handlers saw; callers should use it (its Entry has ID populated)
// rather than the value they passed in.
func (b *bus) Publish(ctx context.Context, evt accounting.JournalPosted, expect accounting.ExpectedSequence) (accounting.JournalPosted, error) {
	b.mu.Lock()
	if expect.Subject != "" {
		if b.lastSubj[expect.Subject] != expect.LastSeq {
			b.mu.Unlock()
			return accounting.JournalPosted{}, accounting.ErrConcurrentUpdate
		}
	}
	b.streamSeq++
	seq := b.streamSeq
	if expect.Subject != "" {
		b.lastSubj[expect.Subject] = seq
	}
	handlers := append([]bookkeeper.EventHandler(nil), b.handlers...)
	b.mu.Unlock()

	evt.Subject = expect.Subject
	evt.Sequence = seq
	evt.Entry.ID = accounting.FormatEntryID(seq)

	for _, h := range handlers {
		if err := h.Handle(ctx, evt); err != nil {
			return evt, err
		}
	}
	return evt, nil
}
