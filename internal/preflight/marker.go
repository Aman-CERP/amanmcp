package preflight

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// MarkerFile is the name of the file that indicates preflight checks have passed.
const MarkerFile = ".preflight-passed"

// NeedsCheck returns true if preflight checks should be run.
// Returns true if the marker file doesn't exist in the data directory.
func NeedsCheck(dataDir string) bool {
	markerPath := filepath.Join(dataDir, MarkerFile)
	_, err := os.Stat(markerPath)
	return os.IsNotExist(err)
}

// MarkPassed creates the marker file to indicate preflight checks passed.
func MarkPassed(dataDir string) error {
	// Ensure data directory exists
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("create marker directory: %w", err)
	}

	markerPath := filepath.Join(dataDir, MarkerFile)
	content := []byte(time.Now().Format(time.RFC3339))
	return os.WriteFile(markerPath, content, 0644)
}

// ClearMarker removes the marker file, forcing a re-check on next run.
func ClearMarker(dataDir string) error {
	markerPath := filepath.Join(dataDir, MarkerFile)
	err := os.Remove(markerPath)
	if os.IsNotExist(err) {
		return nil // Already gone
	}
	if err != nil {
		return fmt.Errorf("remove marker file: %w", err)
	}
	return nil
}

// MarkerAge returns how long ago the preflight check passed.
// Returns zero if marker doesn't exist.
func MarkerAge(dataDir string) time.Duration {
	markerPath := filepath.Join(dataDir, MarkerFile)
	content, err := os.ReadFile(markerPath)
	if err != nil {
		return 0
	}

	t, err := time.Parse(time.RFC3339, string(content))
	if err != nil {
		return 0
	}

	return time.Since(t)
}
