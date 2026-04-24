// Package coder is the ICD-10 clinical coding agent. It orchestrates the
// Stoa reason-validate-execute loop for icd.Intent: ask the LLM for code
// suggestions, validate them against icd domain rules, record accepted
// suggestions, and feed validation errors back as typed context for the
// next cycle.
package coder

import (
	"context"
	"errors"
	"fmt"

	"github.com/flarexio/stoa/harness/loop"
	"github.com/flarexio/stoa/icd"
	"github.com/flarexio/stoa/llm"
)

type Agent struct {
	Engine   llm.ReasoningEngine[icd.Intent]
	Dict     icd.Dictionary
	Recorder icd.Recorder
	MaxTurns int
}

type Result struct {
	Intent      icd.Intent
	Observation llm.Observation
	Turns       int
	Events      []llm.CycleEvent
}

func (a Agent) Code(ctx context.Context, note icd.Note) (Result, error) {
	if a.Engine == nil {
		return Result{}, errors.New("coder: agent has no reasoning engine")
	}
	if a.Dict == nil {
		return Result{}, errors.New("coder: agent has no dictionary")
	}
	if a.Recorder == nil {
		return Result{}, errors.New("coder: agent has no recorder")
	}

	validator := icd.Validator{Note: note, Dict: a.Dict}

	executor := loop.ExecutorFunc[icd.Intent](func(ctx context.Context, intent icd.Intent) (llm.Observation, error) {
		if err := a.Recorder.Record(ctx, note, intent); err != nil {
			return llm.Observation{}, fmt.Errorf("coder: record: %w", err)
		}
		return llm.Observation{
			Summary: fmt.Sprintf("Recorded %d ICD-10 suggestion(s) for note %s.", len(intent.Suggestions), note.ID),
		}, nil
	})

	runner := loop.Runner[icd.Intent]{
		Engine:    a.Engine,
		Validator: validator,
		Executor:  executor,
		MaxTurns:  a.MaxTurns,
	}

	input := llm.ReasoningInput{
		Task:         buildTask(note),
		Instructions: taskInstructions,
	}

	out, err := runner.Run(ctx, input)
	return Result{
		Intent:      out.Reasoning.Intent,
		Observation: out.Observation,
		Turns:       out.Turns,
		Events:      out.Events,
	}, err
}

func buildTask(note icd.Note) string {
	return fmt.Sprintf("Propose ICD-10 codes for the following clinical note.\n\nNote ID: %s\n\nNote:\n%s", note.ID, note.Text)
}

const taskInstructions = `Cite evidence as exact phrases from the note.
Prefer fewer, higher-confidence codes over many speculative ones.
If no codes apply, return an empty suggestions list and explain why in rationale.`
