package mcp

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	amerrors "github.com/Aman-CERP/amanmcp/internal/errors"
)

func TestMapError_NilError(t *testing.T) {
	// Given: nil error
	var err error = nil

	// When: mapping the error
	result := MapError(err)

	// Then: returns nil
	assert.Nil(t, result)
}

func TestMapError_IndexNotFound(t *testing.T) {
	// Given: index not found error
	err := ErrIndexNotFound

	// When: mapping the error
	result := MapError(err)

	// Then: returns correct MCP error
	require.NotNil(t, result)
	assert.Equal(t, ErrCodeIndexNotFound, result.Code)
	assert.Contains(t, result.Message, "Index not found")
}

func TestMapError_EmbeddingFailed(t *testing.T) {
	// Given: embedding failed error
	err := ErrEmbeddingFailed

	// When: mapping the error
	result := MapError(err)

	// Then: returns correct MCP error
	require.NotNil(t, result)
	assert.Equal(t, ErrCodeEmbeddingFailed, result.Code)
	assert.Contains(t, result.Message, "Embedding")
}

func TestMapError_DeadlineExceeded(t *testing.T) {
	// Given: deadline exceeded error
	err := context.DeadlineExceeded

	// When: mapping the error
	result := MapError(err)

	// Then: returns timeout error
	require.NotNil(t, result)
	assert.Equal(t, ErrCodeTimeout, result.Code)
	assert.Contains(t, result.Message, "timed out")
}

func TestMapError_Canceled(t *testing.T) {
	// Given: context canceled error
	err := context.Canceled

	// When: mapping the error
	result := MapError(err)

	// Then: returns timeout error
	require.NotNil(t, result)
	assert.Equal(t, ErrCodeTimeout, result.Code)
	assert.Contains(t, result.Message, "canceled")
}

func TestMapError_ToolNotFound(t *testing.T) {
	// Given: tool not found error
	err := ErrToolNotFound

	// When: mapping the error
	result := MapError(err)

	// Then: returns method not found error
	require.NotNil(t, result)
	assert.Equal(t, ErrCodeMethodNotFound, result.Code)
}

func TestMapError_InvalidParams(t *testing.T) {
	// Given: invalid params error
	err := ErrInvalidParams

	// When: mapping the error
	result := MapError(err)

	// Then: returns invalid params error
	require.NotNil(t, result)
	assert.Equal(t, ErrCodeInvalidParams, result.Code)
}

func TestMapError_UnknownError(t *testing.T) {
	// Given: unknown error
	err := errors.New("some unknown error")

	// When: mapping the error
	result := MapError(err)

	// Then: returns internal error
	require.NotNil(t, result)
	assert.Equal(t, ErrCodeInternalError, result.Code)
	assert.Contains(t, result.Message, "Internal server error")
}

func TestMapError_WrappedError(t *testing.T) {
	// Given: wrapped index not found error
	err := fmt.Errorf("failed to search: %w", ErrIndexNotFound)

	// When: mapping the error
	result := MapError(err)

	// Then: correctly identifies the wrapped error
	require.NotNil(t, result)
	assert.Equal(t, ErrCodeIndexNotFound, result.Code)
}

func TestMCPError_Error(t *testing.T) {
	// Given: an MCP error
	err := &MCPError{
		Code:    ErrCodeInvalidParams,
		Message: "missing required field",
	}

	// When: calling Error()
	msg := err.Error()

	// Then: returns formatted message
	assert.Contains(t, msg, "MCP error")
	assert.Contains(t, msg, "-32602")
	assert.Contains(t, msg, "missing required field")
}

func TestNewInvalidParamsError(t *testing.T) {
	// Given: a custom message
	msg := "query parameter is required"

	// When: creating invalid params error
	err := NewInvalidParamsError(msg)

	// Then: returns error with custom message
	assert.Equal(t, ErrCodeInvalidParams, err.Code)
	assert.Equal(t, msg, err.Message)
}

func TestNewMethodNotFoundError(t *testing.T) {
	// Given: a tool name
	name := "unknown_tool"

	// When: creating method not found error
	err := NewMethodNotFoundError(name)

	// Then: returns error with tool name
	assert.Equal(t, ErrCodeMethodNotFound, err.Code)
	assert.Contains(t, err.Message, name)
}

func TestNewResourceNotFoundError(t *testing.T) {
	// Given: a resource URI
	uri := "file://src/main.go"

	// When: creating resource not found error
	err := NewResourceNotFoundError(uri)

	// Then: returns error with URI
	assert.Equal(t, ErrCodeMethodNotFound, err.Code)
	assert.Contains(t, err.Message, uri)
}

// TS09: MCP error codes correct for AmanError
func TestMapError_AmanError_FileNotFound(t *testing.T) {
	// Given: an AmanError with file not found code
	err := amerrors.New(amerrors.ErrCodeFileNotFound, "file 'config.yaml' not found", nil)

	// When: mapping the error
	result := MapError(err)

	// Then: returns correct MCP error
	require.NotNil(t, result)
	assert.Equal(t, ErrCodeFileNotFound, result.Code)
	assert.Contains(t, result.Message, "config.yaml")
}

func TestMapError_AmanError_NetworkTimeout(t *testing.T) {
	// Given: an AmanError with network timeout
	err := amerrors.New(amerrors.ErrCodeNetworkTimeout, "connection timed out", nil)

	// When: mapping the error
	result := MapError(err)

	// Then: returns timeout error
	require.NotNil(t, result)
	assert.Equal(t, ErrCodeTimeout, result.Code)
}

func TestMapError_AmanError_ValidationError(t *testing.T) {
	// Given: an AmanError with validation error
	err := amerrors.New(amerrors.ErrCodeInvalidInput, "query cannot be empty", nil)

	// When: mapping the error
	result := MapError(err)

	// Then: returns invalid params error
	require.NotNil(t, result)
	assert.Equal(t, ErrCodeInvalidParams, result.Code)
}

func TestMapError_AmanError_WithSuggestion(t *testing.T) {
	// Given: an AmanError with suggestion
	err := amerrors.New(amerrors.ErrCodeFileNotFound, "file not found", nil).
		WithSuggestion("Check the file path exists")

	// When: mapping the error
	result := MapError(err)

	// Then: message includes suggestion
	require.NotNil(t, result)
	assert.Contains(t, result.Message, "file not found")
	assert.Contains(t, result.Message, "Check the file path")
}

func TestMapError_AmanError_Internal(t *testing.T) {
	// Given: an internal AmanError
	err := amerrors.New(amerrors.ErrCodeInternal, "unexpected error", nil)

	// When: mapping the error
	result := MapError(err)

	// Then: returns internal error
	require.NotNil(t, result)
	assert.Equal(t, ErrCodeInternalError, result.Code)
}

func TestMapError_WrappedAmanError(t *testing.T) {
	// Given: a wrapped AmanError
	amanErr := amerrors.New(amerrors.ErrCodeNetworkTimeout, "timeout", nil)
	err := fmt.Errorf("operation failed: %w", amanErr)

	// When: mapping the error
	result := MapError(err)

	// Then: correctly identifies the wrapped AmanError
	require.NotNil(t, result)
	assert.Equal(t, ErrCodeTimeout, result.Code)
}
