package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// FormatBytes Tests (DEBT-028: Coverage improvement)
// =============================================================================

func TestFormatBytes_Bytes(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{512, "512 B"},
		{1023, "1023 B"},
	}

	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			result := FormatBytes(tc.bytes)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestFormatBytes_Kilobytes(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{10240, "10.0 KB"},
		{1048575, "1024.0 KB"}, // Just under 1MB
	}

	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			result := FormatBytes(tc.bytes)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestFormatBytes_Megabytes(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{1048576, "1.0 MB"},     // 1MB
		{5242880, "5.0 MB"},     // 5MB
		{104857600, "100.0 MB"}, // 100MB
	}

	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			result := FormatBytes(tc.bytes)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestFormatBytes_Gigabytes(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{1073741824, "1.0 GB"},      // 1GB
		{5368709120, "5.0 GB"},      // 5GB
		{10737418240, "10.0 GB"},    // 10GB
		{107374182400, "100.0 GB"},  // 100GB
	}

	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			result := FormatBytes(tc.bytes)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// =============================================================================
// FormatTime Tests (DEBT-028: Coverage improvement)
// =============================================================================

func TestFormatTime_Valid(t *testing.T) {
	// Test a specific time
	testTime := time.Date(2026, 1, 15, 10, 30, 45, 0, time.UTC)
	result := FormatTime(testTime)
	assert.Equal(t, "2026-01-15 10:30:45", result)
}

func TestFormatTime_ZeroTime(t *testing.T) {
	result := FormatTime(time.Time{})
	assert.Equal(t, "unknown", result)
}

func TestFormatTime_Epoch(t *testing.T) {
	// Unix epoch should be formatted normally
	epoch := time.Unix(0, 0).UTC()
	result := FormatTime(epoch)
	assert.Equal(t, "1970-01-01 00:00:00", result)
}

// =============================================================================
// containsAny Tests (DEBT-028: Coverage improvement)
// =============================================================================

func TestContainsAny_Found(t *testing.T) {
	tests := []struct {
		name       string
		s          string
		substrings []string
		expected   bool
	}{
		{"single match", "hello world", []string{"world"}, true},
		{"first of many", "hello world", []string{"hello", "foo", "bar"}, true},
		{"middle of many", "hello world", []string{"foo", "world", "bar"}, true},
		{"last of many", "hello world", []string{"foo", "bar", "world"}, true},
		{"prefix match", "mlx-community/model", []string{"mlx-community/"}, true},
		{"contains mlx-", "some-mlx-model", []string{"mlx-"}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := containsAny(tc.s, tc.substrings)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestContainsAny_NotFound(t *testing.T) {
	tests := []struct {
		name       string
		s          string
		substrings []string
		expected   bool
	}{
		{"no match", "hello world", []string{"foo", "bar"}, false},
		{"empty substrings", "hello world", []string{}, false},
		{"empty string", "", []string{"foo"}, false},
		{"substring longer than string", "hi", []string{"hello"}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := containsAny(tc.s, tc.substrings)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// =============================================================================
// inferBackendFromModel Tests (DEBT-028: Coverage improvement)
// =============================================================================

func TestInferBackendFromModel_Static(t *testing.T) {
	tests := []struct {
		model    string
		expected string
	}{
		{"static", "static"},
		{"static768", "static"},
	}

	for _, tc := range tests {
		t.Run(tc.model, func(t *testing.T) {
			result := inferBackendFromModel(tc.model)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestInferBackendFromModel_MLX(t *testing.T) {
	tests := []struct {
		model    string
		expected string
	}{
		{"/path/to/local/model", "mlx"},                          // Absolute path
		{"mlx-community/bge-small", "mlx"},                       // mlx-community prefix
		{"mlx-embedding-model", "mlx"},                           // mlx- prefix
		{"/Users/user/.cache/mlx/models/embedding", "mlx"},       // Absolute path
	}

	for _, tc := range tests {
		t.Run(tc.model, func(t *testing.T) {
			result := inferBackendFromModel(tc.model)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestInferBackendFromModel_Ollama(t *testing.T) {
	tests := []struct {
		model    string
		expected string
	}{
		{"qwen3-embedding:0.6b", "ollama"},
		{"nomic-embed-text", "ollama"},
		{"nomic-embed-text:latest", "ollama"},
		{"mxbai-embed-large", "ollama"},
		{"some-random-model", "ollama"}, // Default to ollama
	}

	for _, tc := range tests {
		t.Run(tc.model, func(t *testing.T) {
			result := inferBackendFromModel(tc.model)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// =============================================================================
// getDirSize Tests (DEBT-028: Coverage improvement)
// =============================================================================

func TestGetDirSize_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	size := getDirSize(tmpDir)
	assert.Equal(t, int64(0), size)
}

func TestGetDirSize_WithFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create some files with known sizes
	file1 := filepath.Join(tmpDir, "file1.txt")
	file2 := filepath.Join(tmpDir, "file2.txt")

	require.NoError(t, os.WriteFile(file1, make([]byte, 1024), 0o644)) // 1KB
	require.NoError(t, os.WriteFile(file2, make([]byte, 2048), 0o644)) // 2KB

	size := getDirSize(tmpDir)
	assert.Equal(t, int64(3072), size) // 3KB total
}

func TestGetDirSize_WithSubdirectories(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")
	require.NoError(t, os.MkdirAll(subDir, 0o755))

	// Create files in both directories
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "root.txt"), make([]byte, 1024), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "nested.txt"), make([]byte, 512), 0o644))

	size := getDirSize(tmpDir)
	assert.Equal(t, int64(1536), size) // 1.5KB total
}

func TestGetDirSize_NonexistentPath(t *testing.T) {
	size := getDirSize("/nonexistent/path/that/does/not/exist")
	assert.Equal(t, int64(0), size)
}
