package errors

import (
	"fmt"
)

// AmanError is the structured error type for AmanMCP.
// It provides rich context for error handling, logging, and user presentation.
type AmanError struct {
	// Code is the unique error code (e.g., "ERR_201_FILE_NOT_FOUND").
	Code string

	// Message is the human-readable error message.
	Message string

	// Category is the error category (Config, IO, Network, etc.).
	Category Category

	// Severity is the error severity level.
	Severity Severity

	// Details contains additional context as key-value pairs.
	Details map[string]string

	// Cause is the underlying error that caused this error.
	Cause error

	// Retryable indicates if the operation can be retried.
	Retryable bool

	// Suggestion is an actionable suggestion for the user.
	Suggestion string
}

// Error implements the error interface.
func (e *AmanError) Error() string {
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Unwrap returns the underlying cause for error chain support.
func (e *AmanError) Unwrap() error {
	return e.Cause
}

// Is checks if this error matches the target error by code.
// This enables errors.Is() to work with AmanError.
func (e *AmanError) Is(target error) bool {
	if t, ok := target.(*AmanError); ok {
		return e.Code == t.Code
	}
	return false
}

// WithDetail adds a key-value detail to the error.
// Returns the error for method chaining.
func (e *AmanError) WithDetail(key, value string) *AmanError {
	if e.Details == nil {
		e.Details = make(map[string]string)
	}
	e.Details[key] = value
	return e
}

// WithSuggestion adds an actionable suggestion for the user.
// Returns the error for method chaining.
func (e *AmanError) WithSuggestion(suggestion string) *AmanError {
	e.Suggestion = suggestion
	return e
}

// New creates a new AmanError with the given code and message.
// Category, severity, and retryable flag are derived from the code.
func New(code string, message string, cause error) *AmanError {
	return &AmanError{
		Code:      code,
		Message:   message,
		Category:  categoryFromCode(code),
		Severity:  severityFromCode(code),
		Cause:     cause,
		Retryable: isRetryableCode(code),
	}
}

// Wrap creates an AmanError from an existing error.
// The error's message becomes the AmanError message.
func Wrap(code string, err error) *AmanError {
	if err == nil {
		return nil
	}
	return New(code, err.Error(), err)
}

// ConfigError creates a configuration-related error.
func ConfigError(message string, cause error) *AmanError {
	return New(ErrCodeConfigInvalid, message, cause)
}

// IOError creates an I/O-related error.
func IOError(message string, cause error) *AmanError {
	return New(ErrCodeFileNotFound, message, cause)
}

// NetworkError creates a network-related error.
// Network errors are typically retryable.
func NetworkError(message string, cause error) *AmanError {
	return New(ErrCodeNetworkTimeout, message, cause)
}

// ValidationError creates a validation-related error.
func ValidationError(message string, cause error) *AmanError {
	return New(ErrCodeInvalidInput, message, cause)
}

// InternalError creates an internal error.
func InternalError(message string, cause error) *AmanError {
	return New(ErrCodeInternal, message, cause)
}

// IsRetryable checks if an error is retryable.
// Returns true if the error is an AmanError with Retryable flag set.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	if ae, ok := err.(*AmanError); ok {
		return ae.Retryable
	}
	return false
}

// IsFatal checks if an error has fatal severity.
// Fatal errors should abort the current operation.
func IsFatal(err error) bool {
	if err == nil {
		return false
	}
	if ae, ok := err.(*AmanError); ok {
		return ae.Severity == SeverityFatal
	}
	return false
}

// GetCode extracts the error code from an AmanError.
// Returns empty string if not an AmanError.
func GetCode(err error) string {
	if ae, ok := err.(*AmanError); ok {
		return ae.Code
	}
	return ""
}

// GetCategory extracts the category from an AmanError.
// Returns empty string if not an AmanError.
func GetCategory(err error) Category {
	if ae, ok := err.(*AmanError); ok {
		return ae.Category
	}
	return ""
}
