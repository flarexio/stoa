package main

// compose.go holds the UI-agnostic composition helpers that wire the stoa
// binary's outbound adapters from a config.Config: repository, messaging bus,
// and reasoning engine.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/flarexio/stoa/accounting"
	"github.com/flarexio/stoa/accounting/agent"
	"github.com/flarexio/stoa/accounting/bookkeeping"
	"github.com/flarexio/stoa/config"
	"github.com/flarexio/stoa/llm"
	"github.com/flarexio/stoa/llm/openai"
	"github.com/flarexio/stoa/messaging/inproc"
	"github.com/flarexio/stoa/persistence/memory"

	natsmsg "github.com/flarexio/stoa/messaging/nats"
	pgrepo "github.com/flarexio/stoa/persistence/postgres"
)

// loadBookConfig reads config.yaml from dir, falling back to
// config.DefaultDir() (~/.flarex/stoa) when dir is empty. The file is required:
// a missing config.yaml is an error, never an implicit in-process fallback.
func loadBookConfig(dir string) (*config.Config, error) {
	if dir == "" {
		def, err := config.DefaultDir()
		if err != nil {
			return nil, fmt.Errorf("book-run: %w", err)
		}
		dir = def
	}
	return config.Load(filepath.Join(dir, config.Filename))
}

// buildRepository materialises the accounting.LedgerRepository chosen by cfg.
// The returned io.Closer is always safe to call -- the memory backend supplies
// a no-op closer.
func buildRepository(ctx context.Context, cfg config.Persistence) (accounting.LedgerRepository, io.Closer, error) {
	switch cfg.Kind {
	case config.PersistenceMemory:
		return memory.NewAccountingRepository(), noopCloser{}, nil
	case config.PersistencePostgres:
		repo, closer, err := pgrepo.NewAccountingRepository(ctx, cfg.Postgres.DSN)
		if err != nil {
			return nil, nil, fmt.Errorf("book-run: postgres: %w", err)
		}
		return repo, closer, nil
	default:
		return nil, nil, fmt.Errorf("book-run: unsupported persistence kind %q", cfg.Kind)
	}
}

// buildMessaging materialises the bookkeeping.EventBus chosen by cfg and
// subscribes a single handler that applies events to repo. The bus's
// Close method tears down whichever transport was opened.
func buildMessaging(ctx context.Context, cfg config.Messaging, repo accounting.LedgerRepository) (bookkeeping.EventBus, error) {
	bus, err := openBus(ctx, cfg)
	if err != nil {
		return nil, err
	}
	apply := bookkeeping.EventHandlerFunc(func(ctx context.Context, evt accounting.JournalPosted) error {
		return repo.Apply(ctx, evt)
	})
	if err := bus.Subscribe(apply); err != nil {
		_ = bus.Close()
		return nil, fmt.Errorf("book-run: subscribe: %w", err)
	}
	return bus, nil
}

// openBus opens the EventBus chosen by cfg without subscribing yet.
func openBus(ctx context.Context, cfg config.Messaging) (bookkeeping.EventBus, error) {
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
			return nil, fmt.Errorf("book-run: nats: %w", err)
		}
		return bus, nil
	default:
		return nil, fmt.Errorf("book-run: unsupported messaging kind %q", cfg.Kind)
	}
}

// noopCloser satisfies io.Closer for adapters that own no external resources.
type noopCloser struct{}

func (noopCloser) Close() error { return nil }

// buildBookEngine selects the reasoning engine the CLI feeds to the
// bookkeeper agent. The scripted engine is offline and deterministic; the
// openai engine drives a real LLM through the same harness loop.
func buildBookEngine(ctx context.Context, kind string, scenario accounting.Scenario, repo accounting.LedgerRepository, amount int64, currency, model string) (llm.ReasoningEngine[bookkeeping.Intent], error) {
	switch kind {
	case "", "scripted":
		expense, err := firstActiveAccount(ctx, repo, accounting.AccountExpense)
		if err != nil {
			return nil, err
		}
		if expense == "" {
			return nil, errors.New("book-run: scripted engine requires an active expense account")
		}
		liability, err := firstActiveAccount(ctx, repo, accounting.AccountLiability)
		if err != nil {
			return nil, err
		}
		if liability == "" {
			return nil, errors.New("book-run: scripted engine requires an active liability account")
		}
		return newScriptedBookEngine(repo, amount, currency), nil
	case "openai":
		renderer, err := agent.NewPromptRenderer(ctx, scenario.Company, repo)
		if err != nil {
			return nil, fmt.Errorf("book-run: openai engine: %w", err)
		}
		adapter, err := openai.NewAdapter(openai.Config[bookkeeping.Intent]{
			Model:        model,
			OutputFormat: openai.OutputFormatJSONObject,
			Renderer:     renderer,
		})
		if err != nil {
			return nil, fmt.Errorf("book-run: openai engine: %w", err)
		}
		return adapter, nil
	default:
		return nil, fmt.Errorf("book-run: unknown --engine %q (want scripted|openai)", kind)
	}
}

// extractFeedback collects validation- and execution-error content from events
// for the "feedback" field of the CLI's JSON report.
func extractFeedback(events []llm.CycleEvent) []string {
	var feedback []string
	for _, e := range events {
		if e.Kind == llm.EventValidationError || e.Kind == llm.EventExecutionError {
			feedback = append(feedback, e.Content)
		}
	}
	return feedback
}
