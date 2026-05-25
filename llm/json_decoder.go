package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

// JSONDecoder decodes the canonical envelope shape
// `{evidence, rationale, intent}` into a ReasoningOutput with Kind=Intent.
// Provider adapters use it when the model returned content rather than a
// native tool-call.
type JSONDecoder[TIntent any] struct{}

func (JSONDecoder[TIntent]) Decode(content string) (ReasoningOutput[TIntent], error) {
	content = strings.TrimSpace(content)
	var envelope struct {
		Evidence  []EvidenceRef `json:"evidence"`
		Rationale string        `json:"rationale"`
		Intent    TIntent       `json:"intent"`
	}
	if err := json.Unmarshal([]byte(content), &envelope); err != nil {
		return ReasoningOutput[TIntent]{}, fmt.Errorf("decode reasoning output: %w: %s", err, content)
	}
	return ReasoningOutput[TIntent]{
		Kind:      ReasoningIntent,
		Evidence:  envelope.Evidence,
		Rationale: envelope.Rationale,
		Intent:    envelope.Intent,
	}, nil
}
