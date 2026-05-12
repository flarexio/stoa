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
	"github.com/flarexio/stoa/llm"
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
		Description: "Loads an accounting scenario JSON file, runs the bookkeeper.Agent loop\n" +
			"with a deterministic scripted reasoning engine, and prints a JSON report to\n" +
			"stdout. The scripted engine first proposes an unbalanced journal so the demo\n" +
			"exercises the validation-feedback self-correction loop before posting.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "request",
				Usage:    "natural-language bookkeeping request",
				Required: true,
			},
			&cli.IntFlag{
				Name:  "amount",
				Usage: "amount in minor currency units (e.g. cents) for the balanced journal",
				Value: 10000,
			},
			&cli.StringFlag{
				Name:  "currency",
				Usage: "ISO currency code for the journal entry",
				Value: "USD",
			},
			&cli.IntFlag{
				Name:  "max-turns",
				Usage: "maximum reasoning turns",
				Value: 3,
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
	amount := int64(c.Int("amount"))
	currency := c.String("currency")
	maxTurns := int(c.Int("max-turns"))

	scenario, err := accounting.LoadScenarioFile(path)
	if err != nil {
		return err
	}
	ledger := scenario.BuildLedger()

	if firstActiveAccount(ledger, accounting.AccountExpense) == "" {
		return errors.New("book-run: scenario has no active expense account")
	}
	if firstActiveAccount(ledger, accounting.AccountLiability) == "" {
		return errors.New("book-run: scenario has no active liability account")
	}
	if firstOpenPeriod(ledger).ID == "" {
		return errors.New("book-run: scenario has no open period")
	}

	engine := newScriptedBookEngine(ledger, amount, currency)
	agent := bookkeeper.Agent{Engine: engine, Ledger: ledger, MaxTurns: maxTurns}

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
