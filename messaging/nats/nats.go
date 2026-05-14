// Package nats provides a generic NATS JetStream message bus. It handles
// connection lifecycle, stream/consumer setup, publish, consume, and drain
// without importing any domain packages. Domain-specific encoding and port
// adaptation live in per-domain factory files (e.g. accounting.go).
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

// defaultAckWait mirrors JetStream's own default. It bounds the
// per-message context the bus derives for each handler invocation, so a
// handler that runs longer than AckWait is canceled at roughly the same
// moment JetStream redelivers the message.
const defaultAckWait = 30 * time.Second

// Config carries the connection + JetStream settings the bus needs.
// URL, Stream, Subject, and Consumer are required. AckWait is optional;
// when zero the package default (30s) is used and propagated to the
// JetStream consumer config so both ends agree on the deadline.
type Config struct {
	URL      string
	Stream   string
	Subject  string
	Consumer string
	AckWait  time.Duration
}

// Bus is a generic NATS JetStream message bus. It owns the connection,
// the JetStream context, the durable consumer, and the consume loop;
// Close tears all of that down. Domain-specific encoding and port
// adaptation happen in per-domain factory files.
type Bus struct {
	nc       *nats.Conn
	js       jetstream.JetStream
	subject  string
	consumer jetstream.Consumer
	ackWait  time.Duration

	mu      sync.Mutex
	consume jetstream.ConsumeContext
}

// Connect opens a NATS connection, attaches a JetStream context, and
// ensures both the stream named cfg.Stream and the durable consumer
// named cfg.Consumer exist before returning.
func Connect(ctx context.Context, cfg Config) (*Bus, error) {
	if cfg.URL == "" || cfg.Stream == "" || cfg.Subject == "" || cfg.Consumer == "" {
		return nil, errors.New("nats: url, stream, subject, and consumer are required")
	}
	ackWait := cfg.AckWait
	if ackWait <= 0 {
		ackWait = defaultAckWait
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
		Subjects:  []string{cfg.Subject},
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
	return &Bus{
		nc:       nc,
		js:       js,
		subject:  cfg.Subject,
		consumer: cons,
		ackWait:  ackWait,
	}, nil
}

// Publish publishes raw data to the configured subject and returns the
// broker-assigned stream sequence. Use opts to pass JetStream publish
// options such as WithExpectLastSequencePerSubject for optimistic
// concurrency.
func (b *Bus) Publish(ctx context.Context, data []byte, opts ...jetstream.PublishOpt) (uint64, error) {
	ack, err := b.js.Publish(ctx, b.subject, data, opts...)
	if err != nil {
		return 0, fmt.Errorf("nats: publish: %w", err)
	}
	return ack.Sequence, nil
}

// SubscribeMessage starts the consume loop. Each message's handler is
// responsible for calling Ack or Nak on the message. Subscribing twice
// on the same bus returns an error; tear it down via Close before
// re-subscribing.
func (b *Bus) SubscribeMessage(handler func(msg jetstream.Msg)) error {
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

// AckWait returns the configured ack wait duration, used by factory
// wrappers to derive per-message handler contexts.
func (b *Bus) AckWait() time.Duration {
	return b.ackWait
}

// Close drains the consume loop (processing any already-delivered
// messages, including their Acks) and then closes the underlying NATS
// connection.
func (b *Bus) Close() error {
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

// IsWrongLastSequence reports whether err is JetStream's "wrong last
// sequence" rejection (APIError code 10071). Factory wrappers use it to
// translate the broker rejection into domain-specific sentinel errors.
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
