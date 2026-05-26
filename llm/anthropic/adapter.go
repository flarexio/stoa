// Package anthropic is the Anthropic Messages adapter for llm.ReasoningEngine.
package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"

	"github.com/flarexio/stoa/llm"
)

// defaultMaxTokens is the fallback for MessageNewParams.MaxTokens, which the
// Messages API marks required.
const defaultMaxTokens int64 = 4096

// Config contains only provider concerns; domain rules and feature prompts
// enter through llm.ReasoningInput.
type Config[TIntent any] struct {
	APIKey  string
	BaseURL string
	Model   string

	// MaxTokens caps the generated response. Zero means use defaultMaxTokens;
	// the Messages API requires the field, so the adapter always sets it.
	MaxTokens int64

	// IntentSchema is the JSON Schema for the intent payload only. When set,
	// the adapter wraps it in the canonical {evidence, rationale, intent}
	// envelope and requests structured outputs via OutputConfig.Format.
	IntentSchema json.RawMessage

	Renderer llm.PromptRenderer
	Decoder  llm.Decoder[TIntent]
}

// Adapter implements llm.ReasoningEngine using the official Anthropic Go SDK.
type Adapter[TIntent any] struct {
	client    anthropic.Client
	model     string
	maxTokens int64
	envelope  map[string]any // assembled OutputConfig schema; nil when not in strict mode
	renderer  llm.PromptRenderer
	decoder   llm.Decoder[TIntent]
}

// NewAdapter wires the SDK client and the renderer/decoder pair. APIKey and
// BaseURL default to $ANTHROPIC_API_KEY and $ANTHROPIC_BASE_URL respectively;
// explicit config values take precedence over environment variables.
// Renderer and Decoder default to llm's generic implementations.
func NewAdapter[TIntent any](cfg Config[TIntent]) (*Adapter[TIntent], error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	}
	if apiKey == "" {
		return nil, errors.New("ANTHROPIC_API_KEY is not set")
	}

	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("ANTHROPIC_BASE_URL"))
	}

	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		return nil, errors.New("model is required")
	}

	renderer := cfg.Renderer
	if renderer == nil {
		renderer = llm.DefaultPromptRenderer{}
	}

	decoder := cfg.Decoder
	if decoder == nil {
		decoder = llm.JSONDecoder[TIntent]{}
	}

	var envelope map[string]any
	if len(cfg.IntentSchema) > 0 {
		built, err := buildEnvelopeSchema(cfg.IntentSchema)
		if err != nil {
			return nil, fmt.Errorf("anthropic: invalid IntentSchema: %w", err)
		}
		envelope = built
	}

	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	clientOpts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL != "" {
		clientOpts = append(clientOpts, option.WithBaseURL(baseURL))
	}

	return &Adapter[TIntent]{
		client:    anthropic.NewClient(clientOpts...),
		model:     model,
		maxTokens: maxTokens,
		envelope:  envelope,
		renderer:  renderer,
		decoder:   decoder,
	}, nil
}

// Predict asks Anthropic for a structured reasoning output. It does not
// validate the intent; validation belongs to domain code after this call returns.
func (a *Adapter[TIntent]) Predict(ctx context.Context, input llm.ReasoningInput) (llm.ReasoningOutput[TIntent], error) {
	var zero llm.ReasoningOutput[TIntent]

	tools, err := translateTools(input.Tools)
	if err != nil {
		return zero, err
	}

	system, messages, err := a.messages(input)
	if err != nil {
		return zero, err
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(a.model),
		MaxTokens: a.maxTokens,
		Messages:  messages,
	}
	if len(system) > 0 {
		params.System = system
	}
	if len(tools) > 0 {
		params.Tools = tools
	}
	if a.envelope != nil {
		params.OutputConfig = anthropic.OutputConfigParam{
			Format: anthropic.JSONOutputFormatParam{Schema: a.envelope},
		}
	}

	resp, err := a.client.Messages.New(ctx, params)
	if err != nil {
		return zero, fmt.Errorf("anthropic messages.new failed: %w", err)
	}
	if resp == nil || len(resp.Content) == 0 {
		return zero, errors.New("anthropic messages.new returned no content")
	}

	var (
		texts []string
		calls []llm.ToolCall
	)
	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			if t := strings.TrimSpace(block.Text); t != "" {
				texts = append(texts, t)
			}
		case "tool_use":
			calls = append(calls, llm.ToolCall{
				Name: block.Name,
				Args: append(json.RawMessage(nil), block.Input...),
			})
		}
	}

	if len(calls) > 0 {
		rationale := strings.TrimSpace(strings.Join(texts, "\n\n"))
		return llm.ToolCallsOutput[TIntent](calls, nil, rationale), nil
	}

	content := strings.TrimSpace(strings.Join(texts, "\n\n"))
	if content == "" {
		return zero, errors.New("anthropic messages.new returned empty text content")
	}
	return a.decoder.Decode(content)
}

// messages renders the input and splits it into Anthropic's top-level system
// prompt and the user/assistant message list. Anthropic has no system role on
// messages; multiple system messages from the renderer are concatenated.
func (a *Adapter[TIntent]) messages(input llm.ReasoningInput) ([]anthropic.TextBlockParam, []anthropic.MessageParam, error) {
	rendered, err := a.renderer.Render(input)
	if err != nil {
		return nil, nil, fmt.Errorf("render prompt: %w", err)
	}

	var (
		systemParts []string
		messages    []anthropic.MessageParam
	)
	for _, message := range rendered {
		content := strings.TrimSpace(message.Content)
		if content == "" {
			continue
		}
		switch message.Role {
		case llm.MessageRoleSystem:
			systemParts = append(systemParts, content)
		case llm.MessageRoleAssistant:
			messages = append(messages, anthropic.NewAssistantMessage(anthropic.NewTextBlock(content)))
		case llm.MessageRoleUser:
			messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(content)))
		default:
			return nil, nil, fmt.Errorf("unsupported message role %q", message.Role)
		}
	}
	if len(messages) == 0 {
		return nil, nil, errors.New("render prompt: no user/assistant messages produced")
	}

	var system []anthropic.TextBlockParam
	if joined := strings.TrimSpace(strings.Join(systemParts, "\n\n")); joined != "" {
		system = []anthropic.TextBlockParam{{Text: joined}}
	}
	return system, messages, nil
}

// translateTools converts harness tool specs into the SDK's native tool param
// list. Each spec's ArgsSchema is treated as a JSON-Schema object; "properties"
// and "required" are lifted into the SDK fields and any remaining keywords
// (e.g. "additionalProperties") pass through as extras.
func translateTools(specs []llm.ToolSpec) ([]anthropic.ToolUnionParam, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	tools := make([]anthropic.ToolUnionParam, 0, len(specs))
	for _, spec := range specs {
		if strings.TrimSpace(spec.Name) == "" {
			return nil, errors.New("anthropic: tool spec missing name")
		}
		tool := anthropic.ToolParam{Name: spec.Name}
		if spec.Description != "" {
			tool.Description = param.NewOpt(spec.Description)
		}
		if len(spec.ArgsSchema) > 0 {
			schema, err := intoToolInputSchema(spec.ArgsSchema)
			if err != nil {
				return nil, fmt.Errorf("anthropic: tool %q has invalid ArgsSchema: %w", spec.Name, err)
			}
			tool.InputSchema = schema
			tool.Strict = param.NewOpt(true)
		}
		tools = append(tools, anthropic.ToolUnionParam{OfTool: &tool})
	}
	return tools, nil
}

// intoToolInputSchema lifts "properties" and "required" into the SDK's
// dedicated fields and routes any remaining JSON-Schema keywords (e.g.
// "additionalProperties", "$defs") through ExtraFields.
func intoToolInputSchema(raw json.RawMessage) (anthropic.ToolInputSchemaParam, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return anthropic.ToolInputSchemaParam{}, err
	}
	schema := anthropic.ToolInputSchemaParam{}
	if props, ok := m["properties"]; ok {
		schema.Properties = props
	}
	if req, ok := m["required"].([]any); ok {
		for _, v := range req {
			if s, ok := v.(string); ok {
				schema.Required = append(schema.Required, s)
			}
		}
	}
	delete(m, "properties")
	delete(m, "required")
	delete(m, "type")
	if len(m) > 0 {
		schema.ExtraFields = m
	}
	return schema, nil
}

// buildEnvelopeSchema wraps the caller-supplied intent schema in the canonical
// {evidence, rationale, intent} envelope used for structured outputs.
func buildEnvelopeSchema(intentSchema json.RawMessage) (map[string]any, error) {
	var intent any
	if err := json.Unmarshal(intentSchema, &intent); err != nil {
		return nil, err
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"evidence", "rationale", "intent"},
		"properties": map[string]any{
			"evidence": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"required":             []string{"source", "fact"},
					"properties": map[string]any{
						"source": map[string]any{"type": "string"},
						"fact":   map[string]any{"type": "string"},
					},
				},
			},
			"rationale": map[string]any{"type": "string"},
			"intent":    intent,
		},
	}, nil
}

var _ llm.ReasoningEngine[struct{}] = (*Adapter[struct{}])(nil)
