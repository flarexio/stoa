package nats

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/flarexio/stoa/accounting"
	"github.com/flarexio/stoa/accounting/bookkeeping"
)

// accountingBus is the NATS JetStream backed bookkeeping.EventBus for the
// accounting domain. It encodes JournalPosted events to JSON, reuses the
// generic *bus for transport, and translates broker rejections into
// accounting.ErrConcurrentUpdate so the inproc and NATS transports surface the
// same sentinel.
type accountingBus struct {
	bus *bus
}

// NewAccountingBus opens a NATS JetStream connection and returns a
// bookkeeping.EventBus configured for accounting JournalPosted events. Close on
// the returned bus drains the consume loop and releases the connection.
func NewAccountingBus(ctx context.Context, cfg Config) (bookkeeping.EventBus, error) {
	b, err := connect(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &accountingBus{bus: b}, nil
}

// Publish marshals evt.Entry to JSON, publishes it with the
// optimistic-concurrency option when expect.Subject is non-empty, then stamps
// the broker-assigned Subject and Sequence on the returned event. A "wrong last
// sequence" rejection becomes accounting.ErrConcurrentUpdate.
func (a *accountingBus) Publish(ctx context.Context, evt accounting.JournalPosted, expect accounting.ExpectedSequence) (accounting.JournalPosted, error) {
	body, err := encodeAccountingEvent(evt)
	if err != nil {
		return accounting.JournalPosted{}, err
	}
	opts := []jetstream.PublishOpt{}
	if expect.Subject != "" {
		opts = append(opts, jetstream.WithExpectLastSequencePerSubject(expect.LastSeq))
	}
	seq, err := a.bus.publishRaw(ctx, body, opts...)
	if err != nil {
		if isWrongLastSequence(err) {
			return accounting.JournalPosted{}, accounting.ErrConcurrentUpdate
		}
		return accounting.JournalPosted{}, fmt.Errorf("nats: publish: %w", err)
	}
	return stampAccountingPubAck(evt, a.bus.subject, seq), nil
}

// Subscribe starts the consume loop. Each message gets its own context with the
// bus's AckWait as deadline; a successful handler call Acks the message, and a
// handler or decode error Naks it for redelivery. Subscribing twice returns an
// error; tear the bus down via Close before re-subscribing.
func (a *accountingBus) Subscribe(handler bookkeeping.EventHandler) error {
	return a.bus.subscribeMessages(func(msg jetstream.Msg) {
		ctx, cancel := context.WithTimeout(context.Background(), a.bus.ackWait)
		defer cancel()
		evt, err := decodeAccountingMsg(msg)
		if err != nil {
			_ = msg.Nak()
			return
		}
		if err := handler.Handle(ctx, evt); err != nil {
			_ = msg.Nak()
			return
		}
		_ = msg.Ack()
	})
}

// Close drains the consume loop and closes the underlying NATS connection.
func (a *accountingBus) Close() error {
	return a.bus.close()
}

// --- pure helpers ---

// encodeAccountingEvent serialises the on-wire body. Subject and Sequence are
// excluded from the JSON (json:"-") because the transport, not the body, is
// their source of truth.
func encodeAccountingEvent(evt accounting.JournalPosted) ([]byte, error) {
	body, err := json.Marshal(evt)
	if err != nil {
		return nil, fmt.Errorf("nats: marshal event: %w", err)
	}
	return body, nil
}

// decodeAccountingEvent reverses encodeAccountingEvent and stamps the
// broker-supplied subject and sequence onto the event.
func decodeAccountingEvent(body []byte, subject string, sequence uint64) (accounting.JournalPosted, error) {
	var evt accounting.JournalPosted
	if err := json.Unmarshal(body, &evt); err != nil {
		return accounting.JournalPosted{}, fmt.Errorf("nats: unmarshal event: %w", err)
	}
	return stampAccountingPubAck(evt, subject, sequence), nil
}

// stampAccountingPubAck applies the broker-assigned subject and sequence to an
// event, leaving the producer-assigned Entry.ID untouched.
func stampAccountingPubAck(evt accounting.JournalPosted, subject string, sequence uint64) accounting.JournalPosted {
	evt.Subject = subject
	evt.Sequence = sequence
	return evt
}

// decodeAccountingMsg pulls the subject and stream sequence out of a JetStream
// message and decodes the body into a JournalPosted.
func decodeAccountingMsg(msg jetstream.Msg) (accounting.JournalPosted, error) {
	meta, err := msg.Metadata()
	if err != nil {
		return accounting.JournalPosted{}, fmt.Errorf("nats: msg metadata: %w", err)
	}
	return decodeAccountingEvent(msg.Data(), msg.Subject(), meta.Sequence.Stream)
}
