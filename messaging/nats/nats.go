// Package nats provides a NATS JetStream backed accounting.EventPublisher
// and a consumer driver that dispatches JournalPosted events to a
// registered accounting.EventHandler. It is the production counterpart of
// messaging/inproc: same interface, same optimistic-concurrency semantics
// (Nats-Expected-Last-Subject-Sequence), but the broker -- not a process
// mutex -- holds the canonical stream.
//
// Producers and consumers both compute Entry.ID from the JetStream stream
// sequence via accounting.FormatEntryID, so neither side has to coordinate
// to converge on the same identifier. The wire body carries only the
// JournalEntry (Subject and Sequence are recovered from broker metadata).
package nats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/flarexio/stoa/accounting"
)

// Config carries the connection + JetStream settings the package needs
// to wire a Publisher and (optionally) a Consumer. URL, Stream, and
// Subject are required; Consumer is only consulted when the caller asks
// for a Consumer.
type Config struct {
	URL      string
	Stream   string
	Subject  string
	Consumer string
}

// Conn bundles the underlying nats.Conn with its JetStream context and
// the config it was opened with. Close shuts the NATS connection down;
// the caller owns the lifecycle.
type Conn struct {
	nc  *nats.Conn
	js  jetstream.JetStream
	cfg Config
}

// Connect opens a NATS connection, attaches a JetStream context, and
// ensures a stream named cfg.Stream exists bound to cfg.Subject.
// The returned *Conn must be closed when the caller is done with it.
func Connect(ctx context.Context, cfg Config) (*Conn, error) {
	if cfg.URL == "" || cfg.Stream == "" || cfg.Subject == "" {
		return nil, errors.New("nats: url, stream, and subject are required")
	}
	nc, err := nats.Connect(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("nats: connect: %w", err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("nats: jetstream context: %w", err)
	}
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      cfg.Stream,
		Subjects:  []string{cfg.Subject},
		Retention: jetstream.LimitsPolicy,
		Storage:   jetstream.FileStorage,
	})
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("nats: ensure stream %q: %w", cfg.Stream, err)
	}
	return &Conn{nc: nc, js: js, cfg: cfg}, nil
}

// Close drains and closes the underlying NATS connection.
func (c *Conn) Close() {
	if c.nc != nil {
		c.nc.Close()
	}
}

// Publisher returns the accounting.EventPublisher bound to this Conn.
// All publishes go to cfg.Subject; ExpectedSequence.Subject is honoured
// only as a non-empty signal that an optimistic-concurrency check is
// required, the actual subject is the configured one (we run one
// subject per ledger today).
func (c *Conn) Publisher() *Publisher {
	return &Publisher{js: c.js, subject: c.cfg.Subject}
}

// Consumer creates (or updates) a durable JetStream consumer named
// cfg.Consumer reading cfg.Subject and returns a Consumer ready to
// Subscribe. The consumer uses explicit acks; a handler error nacks the
// message so JetStream redelivers it later.
func (c *Conn) Consumer(ctx context.Context) (*Consumer, error) {
	if c.cfg.Consumer == "" {
		return nil, errors.New("nats: consumer name is required")
	}
	cons, err := c.js.CreateOrUpdateConsumer(ctx, c.cfg.Stream, jetstream.ConsumerConfig{
		Durable:       c.cfg.Consumer,
		FilterSubject: c.cfg.Subject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverAllPolicy,
	})
	if err != nil {
		return nil, fmt.Errorf("nats: ensure consumer %q: %w", c.cfg.Consumer, err)
	}
	return &Consumer{c: cons}, nil
}

// Publisher implements accounting.EventPublisher over JetStream.
type Publisher struct {
	js      jetstream.JetStream
	subject string
}

// Publish marshals evt.Entry to JSON, publishes it to the configured
// subject with the optimistic-concurrency option when expect.Subject is
// non-empty, then stamps the broker-assigned Subject + Sequence + the
// derived Entry.ID into the returned event. A
// concurrent-update rejection from the broker (APIError 10071) becomes
// accounting.ErrConcurrentUpdate so callers can treat both transports
// the same way.
func (p *Publisher) Publish(ctx context.Context, evt accounting.JournalPosted, expect accounting.ExpectedSequence) (accounting.JournalPosted, error) {
	body, err := EncodeEvent(evt)
	if err != nil {
		return accounting.JournalPosted{}, err
	}
	opts := []jetstream.PublishOpt{}
	if expect.Subject != "" {
		opts = append(opts, jetstream.WithExpectLastSequencePerSubject(expect.LastSeq))
	}
	ack, err := p.js.Publish(ctx, p.subject, body, opts...)
	if err != nil {
		if IsWrongLastSequence(err) {
			return accounting.JournalPosted{}, accounting.ErrConcurrentUpdate
		}
		return accounting.JournalPosted{}, fmt.Errorf("nats: publish: %w", err)
	}
	return StampPubAck(evt, p.subject, ack.Sequence), nil
}

// Consumer wraps a JetStream pull-mode consumer and exposes Subscribe,
// which dispatches every received message to the given EventHandler
// under the consumer's own goroutine pool.
type Consumer struct {
	c       jetstream.Consumer
	context jetstream.ConsumeContext
}

// Subscribe starts the consume loop. The handler receives a fully
// populated JournalPosted (Subject + Sequence + Entry.ID). A successful
// handler call Acks the message; a handler error Naks it for redelivery
// per the consumer's ack policy.
func (c *Consumer) Subscribe(ctx context.Context, handler accounting.EventHandler) error {
	if c.context != nil {
		return errors.New("nats: consumer already subscribed")
	}
	cc, err := c.c.Consume(func(msg jetstream.Msg) {
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
	if err != nil {
		return fmt.Errorf("nats: consume: %w", err)
	}
	c.context = cc
	return nil
}

// Close stops the consume loop. It is safe to call when Subscribe was
// never invoked.
func (c *Consumer) Close() {
	if c.context != nil {
		c.context.Stop()
		c.context = nil
	}
}

// --- pure helpers (unit-testable) ---

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

// StampPubAck applies subject/sequence and the derived Entry.ID to an
// event. Producers call it after PublishMsg returns; consumers call it
// (via DecodeMsg) after pulling a message. Both sides converge on the
// same Entry.ID because both feed the same sequence into
// accounting.FormatEntryID.
func StampPubAck(evt accounting.JournalPosted, subject string, sequence uint64) accounting.JournalPosted {
	evt.Subject = subject
	evt.Sequence = sequence
	evt.Entry.ID = accounting.FormatEntryID(sequence)
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

// IsWrongLastSequence reports whether err is JetStream's "wrong last
// sequence" rejection (APIError code 10071). The publisher uses it to
// translate the broker rejection into accounting.ErrConcurrentUpdate so
// the inproc and NATS transports surface the same sentinel.
func IsWrongLastSequence(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *jetstream.APIError
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode == jetstream.JSErrCodeStreamWrongLastSequence
	}
	return false
}
