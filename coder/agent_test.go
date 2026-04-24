package coder_test

import (
	"context"
	"testing"

	"github.com/flarexio/stoa/coder"
	"github.com/flarexio/stoa/icd"
	"github.com/flarexio/stoa/llm"
)

type fakeEngineFunc func(ctx context.Context, input llm.ReasoningInput) (llm.ReasoningResult[icd.Intent], error)

func (f fakeEngineFunc) Predict(ctx context.Context, input llm.ReasoningInput) (llm.ReasoningResult[icd.Intent], error) {
	return f(ctx, input)
}

func TestAgent_CorrectsAfterValidationFeedback(t *testing.T) {
	note := icd.Note{
		ID:   "n1",
		Text: "Patient reports chest pain and persistent cough for three days.",
	}

	calls := 0
	engine := fakeEngineFunc(func(_ context.Context, input llm.ReasoningInput) (llm.ReasoningResult[icd.Intent], error) {
		calls++
		switch calls {
		case 1:
			return llm.ReasoningResult[icd.Intent]{
				Rationale: "first attempt",
				Intent: icd.Intent{
					Suggestions: []icd.CodeSuggestion{
						{Code: "ZZZ.99", System: icd.SystemICD10, EvidenceSpan: "chest pain", Confidence: 0.8},
					},
				},
			}, nil
		default:
			sawValidationErr := false
			for _, e := range input.Events {
				if e.Kind == llm.EventValidationError {
					sawValidationErr = true
				}
			}
			if !sawValidationErr {
				t.Errorf("expected validation_error event on retry, got events %+v", input.Events)
			}
			return llm.ReasoningResult[icd.Intent]{
				Rationale: "corrected after feedback",
				Intent: icd.Intent{
					Suggestions: []icd.CodeSuggestion{
						{Code: "R07.9", System: icd.SystemICD10, EvidenceSpan: "chest pain", Confidence: 0.9},
						{Code: "R05", System: icd.SystemICD10, EvidenceSpan: "persistent cough", Confidence: 0.85},
					},
				},
			}, nil
		}
	})

	recorder := icd.NewInMemoryRecorder()
	agent := coder.Agent{Engine: engine, Dict: icd.DefaultDictionary(), Recorder: recorder, MaxTurns: 3}

	res, err := agent.Code(context.Background(), note)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if res.Turns != 2 {
		t.Fatalf("expected 2 turns, got %d", res.Turns)
	}
	if calls != 2 {
		t.Fatalf("expected engine to be called twice, got %d", calls)
	}
	if len(res.Intent.Suggestions) != 2 {
		t.Fatalf("expected 2 suggestions, got %d", len(res.Intent.Suggestions))
	}
	if _, ok := recorder.Get("n1"); !ok {
		t.Fatal("expected note to be recorded")
	}
}

func TestAgent_GivesUpAfterMaxTurns(t *testing.T) {
	note := icd.Note{ID: "n2", Text: "Patient reports cough."}

	engine := fakeEngineFunc(func(_ context.Context, _ llm.ReasoningInput) (llm.ReasoningResult[icd.Intent], error) {
		return llm.ReasoningResult[icd.Intent]{
			Rationale: "stubborn",
			Intent: icd.Intent{
				Suggestions: []icd.CodeSuggestion{
					{Code: "NOPE.00", System: icd.SystemICD10, EvidenceSpan: "cough", Confidence: 0.5},
				},
			},
		}, nil
	})

	agent := coder.Agent{Engine: engine, Dict: icd.DefaultDictionary(), Recorder: icd.NewInMemoryRecorder(), MaxTurns: 2}
	_, err := agent.Code(context.Background(), note)
	if err == nil {
		t.Fatal("expected max-turns error")
	}
}
