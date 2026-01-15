package preflight

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNeedsCheck_NoMarker(t *testing.T) {
	// Given: a directory without marker file
	tmpDir := t.TempDir()

	// When: checking if needs check
	needs := NeedsCheck(tmpDir)

	// Then: returns true
	assert.True(t, needs)
}

func TestNeedsCheck_WithMarker(t *testing.T) {
	// Given: a directory with marker file
	tmpDir := t.TempDir()
	require.NoError(t, MarkPassed(tmpDir))

	// When: checking if needs check
	needs := NeedsCheck(tmpDir)

	// Then: returns false
	assert.False(t, needs)
}

func TestMarkPassed_CreatesFile(t *testing.T) {
	// Given: an empty directory
	tmpDir := t.TempDir()

	// When: marking as passed
	err := MarkPassed(tmpDir)

	// Then: marker file exists
	require.NoError(t, err)
	markerPath := filepath.Join(tmpDir, MarkerFile)
	assert.FileExists(t, markerPath)

	// And: contains a valid timestamp
	content, err := os.ReadFile(markerPath)
	require.NoError(t, err)
	_, err = time.Parse(time.RFC3339, string(content))
	assert.NoError(t, err)
}

func TestMarkPassed_CreatesDataDir(t *testing.T) {
	// Given: a non-existent data directory
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "subdir", ".amanmcp")

	// When: marking as passed
	err := MarkPassed(dataDir)

	// Then: directory and marker file are created
	require.NoError(t, err)
	assert.DirExists(t, dataDir)
	assert.FileExists(t, filepath.Join(dataDir, MarkerFile))
}

func TestClearMarker_RemovesFile(t *testing.T) {
	// Given: a directory with marker file
	tmpDir := t.TempDir()
	require.NoError(t, MarkPassed(tmpDir))
	markerPath := filepath.Join(tmpDir, MarkerFile)
	require.FileExists(t, markerPath)

	// When: clearing marker
	err := ClearMarker(tmpDir)

	// Then: marker file is removed
	require.NoError(t, err)
	assert.NoFileExists(t, markerPath)
}

func TestClearMarker_NoFile(t *testing.T) {
	// Given: a directory without marker file
	tmpDir := t.TempDir()

	// When: clearing marker
	err := ClearMarker(tmpDir)

	// Then: no error (idempotent)
	assert.NoError(t, err)
}

func TestMarkerAge_WithMarker(t *testing.T) {
	// Given: a marker file that was just created
	tmpDir := t.TempDir()
	require.NoError(t, MarkPassed(tmpDir))

	// When: checking age
	age := MarkerAge(tmpDir)

	// Then: age is very small (just created)
	assert.Less(t, age, time.Second)
}

func TestMarkerAge_NoMarker(t *testing.T) {
	// Given: no marker file
	tmpDir := t.TempDir()

	// When: checking age
	age := MarkerAge(tmpDir)

	// Then: returns zero
	assert.Equal(t, time.Duration(0), age)
}
