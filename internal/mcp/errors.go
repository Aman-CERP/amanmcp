// Package mcp implements the Model Context Protocol (MCP) server for AmanMCP.
package mcp

import (
	"context"
	"errors"
	"fmt"

	amerrors "github.com/Aman-CERP/amanmcp/internal/errors"
)

// Custom MCP error codes for AmanMCP.
const (
	// ErrCodeIndexNotFound indicates no index exists for the project.
	ErrCodeIndexNotFound = -32001

	// ErrCodeEmbeddingFailed indicates embedding generation failed.
	ErrCodeEmbeddingFailed = -32002

	// ErrCodeTimeout indicates the request timed out.
	ErrCodeTimeout = -32003

	// ErrCodeFileNotFound indicates a file no longer exists on disk.
	ErrCodeFileNotFound = -32004

	// ErrCodeFileTooLarge indicates a file is too large to process.
	ErrCodeFileTooLarge = -32005

	// Standard JSON-RPC error codes.
	ErrCodeInvalidRequest  = -32600
	ErrCodeMethodNotFound  = -32601
	ErrCodeInvalidParams   = -32602
	ErrCodeInternalError   = -32603
)

// Sentinel errors for internal use.
var (
	// ErrIndexNotFound indicates no index exists for the project.
	ErrIndexNotFound = errors.New("index not found")

	// ErrEmbeddingFailed indicates embedding generation failed.
	ErrEmbeddingFailed = errors.New("embedding generation failed")

	// ErrFileTooLarge indicates a file is too large to process.
	ErrFileTooLarge = errors.New("file too large")

	// ErrToolNotFound indicates the requested tool does not exist.
	ErrToolNotFound = errors.New("tool not found")

	// ErrInvalidParams indicates invalid parameters were provided.
	ErrInvalidParams = errors.New("invalid parameters")

	// ErrResourceNotFound indicates the requested resource does not exist.
	ErrResourceNotFound = errors.New("resource not found")
)

// MCPError represents an MCP protocol error with code and message.
type MCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Error implements the error interface.
func (e *MCPError) Error() string {
	return fmt.Sprintf("MCP error %d: %s", e.Code, e.Message)
}

// MapError converts internal errors to MCP errors.
// It maps known error types to appropriate MCP error codes and messages.
func MapError(err error) *MCPError {
	if err == nil {
		return nil
	}

	// Check for AmanError first
	var amanErr *amerrors.AmanError
	if errors.As(err, &amanErr) {
		return mapAmanError(amanErr)
	}

	switch {
	case errors.Is(err, ErrIndexNotFound):
		return &MCPError{
			Code:    ErrCodeIndexNotFound,
			Message: "Index not found. Run 'amanmcp index' first.",
		}
	case errors.Is(err, ErrEmbeddingFailed):
		return &MCPError{
			Code:    ErrCodeEmbeddingFailed,
			Message: "Embedding generation failed. Using BM25-only results.",
		}
	case errors.Is(err, context.DeadlineExceeded):
		return &MCPError{
			Code:    ErrCodeTimeout,
			Message: "Request timed out.",
		}
	case errors.Is(err, context.Canceled):
		return &MCPError{
			Code:    ErrCodeTimeout,
			Message: "Request was canceled.",
		}
	case errors.Is(err, ErrFileTooLarge):
		return &MCPError{
			Code:    ErrCodeFileTooLarge,
			Message: "File is too large to process.",
		}
	case errors.Is(err, ErrToolNotFound):
		return &MCPError{
			Code:    ErrCodeMethodNotFound,
			Message: "Tool not found.",
		}
	case errors.Is(err, ErrInvalidParams):
		return &MCPError{
			Code:    ErrCodeInvalidParams,
			Message: "Invalid parameters.",
		}
	case errors.Is(err, ErrResourceNotFound):
		return &MCPError{
			Code:    ErrCodeMethodNotFound,
			Message: "Resource not found.",
		}
	default:
		return &MCPError{
			Code:    ErrCodeInternalError,
			Message: "Internal server error.",
		}
	}
}

// NewInvalidParamsError creates an error for invalid parameters with a custom message.
func NewInvalidParamsError(msg string) *MCPError {
	return &MCPError{
		Code:    ErrCodeInvalidParams,
		Message: msg,
	}
}

// NewMethodNotFoundError creates an error for unknown methods/tools.
func NewMethodNotFoundError(name string) *MCPError {
	return &MCPError{
		Code:    ErrCodeMethodNotFound,
		Message: fmt.Sprintf("Tool '%s' not found.", name),
	}
}

// NewResourceNotFoundError creates an error for unknown resources.
func NewResourceNotFoundError(uri string) *MCPError {
	return &MCPError{
		Code:    ErrCodeMethodNotFound,
		Message: fmt.Sprintf("Resource '%s' not found.", uri),
	}
}

// mapAmanError converts an AmanError to an MCPError.
func mapAmanError(ae *amerrors.AmanError) *MCPError {
	// Build message with suggestion if available
	message := ae.Message
	if ae.Suggestion != "" {
		message = fmt.Sprintf("%s %s", ae.Message, ae.Suggestion)
	}

	// Map category to MCP error code
	switch ae.Category {
	case amerrors.CategoryConfig:
		return &MCPError{
			Code:    ErrCodeInternalError,
			Message: message,
		}
	case amerrors.CategoryIO:
		switch ae.Code {
		case amerrors.ErrCodeFileNotFound:
			return &MCPError{
				Code:    ErrCodeFileNotFound,
				Message: message,
			}
		case amerrors.ErrCodeFileTooLarge:
			return &MCPError{
				Code:    ErrCodeFileTooLarge,
				Message: message,
			}
		case amerrors.ErrCodeCorruptIndex:
			return &MCPError{
				Code:    ErrCodeIndexNotFound,
				Message: message,
			}
		default:
			return &MCPError{
				Code:    ErrCodeInternalError,
				Message: message,
			}
		}
	case amerrors.CategoryNetwork:
		return &MCPError{
			Code:    ErrCodeTimeout,
			Message: message,
		}
	case amerrors.CategoryValidation:
		return &MCPError{
			Code:    ErrCodeInvalidParams,
			Message: message,
		}
	default: // CategoryInternal and unknown
		return &MCPError{
			Code:    ErrCodeInternalError,
			Message: message,
		}
	}
}
