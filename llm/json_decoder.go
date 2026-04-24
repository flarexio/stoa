package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

// JSONDecoder decodes a JSON-encoded ReasoningResult.
// It is provider-neutral and can be reused by any adapter.
type JSONDecoder[TIntent any] struct{}

func (JSONDecoder[TIntent]) Decode(content string) (ReasoningResult[TIntent], error) {
	var result ReasoningResult[TIntent]
	content = strings.TrimSpace(content)
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return result, fmt.Errorf("decode reasoning result: %w: %s", err, content)
	}
	return result, nil
}
