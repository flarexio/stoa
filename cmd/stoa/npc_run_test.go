package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flarexio/stoa/llm"
	"github.com/flarexio/stoa/world"
)

func tavernPath(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "testdata", "scenarios", "tavern.json")
}

// decodeRunOutput tolerates the leading scenario/summary/etc fields and is
// only concerned with the parts the issue requires.
type runReport struct {
	Scenario    string           `json:"scenario"`
	Summary     string           `json:"summary"`
	Actor       string           `json:"actor"`
	Task        string           `json:"task"`
	Turns       int              `json:"turns"`
	Intent      world.NPCIntent  `json:"intent"`
	Observation llm.Observation  `json:"observation"`
	Events      []llm.CycleEvent `json:"events"`
	Feedback    []string         `json:"feedback"`
}

func TestRunNPC_TavernSelfCorrects(t *testing.T) {
	var stdout, stderr bytes.Buffer
	args := []string{tavernPath(t), "--actor", "mira"}
	if err := runNPC(context.Background(), args, &stdout, &stderr); err != nil {
		t.Fatalf("runNPC returned error: %v\nstderr: %s", err, stderr.String())
	}

	var rep runReport
	if err := json.Unmarshal(stdout.Bytes(), &rep); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, stdout.String())
	}

	if rep.Actor != "mira" {
		t.Errorf("actor: want mira, got %q", rep.Actor)
	}
	if rep.Scenario == "" {
		t.Errorf("expected scenario name in output")
	}
	if rep.Task == "" {
		t.Errorf("expected task in output")
	}
	if rep.Turns != 2 {
		t.Errorf("turns: want 2 (one invalid + one corrected), got %d", rep.Turns)
	}
	if rep.Intent.Action.Type != world.ActionSpeak {
		t.Errorf("final intent action: want speak, got %q", rep.Intent.Action.Type)
	}
	if rep.Observation.Summary == "" {
		t.Errorf("expected non-empty observation summary")
	}
	if len(rep.Feedback) == 0 {
		t.Errorf("expected at least one validation feedback entry, got none")
	}

	var sawValidationErr, sawObservation bool
	for _, ev := range rep.Events {
		switch ev.Kind {
		case llm.EventValidationError:
			sawValidationErr = true
		case llm.EventObservation:
			sawObservation = true
		}
	}
	if !sawValidationErr {
		t.Errorf("events should include a validation_error from the first scripted intent")
	}
	if !sawObservation {
		t.Errorf("events should include an observation from the corrected intent")
	}
}

func TestRunNPC_FlagsBeforePath(t *testing.T) {
	var stdout, stderr bytes.Buffer
	args := []string{"--actor", "mira", tavernPath(t)}
	if err := runNPC(context.Background(), args, &stdout, &stderr); err != nil {
		t.Fatalf("runNPC returned error: %v\nstderr: %s", err, stderr.String())
	}
	if !json.Valid(stdout.Bytes()) {
		t.Fatalf("output is not valid JSON: %s", stdout.String())
	}
}

func TestRunNPC_RequiresActor(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runNPC(context.Background(), []string{tavernPath(t)}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error when --actor is missing")
	}
	if !strings.Contains(err.Error(), "actor") {
		t.Errorf("error should mention actor, got %v", err)
	}
}

func TestRunNPC_RequiresPath(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runNPC(context.Background(), []string{"--actor", "mira"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error when scenario path is missing")
	}
}

func TestRunNPC_UnknownActor(t *testing.T) {
	var stdout, stderr bytes.Buffer
	args := []string{tavernPath(t), "--actor", "ghost"}
	err := runNPC(context.Background(), args, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for unknown actor")
	}
}

func TestRunNPC_CustomTaskOverridesSummary(t *testing.T) {
	var stdout, stderr bytes.Buffer
	const customTask = "A traveler asks Mira about the road."
	args := []string{tavernPath(t), "--actor", "mira", "--task", customTask}
	if err := runNPC(context.Background(), args, &stdout, &stderr); err != nil {
		t.Fatalf("runNPC returned error: %v\nstderr: %s", err, stderr.String())
	}

	var rep runReport
	if err := json.Unmarshal(stdout.Bytes(), &rep); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if rep.Task != customTask {
		t.Errorf("task: want %q, got %q", customTask, rep.Task)
	}
}
