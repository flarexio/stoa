package inproc

import (
	"context"
	"sync"

	"github.com/flarexio/stoa/accounting"
	"github.com/flarexio/stoa/bookkeeper"
)

// accountingBus wraps a generic *Bus to implement bookkeeper.EventBus for
// the accounting domain.
type accountingBus struct {
	bus      *Bus
	mu       sync.Mutex
	handlers []bookkeeper.EventHandler
}

// NewAccountingBus returns an in-process bookkeeper.EventBus.
func NewAccountingBus() bookkeeper.EventBus {
	return &accountingBus{bus: New()}
}

// Publish allocates a broker sequence (with optimistic-concurrency check),
// stamps Subject + Sequence onto the event, and dispatches it synchronously
// to every subscribed handler. Entry.ID is set by the producer before
// Publish is called and the transport carries it through unchanged.
func (b *accountingBus) Publish(ctx context.Context, evt accounting.JournalPosted, expect accounting.ExpectedSequence) (accounting.JournalPosted, error) {
	var expectPtr *uint64
	if expect.Subject != "" {
		expectPtr = &expect.LastSeq
	}
	seq, err := b.bus.AllocateSeq(expect.Subject, expectPtr)
	if err != nil {
		if err == ErrConcurrentUpdate {
			return accounting.JournalPosted{}, accounting.ErrConcurrentUpdate
		}
		return accounting.JournalPosted{}, err
	}

	evt.Subject = expect.Subject
	evt.Sequence = seq

	b.mu.Lock()
	handlers := append([]bookkeeper.EventHandler(nil), b.handlers...)
	b.mu.Unlock()

	for _, h := range handlers {
		if err := h.Handle(ctx, evt); err != nil {
			return evt, err
		}
	}
	return evt, nil
}

// Subscribe registers handler to receive every subsequent JournalPosted
// published through the bus. Handlers run in registration order under
// the calling goroutine.
func (b *accountingBus) Subscribe(handler bookkeeper.EventHandler) error {
	b.mu.Lock()
	b.handlers = append(b.handlers, handler)
	b.mu.Unlock()
	return nil
}

// Close is a no-op for the in-process bus.
func (b *accountingBus) Close() error {
	return b.bus.Close()
}
