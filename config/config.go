package config

// EngineKind names the reasoning engine; empty defaults to EngineScripted.
type EngineKind string

const (
	EngineScripted EngineKind = "scripted"
	EngineOpenAI   EngineKind = "openai"
)

// LLM defaults for the reasoning engine; --engine / --model
// CLI flags override these.
type LLM struct {
	Engine EngineKind `yaml:"engine"`
	Model  string     `yaml:"model"`
}
