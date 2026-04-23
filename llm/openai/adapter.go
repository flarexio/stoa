package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"
	"github.com/flarexio/stoa/llm"
)

// Adapter implements llm.ReasoningEngine using the official OpenAI Go SDK.
type Adapter struct {
	client openai.Client
	model  string
}

// NewAdapter creates a new OpenAI adapter.
func NewAdapter(model string) (*Adapter, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY environment variable is not set")
	}

	client := openai.NewClient(option.WithAPIKey(apiKey))
	
	if model == "" {
		model = "gpt-4o"
	}

	return &Adapter{
		client: client,
		model:  model,
	}, nil
}

// Predict sends the task and history to OpenAI using the official SDK.
func (a *Adapter) Predict(ctx context.Context, task string, history []string, out any) error {
	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("You are a helpful AI agent. Respond ONLY with valid JSON."),
		openai.UserMessage(task),
	}

	for _, h := range history {
		messages = append(messages, openai.AssistantMessage(h))
	}

	// Based on go doc, we use direct assignment for this SDK version
	params := openai.ChatCompletionNewParams{
		Messages: messages,
		Model:    openai.ChatModel(a.model),
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONObject: &shared.ResponseFormatJSONObjectParam{
				Type: "json_object",
			},
		},
	}

	resp, err := a.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return fmt.Errorf("openai api call failed: %w", err)
	}

	content := resp.Choices[0].Message.Content
	if err := json.Unmarshal([]byte(content), out); err != nil {
		return fmt.Errorf("failed to decode LLM response: %w\nContent: %s", err, content)
	}

	return nil
}

var _ llm.ReasoningEngine = (*Adapter)(nil)
