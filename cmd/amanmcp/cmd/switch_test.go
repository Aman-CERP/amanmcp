package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Switch CLI Tests
// DEBT-028: Test coverage for switch command
// ============================================================================

func TestSwitchCmd_RequiresArgument(t *testing.T) {
	// Given: root command without argument
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"switch"})

	// When: running switch without argument
	err := cmd.Execute()

	// Then: should fail with missing argument error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts 1 arg", "should require exactly 1 argument")
}

func TestRunSwitch_SessionNotFound(t *testing.T) {
	// Given: empty sessions directory
	tmpDir := t.TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	require.NoError(t, os.MkdirAll(sessionsDir, 0755))

	// Override config to use temp directory
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_DATA_HOME", tmpDir)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"switch", "nonexistent"})

	// When: trying to switch to non-existent session
	err := cmd.Execute()

	// Then: should fail with session not found error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found", "should indicate session not found")
}
