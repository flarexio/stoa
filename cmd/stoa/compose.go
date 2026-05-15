package main

// compose.go holds the UI-agnostic composition helpers that wire the
// stoa binary's outbound adapters from a config.Config: repository,
// messaging bus, and reasoning engine. The book-run / npc-run commands
// call these directly, and a future in-process TUI command does the
// same -- the wiring lives here, in package main, rather than in a
// separate importable package, because every front-end ships in this
// one binary.

import (
	"context"
	"errors"
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

// loadBookConfig reads config.yaml from the given work directory. When
// dir is empty it falls back to config.DefaultDir() (~/.flarex/stoa).
// The file is required: a missing or unreadable config.yaml surfaces as
// an error rather than silently degrading to in-process defaults, so a
// misplaced config never gets papered over.
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

// buildRepository materialises the accounting.LedgerRepository chosen
// by cfg. The returned io.Closer is always safe to call; the memory
// backend supplies a no-op closer so callers do not have to branch.
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

// buildMessaging materialises the bookkeeper.EventBus chosen by cfg and
// subscribes a single handler that applies events to repo. The bus's
// Close method tears down whichever transport was opened.
func buildMessaging(ctx context.Context, cfg config.Messaging, repo accounting.LedgerRepository) (bookkeeper.EventBus, error) {
	bus, err := openBus(ctx, cfg)
	if err != nil {
		return nil, err
	}
	apply := bookkeeper.EventHandlerFunc(func(ctx context.Context, evt accounting.JournalPosted) error {
		return repo.Apply(ctx, evt)
	})
	if err := bus.Subscribe(apply); err != nil {
		_ = bus.Close()
		return nil, fmt.Errorf("book-run: subscribe: %w", err)
	}
	return bus, nil
}

// openBus opens the EventBus chosen by cfg without subscribing yet.
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
			return nil, fmt.Errorf("book-run: nats: %w", err)
		}
		return bus, nil
	default:
		return nil, fmt.Errorf("book-run: unsupported messaging kind %q", cfg.Kind)
	}
}

// noopCloser satisfies io.Closer for adapters that own no external
// resources (the in-memory repository).
type noopCloser struct{}

func (noopCloser) Close() error { return nil }

// buildBookEngine selects the reasoning engine the CLI feeds to the
// bookkeeper agent. The scripted engine is offline and deterministic; the
// openai engine drives a real LLM through the same harness loop.
func buildBookEngine(ctx context.Context, kind string, scenario accounting.Scenario, repo accounting.LedgerRepository, amount int64, currency, model string) (llm.ReasoningEngine[accounting.JournalIntent], error) {
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
		renderer, err := bookkeeper.NewPromptRenderer(ctx, scenario.Company, repo)
		if err != nil {
			return nil, fmt.Errorf("book-run: openai engine: %w", err)
		}
		adapter, err := openai.NewAdapter(openai.Config[accounting.JournalIntent]{
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

// extractFeedback collects the validation- and execution-error content
// from a slice of cycle events, for the "feedback" field of the CLI's
// JSON report.
func extractFeedback(events []llm.CycleEvent) []string {
	var feedback []string
	for _, e := range events {
		if e.Kind == llm.EventValidationError || e.Kind == llm.EventExecutionError {
			feedback = append(feedback, e.Content)
		}
	}
	return feedback
}
