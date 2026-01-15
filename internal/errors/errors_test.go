package errors

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TS01: Error wrapping preserves original error
func TestAmanError_Unwrap_PreservesOriginalError(t *testing.T) {
	// Given: an original error
	originalErr := errors.New("original error")

	// When: wrapping with AmanError
	amanErr := New(ErrCodeFileNotFound, "file not found: test.txt", originalErr)

	// Then: unwrapping returns original error
	require.NotNil(t, amanErr)
	assert.Equal(t, originalErr, errors.Unwrap(amanErr))
	assert.True(t, errors.Is(amanErr, originalErr))
}

func TestAmanError_Error_ReturnsFormattedMessage(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		message  string
		expected string
	}{
		{
			name:     "config error",
			code:     ErrCodeConfigNotFound,
			message:  "config file not found",
			expected: "[ERR_101_CONFIG_NOT_FOUND] config file not found",
		},
		{
			name:     "file error",
			code:     ErrCodeFileNotFound,
			message:  "file.go not found",
			expected: "[ERR_201_FILE_NOT_FOUND] file.go not found",
		},
		{
			name:     "network error",
			code:     ErrCodeNetworkTimeout,
			message:  "request timed out",
			expected: "[ERR_301_NETWORK_TIMEOUT] request timed out",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := New(tt.code, tt.message, nil)
			assert.Equal(t, tt.expected, err.Error())
		})
	}
}

func TestAmanError_Is_MatchesByCode(t *testing.T) {
	// Given: two errors with same code
	err1 := New(ErrCodeFileNotFound, "file A not found", nil)
	err2 := New(ErrCodeFileNotFound, "file B not found", nil)

	// Then: they match by code
	assert.True(t, errors.Is(err1, err2))
}

func TestAmanError_Is_DoesNotMatchDifferentCodes(t *testing.T) {
	// Given: two errors with different codes
	err1 := New(ErrCodeFileNotFound, "file not found", nil)
	err2 := New(ErrCodeConfigNotFound, "config not found", nil)

	// Then: they don't match
	assert.False(t, errors.Is(err1, err2))
}

func TestAmanError_WithDetails_AddsContext(t *testing.T) {
	// Given: a base error
	err := New(ErrCodeFileNotFound, "file not found", nil)

	// When: adding details
	err = err.WithDetail("path", "/foo/bar.go")
	err = err.WithDetail("size", "1024")

	// Then: details are available
	assert.Equal(t, "/foo/bar.go", err.Details["path"])
	assert.Equal(t, "1024", err.Details["size"])
}

func TestAmanError_WithSuggestion_AddsSuggestion(t *testing.T) {
	// Given: a network error
	err := New(ErrCodeNetworkTimeout, "connection timed out", nil)

	// When: adding suggestion
	err = err.WithSuggestion("Check your network connection")

	// Then: suggestion is available
	assert.Equal(t, "Check your network connection", err.Suggestion)
}

func TestAmanError_CategoryFromCode(t *testing.T) {
	tests := []struct {
		code         string
		wantCategory Category
	}{
		{ErrCodeConfigNotFound, CategoryConfig},
		{ErrCodeConfigInvalid, CategoryConfig},
		{ErrCodeFileNotFound, CategoryIO},
		{ErrCodeFilePermission, CategoryIO},
		{ErrCodeNetworkTimeout, CategoryNetwork},
		{ErrCodeNetworkUnavailable, CategoryNetwork},
		{ErrCodeInvalidInput, CategoryValidation},
		{ErrCodeDimensionMismatch, CategoryValidation},
		{ErrCodeInternal, CategoryInternal},
		{ErrCodeEmbeddingFailed, CategoryInternal},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			err := New(tt.code, "test message", nil)
			assert.Equal(t, tt.wantCategory, err.Category)
		})
	}
}

func TestAmanError_SeverityFromCode(t *testing.T) {
	tests := []struct {
		code         string
		wantSeverity Severity
	}{
		{ErrCodeCorruptIndex, SeverityFatal},
		{ErrCodeDiskFull, SeverityFatal},
		{ErrCodeFileNotFound, SeverityError},
		{ErrCodeNetworkTimeout, SeverityWarning}, // Retryable, so warning
		{ErrCodeNetworkUnavailable, SeverityWarning},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			err := New(tt.code, "test message", nil)
			assert.Equal(t, tt.wantSeverity, err.Severity)
		})
	}
}

func TestAmanError_RetryableFromCode(t *testing.T) {
	tests := []struct {
		code          string
		wantRetryable bool
	}{
		{ErrCodeNetworkTimeout, true},
		{ErrCodeNetworkUnavailable, true},
		{ErrCodeModelDownload, true},
		{ErrCodeFileNotFound, false},
		{ErrCodeConfigInvalid, false},
		{ErrCodeCorruptIndex, false},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			err := New(tt.code, "test message", nil)
			assert.Equal(t, tt.wantRetryable, err.Retryable)
		})
	}
}

func TestWrap_CreatesAmanErrorFromError(t *testing.T) {
	// Given: a standard error
	originalErr := errors.New("something went wrong")

	// When: wrapping with a code
	amanErr := Wrap(ErrCodeInternal, originalErr)

	// Then: creates proper AmanError
	require.NotNil(t, amanErr)
	assert.Equal(t, ErrCodeInternal, amanErr.Code)
	assert.Equal(t, "something went wrong", amanErr.Message)
	assert.Equal(t, originalErr, amanErr.Cause)
}

func TestConfigError_CreatesConfigCategoryError(t *testing.T) {
	err := ConfigError("invalid yaml syntax", nil)

	assert.Equal(t, CategoryConfig, err.Category)
	assert.Contains(t, err.Code, "CONFIG")
}

func TestIOError_CreatesIOCategoryError(t *testing.T) {
	err := IOError("cannot read file", nil)

	assert.Equal(t, CategoryIO, err.Category)
}

func TestNetworkError_CreatesRetryableError(t *testing.T) {
	err := NetworkError("connection refused", nil)

	assert.Equal(t, CategoryNetwork, err.Category)
	assert.True(t, err.Retryable)
}

func TestValidationError_CreatesValidationCategoryError(t *testing.T) {
	err := ValidationError("query cannot be empty", nil)

	assert.Equal(t, CategoryValidation, err.Category)
}

func TestIsRetryable_ChecksRetryableFlag(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "retryable AmanError",
			err:      New(ErrCodeNetworkTimeout, "timeout", nil),
			expected: true,
		},
		{
			name:     "non-retryable AmanError",
			err:      New(ErrCodeFileNotFound, "not found", nil),
			expected: false,
		},
		{
			name:     "wrapped retryable error",
			err:      Wrap(ErrCodeNetworkTimeout, errors.New("wrapped")),
			expected: true,
		},
		{
			name:     "standard error",
			err:      errors.New("standard error"),
			expected: false,
		},
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, IsRetryable(tt.err))
		})
	}
}

func TestIsFatal_ChecksFatalSeverity(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "fatal error",
			err:      New(ErrCodeCorruptIndex, "index corrupt", nil),
			expected: true,
		},
		{
			name:     "disk full error",
			err:      New(ErrCodeDiskFull, "no space left", nil),
			expected: true,
		},
		{
			name:     "non-fatal error",
			err:      New(ErrCodeFileNotFound, "not found", nil),
			expected: false,
		},
		{
			name:     "standard error",
			err:      errors.New("standard error"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, IsFatal(tt.err))
		})
	}
}
