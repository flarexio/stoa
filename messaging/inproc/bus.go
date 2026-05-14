// Package inproc provides in-process message-bus adapters. It is the
// test/dev counterpart of messaging/nats: same EventBus interface, same
// optimistic-concurrency semantics, no external infrastructure. It is
// not suitable for multi-process production deployments because the
// broker state lives in process memory.
//
// One file per domain: domain-specific bus code (struct, constructor,
// EventBus implementation) lives in <domain>.go (e.g. accounting.go).
// When a second domain needs an in-process bus, it adds a sibling file
// rather than extending an existing struct -- the in-memory
// implementation is small enough that sharing a generic core would
// cost more than it saves.
package inproc
