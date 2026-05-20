// Package config parses the YAML file for stoa CLI defaults. Domain packages
// must not import it. See config.example.yaml for the full shape.
package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Filename is the fixed config file name inside the stoa work directory.
const Filename = "config.yaml"

// DefaultDir is the per-user work directory: ~/.flarex/stoa.
func DefaultDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: resolve home directory: %w", err)
	}
	return filepath.Join(home, ".flarex", "stoa"), nil
}

// EngineKind names the reasoning engine; empty defaults to EngineScripted.
type EngineKind string

const (
	EngineScripted EngineKind = "scripted"
	EngineOpenAI   EngineKind = "openai"
)

// Config is the decoded representation of config.yaml.
type Config struct {
	LLM LLM `yaml:"llm"`
}

// LLM defaults for the reasoning engine; --engine / --model
// CLI flags override these.
type LLM struct {
	Engine EngineKind `yaml:"engine"`
	Model  string     `yaml:"model"`
}

// Load reads path, decodes strictly (unknown fields rejected), and validates.
// Empty kinds are defaulted before return so callers can switch on Kind
// without re-checking for "".
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %q: %w", path, err)
	}
	var cfg Config
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("config: decode %q: %w", path, err)
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config: %q: %w", path, err)
	}
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.LLM.Engine == "" {
		c.LLM.Engine = EngineScripted
	}
}

// Validate returns a joined error of every misconfiguration found.
func (c *Config) Validate() error {
	var errs []error

	switch c.LLM.Engine {
	case EngineScripted:
	case EngineOpenAI:
		// llm.model is optional at config time; --model can supply it.
	default:
		errs = append(errs, fmt.Errorf("llm.engine %q is not supported (scripted|openai)", c.LLM.Engine))
	}

	return errors.Join(errs...)
}
