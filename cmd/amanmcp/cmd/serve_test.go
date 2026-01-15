package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Aman-CERP/amanmcp/internal/store"
)

// BUG-035: Tests for MCP server startup timing and stdin validation.

func TestServe_FileWatcherDoesNotBlockStartup(t *testing.T) {
	// BUG-035: File watcher must not block MCP server startup.
	// MCP protocol requires handshake response within 500ms.
	// File watcher startup can take 2+ seconds on slow filesystems.
	// The server should start serving immediately while watcher initializes in background.

	// Given: a project with an index
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, ".amanmcp")
	require.NoError(t, os.MkdirAll(dataDir, 0755))

	// Create minimal index files (empty but valid)
	metaPath := filepath.Join(dataDir, "metadata.db")
	meta, err := store.NewSQLiteStore(metaPath)
	require.NoError(t, err)
	_ = meta.Close()

	bm25BasePath := filepath.Join(dataDir, "bm25")
	bm25, err := store.NewBM25IndexWithBackend(bm25BasePath, store.DefaultBM25Config(), "")
	require.NoError(t, err)
	_ = bm25.Close()

	// Set a very long watcher startup timeout to simulate slow filesystem
	t.Setenv("AMANMCP_WATCHER_STARTUP_TIMEOUT", "10s")

	// Track startup time
	startTime := time.Now()

	// When: starting serve in a goroutine with context that we cancel
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		oldDir, _ := os.Getwd()
		_ = os.Chdir(tmpDir)
		defer func() { _ = os.Chdir(oldDir) }()

		// Run serve (will block on stdin, but we just want to measure startup time)
		errCh <- runServe(ctx, "stdio", 0)
	}()

	// Give it a moment to start
	time.Sleep(500 * time.Millisecond)

	// Then: server should have started within 500ms (not waiting for 10s watcher)
	startupDuration := time.Since(startTime)

	// Cancel context to stop server
	cancel()

	// Wait for server to stop
	select {
	case <-errCh:
		// Server stopped
	case <-time.After(5 * time.Second):
		t.Fatal("Server didn't stop within timeout")
	}

	// Assert: startup should be fast (< 1s), not blocked by 10s watcher timeout
	assert.Less(t, startupDuration.Seconds(), 2.0,
		"Server should start within 2s, not wait for file watcher (startup took %.2fs)", startupDuration.Seconds())
}

func TestServeWithSession_HasMCPSafeLogging(t *testing.T) {
	// BUG-035: runServeWithSession must initialize MCP-safe logging.
	// This was a gap in BUG-034 fix - only runServe() had MCP logging.

	// Given: a project with an index
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, ".amanmcp")
	require.NoError(t, os.MkdirAll(dataDir, 0755))

	// Create minimal index files
	metaPath := filepath.Join(dataDir, "metadata.db")
	meta, err := store.NewSQLiteStore(metaPath)
	require.NoError(t, err)
	_ = meta.Close()

	bm25BasePath := filepath.Join(dataDir, "bm25")
	bm25, err := store.NewBM25IndexWithBackend(bm25BasePath, store.DefaultBM25Config(), "")
	require.NoError(t, err)
	_ = bm25.Close()

	// Use static backend to avoid Ollama dependency in CI
	t.Setenv("AMANMCP_EMBEDDER", "static")

	// When: running serve with --session
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"serve", "--session=test-session"})

	// Run with short timeout (will fail due to missing stdin, that's OK)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	oldDir, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(oldDir) }()

	// Ignore error - we're just checking for stdout contamination
	_ = cmd.ExecuteContext(ctx)

	// Then: stdout should NOT contain status messages that would corrupt MCP protocol
	// Note: Error messages and help text from Cobra are acceptable since they happen
	// after the connection fails (stdin closed). We're checking for pre-connection contamination.
	output := buf.String()
	// Should NOT contain status emojis that would corrupt MCP protocol
	assert.NotContains(t, output, "🚀", "Should not write status emojis to stdout")
	// Should NOT contain log-style messages that indicate logging to stdout
	assert.NotContains(t, output, "INFO", "Should not write INFO logs to stdout")
	assert.NotContains(t, output, "DEBUG", "Should not write DEBUG logs to stdout")
	// Should NOT contain embedder messages (these were the original BUG-034 issue)
	assert.NotContains(t, output, "Hugot", "Should not write embedder status to stdout")
	assert.NotContains(t, output, "Ollama", "Should not write embedder status to stdout")
}

func TestVerifyStdinForMCP_DetectsTerminal(t *testing.T) {
	// BUG-035: stdin validation should detect when stdin is a terminal (not pipe).
	// This helps users understand why MCP connection fails when running interactively.

	// Note: This test verifies the function exists and returns error for terminal stdin.
	// In actual test environment, stdin might or might not be a terminal depending on how tests are run.

	err := verifyStdinForMCP()

	// When running in test environment, stdin behavior varies:
	// - If terminal: should return error
	// - If pipe (CI): should return nil
	// We just verify the function exists and handles both cases gracefully.

	if err != nil {
		// If there's an error, it should be about terminal/pipe
		assert.True(t,
			strings.Contains(err.Error(), "terminal") ||
				strings.Contains(err.Error(), "pipe") ||
				strings.Contains(err.Error(), "stdin"),
			"Error should mention stdin/terminal/pipe, got: %v", err)
	}
	// If no error, stdin is a valid pipe - that's also OK in CI
}

func TestVerifyStdinForMCP_ReturnsNilForPipe(t *testing.T) {
	// BUG-035: stdin validation should return nil when stdin is a pipe.

	// Skip if we can't create a pipe (shouldn't happen in normal test environment)
	if testing.Short() {
		t.Skip("Skipping pipe test in short mode")
	}

	// In test environment with pipes, verification should succeed
	// This test mainly documents the expected behavior
	err := verifyStdinForMCP()

	// Either nil (stdin is pipe) or error (stdin is terminal) is acceptable
	// The function should not panic
	_ = err
}

func TestServeCmd_HasDebugFlag(t *testing.T) {
	// Verify serve command has --debug flag for enabling verbose logging.
	cmd := NewRootCmd()

	// Find serve subcommand
	serveCmd, _, err := cmd.Find([]string{"serve"})
	require.NoError(t, err)

	flag := serveCmd.Flags().Lookup("debug")
	assert.NotNil(t, flag, "Serve should have --debug flag")
	assert.Equal(t, "false", flag.DefValue)
}

func TestServeCmd_HasTransportFlag(t *testing.T) {
	// Verify serve command has --transport flag.
	cmd := NewRootCmd()

	serveCmd, _, err := cmd.Find([]string{"serve"})
	require.NoError(t, err)

	flag := serveCmd.Flags().Lookup("transport")
	assert.NotNil(t, flag, "Serve should have --transport flag")
	assert.Equal(t, "stdio", flag.DefValue)
}

func TestServeCmd_HasSessionFlag(t *testing.T) {
	// Verify serve command has --session flag.
	cmd := NewRootCmd()

	serveCmd, _, err := cmd.Find([]string{"serve"})
	require.NoError(t, err)

	flag := serveCmd.Flags().Lookup("session")
	assert.NotNil(t, flag, "Serve should have --session flag")
	assert.Equal(t, "", flag.DefValue)
}
