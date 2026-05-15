package runtime

import (
	"context"
	"fmt"
	"io"

	"github.com/flarexio/stoa/accounting"
	"github.com/flarexio/stoa/bookkeeper"
	"github.com/flarexio/stoa/config"
	"github.com/flarexio/stoa/harness/loop"
	"github.com/flarexio/stoa/llm"
)

// BookSession manages the lifecycle of a bookkeeping agent session. It
// wires all infrastructure (repository, messaging, engine, agent) and
// exposes a Run method that a conversational front-end can call per user
// request, with an optional EventSink for per-turn event observation.
type BookSession struct {
	Repo    accounting.LedgerRepository
	Bus     bookkeeper.EventBus
	Engine  llm.ReasoningEngine[accounting.JournalIntent]
	Agent   bookkeeper.Agent
	closers []io.Closer
}

// BookSessionOptions carries the parameters needed to initialise a
// bookkeeping session.
type BookSessionOptions struct {
	Config      *config.Config
	ScenarioPath string
	EngineKind  string
	Model       string
	MaxTurns    int
	// Amount and Currency are used only by the scripted engine.
	Amount   int64
	Currency string
}

// NewBookSession wires a complete bookkeeping session from the given
// options: it loads the config, creates the repository and messaging
// bus, loads and seeds the scenario, constructs the reasoning engine,
// and builds the agent. The returned session's Close method tears
// everything down.
func NewBookSession(ctx context.Context, opts BookSessionOptions) (*BookSession, error) {
	cfg := opts.Config

	repo, repoCloser, err := NewRepository(ctx, cfg.Persistence)
	if err != nil {
		return nil, err
	}

	bus, err := NewMessaging(ctx, cfg.Messaging, repo)
	if err != nil {
		repoCloser.Close()
		return nil, err
	}

	scenario, err := accounting.LoadScenarioFile(opts.ScenarioPath)
	if err != nil {
		bus.Close()
		repoCloser.Close()
		return nil, err
	}

	if err := scenario.Seed(ctx, repo); err != nil {
		bus.Close()
		repoCloser.Close()
		return nil, fmt.Errorf("runtime: seed scenario: %w", err)
	}

	period, err := FirstOpenPeriod(ctx, repo)
	if err != nil {
		bus.Close()
		repoCloser.Close()
		return nil, err
	}
	if period.ID == "" {
		bus.Close()
		repoCloser.Close()
		return nil, fmt.Errorf("runtime: scenario has no open period")
	}

	engineKind := opts.EngineKind
	if engineKind == "" {
		engineKind = string(cfg.LLM.Engine)
	}
	model := opts.Model
	if model == "" {
		model = cfg.LLM.Model
	}
	amount := opts.Amount
	if amount <= 0 {
		amount = 10000
	}
	currency := opts.Currency
	if currency == "" {
		currency = "USD"
	}

	engine, err := NewBookEngine(ctx, engineKind, scenario, repo, amount, currency, model)
	if err != nil {
		bus.Close()
		repoCloser.Close()
		return nil, err
	}

	maxTurns := opts.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 3
	}

	agent := bookkeeper.Agent{
		Engine:    engine,
		Repo:      repo,
		Publisher: bus,
		MaxTurns:  maxTurns,
	}

	closers := []io.Closer{bus, repoCloser}
	return &BookSession{
		Repo:    repo,
		Bus:     bus,
		Engine:  engine,
		Agent:   agent,
		closers: closers,
	}, nil
}

// Run executes a single bookkeeping request. If sink is non-nil, per-turn
// events are streamed through it as they happen rather than only being
// available after the loop completes.
func (s *BookSession) Run(ctx context.Context, request string, sink loop.EventSink) (bookkeeper.Result, error) {
	agent := s.Agent
	agent.Sink = sink
	return agent.Book(ctx, request)
}

// Close tears down the messaging bus and repository.
func (s *BookSession) Close() error {
	var errs []error
	for _, c := range s.closers {
		if err := c.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("runtime: close: %v", errs)
	}
	return nil
}
