package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/urfave/cli/v3"

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

// bookRunOutput is the machine-readable JSON document the CLI prints on
// success.
type bookRunOutput struct {
	Scenario    string                   `json:"scenario,omitempty"`
	Description string                   `json:"description,omitempty"`
	Request     string                   `json:"request"`
	Turns       int                      `json:"turns"`
	Intent      accounting.JournalIntent `json:"intent"`
	Entry       accounting.JournalEntry  `json:"entry"`
	Observation llm.Observation          `json:"observation"`
	Events      []llm.CycleEvent         `json:"events"`
	Feedback    []string                 `json:"feedback"`
}

func newBookRunCommand(stdout io.Writer) *cli.Command {
	return &cli.Command{
		Name:      "book-run",
		Usage:     "Run a bookkeeping reasoning loop against an accounting scenario JSON file.",
		ArgsUsage: "<scenario.json>",
		Description: "Loads an accounting scenario JSON file, seeds the configured repository,\n" +
			"runs the bookkeeper.Agent loop, and prints a JSON report to stdout. The\n" +
			"binary reads config.yaml from --work-dir, defaulting to ~/.flarex/stoa;\n" +
			"the file must exist (no implicit in-process fallback). Use --engine\n" +
			"scripted (default) for the deterministic offline reasoning engine, or\n" +
			"--engine openai to drive a real LLM through the same harness; the openai\n" +
			"engine needs OPENAI_API_KEY.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "request",
				Usage:    "natural-language bookkeeping request",
				Required: true,
			},
			&cli.StringFlag{
				Name:  "engine",
				Usage: "reasoning engine: scripted (offline) or openai (live)",
				Value: "scripted",
			},
			&cli.StringFlag{
				Name:  "model",
				Usage: "OpenAI model name (used only when --engine openai)",
			},
			&cli.IntFlag{
				Name:  "amount",
				Usage: "amount in minor currency units for the scripted engine's balanced journal",
				Value: 10000,
			},
			&cli.StringFlag{
				Name:  "currency",
				Usage: "ISO currency code used by the scripted engine",
				Value: "USD",
			},
			&cli.IntFlag{
				Name:  "max-turns",
				Usage: "maximum reasoning turns",
				Value: 3,
			},
			&cli.StringFlag{
				Name:  "work-dir",
				Usage: "stoa work directory (holds config.yaml today, more state later); defaults to ~/.flarex/stoa",
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			return runBook(ctx, c, stdout)
		},
	}
}

func runBook(ctx context.Context, c *cli.Command, stdout io.Writer) error {
	if c.NArg() == 0 {
		return errors.New("book-run: scenario path is required")
	}
	path := c.Args().First()
	request := c.String("request")
	engineKind := c.String("engine")
	amount := int64(c.Int("amount"))
	currency := c.String("currency")
	maxTurns := int(c.Int("max-turns"))
	model := c.String("model")
	workDir := c.String("work-dir")

	cfg, err := loadBookConfig(workDir)
	if err != nil {
		return err
	}

	scenario, err := accounting.LoadScenarioFile(path)
	if err != nil {
		return err
	}

	repo, repoCloser, err := buildRepository(ctx, cfg.Persistence)
	if err != nil {
		return err
	}
	defer repoCloser.Close()

	if err := scenario.Seed(ctx, repo); err != nil {
		return err
	}

	period, err := firstOpenPeriod(ctx, repo)
	if err != nil {
		return err
	}
	if period.ID == "" {
		return errors.New("book-run: scenario has no open period")
	}

	bus, err := buildMessaging(ctx, cfg.Messaging, repo)
	if err != nil {
		return err
	}
	defer bus.Close()

	engine, err := buildBookEngine(ctx, engineKind, scenario, repo, amount, currency, model)
	if err != nil {
		return err
	}
	agent := bookkeeper.Agent{
		Engine:    engine,
		Repo:      repo,
		Publisher: bus,
		MaxTurns:  maxTurns,
	}

	res, runErr := agent.Book(ctx, request)

	out := bookRunOutput{
		Scenario:    scenario.Name,
		Description: scenario.Description,
		Request:     request,
		Turns:       res.Turns,
		Intent:      res.Intent,
		Entry:       res.Entry,
		Observation: res.Observation,
		Events:      res.Events,
		Feedback:    extractFeedback(res.Events),
	}

	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("book-run: encode output: %w", err)
	}
	return runErr
}

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
		return memory.New(), noopCloser{}, nil
	case config.PersistencePostgres:
		repo, closer, err := pgrepo.Connect(ctx, cfg.Postgres.DSN)
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
		return inproc.New(), nil
	case config.MessagingNATS:
		bus, err := natsmsg.Connect(ctx, natsmsg.Config{
			URL:      cfg.NATS.URL,
			Stream:   cfg.NATS.Stream,
			Subject:  cfg.NATS.Subject,
			Consumer: cfg.NATS.Consumer,
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
