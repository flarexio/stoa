package nats

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/flarexio/stoa/accounting"
	"github.com/flarexio/stoa/bookkeeper"
)

// accountingBus is the NATS JetStream backed bookkeeper.EventBus for
// the accounting domain. It encodes JournalPosted events to JSON,
// reuses the generic *bus for transport, and translates broker
// rejections into accounting.ErrConcurrentUpdate so the inproc and
// NATS transports surface the same sentinel.
//
// Entry.ID is producer-assigned (see bookkeeper.Agent) before Publish
// is called: the agent reads the current last_sequence from the
// repository, adds one, formats it with accounting.FormatEntryID, and
// stamps the result on the event. The transport leaves Entry.ID alone
// and only stamps Subject + Sequence as broker metadata; consumers
// therefore read the same identifier the producer wrote, with no
// derivation step on either side.
type accountingBus struct {
	bus *bus
}

// NewAccountingBus opens a NATS JetStream connection and returns a
// bookkeeper.EventBus configured for accounting JournalPosted events.
// Close on the returned bus drains the consume loop and releases the
// connection.
func NewAccountingBus(ctx context.Context, cfg Config) (bookkeeper.EventBus, error) {
	b, err := connect(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &accountingBus{bus: b}, nil
}

// Publish marshals evt.Entry to JSON, publishes it to the configured
// subject with the optimistic-concurrency option when expect.Subject
// is non-empty, then stamps the broker-assigned Subject + Sequence on
// the returned event. A "wrong last sequence" rejection from the
// broker (APIError 10071) becomes accounting.ErrConcurrentUpdate.
func (a *accountingBus) Publish(ctx context.Context, evt accounting.JournalPosted, expect accounting.ExpectedSequence) (accounting.JournalPosted, error) {
	body, err := encodeEvent(evt)
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
	return stampPubAck(evt, a.bus.subject, seq), nil
}

// Subscribe starts the consume loop. Each message gets its own context
// derived from context.Background() with the bus's AckWait as
// deadline, so a slow handler is canceled at roughly the same moment
// JetStream redelivers the message. A successful handler call Acks
// the message; a handler error (or a decode error) Naks it for
// redelivery. Subscribing twice on the same bus returns an error;
// tear it down via Close before re-subscribing.
func (a *accountingBus) Subscribe(handler bookkeeper.EventHandler) error {
	return a.bus.subscribeMessages(func(msg jetstream.Msg) {
		ctx, cancel := context.WithTimeout(context.Background(), a.bus.ackWait)
		defer cancel()
		evt, err := decodeMsg(msg)
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

// Close drains the consume loop and closes the underlying NATS
// connection.
func (a *accountingBus) Close() error {
	return a.bus.close()
}

// --- pure helpers (unit-testable; unexported because only the test
// file in this package needs them) ---

// encodeEvent serialises the on-wire body. The transport is the
// source of truth for Subject/Sequence so they are deliberately
// excluded from the JSON via their json:"-" tags on
// accounting.JournalPosted.
func encodeEvent(evt accounting.JournalPosted) ([]byte, error) {
	body, err := json.Marshal(evt)
	if err != nil {
		return nil, fmt.Errorf("nats: marshal event: %w", err)
	}
	return body, nil
}

// decodeEvent reverses encodeEvent and stamps the broker-supplied
// subject + sequence onto the event.
func decodeEvent(body []byte, subject string, sequence uint64) (accounting.JournalPosted, error) {
	var evt accounting.JournalPosted
	if err := json.Unmarshal(body, &evt); err != nil {
		return accounting.JournalPosted{}, fmt.Errorf("nats: unmarshal event: %w", err)
	}
	return stampPubAck(evt, subject, sequence), nil
}

// stampPubAck applies the broker-assigned subject and sequence to an
// event. Entry.ID is not touched: the producer (bookkeeper.Agent)
// picks it before publishing and the transport carries it through the
// wire unchanged.
func stampPubAck(evt accounting.JournalPosted, subject string, sequence uint64) accounting.JournalPosted {
	evt.Subject = subject
	evt.Sequence = sequence
	return evt
}

// decodeMsg pulls the subject + stream sequence out of a JetStream
// message and decodes the body into a JournalPosted.
func decodeMsg(msg jetstream.Msg) (accounting.JournalPosted, error) {
	meta, err := msg.Metadata()
	if err != nil {
		return accounting.JournalPosted{}, fmt.Errorf("nats: msg metadata: %w", err)
	}
	return decodeEvent(msg.Data(), msg.Subject(), meta.Sequence.Stream)
}
