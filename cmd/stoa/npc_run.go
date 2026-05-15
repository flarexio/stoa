package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/urfave/cli/v3"

	"github.com/flarexio/stoa/llm"
	"github.com/flarexio/stoa/npc"
	"github.com/flarexio/stoa/runtime"
	"github.com/flarexio/stoa/world"
)

// runOutput is the machine-readable JSON document the CLI prints on success.
type runOutput struct {
	Scenario    string           `json:"scenario,omitempty"`
	Summary     string           `json:"summary,omitempty"`
	Actor       string           `json:"actor"`
	Task        string           `json:"task"`
	Turns       int              `json:"turns"`
	Intent      world.NPCIntent  `json:"intent"`
	Observation llm.Observation  `json:"observation"`
	Events      []llm.CycleEvent `json:"events"`
	Feedback    []string         `json:"feedback"`
}

func newNPCRunCommand(stdout io.Writer) *cli.Command {
	return &cli.Command{
		Name:      "npc-run",
		Usage:     "Run an NPC reasoning loop against a scenario JSON file.",
		ArgsUsage: "<scenario.json>",
		Description: "Loads a scenario JSON file, runs the npc.Agent loop with a deterministic\n" +
			"scripted reasoning engine, and prints a JSON report to stdout. The scripted\n" +
			"engine first proposes an invalid intent so the demo exercises Stoa's\n" +
			"validation-feedback self-correction loop.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "actor",
				Usage:    "actor ID to drive",
				Required: true,
			},
			&cli.StringFlag{
				Name:  "task",
				Usage: "in-world task description (defaults to scenario summary)",
			},
			&cli.IntFlag{
				Name:  "max-turns",
				Usage: "maximum reasoning turns",
				Value: 3,
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			return runNPC(ctx, c, stdout)
		},
	}
}

func runNPC(ctx context.Context, c *cli.Command, stdout io.Writer) error {
	if c.NArg() == 0 {
		return errors.New("npc-run: scenario path is required")
	}
	path := c.Args().First()
	actor := c.String("actor")
	task := c.String("task")
	maxTurns := int(c.Int("max-turns"))

	scenario, err := world.LoadScenarioFile(path)
	if err != nil {
		return err
	}
	if _, ok := scenario.State.Actors[actor]; !ok {
		return fmt.Errorf("npc-run: actor %q not present in scenario", actor)
	}

	taskText := task
	if taskText == "" {
		taskText = scenario.Summary
	}
	if taskText == "" {
		taskText = fmt.Sprintf("Decide what %s does next.", actor)
	}

	engine := runtime.NewScriptedNPCEngine(scenario.State, actor)
	agent := npc.Agent{Engine: engine, MaxTurns: maxTurns}

	res, runErr := agent.Act(ctx, actor, scenario.State, taskText)

	out := runOutput{
		Scenario:    scenario.Name,
		Summary:     scenario.Summary,
		Actor:       actor,
		Task:        taskText,
		Turns:       res.Turns,
		Intent:      res.Intent,
		Observation: res.Observation,
		Events:      res.Events,
		Feedback:    runtime.ExtractFeedback(res.Events),
	}

	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("npc-run: encode output: %w", err)
	}
	return runErr
}
