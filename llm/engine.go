package llm

import (
	"context"
)

// ReasoningEngine is the core interface for agent thinking.
// It abstracts the LLM provider and focuses on structured output.
type ReasoningEngine interface {
	// Predict takes a task description and history, then fills the 'out' 
	// parameter with the structured intent. The 'out' parameter should 
	// be a pointer to a struct.
	Predict(ctx context.Context, task string, history []string, out any) error
}

// ModelInfo provides metadata about the underlying model.
type ModelInfo struct {
	Name        string
	MaxTokens   int
	Temperature float32
}
