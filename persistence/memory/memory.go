// Package memory provides in-memory repository implementations. It is
// intended for tests, single-process development runs, and the local
// read-model embedded in a producer that needs fresh state to validate
// intents against. State is held in process memory; nothing is durable
// across restarts. For production projection storage, use
// persistence/postgres.
//
// Domain-specific repository implementations live in per-domain factory
// files (e.g. accounting.go).
package memory
