// Package memory provides in-memory repository adapters. It is intended
// for tests, single-process development runs, and the local read-model
// embedded in a producer that needs fresh state to validate intents
// against. State is held in process memory; nothing is durable across
// restarts. For production projection storage, use persistence/postgres.
//
// One file per domain: domain-specific repository code (struct,
// constructor, port implementation) lives in <domain>.go (e.g.
// accounting.go). When a second domain needs an in-memory adapter, it
// adds a sibling file rather than extending an existing struct -- the
// in-memory implementation is small enough that sharing a generic core
// would cost more than it saves.
package memory
