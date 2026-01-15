package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Index Info CLI Tests
// DEBT-028: Test coverage for index info command
// ============================================================================

func TestIndexInfoCmd_HasJSONFlag(t *testing.T) {
	// Given: root command
	cmd := NewRootCmd()

	// When: finding index info command
	infoCmd, _, err := cmd.Find([]string{"index", "info"})
	require.NoError(t, err)

	// Then: should have --json flag
	flag := infoCmd.Flags().Lookup("json")
	assert.NotNil(t, flag, "should have --json flag")
	assert.Equal(t, "false", flag.DefValue, "default should be false")
}

func TestIndexInfoCmd_AcceptsOptionalPath(t *testing.T) {
	// Given: root command
	cmd := NewRootCmd()

	// When: finding index info command
	infoCmd, _, err := cmd.Find([]string{"index", "info"})
	require.NoError(t, err)

	// Then: should be valid command
	assert.NotNil(t, infoCmd)

	// And: verify by trying to run with multiple args which should fail
	cmd2 := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd2.SetOut(buf)
	cmd2.SetErr(buf)
	cmd2.SetArgs([]string{"index", "info", "arg1", "arg2"})

	err = cmd2.Execute()
	require.Error(t, err, "should reject more than 1 argument")
}

func TestRunIndexInfo_NoIndex(t *testing.T) {
	// Given: directory without index
	tmpDir := t.TempDir()

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", "info", tmpDir})

	// When: running index info on directory without index
	err := cmd.Execute()

	// Then: should fail with no index error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no index found", "should indicate no index found")
}

func TestRunIndexInfo_NonexistentPath(t *testing.T) {
	// Note: For nonexistent paths, the command will look for project root
	// and fall back to using the path directly. Since .amanmcp/metadata.db
	// won't exist, it will return "no index found" error.

	// Given: a nonexistent path
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", "info", "/nonexistent/path/xyz123"})

	// When: running index info on nonexistent path
	err := cmd.Execute()

	// Then: should fail (either path error or no index error)
	require.Error(t, err)
}
