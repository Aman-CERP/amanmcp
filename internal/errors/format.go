package errors

import (
	"encoding/json"
	"fmt"
	"strings"
)

// FormatForUser returns a user-friendly error message.
// If debug is true, includes additional technical details.
func FormatForUser(err error, debug bool) string {
	if err == nil {
		return ""
	}

	ae, ok := err.(*AmanError)
	if !ok {
		// Standard error - just return message
		return err.Error()
	}

	var sb strings.Builder

	// Main error message
	sb.WriteString("Error: ")
	sb.WriteString(ae.Message)
	sb.WriteString("\n")

	// Suggestion if available
	if ae.Suggestion != "" {
		sb.WriteString("\nSuggestion: ")
		sb.WriteString(ae.Suggestion)
		sb.WriteString("\n")
	}

	// Error code for reference
	sb.WriteString(fmt.Sprintf("\n[%s]", ae.Code))

	return sb.String()
}

// FormatForCLI formats an error for CLI output.
// Uses a concise format suitable for terminal display.
func FormatForCLI(err error) string {
	if err == nil {
		return ""
	}

	ae, ok := err.(*AmanError)
	if !ok {
		// Wrap standard error
		ae = Wrap(ErrCodeInternal, err)
	}

	var sb strings.Builder

	// Error message with code
	sb.WriteString(fmt.Sprintf("Error: %s\n", ae.Message))

	// Suggestion if available
	if ae.Suggestion != "" {
		sb.WriteString(fmt.Sprintf("  Hint: %s\n", ae.Suggestion))
	}

	// Code reference
	sb.WriteString(fmt.Sprintf("  Code: %s\n", ae.Code))

	return sb.String()
}

// jsonError is the JSON representation of an error.
type jsonError struct {
	Code       string            `json:"code"`
	Message    string            `json:"message"`
	Category   string            `json:"category"`
	Severity   string            `json:"severity"`
	Details    map[string]string `json:"details,omitempty"`
	Suggestion string            `json:"suggestion,omitempty"`
	Cause      string            `json:"cause,omitempty"`
	Retryable  bool              `json:"retryable"`
}

// FormatJSON returns a JSON representation of the error.
// Suitable for machine consumption and structured logging.
func FormatJSON(err error) ([]byte, error) {
	if err == nil {
		return json.Marshal(nil)
	}

	ae, ok := err.(*AmanError)
	if !ok {
		// Wrap standard error
		ae = Wrap(ErrCodeInternal, err)
	}

	je := jsonError{
		Code:       ae.Code,
		Message:    ae.Message,
		Category:   string(ae.Category),
		Severity:   string(ae.Severity),
		Details:    ae.Details,
		Suggestion: ae.Suggestion,
		Retryable:  ae.Retryable,
	}

	if ae.Cause != nil {
		je.Cause = ae.Cause.Error()
	}

	return json.Marshal(je)
}

// FormatForLog formats an error for structured logging.
// Returns key-value pairs suitable for slog attributes.
func FormatForLog(err error) map[string]any {
	if err == nil {
		return nil
	}

	ae, ok := err.(*AmanError)
	if !ok {
		return map[string]any{
			"error": err.Error(),
		}
	}

	result := map[string]any{
		"error_code": ae.Code,
		"message":    ae.Message,
		"category":   string(ae.Category),
		"severity":   string(ae.Severity),
		"retryable":  ae.Retryable,
	}

	if ae.Cause != nil {
		result["cause"] = ae.Cause.Error()
	}

	if ae.Suggestion != "" {
		result["suggestion"] = ae.Suggestion
	}

	for k, v := range ae.Details {
		result["detail_"+k] = v
	}

	return result
}
