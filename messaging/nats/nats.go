// Package nats provides a NATS JetStream backed bookkeeper.EventBus.
// It is the production counterpart of messaging/inproc: same EventBus
// interface, same optimistic-concurrency semantics
// (Nats-Expected-Last-Subject-Sequence), but the broker -- not a process
// mutex -- holds the canonical stream.
//
// Entry.ID is producer-assigned (see bookkeeper.Agent) before Publish is
// called: the agent reads the current last_sequence from the repository,
// adds one, formats it with accounting.FormatEntryID, and stamps the
// result on the event. The transport leaves Entry.ID alone and only
// stamps Subject + Sequence as broker metadata; consumers therefore read
// the same identifier the producer wrote, with no derivation step on
// either side. Optimistic concurrency at the broker keeps the chosen ID
// safe from races -- a stale lastSeq guess is rejected as
// ErrConcurrentUpdate before any duplicate ID can land.
package nats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/flarexio/stoa/accounting"
	"github.com/flarexio/stoa/bookkeeper"
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

// bus implements bookkeeper.EventBus over NATS JetStream. It owns the
// connection, the JetStream context, the durable consumer, and the
// consume loop; Close tears all of that down.
type bus struct {
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
// named cfg.Consumer exist before returning. The returned EventBus
// publishes to and subscribes from cfg.Subject; Close releases the
// underlying NATS resources.
func Connect(ctx context.Context, cfg Config) (bookkeeper.EventBus, error) {
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
	return &bus{
		nc:       nc,
		js:       js,
		subject:  cfg.Subject,
		consumer: cons,
		ackWait:  ackWait,
	}, nil
}

// Publish marshals evt.Entry to JSON, publishes it to the configured
// subject with the optimistic-concurrency option when expect.Subject is
// non-empty, then stamps the broker-assigned Subject + Sequence + the
// derived Entry.ID into the returned event. A concurrent-update
// rejection from the broker (APIError 10071) becomes
// accounting.ErrConcurrentUpdate so callers can treat both transports
// the same way.
func (b *bus) Publish(ctx context.Context, evt accounting.JournalPosted, expect accounting.ExpectedSequence) (accounting.JournalPosted, error) {
	body, err := EncodeEvent(evt)
	if err != nil {
		return accounting.JournalPosted{}, err
	}
	opts := []jetstream.PublishOpt{}
	if expect.Subject != "" {
		opts = append(opts, jetstream.WithExpectLastSequencePerSubject(expect.LastSeq))
	}
	ack, err := b.js.Publish(ctx, b.subject, body, opts...)
	if err != nil {
		if IsWrongLastSequence(err) {
			return accounting.JournalPosted{}, accounting.ErrConcurrentUpdate
		}
		return accounting.JournalPosted{}, fmt.Errorf("nats: publish: %w", err)
	}
	return StampPubAck(evt, b.subject, ack.Sequence), nil
}

// Subscribe starts the consume loop. Each message gets its own context
// derived from context.Background() with the bus's AckWait as deadline,
// so a slow handler is canceled at roughly the same moment JetStream
// redelivers the message. A successful handler call Acks the message;
// a handler error (or a decode error) Naks it for redelivery per the
// consumer's ack policy. Subscribing twice on the same bus returns an
// error; tear it down via Close before re-subscribing.
func (b *bus) Subscribe(handler bookkeeper.EventHandler) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.consume != nil {
		return errors.New("nats: bus already subscribed")
	}
	cc, err := b.consumer.Consume(func(msg jetstream.Msg) {
		ctx, cancel := context.WithTimeout(context.Background(), b.ackWait)
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
	if err != nil {
		return fmt.Errorf("nats: consume: %w", err)
	}
	b.consume = cc
	return nil
}

// Close drains the consume loop (processing any already-delivered
// messages, including their Acks) and then closes the underlying NATS
// connection. The drain step has to complete before the NATS connection
// goes away because Ack itself publishes over NATS; tearing the
// connection down first would silently lose acks and leave messages
// stuck in num_pending. Close is safe to call when Subscribe was never
// invoked and safe to call multiple times.
func (b *bus) Close() error {
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

// StampPubAck applies the broker-assigned subject and sequence to an
// event. Producers call it after PublishMsg returns; consumers call it
// (via DecodeMsg) after pulling a message. Entry.ID is not touched here:
// the producer (bookkeeper.Agent) picks it as FormatEntryID(lastSeq+1)
// before publishing and the transport carries it through the wire
// unchanged, so consumer-side reads see the same identifier without any
// derivation step.
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

// IsWrongLastSequence reports whether err is JetStream's "wrong last
// sequence" rejection (APIError code 10071). Publish uses it to
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
