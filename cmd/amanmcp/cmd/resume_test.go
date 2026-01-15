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
// Resume CLI Tests
// DEBT-028: Test coverage for resume command
// ============================================================================

func TestResumeCmd_RequiresArgument(t *testing.T) {
	// Given: root command without argument
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"resume"})

	// When: running resume without argument
	err := cmd.Execute()

	// Then: should fail with missing argument error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts 1 arg", "should require exactly 1 argument")
}

func TestResumeCmd_HasTransportFlag(t *testing.T) {
	// Given: root command
	cmd := NewRootCmd()

	// When: finding resume command
	resumeCmd, _, err := cmd.Find([]string{"resume"})
	require.NoError(t, err)

	// Then: should have --transport flag with stdio default
	flag := resumeCmd.Flags().Lookup("transport")
	assert.NotNil(t, flag, "should have --transport flag")
	assert.Equal(t, "stdio", flag.DefValue, "default should be stdio")
}

func TestResumeCmd_HasPortFlag(t *testing.T) {
	// Given: root command
	cmd := NewRootCmd()

	// When: finding resume command
	resumeCmd, _, err := cmd.Find([]string{"resume"})
	require.NoError(t, err)

	// Then: should have --port flag with 8765 default
	flag := resumeCmd.Flags().Lookup("port")
	assert.NotNil(t, flag, "should have --port flag")
	assert.Equal(t, "8765", flag.DefValue, "default should be 8765")
}

func TestRunResume_SessionNotFound(t *testing.T) {
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
	cmd.SetArgs([]string{"resume", "nonexistent"})

	// When: trying to resume non-existent session
	err := cmd.Execute()

	// Then: should fail with session not found error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session not found", "should indicate session not found")
}
