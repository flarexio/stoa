package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/urfave/cli/v3"

	"github.com/flarexio/stoa/accounting"
	"github.com/flarexio/stoa/bookkeeper"
	"github.com/flarexio/stoa/config"
	"github.com/flarexio/stoa/llm"
	"github.com/flarexio/stoa/llm/openai"
	"github.com/flarexio/stoa/messaging/inproc"
	natsmsg "github.com/flarexio/stoa/messaging/nats"
	"github.com/flarexio/stoa/persistence/memory"
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
			"runs the bookkeeper.Agent loop, and prints a JSON report to stdout. With\n" +
			"no --config flag the demo runs entirely in-process (memory + inproc bus).\n" +
			"Pass --config to point at a config.yaml that selects persistence/postgres\n" +
			"and/or messaging/nats backends. Use --engine scripted (default) for the\n" +
			"deterministic offline reasoning engine, or --engine openai to drive a real\n" +
			"LLM through the same harness; the openai engine needs OPENAI_API_KEY.",
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
				Name:  "config",
				Usage: "path to config.yaml selecting persistence/messaging backends (defaults to memory + inproc)",
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
	configPath := c.String("config")

	cfg, err := loadBookConfig(configPath)
	if err != nil {
		return err
	}

	scenario, err := accounting.LoadScenarioFile(path)
	if err != nil {
		return err
	}

	repo, repoClose, err := buildRepository(ctx, cfg.Persistence)
	if err != nil {
		return err
	}
	defer repoClose()

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

	publisher, busClose, err := buildMessaging(ctx, cfg.Messaging, repo)
	if err != nil {
		return err
	}
	defer busClose()

	engine, err := buildBookEngine(ctx, engineKind, scenario, repo, amount, currency, model)
	if err != nil {
		return err
	}
	agent := bookkeeper.Agent{
		Engine:    engine,
		Repo:      repo,
		Publisher: publisher,
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

// loadBookConfig returns the config to apply: when path is empty, the
// in-process defaults; otherwise the parsed file.
func loadBookConfig(path string) (*config.Config, error) {
	if path == "" {
		return &config.Config{
			Persistence: config.Persistence{Kind: config.PersistenceMemory},
			Messaging:   config.Messaging{Kind: config.MessagingInproc},
		}, nil
	}
	return config.Load(path)
}

// buildRepository materialises the accounting.LedgerRepository chosen
// by cfg. The returned close func is always safe to call (it is a no-op
// for the memory backend).
func buildRepository(ctx context.Context, cfg config.Persistence) (accounting.LedgerRepository, func(), error) {
	switch cfg.Kind {
	case config.PersistenceMemory, "":
		return memory.New(), func() {}, nil
	case config.PersistencePostgres:
		repo, pool, err := pgrepo.Connect(ctx, cfg.Postgres.DSN)
		if err != nil {
			return nil, nil, fmt.Errorf("book-run: postgres: %w", err)
		}
		return repo, func() { pool.Close() }, nil
	default:
		return nil, nil, fmt.Errorf("book-run: unsupported persistence kind %q", cfg.Kind)
	}
}

// buildMessaging materialises the accounting.EventPublisher chosen by
// cfg and subscribes a single handler that applies events to repo. The
// returned close func tears down whichever transport was opened.
func buildMessaging(ctx context.Context, cfg config.Messaging, repo accounting.LedgerRepository) (accounting.EventPublisher, func(), error) {
	apply := accounting.EventHandlerFunc(func(ctx context.Context, evt accounting.JournalPosted) error {
		return repo.Apply(ctx, evt)
	})

	switch cfg.Kind {
	case config.MessagingInproc, "":
		bus := inproc.New()
		bus.Subscribe(apply)
		return bus, func() {}, nil
	case config.MessagingNATS:
		conn, err := natsmsg.Connect(ctx, natsmsg.Config{
			URL:      cfg.NATS.URL,
			Stream:   cfg.NATS.Stream,
			Subject:  cfg.NATS.Subject,
			Consumer: cfg.NATS.Consumer,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("book-run: nats: %w", err)
		}
		cons, err := conn.Consumer(ctx)
		if err != nil {
			conn.Close()
			return nil, nil, fmt.Errorf("book-run: nats consumer: %w", err)
		}
		if err := cons.Subscribe(ctx, apply); err != nil {
			cons.Close()
			conn.Close()
			return nil, nil, fmt.Errorf("book-run: nats subscribe: %w", err)
		}
		return conn.Publisher(), func() {
			cons.Close()
			conn.Close()
		}, nil
	default:
		return nil, nil, fmt.Errorf("book-run: unsupported messaging kind %q", cfg.Kind)
	}
}

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
