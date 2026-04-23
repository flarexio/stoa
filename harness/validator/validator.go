package validator

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Intent is the interface that all structured intents from LLM must satisfy.
type Intent interface {
	// Validate performs domain-specific validation.
	Validate(ctx context.Context) error
}

// ValidationError represents a domain rule violation formatted for LLM feedback.
type ValidationError struct {
	Field   string
	Message string
	Value   any
}

func (e *ValidationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("invalid value '%v' for field '%s': %s", e.Value, e.Field, e.Message)
	}
	return e.Message
}

// FormatForLLM converts validation errors into a clear instruction for self-correction.
func FormatForLLM(err error) string {
	var valErr *ValidationError
	if errors.As(err, &valErr) {
		return fmt.Sprintf("Validation failed: %s. Please correct this and try again.", valErr.Error())
	}
	
	// Handle multiple errors if they are joined
	return fmt.Sprintf("Validation failed: %v. Please adjust your reasoning.", err)
}

// Wrap many errors for the LLM
func Join(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	var msg []string
	for _, e := range errs {
		msg = append(msg, e.Error())
	}
	return errors.New(strings.Join(msg, "; "))
}
