package llm

import (
	"fmt"
	"strings"
)

// DefaultPromptRenderer renders a generic Stoa reasoning prompt. Feature
// packages should prefer their own renderers when they need domain-specific
// constraints, examples, or output shapes.
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

// RenderReasoningInput renders the task and instructions as a user-message body.
// The reply shape is enforced by the provider's structured-output mode (e.g.
// json_schema) and by registered tools, so it does not appear here.
func RenderReasoningInput(input ReasoningInput) string {
	var b strings.Builder
	b.WriteString("Task:\n")
	b.WriteString(strings.TrimSpace(input.Task))
	if strings.TrimSpace(input.Instructions) != "" {
		b.WriteString("\n\nFeature instructions:\n")
		b.WriteString(strings.TrimSpace(input.Instructions))
	}
	return b.String()
}

// RenderCycleEvent formats a CycleEvent as a tagged text block for the prompt.
func RenderCycleEvent(event CycleEvent) string {
	return fmt.Sprintf("[%s:%s]\n%s", event.Role, event.Kind, strings.TrimSpace(event.Content))
}

const defaultSystemPrompt = `You are a Stoa reasoning engine.

You propose a typed intent for Go code to validate; you do not execute actions.
Use only supplied facts as evidence.
If validation, execution, or tool-result feedback is present, correct the next intent accordingly.
When you need information before you can commit to an intent, call a registered tool instead.`
