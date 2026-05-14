package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flarexio/stoa/llm"
)

func awsBillPath(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "testdata", "accounting", "aws_bill.json")
}

func runBookCLI(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	app := newApp(stdout, stderr)
	full := append([]string{"stoa", "book-run"}, args...)
	return app.Run(ctx, full)
}

func TestRunBook_AWSBillSelfCorrects(t *testing.T) {
	var stdout, stderr bytes.Buffer
	args := []string{awsBillPath(t), "--request", "Paid AWS bill 100 USD using company credit card"}
	if err := runBookCLI(context.Background(), args, &stdout, &stderr); err != nil {
		t.Fatalf("runBookCLI returned error: %v\nstderr: %s", err, stderr.String())
	}

	var rep bookRunOutput
	if err := json.Unmarshal(stdout.Bytes(), &rep); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, stdout.String())
	}

	if rep.Request == "" {
		t.Errorf("expected request in output")
	}
	if rep.Turns != 2 {
		t.Errorf("turns: want 2 (one unbalanced + one corrected), got %d", rep.Turns)
	}
	if rep.Entry.ID == "" {
		t.Errorf("expected a posted entry ID on success")
	}
	if rep.Entry.PeriodID != "2026-05" {
		t.Errorf("expected entry posted to open period 2026-05, got %q", rep.Entry.PeriodID)
	}
	if rep.Intent.Lines[0].Amount != rep.Intent.Lines[1].Amount {
		t.Errorf("final intent should be balanced, got %d vs %d",
			rep.Intent.Lines[0].Amount, rep.Intent.Lines[1].Amount)
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
		t.Errorf("events should include validation_error from the first scripted intent")
	}
	if !sawObservation {
		t.Errorf("events should include an observation from the corrected intent")
	}
}

func TestRunBook_FlagsBeforePath(t *testing.T) {
	var stdout, stderr bytes.Buffer
	args := []string{"--request", "Paid AWS bill", awsBillPath(t)}
	if err := runBookCLI(context.Background(), args, &stdout, &stderr); err != nil {
		t.Fatalf("runBookCLI returned error: %v\nstderr: %s", err, stderr.String())
	}
	if !json.Valid(stdout.Bytes()) {
		t.Fatalf("output is not valid JSON: %s", stdout.String())
	}
}

func TestRunBook_RequiresRequest(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runBookCLI(context.Background(), []string{awsBillPath(t)}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error when --request is missing")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "request") {
		t.Errorf("error should mention request, got %v", err)
	}
}

func TestRunBook_RequiresPath(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runBookCLI(context.Background(), []string{"--request", "x"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error when scenario path is missing")
	}
}

func TestRunBook_UnknownEngine(t *testing.T) {
	var stdout, stderr bytes.Buffer
	args := []string{awsBillPath(t), "--request", "x", "--engine", "anthropic"}
	err := runBookCLI(context.Background(), args, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for unknown --engine")
	}
	if !strings.Contains(err.Error(), "anthropic") {
		t.Errorf("error should name the bad engine, got %v", err)
	}
}

func TestRunBook_OpenAIRequiresAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	var stdout, stderr bytes.Buffer
	args := []string{awsBillPath(t), "--request", "x", "--engine", "openai", "--model", "gpt-5.4-mini"}
	err := runBookCLI(context.Background(), args, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error when --engine openai is selected without OPENAI_API_KEY")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "openai_api_key") {
		t.Errorf("error should mention OPENAI_API_KEY, got %v", err)
	}
}

func TestRunBook_OpenAIRequiresModel(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "fake-key-for-test")
	var stdout, stderr bytes.Buffer
	args := []string{awsBillPath(t), "--request", "x", "--engine", "openai"}
	err := runBookCLI(context.Background(), args, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error when --engine openai is selected without --model")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "model") {
		t.Errorf("error should mention model, got %v", err)
	}
}

func TestRunBook_ConfigSelectsMemoryAndInproc(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	body := "persistence:\n  kind: memory\nmessaging:\n  kind: inproc\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var stdout, stderr bytes.Buffer
	args := []string{awsBillPath(t), "--request", "Paid AWS bill", "--config", cfgPath}
	if err := runBookCLI(context.Background(), args, &stdout, &stderr); err != nil {
		t.Fatalf("runBookCLI returned error: %v\nstderr: %s", err, stderr.String())
	}
	var rep bookRunOutput
	if err := json.Unmarshal(stdout.Bytes(), &rep); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if rep.Entry.ID == "" {
		t.Errorf("expected a posted entry under explicit memory+inproc config")
	}
}

func TestRunBook_ConfigRejectsBadKind(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("persistence:\n  kind: mongodb\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var stdout, stderr bytes.Buffer
	args := []string{awsBillPath(t), "--request", "x", "--config", cfgPath}
	err := runBookCLI(context.Background(), args, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error from unsupported persistence kind")
	}
	if !strings.Contains(err.Error(), "mongodb") {
		t.Errorf("error should name the bad kind, got %v", err)
	}
}

func TestRunBook_ConfigMissingFile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	args := []string{awsBillPath(t), "--request", "x", "--config", filepath.Join(t.TempDir(), "missing.yaml")}
	err := runBookCLI(context.Background(), args, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error when --config points at a missing file")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "config") {
		t.Errorf("error should mention config, got %v", err)
	}
}

func TestRunBook_CustomAmount(t *testing.T) {
	var stdout, stderr bytes.Buffer
	args := []string{awsBillPath(t), "--request", "Paid larger bill", "--amount", "50000"}
	if err := runBookCLI(context.Background(), args, &stdout, &stderr); err != nil {
		t.Fatalf("runBookCLI returned error: %v\nstderr: %s", err, stderr.String())
	}
	var rep bookRunOutput
	if err := json.Unmarshal(stdout.Bytes(), &rep); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if rep.Intent.Lines[0].Amount != 50000 {
		t.Errorf("debit amount: want 50000, got %d", rep.Intent.Lines[0].Amount)
	}
	if rep.Intent.Lines[1].Amount != 50000 {
		t.Errorf("credit amount: want 50000, got %d", rep.Intent.Lines[1].Amount)
	}
}
