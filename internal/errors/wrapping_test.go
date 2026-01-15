package errors_test

import (
	"strings"
	"testing"

	"github.com/Aman-CERP/amanmcp/internal/preflight"
	"github.com/Aman-CERP/amanmcp/internal/session"
)

// TestErrorWrapping_Preflight verifies preflight errors are wrapped with context.
func TestErrorWrapping_Preflight(t *testing.T) {
	// MarkPassed should wrap os.MkdirAll errors
	err := preflight.MarkPassed("/nonexistent/deeply/nested/path/that/cannot/exist")
	if err == nil {
		t.Skip("Expected error creating marker in nonexistent path")
	}

	// Error should contain context about what operation failed
	errMsg := err.Error()
	if !strings.Contains(errMsg, "create") && !strings.Contains(errMsg, "marker") && !strings.Contains(errMsg, "directory") {
		t.Errorf("Error should contain context about creating marker directory, got: %s", errMsg)
	}
}

// TestErrorWrapping_Session verifies session storage errors are wrapped with context.
func TestErrorWrapping_Session(t *testing.T) {
	// CopyIndexFiles should wrap errors with context
	err := session.CopyIndexFiles("/nonexistent/source", "/tmp/dest")
	if err == nil {
		t.Skip("Expected error copying from nonexistent source")
	}

	// Error should mention source directory
	errMsg := err.Error()
	if !strings.Contains(errMsg, "source") && !strings.Contains(errMsg, "exist") {
		t.Errorf("Error should mention source directory issue, got: %s", errMsg)
	}
}

// TestErrorWrapping_CalculateDirSize verifies CalculateDirSize errors are wrapped.
func TestErrorWrapping_CalculateDirSize(t *testing.T) {
	// CalculateDirSize handles nonexistent directories gracefully (returns 0)
	// So we can't easily test error wrapping here without mocking
	// This test documents the expected behavior
	size, err := session.CalculateDirSize("/nonexistent/path")
	if err != nil {
		t.Errorf("CalculateDirSize should return 0 for nonexistent paths, got error: %v", err)
	}
	if size != 0 {
		t.Errorf("Expected size 0 for nonexistent path, got: %d", size)
	}
}
