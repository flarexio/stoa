// Package runtime provides a UI-agnostic application runtime for Stoa.
// It extracts shared composition logic (config loading, scenario loading,
// repository/messaging/engine construction) from cmd/stoa so both the
// existing CLI and a future TUI can reuse it without importing cmd/stoa
// internals.
package runtime

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/flarexio/stoa/accounting"
	"github.com/flarexio/stoa/bookkeeper"
	"github.com/flarexio/stoa/config"
	"github.com/flarexio/stoa/llm"
	"github.com/flarexio/stoa/llm/openai"
	"github.com/flarexio/stoa/messaging/inproc"
	"github.com/flarexio/stoa/persistence/memory"

	natsmsg "github.com/flarexio/stoa/messaging/nats"
	pgrepo "github.com/flarexio/stoa/persistence/postgres"
)

// LoadConfig reads config.yaml from the given work directory. When dir is
// empty it falls back to config.DefaultDir() (~/.flarex/stoa).
func LoadConfig(dir string) (*config.Config, error) {
	if dir == "" {
		def, err := config.DefaultDir()
		if err != nil {
			return nil, fmt.Errorf("runtime: %w", err)
		}
		dir = def
	}
	return config.Load(filepath.Join(dir, config.Filename))
}

// NewRepository materialises the accounting.LedgerRepository chosen by
// cfg. The returned io.Closer is always safe to call; the memory backend
// supplies a no-op closer.
func NewRepository(ctx context.Context, cfg config.Persistence) (accounting.LedgerRepository, io.Closer, error) {
	switch cfg.Kind {
	case config.PersistenceMemory:
		return memory.NewAccountingRepository(), noopCloser{}, nil
	case config.PersistencePostgres:
		repo, closer, err := pgrepo.NewAccountingRepository(ctx, cfg.Postgres.DSN)
		if err != nil {
			return nil, nil, fmt.Errorf("runtime: postgres: %w", err)
		}
		return repo, closer, nil
	default:
		return nil, nil, fmt.Errorf("runtime: unsupported persistence kind %q", cfg.Kind)
	}
}

// NewMessaging materialises the bookkeeper.EventBus chosen by cfg and
// subscribes a handler that applies events to repo.
func NewMessaging(ctx context.Context, cfg config.Messaging, repo accounting.LedgerRepository) (bookkeeper.EventBus, error) {
	bus, err := openBus(ctx, cfg)
	if err != nil {
		return nil, err
	}
	apply := bookkeeper.EventHandlerFunc(func(ctx context.Context, evt accounting.JournalPosted) error {
		return repo.Apply(ctx, evt)
	})
	if err := bus.Subscribe(apply); err != nil {
		_ = bus.Close()
		return nil, fmt.Errorf("runtime: subscribe: %w", err)
	}
	return bus, nil
}

func openBus(ctx context.Context, cfg config.Messaging) (bookkeeper.EventBus, error) {
	switch cfg.Kind {
	case config.MessagingInproc:
		return inproc.NewAccountingBus(), nil
	case config.MessagingNATS:
		bus, err := natsmsg.NewAccountingBus(ctx, natsmsg.Config{
			URL:           cfg.NATS.URL,
			Stream:        cfg.NATS.Stream,
			Subject:       cfg.NATS.Subject,
			StreamSubject: cfg.NATS.StreamSubject,
			Consumer:      cfg.NATS.Consumer,
		})
		if err != nil {
			return nil, fmt.Errorf("runtime: nats: %w", err)
		}
		return bus, nil
	default:
		return nil, fmt.Errorf("runtime: unsupported messaging kind %q", cfg.Kind)
	}
}

// NewBookEngine selects the reasoning engine for the bookkeeper agent.
func NewBookEngine(ctx context.Context, kind string, scenario accounting.Scenario, repo accounting.LedgerRepository, amount int64, currency, model string) (llm.ReasoningEngine[accounting.JournalIntent], error) {
	switch kind {
	case "", "scripted":
		expense, err := FirstActiveAccount(ctx, repo, accounting.AccountExpense)
		if err != nil {
			return nil, err
		}
		if expense == "" {
			return nil, fmt.Errorf("runtime: scripted engine requires an active expense account")
		}
		liability, err := FirstActiveAccount(ctx, repo, accounting.AccountLiability)
		if err != nil {
			return nil, err
		}
		if liability == "" {
			return nil, fmt.Errorf("runtime: scripted engine requires an active liability account")
		}
		return NewScriptedBookEngine(repo, amount, currency), nil
	case "openai":
		renderer, err := bookkeeper.NewPromptRenderer(ctx, scenario.Company, repo)
		if err != nil {
			return nil, fmt.Errorf("runtime: openai engine: %w", err)
		}
		adapter, err := openai.NewAdapter(openai.Config[accounting.JournalIntent]{
			Model:        model,
			OutputFormat: openai.OutputFormatJSONObject,
			Renderer:     renderer,
		})
		if err != nil {
			return nil, fmt.Errorf("runtime: openai engine: %w", err)
		}
		return adapter, nil
	default:
		return nil, fmt.Errorf("runtime: unknown engine %q (want scripted|openai)", kind)
	}
}

// ExtractFeedback collects validation and execution error content from
// a slice of cycle events.
func ExtractFeedback(events []llm.CycleEvent) []string {
	var feedback []string
	for _, e := range events {
		if e.Kind == llm.EventValidationError || e.Kind == llm.EventExecutionError {
			feedback = append(feedback, e.Content)
		}
	}
	return feedback
}

type noopCloser struct{}

func (noopCloser) Close() error { return nil }
