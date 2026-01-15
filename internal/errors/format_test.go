package errors

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatForUser_BasicError(t *testing.T) {
	// Given: an AmanError
	err := New(ErrCodeFileNotFound, "file 'config.yaml' not found", nil)

	// When: formatting for user (no debug)
	result := FormatForUser(err, false)

	// Then: contains message
	assert.Contains(t, result, "file 'config.yaml' not found")
	// And: contains error code at end
	assert.Contains(t, result, "[ERR_201_FILE_NOT_FOUND]")
}

func TestFormatForUser_WithSuggestion(t *testing.T) {
	// Given: an error with suggestion
	err := New(ErrCodeNetworkUnavailable, "Ollama is not running", nil).
		WithSuggestion("Start Ollama with 'ollama serve' or use --offline flag")

	// When: formatting for user
	result := FormatForUser(err, false)

	// Then: contains suggestion
	assert.Contains(t, result, "Suggestion:")
	assert.Contains(t, result, "ollama serve")
}

func TestFormatForUser_NoStackTraceInNormalMode(t *testing.T) {
	// Given: an error
	err := New(ErrCodeInternal, "unexpected error", nil)

	// When: formatting without debug
	result := FormatForUser(err, false)

	// Then: no stack trace
	assert.NotContains(t, result, "Stack trace:")
	assert.NotContains(t, result, "goroutine")
}

func TestFormatForUser_StandardError(t *testing.T) {
	// Given: a standard Go error
	err := errors.New("something went wrong")

	// When: formatting for user
	result := FormatForUser(err, false)

	// Then: shows generic message
	assert.Contains(t, result, "something went wrong")
}

func TestFormatForUser_NilError(t *testing.T) {
	// When: formatting nil
	result := FormatForUser(nil, false)

	// Then: returns empty string
	assert.Empty(t, result)
}

func TestFormatJSON_BasicError(t *testing.T) {
	// Given: an AmanError with details
	err := New(ErrCodeFileNotFound, "file not found", nil).
		WithDetail("path", "/foo/bar.txt").
		WithSuggestion("Check the file path")

	// When: formatting as JSON
	data, jsonErr := FormatJSON(err)

	// Then: valid JSON
	require.NoError(t, jsonErr)

	var result map[string]any
	require.NoError(t, json.Unmarshal(data, &result))

	// And: contains expected fields
	assert.Equal(t, ErrCodeFileNotFound, result["code"])
	assert.Equal(t, "file not found", result["message"])
	assert.Equal(t, string(CategoryIO), result["category"])
	assert.Equal(t, string(SeverityError), result["severity"])
	assert.Equal(t, "Check the file path", result["suggestion"])

	details, ok := result["details"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "/foo/bar.txt", details["path"])
}

func TestFormatJSON_StandardError(t *testing.T) {
	// Given: a standard error
	err := errors.New("generic error")

	// When: formatting as JSON
	data, jsonErr := FormatJSON(err)

	// Then: valid JSON with internal error code
	require.NoError(t, jsonErr)

	var result map[string]any
	require.NoError(t, json.Unmarshal(data, &result))

	assert.Equal(t, ErrCodeInternal, result["code"])
	assert.Equal(t, "generic error", result["message"])
}

func TestFormatJSON_NilError(t *testing.T) {
	// When: formatting nil
	data, err := FormatJSON(nil)

	// Then: returns empty result
	assert.NoError(t, err)
	assert.Equal(t, "null", strings.TrimSpace(string(data)))
}

func TestFormatJSON_WithCause(t *testing.T) {
	// Given: an error with cause
	cause := errors.New("underlying error")
	err := New(ErrCodeInternal, "operation failed", cause)

	// When: formatting as JSON
	data, jsonErr := FormatJSON(err)

	// Then: includes cause
	require.NoError(t, jsonErr)

	var result map[string]any
	require.NoError(t, json.Unmarshal(data, &result))

	assert.Equal(t, "underlying error", result["cause"])
}

func TestFormatForCLI_FormatsWithColor(t *testing.T) {
	// Given: a fatal error
	err := New(ErrCodeCorruptIndex, "index is corrupted", nil).
		WithSuggestion("Run 'amanmcp reindex --force' to rebuild")

	// When: formatting for CLI
	result := FormatForCLI(err)

	// Then: contains error info
	assert.Contains(t, result, "index is corrupted")
	assert.Contains(t, result, "ERR_205_CORRUPT_INDEX")
}

func TestFormatForCLI_ShortFormat(t *testing.T) {
	// Given: a simple error
	err := New(ErrCodeFileNotFound, "file not found", nil)

	// When: formatting for CLI
	result := FormatForCLI(err)

	// Then: is concise
	lines := strings.Split(strings.TrimSpace(result), "\n")
	assert.LessOrEqual(t, len(lines), 5, "Should be concise")
}
