package llm

import (
	"fmt"
	"strings"
)

// DefaultPromptRenderer renders a generic Stoa reasoning prompt.
// Feature packages should prefer their own renderers when they need domain-
// specific constraints, examples, or output shapes.
type DefaultPromptRenderer struct {
	SystemPrompt string
}

func (r DefaultPromptRenderer) Render(input ReasoningInput) ([]Message, error) {
	systemPrompt := strings.TrimSpace(r.SystemPrompt)
	if systemPrompt == "" {
		systemPrompt = defaultSystemPrompt
	}

	messages := []Message{
		{Role: MessageRoleSystem, Content: systemPrompt},
		{Role: MessageRoleUser, Content: RenderReasoningInput(input)},
	}

	for _, event := range input.Events {
		content := RenderCycleEvent(event)
		role := MessageRoleUser
		if event.Role == EventRoleAssistant {
			role = MessageRoleAssistant
		}
		messages = append(messages, Message{Role: role, Content: content})
	}

	return messages, nil
}

func RenderReasoningInput(input ReasoningInput) string {
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

func RenderCycleEvent(event CycleEvent) string {
	return fmt.Sprintf("[%s:%s]\n%s", event.Role, event.Kind, strings.TrimSpace(event.Content))
}

const defaultSystemPrompt = `You are a Stoa reasoning engine.

You must produce structured JSON only.
You do not execute actions.
You propose a typed intent for Go code to validate.
Use only supplied facts as evidence.
If validation or execution feedback is present, correct the next intent accordingly.
The top-level JSON object must contain evidence, rationale, and intent.`
