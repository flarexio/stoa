package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flarexio/stoa/config"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoad_EmptyFileDefaultsToInProcess(t *testing.T) {
	path := writeConfig(t, "")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LLM.Engine != config.EngineScripted {
		t.Errorf("llm engine default: want scripted, got %q", cfg.LLM.Engine)
	}
}

func TestLoad_LLMBlockParsed(t *testing.T) {
	path := writeConfig(t, "llm:\n  engine: openai\n  model: gpt-5.4-mini\n")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LLM.Engine != config.EngineOpenAI {
		t.Errorf("llm engine: want openai, got %q", cfg.LLM.Engine)
	}
	if cfg.LLM.Model != "gpt-5.4-mini" {
		t.Errorf("llm model: want gpt-5.4-mini, got %q", cfg.LLM.Model)
	}
}

func TestLoad_UnknownEngineRejected(t *testing.T) {
	path := writeConfig(t, "llm:\n  engine: anthropic\n")
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("expected error for unsupported llm engine")
	}
	if !strings.Contains(err.Error(), "anthropic") {
		t.Errorf("error should name the bad engine, got %v", err)
	}
}

func TestLoad_OpenAIEngineDoesNotRequireModel(t *testing.T) {
	// --model may supply it later; config validation must not reject openai without one.
	path := writeConfig(t, "llm:\n  engine: openai\n")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LLM.Engine != config.EngineOpenAI || cfg.LLM.Model != "" {
		t.Errorf("unexpected llm block: %+v", cfg.LLM)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := config.Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestDefaultDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	got, err := config.DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	want := filepath.Join(home, ".flarex", "stoa")
	if got != want {
		t.Errorf("DefaultDir: got %q, want %q", got, want)
	}
	if config.Filename != "config.yaml" {
		t.Errorf("Filename: got %q, want %q", config.Filename, "config.yaml")
	}
}
