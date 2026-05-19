// Package nats provides NATS JetStream backed message-bus adapters: the
// production counterpart of messaging/inproc, with the same EventBus contract
// and optimistic-concurrency semantics (Nats-Expected-Last-Subject-Sequence),
// but with the broker -- not a process mutex -- holding the canonical stream.
//
// One file per domain (e.g. accounting.go) holds the domain encoding, port
// adaptation, and NewXxxBus factory; this file holds the connection, stream,
// consumer, and drain plumbing shared across domains.
package nats

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// defaultAckWait mirrors JetStream's own default and bounds per-message handler
// contexts, so a slow handler is canceled around when JetStream redelivers.
const defaultAckWait = 30 * time.Second

// Config carries the connection and JetStream settings the bus needs. URL,
// Stream, Subject, and Consumer are required; AckWait defaults to 30s when zero.
//
// Subject is the concrete subject the bus publishes to and the consumer filters
// on. StreamSubject is the subject pattern the stream is bound to -- a wildcard
// like "accounting.>" lets the stream capture future subjects without
// reconfiguration; it defaults to Subject when empty.
type Config struct {
	URL           string
	Stream        string
	Subject       string
	StreamSubject string
	Consumer      string
	AckWait       time.Duration
}

// bus owns the NATS connection, the JetStream context, the durable consumer,
// and the consume loop. It exposes raw publish/subscribe primitives to
// same-package domain factories, which add encoding and port adaptation.
type bus struct {
	nc       *nats.Conn
	js       jetstream.JetStream
	subject  string
	consumer jetstream.Consumer
	ackWait  time.Duration

	mu      sync.Mutex
	consume jetstream.ConsumeContext
}

// connect opens a NATS connection, attaches a JetStream context, and ensures
// the stream and durable consumer named in cfg exist before returning.
func connect(ctx context.Context, cfg Config) (*bus, error) {
	if cfg.URL == "" || cfg.Stream == "" || cfg.Subject == "" || cfg.Consumer == "" {
		return nil, errors.New("nats: url, stream, subject, and consumer are required")
	}
	ackWait := cfg.AckWait
	if ackWait <= 0 {
		ackWait = defaultAckWait
	}
	streamSubject := cfg.StreamSubject
	if streamSubject == "" {
		streamSubject = cfg.Subject
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
	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      cfg.Stream,
		Subjects:  []string{streamSubject},
		Retention: jetstream.LimitsPolicy,
		Storage:   jetstream.FileStorage,
	}); err != nil {
		nc.Close()
		return nil, fmt.Errorf("nats: ensure stream %q: %w", cfg.Stream, err)
	}
	cons, err := js.CreateOrUpdateConsumer(ctx, cfg.Stream, jetstream.ConsumerConfig{
		Durable:       cfg.Consumer,
		FilterSubject: cfg.Subject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverAllPolicy,
		AckWait:       ackWait,
	})
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("nats: ensure consumer %q: %w", cfg.Consumer, err)
	}
	return &bus{
		nc:       nc,
		js:       js,
		subject:  cfg.Subject,
		consumer: cons,
		ackWait:  ackWait,
	}, nil
}

// publishRaw publishes a wire-format body to the configured subject and returns
// the broker-assigned stream sequence. opts carries transport options such as
// WithExpectLastSequencePerSubject for optimistic concurrency.
func (b *bus) publishRaw(ctx context.Context, body []byte, opts ...jetstream.PublishOpt) (uint64, error) {
	ack, err := b.js.Publish(ctx, b.subject, body, opts...)
	if err != nil {
		return 0, err
	}
	return ack.Sequence, nil
}

// subscribeMessages starts the consume loop. handler receives raw JetStream
// messages and is responsible for Ack/Nak. Subscribing twice on the same bus
// returns an error; tear it down via close before re-subscribing.
func (b *bus) subscribeMessages(handler func(msg jetstream.Msg)) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.consume != nil {
		return errors.New("nats: bus already subscribed")
	}
	cc, err := b.consumer.Consume(handler)
	if err != nil {
		return fmt.Errorf("nats: consume: %w", err)
	}
	b.consume = cc
	return nil
}

// close drains the consume loop and then closes the NATS connection. The drain
// must complete first because Ack itself publishes over NATS -- tearing the
// connection down first would silently lose acks and leave messages stuck in
// num_pending. Safe to call when never subscribed and safe to call repeatedly.
func (b *bus) close() error {
	b.mu.Lock()
	cc := b.consume
	b.consume = nil
	b.mu.Unlock()
	if cc != nil {
		cc.Drain()
		<-cc.Closed()
	}
	if b.nc != nil {
		b.nc.Close()
		b.nc = nil
	}
	return nil
}

// isWrongLastSequence reports whether err is JetStream's "wrong last sequence"
// rejection (APIError code 10071), which domain factories translate into their
// own concurrency sentinel.
func isWrongLastSequence(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *jetstream.APIError
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode == jetstream.JSErrCodeStreamWrongLastSequence
	}
	return false
}
