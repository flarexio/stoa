// Package config parses the YAML file that picks which outbound adapters
// the stoa binary wires when it boots. It is read by cmd/stoa only;
// domain and adapter packages must not import it.
//
// The schema is intentionally narrow: pick a persistence kind, a
// messaging kind, and a reasoning engine, and provide the nested block
// each one needs. See config.example.yaml for the full shape.
package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Filename is the fixed config file name the stoa CLI looks for inside
// the work directory. Adapters and tooling that need to write or locate
// the file should compose against this constant so the name stays
// single-sourced.
const Filename = "config.yaml"

// DefaultDir returns the per-user work directory the stoa CLI uses when
// --work-dir is not provided: ~/.flarex/stoa. The CLI reads config.yaml
// from this directory today and may grow other per-user state
// (credentials, cache, local sqlite, etc.) under the same root later,
// which is why the name is "work" rather than "config". The directory
// and the config.yaml inside it are both required at run time; callers
// do not fall back to in-memory defaults when either is missing.
func DefaultDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: resolve home directory: %w", err)
	}
	return filepath.Join(home, ".flarex", "stoa"), nil
}

// PersistenceKind names a persistence backend the binary knows how to
// wire. The empty string is treated as PersistenceMemory at validation
// time so an absent persistence block degrades to in-memory.
type PersistenceKind string

const (
	PersistenceMemory   PersistenceKind = "memory"
	PersistencePostgres PersistenceKind = "postgres"
)

// MessagingKind names a messaging backend the binary knows how to wire.
// The empty string is treated as MessagingInproc.
type MessagingKind string

const (
	MessagingInproc MessagingKind = "inproc"
	MessagingNATS   MessagingKind = "nats"
)

// EngineKind names a reasoning engine the binary knows how to wire. The
// empty string is treated as EngineScripted so an absent llm block
// degrades to the offline engine.
type EngineKind string

const (
	EngineScripted EngineKind = "scripted"
	EngineOpenAI   EngineKind = "openai"
)

// Config is the decoded representation of config.yaml.
type Config struct {
	Persistence Persistence `yaml:"persistence"`
	Messaging   Messaging   `yaml:"messaging"`
	LLM         LLM         `yaml:"llm"`
}

// Persistence selects and configures the LedgerRepository backend.
type Persistence struct {
	Kind     PersistenceKind `yaml:"kind"`
	Postgres Postgres        `yaml:"postgres"`
}

// Postgres carries the connection settings for persistence/postgres.
// Only DSN is required; everything else is currently derived from the
// DSN by pgx.
type Postgres struct {
	DSN string `yaml:"dsn"`
}

// Messaging selects and configures the EventPublisher backend.
type Messaging struct {
	Kind MessagingKind `yaml:"kind"`
	NATS NATS          `yaml:"nats"`
}

// NATS carries the connection + JetStream settings for messaging/nats.
type NATS struct {
	URL      string `yaml:"url"`
	Stream   string `yaml:"stream"`
	Subject  string `yaml:"subject"`
	Consumer string `yaml:"consumer"`
}

// LLM selects and configures the reasoning engine the bookkeeper agent
// runs. Model is consulted only by the openai engine. Both fields are
// defaults: the --engine and --model CLI flags override them when set.
type LLM struct {
	Engine EngineKind `yaml:"engine"`
	Model  string     `yaml:"model"`
}

// Load reads path, decodes it strictly (unknown fields are rejected),
// and validates the result. The returned Config has had its empty kinds
// defaulted to the in-process backends, so callers can switch on
// PersistenceKind / MessagingKind without re-checking for "".
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %q: %w", path, err)
	}
	var cfg Config
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("config: decode %q: %w", path, err)
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config: %q: %w", path, err)
	}
	return &cfg, nil
}

// applyDefaults fills empty kind selectors with the in-process backends
// so an otherwise-empty config file still produces a runnable Config.
func (c *Config) applyDefaults() {
	if c.Persistence.Kind == "" {
		c.Persistence.Kind = PersistenceMemory
	}
	if c.Messaging.Kind == "" {
		c.Messaging.Kind = MessagingInproc
	}
	if c.LLM.Engine == "" {
		c.LLM.Engine = EngineScripted
	}
}

// Validate returns an error when the selected kinds are unknown or when
// the block a selected kind requires has been left empty. The error is
// joined so a single Load call surfaces every problem at once.
func (c *Config) Validate() error {
	var errs []error

	switch c.Persistence.Kind {
	case PersistenceMemory:
		// no nested block required
	case PersistencePostgres:
		if c.Persistence.Postgres.DSN == "" {
			errs = append(errs, errors.New("persistence.postgres.dsn is required when persistence.kind is postgres"))
		}
	default:
		errs = append(errs, fmt.Errorf("persistence.kind %q is not supported (memory|postgres)", c.Persistence.Kind))
	}

	switch c.Messaging.Kind {
	case MessagingInproc:
		// no nested block required
	case MessagingNATS:
		if c.Messaging.NATS.URL == "" {
			errs = append(errs, errors.New("messaging.nats.url is required when messaging.kind is nats"))
		}
		if c.Messaging.NATS.Stream == "" {
			errs = append(errs, errors.New("messaging.nats.stream is required when messaging.kind is nats"))
		}
		if c.Messaging.NATS.Subject == "" {
			errs = append(errs, errors.New("messaging.nats.subject is required when messaging.kind is nats"))
		}
	default:
		errs = append(errs, fmt.Errorf("messaging.kind %q is not supported (inproc|nats)", c.Messaging.Kind))
	}

	switch c.LLM.Engine {
	case EngineScripted:
		// no nested settings required
	case EngineOpenAI:
		// llm.model may be supplied here or via the --model flag, so it
		// is not required at config time; the openai adapter enforces a
		// non-empty model when the engine is actually wired.
	default:
		errs = append(errs, fmt.Errorf("llm.engine %q is not supported (scripted|openai)", c.LLM.Engine))
	}

	return errors.Join(errs...)
}
