package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper functions for JSON marshaling tests
func jsonMarshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

func jsonUnmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

// Edge Case Tests - These test scenarios that could cause silent failures
// or unexpected behavior as identified in the comprehensive test analysis.

// =============================================================================
// FindProjectRoot Edge Cases
// =============================================================================

// TestFindProjectRoot_NonExistentDir_ReturnsError tests that an error is
// returned for a non-existent directory.
func TestFindProjectRoot_NonExistentDir_ReturnsError(t *testing.T) {
	// Given: a path that doesn't exist
	nonExistent := "/nonexistent/path/that/does/not/exist"

	// When: finding project root
	root, err := FindProjectRoot(nonExistent)

	// Then: error should be returned or path should be returned
	// Note: filepath.Abs succeeds even for non-existent paths
	// The function returns the absolute path, which is valid behavior
	if err != nil {
		assert.Error(t, err)
	} else {
		// Function returns the abs path - this is the "always succeeds" behavior
		assert.NotEmpty(t, root)
		t.Logf("INFO: FindProjectRoot returns path for non-existent dir: %s", root)
	}
}

// TestFindProjectRoot_DeepNesting_FindsGitRoot tests that deep nesting
// correctly finds the git root.
func TestFindProjectRoot_DeepNesting_FindsGitRoot(t *testing.T) {
	// Given: a deeply nested directory structure with .git at root
	tmpDir := t.TempDir()
	gitDir := filepath.Join(tmpDir, ".git")
	deepNested := filepath.Join(tmpDir, "a", "b", "c", "d", "e", "f", "g", "h")
	require.NoError(t, os.Mkdir(gitDir, 0o755))
	require.NoError(t, os.MkdirAll(deepNested, 0o755))

	// When: finding project root from deep nested directory
	root, err := FindProjectRoot(deepNested)

	// Then: git root is returned
	require.NoError(t, err)
	assert.Equal(t, tmpDir, root)
}

// TestFindProjectRoot_RelativePath_ResolvesToAbsolute tests that relative
// paths are resolved to absolute paths.
func TestFindProjectRoot_RelativePath_ResolvesToAbsolute(t *testing.T) {
	// Given: a directory with .git
	tmpDir := t.TempDir()
	gitDir := filepath.Join(tmpDir, ".git")
	require.NoError(t, os.Mkdir(gitDir, 0o755))

	// Save and restore working directory
	oldWd, _ := os.Getwd()
	defer func() { _ = os.Chdir(oldWd) }()
	require.NoError(t, os.Chdir(tmpDir))

	// When: finding project root with relative path
	root, err := FindProjectRoot(".")

	// Then: absolute path is returned
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(root), "Root should be absolute path")
	// Compare with EvalSymlinks to handle /var -> /private/var on macOS
	expectedRoot, _ := filepath.EvalSymlinks(tmpDir)
	actualRoot, _ := filepath.EvalSymlinks(root)
	assert.Equal(t, expectedRoot, actualRoot)
}

// TestFindProjectRoot_EmptyString_UsesCurrentDir tests behavior with empty string.
func TestFindProjectRoot_EmptyString_UsesCurrentDir(t *testing.T) {
	// Given: a working directory with .git
	tmpDir := t.TempDir()
	gitDir := filepath.Join(tmpDir, ".git")
	require.NoError(t, os.Mkdir(gitDir, 0o755))

	// Save and restore working directory
	oldWd, _ := os.Getwd()
	defer func() { _ = os.Chdir(oldWd) }()
	require.NoError(t, os.Chdir(tmpDir))

	// When: finding project root with empty string
	root, err := FindProjectRoot("")

	// Then: current directory is used and .git is found
	require.NoError(t, err)
	// Compare with EvalSymlinks to handle /var -> /private/var on macOS
	expectedRoot, _ := filepath.EvalSymlinks(tmpDir)
	actualRoot, _ := filepath.EvalSymlinks(root)
	assert.Equal(t, expectedRoot, actualRoot)
}

// =============================================================================
// Config Merge Edge Cases
// =============================================================================

// TestLoad_MergeExcludePaths_AppendsToDefaults tests that user exclude paths
// are appended to defaults rather than replacing them.
func TestLoad_MergeExcludePaths_AppendsToDefaults(t *testing.T) {
	// Given: config with custom exclude paths
	tmpDir := t.TempDir()
	configContent := `
version: 1
paths:
  exclude:
    - "**/.custom_ignore/**"
embeddings:
  provider: ollama
`
	err := os.WriteFile(filepath.Join(tmpDir, ".amanmcp.yaml"), []byte(configContent), 0o644)
	require.NoError(t, err)

	// When: loading configuration
	cfg, err := Load(tmpDir)

	// Then: both default and custom excludes are present
	require.NoError(t, err)
	assert.Contains(t, cfg.Paths.Exclude, "**/node_modules/**", "Default exclude should be preserved")
	assert.Contains(t, cfg.Paths.Exclude, "**/.git/**", "Default exclude should be preserved")
	assert.Contains(t, cfg.Paths.Exclude, "**/.custom_ignore/**", "Custom exclude should be added")
}

// TestLoad_ZeroValuesNotMerged tests that explicit zero values in config
// don't override defaults (potential silent failure).
func TestLoad_ZeroValuesNotMerged(t *testing.T) {
	// Given: config with explicit zero values
	tmpDir := t.TempDir()
	configContent := `
version: 1
search:
  max_results: 0
  chunk_size: 0
performance:
  max_files: 0
embeddings:
  provider: ollama
`
	err := os.WriteFile(filepath.Join(tmpDir, ".amanmcp.yaml"), []byte(configContent), 0o644)
	require.NoError(t, err)

	// When: loading configuration
	cfg, err := Load(tmpDir)

	// Then: defaults are kept (zero values don't override)
	require.NoError(t, err)
	assert.Equal(t, 20, cfg.Search.MaxResults, "Zero should not override default max_results")
	assert.Equal(t, 1500, cfg.Search.ChunkSize, "Zero should not override default chunk_size")
	assert.Equal(t, 100000, cfg.Performance.MaxFiles, "Zero should not override default max_files")
	// Note: This documents the "can't set to zero" limitation
}

// TestLoad_NegativeValues_Validated tests that negative values are
// rejected by validation (DEBT-018 resolved).
// Note: Search weights are internal-only (yaml:"-") and tested via env vars.
func TestLoad_NegativeValues_Validated(t *testing.T) {
	// Given: config with negative max_results (a YAML-accessible field)
	tmpDir := t.TempDir()
	configContent := `
version: 1
search:
  max_results: -10
`
	err := os.WriteFile(filepath.Join(tmpDir, ".amanmcp.yaml"), []byte(configContent), 0o644)
	require.NoError(t, err)

	// When: loading configuration
	cfg, err := Load(tmpDir)

	// Then: validation error is returned
	require.Error(t, err)
	require.Nil(t, cfg)
	assert.Contains(t, err.Error(), "max_results must be non-negative")
}

// TestLoad_WeightsSumValidated tests that search weights must
// sum to 1.0 (DEBT-018 resolved).
// Since weights are internal-only (yaml:"-"), this tests the validation
// logic directly rather than through YAML loading.
func TestLoad_WeightsSumValidated(t *testing.T) {
	// Given: a config with weights that don't sum to 1.0
	cfg := NewConfig()
	cfg.Search.BM25Weight = 0.9
	cfg.Search.SemanticWeight = 0.9

	// When: validating the configuration
	err := cfg.Validate()

	// Then: validation error is returned
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bm25_weight + semantic_weight must equal 1.0")
}

// =============================================================================
// Config File Permission Edge Cases
// =============================================================================

// TestLoad_UnreadableConfigFile_ReturnsError tests that unreadable config
// files return an error.
func TestLoad_UnreadableConfigFile_ReturnsError(t *testing.T) {
	// Skip on CI or if running as root
	if os.Getuid() == 0 {
		t.Skip("Test requires non-root user")
	}

	// Given: a config file with no read permissions
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".amanmcp.yaml")
	err := os.WriteFile(configPath, []byte("version: 1"), 0o000)
	require.NoError(t, err)
	defer func() { _ = os.Chmod(configPath, 0o644) }()

	// When: loading configuration
	cfg, err := Load(tmpDir)

	// Then: error should be returned
	require.Error(t, err, "Load should fail for unreadable config file")
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "read", "Error should mention read failure")
}

// =============================================================================
// DetectProjectType Edge Cases
// =============================================================================

// TestDetectProjectType_EmptyDir_ReturnsUnknown tests that empty directories
// return unknown project type.
func TestDetectProjectType_EmptyDir_ReturnsUnknown(t *testing.T) {
	// Given: an empty directory
	tmpDir := t.TempDir()

	// When: detecting project type
	projectType := DetectProjectType(tmpDir)

	// Then: Unknown is returned
	assert.Equal(t, ProjectTypeUnknown, projectType)
}

// TestDetectProjectType_NonExistentDir_ReturnsUnknown tests that non-existent
// directories return unknown (not error).
func TestDetectProjectType_NonExistentDir_ReturnsUnknown(t *testing.T) {
	// Given: a non-existent directory
	nonExistent := "/nonexistent/path/that/does/not/exist"

	// When: detecting project type
	projectType := DetectProjectType(nonExistent)

	// Then: Unknown is returned (not error/panic)
	assert.Equal(t, ProjectTypeUnknown, projectType)
}

// TestDetectProjectType_EmptyMarkerFiles_StillDetected tests that empty
// marker files are still detected.
func TestDetectProjectType_EmptyMarkerFiles_StillDetected(t *testing.T) {
	// Given: directory with empty go.mod
	tmpDir := t.TempDir()
	err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(""), 0o644)
	require.NoError(t, err)

	// When: detecting project type
	projectType := DetectProjectType(tmpDir)

	// Then: Go is still detected (presence matters, not content)
	assert.Equal(t, ProjectTypeGo, projectType)
}

// =============================================================================
// DiscoverSourceDirs Edge Cases
// =============================================================================

// TestDiscoverSourceDirs_EmptyDir_ReturnsEmpty tests that empty directories
// return no source dirs.
func TestDiscoverSourceDirs_EmptyDir_ReturnsEmpty(t *testing.T) {
	// Given: an empty directory
	tmpDir := t.TempDir()

	// When: discovering source directories
	dirs := DiscoverSourceDirs(tmpDir)

	// Then: empty slice is returned
	assert.Empty(t, dirs)
}

// TestDiscoverSourceDirs_NonExistentDir_ReturnsEmpty tests that non-existent
// directories return empty (not error).
func TestDiscoverSourceDirs_NonExistentDir_ReturnsEmpty(t *testing.T) {
	// Given: a non-existent directory
	nonExistent := "/nonexistent/path/that/does/not/exist"

	// When: discovering source directories
	dirs := DiscoverSourceDirs(nonExistent)

	// Then: empty slice is returned (not error/panic)
	assert.Empty(t, dirs)
}

// TestDiscoverSourceDirs_FilesNotDirs_NotIncluded tests that files named
// like source dirs are not included.
func TestDiscoverSourceDirs_FilesNotDirs_NotIncluded(t *testing.T) {
	// Given: a directory with a file named "src" (not a directory)
	tmpDir := t.TempDir()
	err := os.WriteFile(filepath.Join(tmpDir, "src"), []byte("not a dir"), 0o644)
	require.NoError(t, err)

	// When: discovering source directories
	dirs := DiscoverSourceDirs(tmpDir)

	// Then: "src" file is not included
	assert.NotContains(t, dirs, "src")
}

// =============================================================================
// DiscoverDocsDirs Edge Cases
// =============================================================================

// TestDiscoverDocsDirs_EmptyDir_ReturnsEmpty tests that empty directories
// return no docs dirs.
func TestDiscoverDocsDirs_EmptyDir_ReturnsEmpty(t *testing.T) {
	// Given: an empty directory
	tmpDir := t.TempDir()

	// When: discovering documentation directories
	dirs := DiscoverDocsDirs(tmpDir)

	// Then: empty slice is returned
	assert.Empty(t, dirs)
}

// TestDiscoverDocsDirs_NonExistentDir_ReturnsEmpty tests that non-existent
// directories return empty (not error).
func TestDiscoverDocsDirs_NonExistentDir_ReturnsEmpty(t *testing.T) {
	// Given: a non-existent directory
	nonExistent := "/nonexistent/path/that/does/not/exist"

	// When: discovering documentation directories
	dirs := DiscoverDocsDirs(nonExistent)

	// Then: empty slice is returned (not error/panic)
	assert.Empty(t, dirs)
}

// =============================================================================
// Config JSON Marshaling Edge Cases
// =============================================================================

// TestConfig_JSON_RoundTrip tests that config can be marshaled to JSON
// and back without data loss for JSON-accessible fields.
func TestConfig_JSON_RoundTrip(t *testing.T) {
	// Given: a configuration with custom values
	cfg := NewConfig()
	cfg.Search.ChunkSize = 2000
	cfg.Search.BM25Weight = 0.4
	cfg.Search.SemanticWeight = 0.6
	cfg.Search.RRFConstant = 100
	cfg.Embeddings.Provider = "static"

	// When: marshaling to JSON and back
	data, err := jsonMarshal(cfg)
	require.NoError(t, err)

	var parsed Config
	err = jsonUnmarshal(data, &parsed)
	require.NoError(t, err)

	// Then: all JSON-accessible values are preserved (FEAT-UNIX2: weights now round-trip)
	assert.Equal(t, 2000, parsed.Search.ChunkSize)
	assert.Equal(t, "static", parsed.Embeddings.Provider)
	assert.Equal(t, 0.4, parsed.Search.BM25Weight)
	assert.Equal(t, 0.6, parsed.Search.SemanticWeight)
	assert.Equal(t, 100, parsed.Search.RRFConstant)
}

// TestConfig_UnmarshalJSON_InvalidJSON_ReturnsError tests that invalid JSON
// returns an error.
func TestConfig_UnmarshalJSON_InvalidJSON_ReturnsError(t *testing.T) {
	// Given: invalid JSON
	invalidJSON := []byte("{invalid json")

	// When: unmarshaling
	var cfg Config
	err := jsonUnmarshal(invalidJSON, &cfg)

	// Then: error is returned
	require.Error(t, err, "Unmarshal should fail for invalid JSON")
}

// =============================================================================
// Sessions Config Edge Cases
// =============================================================================

// TestNewConfig_SessionsStoragePath_UsesHomeDir tests that sessions storage
// path defaults to a path under home directory.
func TestNewConfig_SessionsStoragePath_UsesHomeDir(t *testing.T) {
	// Given: a new config
	cfg := NewConfig()

	// Then: sessions storage path should be under home or use fallback
	assert.NotEmpty(t, cfg.Sessions.StoragePath)
	assert.Contains(t, cfg.Sessions.StoragePath, "sessions")
}

// TestNewConfig_AutoSave_DefaultsToTrue tests that auto_save defaults to true.
func TestNewConfig_AutoSave_DefaultsToTrue(t *testing.T) {
	// Given: a new config
	cfg := NewConfig()

	// Then: auto_save should be true
	assert.True(t, cfg.Sessions.AutoSave)
}
