package icd

import (
	"context"
	"strings"
	"testing"
)

func fixtureValidator() Validator {
	note := Note{
		ID:   "fixture",
		Text: "Patient reports chest pain and persistent cough for three days. History of hypertension.",
	}
	return Validator{Note: note, Dict: DefaultDictionary()}
}

func TestValidator_ValidIntent(t *testing.T) {
	v := fixtureValidator()
	intent := Intent{
		Suggestions: []CodeSuggestion{
			{Code: "R07.9", System: SystemICD10, EvidenceSpan: "chest pain", Confidence: 0.9},
			{Code: "R05", System: SystemICD10, EvidenceSpan: "persistent cough", Confidence: 0.85},
			{Code: "I10", System: SystemICD10, EvidenceSpan: "History of hypertension", Confidence: 0.8},
		},
	}
	if err := v.Validate(context.Background(), intent); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestValidator_RejectsUnknownCode(t *testing.T) {
	v := fixtureValidator()
	intent := Intent{
		Suggestions: []CodeSuggestion{
			{Code: "ZZZ.99", System: SystemICD10, EvidenceSpan: "chest pain", Confidence: 0.8},
		},
	}
	err := v.Validate(context.Background(), intent)
	if err == nil || !strings.Contains(err.Error(), "not in allowed") {
		t.Fatalf("expected dictionary error, got %v", err)
	}
}

func TestValidator_RejectsSpanNotInNote(t *testing.T) {
	v := fixtureValidator()
	intent := Intent{
		Suggestions: []CodeSuggestion{
			{Code: "R05", System: SystemICD10, EvidenceSpan: "severe wheezing", Confidence: 0.7},
		},
	}
	err := v.Validate(context.Background(), intent)
	if err == nil || !strings.Contains(err.Error(), "verbatim") {
		t.Fatalf("expected verbatim error, got %v", err)
	}
}

func TestValidator_RejectsDuplicates(t *testing.T) {
	v := fixtureValidator()
	intent := Intent{
		Suggestions: []CodeSuggestion{
			{Code: "R05", System: SystemICD10, EvidenceSpan: "cough", Confidence: 0.9},
			{Code: "R05", System: SystemICD10, EvidenceSpan: "cough", Confidence: 0.85},
		},
	}
	err := v.Validate(context.Background(), intent)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestValidator_RejectsOutOfRangeConfidence(t *testing.T) {
	v := fixtureValidator()
	intent := Intent{
		Suggestions: []CodeSuggestion{
			{Code: "I10", System: SystemICD10, EvidenceSpan: "hypertension", Confidence: 1.5},
		},
	}
	err := v.Validate(context.Background(), intent)
	if err == nil || !strings.Contains(err.Error(), "confidence") {
		t.Fatalf("expected confidence error, got %v", err)
	}
}

func TestValidator_NormalizesSpanWhitespace(t *testing.T) {
	v := fixtureValidator()
	intent := Intent{
		Suggestions: []CodeSuggestion{
			{Code: "R05", System: SystemICD10, EvidenceSpan: "persistent   cough", Confidence: 0.8},
		},
	}
	if err := v.Validate(context.Background(), intent); err != nil {
		t.Fatalf("expected whitespace-normalized span to pass, got %v", err)
	}
}

func TestValidator_EmptyIntentIsAllowed(t *testing.T) {
	v := fixtureValidator()
	if err := v.Validate(context.Background(), Intent{}); err != nil {
		t.Fatalf("empty intent should be allowed (no applicable codes), got %v", err)
	}
}
