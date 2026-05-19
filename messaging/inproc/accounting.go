package inproc

import (
	"context"
	"sync"

	"github.com/flarexio/stoa/accounting"
	"github.com/flarexio/stoa/accounting/bookkeeping"
)

// accountingBus is an in-process bookkeeping.EventBus. It dispatches every
// published event synchronously to all subscribed handlers under a single
// mutex, so Publish returns only after every handler has finished -- handy for
// tests that assert projection state immediately after Publish.
//
// Optimistic concurrency mirrors NATS JetStream's
// Nats-Expected-Last-Subject-Sequence: a producer whose ExpectedSequence.LastSeq
// does not match the bus's view is rejected with accounting.ErrConcurrentUpdate
// before any handler runs.
type accountingBus struct {
	mu        sync.Mutex
	streamSeq uint64
	lastSubj  map[string]uint64
	handlers  []bookkeeping.EventHandler
}

// NewAccountingBus returns an empty in-process bookkeeping.EventBus for
// JournalPosted events.
func NewAccountingBus() bookkeeping.EventBus {
	return &accountingBus{lastSubj: make(map[string]uint64)}
}

// Subscribe registers handler to receive every subsequent JournalPosted.
// Handlers run in registration order on the publishing goroutine.
func (b *accountingBus) Subscribe(handler bookkeeping.EventHandler) error {
	b.mu.Lock()
	b.handlers = append(b.handlers, handler)
	b.mu.Unlock()
	return nil
}

// Close is a no-op: the in-process bus owns nothing.
func (b *accountingBus) Close() error {
	return nil
}

// Publish assigns the next broker sequence under the bus's mutex (so the
// optimistic-concurrency check and the assignment are atomic), stamps Subject
// and Sequence onto the event, and dispatches it to every subscribed handler.
func (b *accountingBus) Publish(ctx context.Context, evt accounting.JournalPosted, expect accounting.ExpectedSequence) (accounting.JournalPosted, error) {
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
	handlers := append([]bookkeeping.EventHandler(nil), b.handlers...)
	b.mu.Unlock()

	evt.Subject = expect.Subject
	evt.Sequence = seq

	for _, h := range handlers {
		if err := h.Handle(ctx, evt); err != nil {
			return evt, err
		}
	}
	return evt, nil
}
