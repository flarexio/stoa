// Package openai is the OpenAI adapter for llm.ReasoningEngine.
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
	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/shared"

	"github.com/flarexio/stoa/llm"
)

type OutputFormat string

const (
	OutputFormatText       OutputFormat = "text"
	OutputFormatJSONObject OutputFormat = "json_object"
	OutputFormatJSONSchema OutputFormat = "json_schema"
)

// envelopeSchemaName is the name OpenAI requires on a json_schema response
// format; it has no semantic meaning beyond appearing in error messages.
const envelopeSchemaName = "stoa_reasoning_envelope"

// Config contains only provider concerns; domain rules and feature prompts
// enter through llm.ReasoningInput.
type Config[TIntent any] struct {
	APIKey  string
	BaseURL string
	Model   string

	// OutputFormat overrides the auto-selected response_format. When empty,
	// NewAdapter picks json_schema if IntentSchema is set, json_object if a
	// default JSON decoder is used, or none if a custom decoder is supplied.
	OutputFormat OutputFormat

	// IntentSchema is the JSON Schema for the intent payload only. When set,
	// the adapter wraps it in the canonical {evidence, rationale, intent}
	// envelope and requests json_schema structured outputs in strict mode.
	IntentSchema json.RawMessage

	Renderer llm.PromptRenderer
	Decoder  llm.Decoder[TIntent]
}

// Adapter implements llm.ReasoningEngine using the official OpenAI Go SDK.
type Adapter[TIntent any] struct {
	client       openai.Client
	model        string
	outputFormat OutputFormat
	envelope     any // assembled response_format schema; nil when not json_schema
	renderer     llm.PromptRenderer
	decoder      llm.Decoder[TIntent]
}

// NewAdapter wires the SDK client and the renderer/decoder pair. APIKey and
// BaseURL default to $OPENAI_API_KEY and $OPENAI_BASE_URL respectively; explicit
// config values take precedence over environment variables.
// Renderer and Decoder default to llm's generic implementations.
func NewAdapter[TIntent any](cfg Config[TIntent]) (*Adapter[TIntent], error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	}
	if apiKey == "" {
		return nil, errors.New("OPENAI_API_KEY is not set")
	}

	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("OPENAI_BASE_URL"))
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
	customDecoder := decoder != nil
	if decoder == nil {
		decoder = llm.JSONDecoder[TIntent]{}
	}

	outputFormat := cfg.OutputFormat
	if outputFormat == "" {
		switch {
		case len(cfg.IntentSchema) > 0:
			outputFormat = OutputFormatJSONSchema
		case customDecoder:
			// custom decoder: caller drives format via prompt; we set nothing.
		default:
			outputFormat = OutputFormatJSONObject
		}
	}

	var envelope any
	if outputFormat == OutputFormatJSONSchema {
		if len(cfg.IntentSchema) == 0 {
			return nil, errors.New("openai: OutputFormatJSONSchema requires IntentSchema")
		}
		built, err := buildEnvelopeSchema(cfg.IntentSchema)
		if err != nil {
			return nil, fmt.Errorf("openai: invalid IntentSchema: %w", err)
		}
		envelope = built
	}

	clientOpts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL != "" {
		clientOpts = append(clientOpts, option.WithBaseURL(baseURL))
	}

	return &Adapter[TIntent]{
		client:       openai.NewClient(clientOpts...),
		model:        model,
		outputFormat: outputFormat,
		envelope:     envelope,
		renderer:     renderer,
		decoder:      decoder,
	}, nil
}

// Predict asks OpenAI for a structured reasoning output. It does not validate
// the intent; validation belongs to domain code after this call returns.
func (a *Adapter[TIntent]) Predict(ctx context.Context, input llm.ReasoningInput) (llm.ReasoningOutput[TIntent], error) {
	var zero llm.ReasoningOutput[TIntent]

	messages, err := a.messages(input)
	if err != nil {
		return zero, err
	}

	params := openai.ChatCompletionNewParams{
		Messages: messages,
		Model:    openai.ChatModel(a.model),
	}

	switch a.outputFormat {
	case OutputFormatJSONObject:
		params.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONObject: &shared.ResponseFormatJSONObjectParam{Type: "json_object"},
		}
	case OutputFormatJSONSchema:
		params.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &shared.ResponseFormatJSONSchemaParam{
				Type: "json_schema",
				JSONSchema: shared.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:   envelopeSchemaName,
					Strict: param.NewOpt(true),
					Schema: a.envelope,
				},
			},
		}
	}

	if tools, err := translateTools(input.Tools); err != nil {
		return zero, err
	} else if len(tools) > 0 {
		params.Tools = tools
	}

	resp, err := a.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return zero, fmt.Errorf("openai chat completion failed: %w", err)
	}
	if len(resp.Choices) == 0 {
		return zero, errors.New("openai chat completion returned no choices")
	}

	message := resp.Choices[0].Message
	if len(message.ToolCalls) > 0 {
		calls := make([]llm.ToolCall, 0, len(message.ToolCalls))
		for _, c := range message.ToolCalls {
			calls = append(calls, llm.ToolCall{
				Name: c.Function.Name,
				Args: json.RawMessage(c.Function.Arguments),
			})
		}
		return llm.ToolCallsOutput[TIntent](calls, nil, strings.TrimSpace(message.Content)), nil
	}

	content := strings.TrimSpace(message.Content)
	if content == "" {
		return zero, errors.New("openai chat completion returned empty content")
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

// translateTools converts harness tool specs into the SDK's native tool param
// list. Each spec's ArgsSchema is treated as a JSON-Schema object.
func translateTools(specs []llm.ToolSpec) ([]openai.ChatCompletionToolParam, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	tools := make([]openai.ChatCompletionToolParam, 0, len(specs))
	for _, spec := range specs {
		if strings.TrimSpace(spec.Name) == "" {
			return nil, errors.New("openai: tool spec missing name")
		}
		fn := shared.FunctionDefinitionParam{Name: spec.Name}
		if spec.Description != "" {
			fn.Description = param.NewOpt(spec.Description)
		}
		if len(spec.ArgsSchema) > 0 {
			var params shared.FunctionParameters
			if err := json.Unmarshal(spec.ArgsSchema, &params); err != nil {
				return nil, fmt.Errorf("openai: tool %q has invalid ArgsSchema: %w", spec.Name, err)
			}
			fn.Parameters = params
			fn.Strict = param.NewOpt(true)
		}
		tools = append(tools, openai.ChatCompletionToolParam{Function: fn})
	}
	return tools, nil
}

// buildEnvelopeSchema wraps the caller-supplied intent schema in the canonical
// {evidence, rationale, intent} envelope OpenAI structured outputs requires.
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
