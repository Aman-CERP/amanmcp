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
// Compact CLI Tests
// DEBT-028: Test coverage for compact command
// ============================================================================

func TestCompactCmd_AcceptsOptionalPath(t *testing.T) {
	// Given: root command
	cmd := NewRootCmd()

	// When: finding compact command
	compactCmd, _, err := cmd.Find([]string{"compact"})
	require.NoError(t, err)

	// Then: should accept 0 or 1 args (MaximumNArgs(1))
	// Verify by trying to run with multiple args which should fail
	cmd2 := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd2.SetOut(buf)
	cmd2.SetErr(buf)
	cmd2.SetArgs([]string{"compact", "arg1", "arg2"})

	err = cmd2.Execute()
	require.Error(t, err, "should reject more than 1 argument")

	// Verify the command is found
	assert.NotNil(t, compactCmd)
}

func TestRunCompact_NoIndex(t *testing.T) {
	// Given: directory without index
	tmpDir := t.TempDir()

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"compact", tmpDir})

	// When: running compact on directory without index
	err := cmd.Execute()

	// Then: should fail with no index error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no index found", "should indicate no index found")
}

func TestRunCompact_InvalidPath(t *testing.T) {
	// Given: a file instead of directory
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "testfile.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("test"), 0644))

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"compact", filePath})

	// When: running compact on a file
	err := cmd.Execute()

	// Then: should fail with not a directory error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory", "should indicate path is not a directory")
}

func TestRunCompact_NonexistentPath(t *testing.T) {
	// Given: a path that doesn't exist
	tmpDir := t.TempDir()
	nonexistentPath := filepath.Join(tmpDir, "nonexistent")

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"compact", nonexistentPath})

	// When: running compact on nonexistent path
	err := cmd.Execute()

	// Then: should fail with access error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to access path", "should indicate path access failure")
}

func TestRunCompact_NoMetadata(t *testing.T) {
	// Given: directory with .amanmcp but no metadata.db
	tmpDir := t.TempDir()
	amanDir := filepath.Join(tmpDir, ".amanmcp")
	require.NoError(t, os.MkdirAll(amanDir, 0755))

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"compact", tmpDir})

	// When: running compact without metadata.db
	err := cmd.Execute()

	// Then: should fail with no index error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no index found", "should indicate no index found")
}
