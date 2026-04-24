// Package icd is the ICD-10 clinical coding domain.
// It defines the typed Note, Intent, and CodeSuggestion value types, the
// Validator that enforces verbatim evidence and dictionary membership, and
// the port interfaces (Dictionary, Recorder) that agents depend on. The
// package has no dependency on LLM SDKs or the agent harness, so downstream
// callers can import it for offline validation, handoff contracts, or batch
// processing without pulling provider code.
package icd

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const SystemICD10 = "ICD-10"

type Note struct {
	ID   string
	Text string
}

// CodeSuggestion is one proposed ICD-10 assignment. EvidenceSpan must be a
// verbatim fragment of the source note so validators can check traceability
// without trusting the model.
type CodeSuggestion struct {
	Code         string  `json:"code"`
	System       string  `json:"system"`
	Description  string  `json:"description"`
	EvidenceSpan string  `json:"evidence_span"`
	Confidence   float64 `json:"confidence"`
}

// Intent is the typed output of the coder agent for one note.
type Intent struct {
	Suggestions []CodeSuggestion `json:"suggestions"`
}

// Validator enforces domain rules for an Intent against its source note and
// the allowed ICD-10 dictionary.
type Validator struct {
	Note Note
	Dict Dictionary
}

func (v Validator) Validate(_ context.Context, intent Intent) error {
	if v.Dict == nil {
		return errors.New("icd: validator has no dictionary")
	}

	normalizedNote := normalizeWhitespace(v.Note.Text)
	seen := make(map[string]struct{}, len(intent.Suggestions))

	var errs []error
	for i, s := range intent.Suggestions {
		label := fmt.Sprintf("suggestion[%d]", i)
		if s.Code != "" {
			label = fmt.Sprintf("suggestion[%d] %s", i, s.Code)
		}

		if s.Code == "" {
			errs = append(errs, fmt.Errorf("%s: code is empty", label))
			continue
		}
		if s.System != "" && s.System != SystemICD10 {
			errs = append(errs, fmt.Errorf("%s: system must be %q, got %q", label, SystemICD10, s.System))
		}
		if _, dup := seen[s.Code]; dup {
			errs = append(errs, fmt.Errorf("%s: duplicate code", label))
			continue
		}
		seen[s.Code] = struct{}{}

		if _, ok := v.Dict.Lookup(s.Code); !ok {
			errs = append(errs, fmt.Errorf("%s: not in allowed ICD-10 dictionary", label))
		}
		if s.Confidence < 0 || s.Confidence > 1 {
			errs = append(errs, fmt.Errorf("%s: confidence %.2f is out of range [0,1]", label, s.Confidence))
		}

		span := normalizeWhitespace(s.EvidenceSpan)
		switch {
		case span == "":
			errs = append(errs, fmt.Errorf("%s: evidence_span is empty", label))
		case !strings.Contains(normalizedNote, span):
			errs = append(errs, fmt.Errorf("%s: evidence_span %q is not a verbatim fragment of the note", label, s.EvidenceSpan))
		}
	}

	return errors.Join(errs...)
}

func normalizeWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
