package cmd

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Daemon CLI Tests
// DEBT-028: Test coverage for daemon commands
// ============================================================================

func TestDaemonCmd_HasSubcommands(t *testing.T) {
	// Given: root command
	cmd := NewRootCmd()

	// When: finding daemon command
	daemonCmd, _, err := cmd.Find([]string{"daemon"})
	require.NoError(t, err)

	// Then: daemon command should have subcommands
	subcommands := daemonCmd.Commands()
	assert.GreaterOrEqual(t, len(subcommands), 3, "daemon should have start, stop, status subcommands")

	// And: each subcommand should exist
	names := make(map[string]bool)
	for _, sc := range subcommands {
		names[sc.Name()] = true
	}
	assert.True(t, names["start"], "should have start command")
	assert.True(t, names["stop"], "should have stop command")
	assert.True(t, names["status"], "should have status command")
}

func TestDaemonStartCmd_HasForegroundFlag(t *testing.T) {
	// Given: root command
	cmd := NewRootCmd()

	// When: finding daemon start command
	startCmd, _, err := cmd.Find([]string{"daemon", "start"})
	require.NoError(t, err)

	// Then: should have --foreground/-f flag
	flag := startCmd.Flags().Lookup("foreground")
	assert.NotNil(t, flag, "should have --foreground flag")
	assert.Equal(t, "f", flag.Shorthand, "should have -f shorthand")
	assert.Equal(t, "false", flag.DefValue)
}

func TestDaemonStatusCmd_HasJSONFlag(t *testing.T) {
	// Given: root command
	cmd := NewRootCmd()

	// When: finding daemon status command
	statusCmd, _, err := cmd.Find([]string{"daemon", "status"})
	require.NoError(t, err)

	// Then: should have --json flag
	flag := statusCmd.Flags().Lookup("json")
	assert.NotNil(t, flag, "should have --json flag")
	assert.Equal(t, "false", flag.DefValue)
}

func TestRunDaemonStatus_NotRunning(t *testing.T) {
	// Given: daemon is not running (default state for tests)
	// Use a unique socket path to avoid interfering with real daemon
	t.Setenv("AMANMCP_DAEMON_SOCKET", "/tmp/amanmcp-test-nonexistent.sock")

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"daemon", "status"})

	// When: checking status
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := cmd.ExecuteContext(ctx)

	// Then: should succeed and report not running
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "not running", "should indicate daemon is not running")
}

func TestRunDaemonStatus_JSONOutput_NotRunning(t *testing.T) {
	// Given: daemon is not running
	t.Setenv("AMANMCP_DAEMON_SOCKET", "/tmp/amanmcp-test-nonexistent.sock")

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"daemon", "status", "--json"})

	// When: checking status with JSON output
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := cmd.ExecuteContext(ctx)

	// Then: should succeed and output JSON
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, `"running": false`, "JSON should indicate not running")
}

func TestRunDaemonStop_NotRunning(t *testing.T) {
	// Given: daemon is not running
	t.Setenv("AMANMCP_DAEMON_SOCKET", "/tmp/amanmcp-test-nonexistent.sock")
	t.Setenv("AMANMCP_DAEMON_PID", "/tmp/amanmcp-test-nonexistent.pid")

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"daemon", "stop"})

	// When: attempting to stop
	err := cmd.Execute()

	// Then: should succeed and report not running
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "not running", "should indicate daemon is not running")
}

func TestRunDaemonStart_AlreadyRunning(t *testing.T) {
	// This test would require mocking the daemon client
	// For now, we test the flag parsing and command structure
	t.Skip("Requires daemon mock infrastructure")
}
