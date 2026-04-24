package coder

import (
	"fmt"
	"strings"

	"github.com/flarexio/stoa/icd"
	"github.com/flarexio/stoa/llm"
)

// PromptRenderer is the coder feature's llm.PromptRenderer. It injects the
// allowed code list and the exact JSON shape for icd.Intent so the provider
// adapter only translates messages, never knows about ICD-10.
type PromptRenderer struct {
	Dict icd.Dictionary
}

func (r PromptRenderer) Render(input llm.ReasoningInput) ([]llm.Message, error) {
	messages := []llm.Message{
		{Role: llm.MessageRoleSystem, Content: systemPrompt},
		{Role: llm.MessageRoleUser, Content: r.buildUserPrompt(input)},
	}

	for _, event := range input.Events {
		content := fmt.Sprintf("[%s:%s]\n%s", event.Role, event.Kind, strings.TrimSpace(event.Content))
		role := llm.MessageRoleUser
		if event.Role == llm.EventRoleAssistant {
			role = llm.MessageRoleAssistant
		}
		messages = append(messages, llm.Message{Role: role, Content: content})
	}

	return messages, nil
}

func (r PromptRenderer) buildUserPrompt(input llm.ReasoningInput) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(input.Task))
	if strings.TrimSpace(input.Instructions) != "" {
		b.WriteString("\n\nFeature instructions:\n")
		b.WriteString(strings.TrimSpace(input.Instructions))
	}

	if r.Dict != nil {
		b.WriteString("\n\nAllowed ICD-10 codes (use only these):\n")
		for _, code := range r.Dict.Codes() {
			desc, _ := r.Dict.Lookup(code)
			fmt.Fprintf(&b, "- %s: %s\n", code, desc)
		}
	}

	b.WriteString("\nReturn JSON with this exact shape:\n")
	b.WriteString(`{"evidence":[{"source":"note","fact":"..."}],"rationale":"...","intent":{"suggestions":[{"code":"I10","system":"ICD-10","description":"...","evidence_span":"verbatim phrase from the note","confidence":0.9}]}}`)
	return b.String()
}

const systemPrompt = `You are a clinical coding assistant working inside a validated agent harness.

Rules you must follow:
- Propose ICD-10 codes for the supplied clinical note.
- Use ONLY codes from the supplied allowed list. Any other code will be rejected.
- For each suggestion, copy evidence_span VERBATIM from the note. The harness checks it by substring match, so paraphrasing or translation will fail.
- Provide a confidence between 0 and 1.
- Do not diagnose, treat, or give medical advice. You are organizing documentation, not practicing medicine.
- If validation feedback is present in the message history, fix only the problems it names and resubmit.
- Output JSON only. No prose outside the JSON object.`
