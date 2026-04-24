package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"

	"github.com/flarexio/stoa/llm"
)

const defaultModel = "gpt-5.4-mini"

type OutputFormat string

const (
	OutputFormatText       OutputFormat = "text"
	OutputFormatJSONObject OutputFormat = "json_object"
)

// Config contains only provider concerns. Domain rules and feature prompts stay
// outside this adapter and enter through llm.ReasoningInput.
type Config[TIntent any] struct {
	APIKey       string
	Model        string
	OutputFormat OutputFormat
	SystemPrompt string
	Renderer     llm.PromptRenderer
	Decoder      llm.Decoder[TIntent]
}

// Adapter implements llm.ReasoningEngine using the official OpenAI Go SDK.
type Adapter[TIntent any] struct {
	client       openai.Client
	model        string
	outputFormat OutputFormat
	renderer     llm.PromptRenderer
	decoder      llm.Decoder[TIntent]
}

func NewAdapter[TIntent any](cfg Config[TIntent]) (*Adapter[TIntent], error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	}
	if apiKey == "" {
		return nil, errors.New("OPENAI_API_KEY is not set")
	}

	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = defaultModel
	}

	renderer := cfg.Renderer
	if renderer == nil {
		renderer = defaultPromptRenderer{systemPrompt: strings.TrimSpace(cfg.SystemPrompt)}
	}

	decoder := cfg.Decoder
	outputFormat := cfg.OutputFormat
	if decoder == nil {
		decoder = JSONDecoder[TIntent]{}
		if outputFormat == "" {
			outputFormat = OutputFormatJSONObject
		}
	}

	return &Adapter[TIntent]{
		client:       openai.NewClient(option.WithAPIKey(apiKey)),
		model:        model,
		outputFormat: outputFormat,
		renderer:     renderer,
		decoder:      decoder,
	}, nil
}

// Predict asks OpenAI for a structured reasoning result. It does not validate
// the intent; validation belongs to domain code after this call returns.
func (a *Adapter[TIntent]) Predict(ctx context.Context, input llm.ReasoningInput) (llm.ReasoningResult[TIntent], error) {
	var result llm.ReasoningResult[TIntent]

	messages, err := a.messages(input)
	if err != nil {
		return result, err
	}

	params := openai.ChatCompletionNewParams{
		Messages: messages,
		Model:    openai.ChatModel(a.model),
	}
	if a.outputFormat == OutputFormatJSONObject {
		params.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONObject: &shared.ResponseFormatJSONObjectParam{
				Type: "json_object",
			},
		}
	}

	resp, err := a.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return result, fmt.Errorf("openai chat completion failed: %w", err)
	}
	if len(resp.Choices) == 0 {
		return result, errors.New("openai chat completion returned no choices")
	}

	content := strings.TrimSpace(resp.Choices[0].Message.Content)
	if content == "" {
		return result, errors.New("openai chat completion returned empty content")
	}
	return a.decoder.Decode(content)
}

func (a *Adapter[TIntent]) messages(input llm.ReasoningInput) ([]openai.ChatCompletionMessageParamUnion, error) {
	messages, err := a.renderer.Render(input)
	if err != nil {
		return nil, fmt.Errorf("render prompt: %w", err)
	}

	translated := make([]openai.ChatCompletionMessageParamUnion, 0, len(messages))
	for _, message := range messages {
		content := strings.TrimSpace(message.Content)
		if content == "" {
			continue
		}
		switch message.Role {
		case llm.MessageRoleSystem:
			translated = append(translated, openai.SystemMessage(content))
		case llm.MessageRoleAssistant:
			translated = append(translated, openai.AssistantMessage(content))
		case llm.MessageRoleUser:
			translated = append(translated, openai.UserMessage(content))
		default:
			return nil, fmt.Errorf("unsupported message role %q", message.Role)
		}
	}
	if len(translated) == 0 {
		return nil, errors.New("render prompt: no messages produced")
	}
	return translated, nil
}

type defaultPromptRenderer struct {
	systemPrompt string
}

func (r defaultPromptRenderer) Render(input llm.ReasoningInput) ([]llm.Message, error) {
	systemPrompt := strings.TrimSpace(r.systemPrompt)
	if systemPrompt == "" {
		systemPrompt = defaultSystemPrompt
	}

	messages := []llm.Message{
		{Role: llm.MessageRoleSystem, Content: systemPrompt},
		{Role: llm.MessageRoleUser, Content: renderReasoningInput(input)},
	}

	for _, event := range input.Events {
		content := renderCycleEvent(event)
		switch event.Role {
		case llm.EventRoleAssistant:
			messages = append(messages, llm.Message{Role: llm.MessageRoleAssistant, Content: content})
		default:
			// Environment feedback is deliberately sent as user-visible context so
			// the model treats validation and execution errors as new constraints.
			messages = append(messages, llm.Message{Role: llm.MessageRoleUser, Content: content})
		}
	}

	return messages, nil
}

type JSONDecoder[TIntent any] struct{}

func (JSONDecoder[TIntent]) Decode(content string) (llm.ReasoningResult[TIntent], error) {
	var result llm.ReasoningResult[TIntent]
	content = strings.TrimSpace(content)
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return result, fmt.Errorf("decode reasoning result: %w: %s", err, content)
	}
	return result, nil
}

func renderReasoningInput(input llm.ReasoningInput) string {
	var b strings.Builder
	b.WriteString("Task:\n")
	b.WriteString(strings.TrimSpace(input.Task))
	if strings.TrimSpace(input.Instructions) != "" {
		b.WriteString("\n\nFeature instructions:\n")
		b.WriteString(strings.TrimSpace(input.Instructions))
	}
	b.WriteString("\n\nReturn JSON with this exact shape:\n")
	b.WriteString(`{"evidence":[{"source":"...","fact":"..."}],"rationale":"...","intent":{...}}`)
	return b.String()
}

func renderCycleEvent(event llm.CycleEvent) string {
	return fmt.Sprintf("[%s:%s]\n%s", event.Role, event.Kind, strings.TrimSpace(event.Content))
}

const defaultSystemPrompt = `You are a Stoa reasoning engine.

You must produce structured JSON only.
You do not execute actions.
You propose a typed intent for Go code to validate.
Use only supplied facts as evidence.
If validation or execution feedback is present, correct the next intent accordingly.
The top-level JSON object must contain evidence, rationale, and intent.`

var _ llm.ReasoningEngine[struct{}] = (*Adapter[struct{}])(nil)
