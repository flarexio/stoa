package nats

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/flarexio/stoa/accounting"
	"github.com/flarexio/stoa/bookkeeper"
)

// accountingBus wraps a generic *Bus to implement bookkeeper.EventBus for
// the accounting domain. It handles encode/decode of accounting.JournalPosted
// events and translates transport errors into accounting sentinels.
type accountingBus struct {
	bus     *Bus
	subject string
}

// NewAccountingBus connects to NATS and returns a bookkeeper.EventBus
// configured for accounting JournalPosted events.
func NewAccountingBus(ctx context.Context, cfg Config) (bookkeeper.EventBus, error) {
	bus, err := Connect(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &accountingBus{bus: bus, subject: cfg.Subject}, nil
}

// Publish encodes evt.Entry to JSON, publishes it with the
// optimistic-concurrency option when expect.Subject is non-empty, and
// stamps broker metadata onto the returned event.
func (b *accountingBus) Publish(ctx context.Context, evt accounting.JournalPosted, expect accounting.ExpectedSequence) (accounting.JournalPosted, error) {
	body, err := EncodeEvent(evt)
	if err != nil {
		return accounting.JournalPosted{}, err
	}
	var opts []jetstream.PublishOpt
	if expect.Subject != "" {
		opts = append(opts, jetstream.WithExpectLastSequencePerSubject(expect.LastSeq))
	}
	seq, err := b.bus.Publish(ctx, body, opts...)
	if err != nil {
		if IsWrongLastSequence(err) {
			return accounting.JournalPosted{}, accounting.ErrConcurrentUpdate
		}
		return accounting.JournalPosted{}, err
	}
	return StampPubAck(evt, b.subject, seq), nil
}

// Subscribe starts the consume loop. Each message gets its own context
// derived from context.Background() with the bus's AckWait as deadline.
func (b *accountingBus) Subscribe(handler bookkeeper.EventHandler) error {
	ackWait := b.bus.AckWait()
	return b.bus.SubscribeMessage(func(msg jetstream.Msg) {
		ctx, cancel := context.WithTimeout(context.Background(), ackWait)
		defer cancel()
		evt, err := DecodeMsg(msg)
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

// Close drains and closes the underlying NATS bus.
func (b *accountingBus) Close() error {
	return b.bus.Close()
}

// --- accounting-specific helpers ---

// EncodeEvent serialises the on-wire body. The transport is the source
// of truth for Subject/Sequence so they are deliberately excluded from
// the JSON via their json:"-" tags on accounting.JournalPosted.
func EncodeEvent(evt accounting.JournalPosted) ([]byte, error) {
	body, err := json.Marshal(evt)
	if err != nil {
		return nil, fmt.Errorf("nats: marshal event: %w", err)
	}
	return body, nil
}

// DecodeEvent reverses EncodeEvent and stamps the broker-supplied
// subject + sequence (and the derived Entry.ID) onto the event.
func DecodeEvent(body []byte, subject string, sequence uint64) (accounting.JournalPosted, error) {
	var evt accounting.JournalPosted
	if err := json.Unmarshal(body, &evt); err != nil {
		return accounting.JournalPosted{}, fmt.Errorf("nats: unmarshal event: %w", err)
	}
	return StampPubAck(evt, subject, sequence), nil
}

// StampPubAck applies the broker-assigned subject and sequence to an
// event. Entry.ID is not touched: the producer (bookkeeper.Agent) picks
// it as FormatEntryID(lastSeq+1) before publishing and the transport
// carries it through the wire unchanged.
func StampPubAck(evt accounting.JournalPosted, subject string, sequence uint64) accounting.JournalPosted {
	evt.Subject = subject
	evt.Sequence = sequence
	return evt
}

// DecodeMsg pulls the subject + stream sequence out of a JetStream
// message and decodes the body into a JournalPosted.
func DecodeMsg(msg jetstream.Msg) (accounting.JournalPosted, error) {
	meta, err := msg.Metadata()
	if err != nil {
		return accounting.JournalPosted{}, fmt.Errorf("nats: msg metadata: %w", err)
	}
	return DecodeEvent(msg.Data(), msg.Subject(), meta.Sequence.Stream)
}
