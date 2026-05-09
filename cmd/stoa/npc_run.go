package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/flarexio/stoa/llm"
	"github.com/flarexio/stoa/npc"
	"github.com/flarexio/stoa/world"
)

const npcRunUsage = `Usage: stoa npc-run <scenario.json> --actor <actor_id> [--task <text>] [--max-turns N]

Loads a scenario JSON file, runs the npc.Agent loop with a deterministic
scripted reasoning engine, and prints a JSON report to stdout. The scripted
engine first proposes an invalid intent so the demo exercises Stoa's
validation-feedback self-correction loop.`

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

func runNPC(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("npc-run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprintln(stderr, npcRunUsage) }

	actor := fs.String("actor", "", "actor ID to drive (required)")
	task := fs.String("task", "", "in-world task description (defaults to scenario summary)")
	maxTurns := fs.Int("max-turns", 3, "maximum reasoning turns")

	// Allow the scenario path either before or after the flags so the issue's
	// example invocation (path first, then --actor) works as written.
	var path string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		path, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if path == "" && fs.NArg() > 0 {
		path = fs.Arg(0)
	}
	if path == "" {
		fs.Usage()
		return errors.New("npc-run: scenario path is required")
	}
	if *actor == "" {
		fs.Usage()
		return errors.New("npc-run: --actor is required")
	}

	scenario, err := world.LoadScenarioFile(path)
	if err != nil {
		return err
	}
	if _, ok := scenario.State.Actors[*actor]; !ok {
		return fmt.Errorf("npc-run: actor %q not present in scenario", *actor)
	}

	taskText := *task
	if taskText == "" {
		taskText = scenario.Summary
	}
	if taskText == "" {
		taskText = fmt.Sprintf("Decide what %s does next.", *actor)
	}

	engine := newScriptedEngine(scenario.State, *actor)
	agent := npc.Agent{Engine: engine, MaxTurns: *maxTurns}

	res, runErr := agent.Act(ctx, *actor, scenario.State, taskText)

	out := runOutput{
		Scenario:    scenario.Name,
		Summary:     scenario.Summary,
		Actor:       *actor,
		Task:        taskText,
		Turns:       res.Turns,
		Intent:      res.Intent,
		Observation: res.Observation,
		Events:      res.Events,
		Feedback:    extractFeedback(res.Events),
	}

	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("npc-run: encode output: %w", err)
	}
	return runErr
}

func extractFeedback(events []llm.CycleEvent) []string {
	var feedback []string
	for _, e := range events {
		if e.Kind == llm.EventValidationError || e.Kind == llm.EventExecutionError {
			feedback = append(feedback, e.Content)
		}
	}
	return feedback
}
