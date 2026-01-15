package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitCmd_NoGoroutineLeak(t *testing.T) {
	// Get baseline goroutine count
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	// Create temp directory for testing
	tmpDir := t.TempDir()

	// Run init command multiple times
	for i := 0; i < 3; i++ {
		cmd := newInitCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		// Use offline to avoid network calls
		cmd.SetArgs([]string{"--offline"})

		// Change to temp dir
		oldWd, _ := os.Getwd()
		_ = os.Chdir(tmpDir)
		_ = cmd.Execute()
		_ = os.Chdir(oldWd)

		// Clean up for next iteration
		_ = os.RemoveAll(filepath.Join(tmpDir, ".mcp.json"))
		_ = os.RemoveAll(filepath.Join(tmpDir, ".amanmcp"))
	}

	// Allow time for any leaked goroutines to settle
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	// Check goroutine count hasn't grown significantly
	current := runtime.NumGoroutine()
	leaked := current - baseline

	// Should not leak more than 2 goroutines
	assert.LessOrEqual(t, leaked, 2, "goroutine leak detected: baseline=%d, current=%d, leaked=%d", baseline, current, leaked)
}

func TestInitCmd_BasicExecution(t *testing.T) {
	tmpDir := t.TempDir()

	var stdout bytes.Buffer
	cmd := newInitCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--offline"})

	// Change to temp dir
	oldWd, _ := os.Getwd()
	err := os.Chdir(tmpDir)
	require.NoError(t, err)
	defer func() { _ = os.Chdir(oldWd) }()

	// Execute
	_ = cmd.Execute()

	// Should produce some output
	output := stdout.String()
	assert.Contains(t, output, "AmanMCP")
	assert.Contains(t, output, "Initializing")
}

func TestInitCmd_CreatesMCPJSON(t *testing.T) {
	tmpDir := t.TempDir()

	var stdout bytes.Buffer
	cmd := newInitCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--offline"})

	// Change to temp dir
	oldWd, _ := os.Getwd()
	err := os.Chdir(tmpDir)
	require.NoError(t, err)
	defer func() { _ = os.Chdir(oldWd) }()

	// Execute
	_ = cmd.Execute()

	// Check .mcp.json was created
	mcpPath := filepath.Join(tmpDir, ".mcp.json")
	data, err := os.ReadFile(mcpPath)
	if err == nil {
		// Parse and validate structure
		var config MCPConfig
		err = json.Unmarshal(data, &config)
		assert.NoError(t, err)
		assert.Contains(t, config.MCPServers, "amanmcp")
	}
	// Note: .mcp.json may not be created if claude CLI is detected
}

func TestInitCmd_AlreadyInitialized(t *testing.T) {
	tmpDir := t.TempDir()

	// Create existing VALID .mcp.json (with all required fields)
	mcpPath := filepath.Join(tmpDir, ".mcp.json")
	validConfig := `{
  "mcpServers": {
    "amanmcp": {
      "type": "stdio",
      "command": "/usr/local/bin/amanmcp",
      "args": ["serve"],
      "cwd": "/home/user/project"
    }
  }
}`
	err := os.WriteFile(mcpPath, []byte(validConfig), 0644)
	require.NoError(t, err)

	var stdout bytes.Buffer
	cmd := newInitCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--offline"})

	// Change to temp dir
	oldWd, _ := os.Getwd()
	err = os.Chdir(tmpDir)
	require.NoError(t, err)
	defer func() { _ = os.Chdir(oldWd) }()

	// Execute
	err = cmd.Execute()
	assert.NoError(t, err)

	// Should warn about already initialized
	output := stdout.String()
	assert.Contains(t, output, "already initialized")
}

func TestInitCmd_ForceReinitialize(t *testing.T) {
	tmpDir := t.TempDir()

	// Create existing .mcp.json
	mcpPath := filepath.Join(tmpDir, ".mcp.json")
	err := os.WriteFile(mcpPath, []byte(`{"mcpServers":{}}`), 0644)
	require.NoError(t, err)

	var stdout bytes.Buffer
	cmd := newInitCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--offline", "--force"})

	// Change to temp dir
	oldWd, _ := os.Getwd()
	err = os.Chdir(tmpDir)
	require.NoError(t, err)
	defer func() { _ = os.Chdir(oldWd) }()

	// Execute with --force
	_ = cmd.Execute()

	// Should NOT warn about already initialized when using --force
	output := stdout.String()
	assert.NotContains(t, output, "already initialized")
}

func TestFindAmanmcpBinary(t *testing.T) {
	// Should be able to find itself (the test binary won't be amanmcp, but function shouldn't panic)
	path, err := findAmanmcpBinary()

	// May succeed or fail depending on environment
	// But should not panic
	if err == nil {
		assert.NotEmpty(t, path)
	}
}

func TestMCPConfigStructure(t *testing.T) {
	config := MCPConfig{
		MCPServers: map[string]MCPServerConfig{
			"amanmcp": {
				Command: "/usr/local/bin/amanmcp",
				Args:    []string{"serve"},
				Cwd:     "/home/user/project",
			},
		},
	}

	data, err := json.MarshalIndent(config, "", "  ")
	require.NoError(t, err)

	// Unmarshal back
	var parsed MCPConfig
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	assert.Equal(t, config.MCPServers["amanmcp"].Command, parsed.MCPServers["amanmcp"].Command)
	assert.Equal(t, config.MCPServers["amanmcp"].Args, parsed.MCPServers["amanmcp"].Args)
	assert.Equal(t, config.MCPServers["amanmcp"].Cwd, parsed.MCPServers["amanmcp"].Cwd)
}

// BUG-040: Test that MCPServerConfig has Type field
func TestMCPServerConfig_HasTypeField(t *testing.T) {
	config := MCPServerConfig{
		Type:    "stdio",
		Command: "/usr/local/bin/amanmcp",
		Args:    []string{"serve"},
		Cwd:     "/home/user/project",
	}

	data, err := json.MarshalIndent(config, "", "  ")
	require.NoError(t, err)

	// Should contain type field in JSON
	jsonStr := string(data)
	assert.Contains(t, jsonStr, `"type"`, "JSON output should contain type field")
	assert.Contains(t, jsonStr, `"stdio"`, "JSON output should contain stdio value")

	// Unmarshal and verify type is preserved
	var parsed MCPServerConfig
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)
	assert.Equal(t, "stdio", parsed.Type, "Type field should be preserved after round-trip")
}

// BUG-040: Test that generated .mcp.json includes type field
func TestInitCmd_GeneratedConfigHasType(t *testing.T) {
	tmpDir := t.TempDir()

	var stdout bytes.Buffer
	cmd := newInitCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--offline"})

	oldWd, _ := os.Getwd()
	err := os.Chdir(tmpDir)
	require.NoError(t, err)
	defer func() { _ = os.Chdir(oldWd) }()

	_ = cmd.Execute()

	// Read and parse .mcp.json
	mcpPath := filepath.Join(tmpDir, ".mcp.json")
	data, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Skip("Claude CLI may have been used instead of .mcp.json")
	}

	var config MCPConfig
	err = json.Unmarshal(data, &config)
	require.NoError(t, err)

	amanmcp, exists := config.MCPServers["amanmcp"]
	require.True(t, exists, "amanmcp should be in mcpServers")
	assert.Equal(t, "stdio", amanmcp.Type, "Generated config should have type=stdio")
}

// BUG-040/041: Test that generated .mcp.json includes cwd field
func TestInitCmd_GeneratedConfigHasCwd(t *testing.T) {
	tmpDir := t.TempDir()

	var stdout bytes.Buffer
	cmd := newInitCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--offline"})

	oldWd, _ := os.Getwd()
	err := os.Chdir(tmpDir)
	require.NoError(t, err)
	defer func() { _ = os.Chdir(oldWd) }()

	_ = cmd.Execute()

	// Read and parse .mcp.json
	mcpPath := filepath.Join(tmpDir, ".mcp.json")
	data, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Skip("Claude CLI may have been used instead of .mcp.json")
	}

	var config MCPConfig
	err = json.Unmarshal(data, &config)
	require.NoError(t, err)

	amanmcp, exists := config.MCPServers["amanmcp"]
	require.True(t, exists, "amanmcp should be in mcpServers")
	assert.NotEmpty(t, amanmcp.Cwd, "Generated config should have non-empty cwd")

	// Resolve symlinks for comparison (macOS /var -> /private/var)
	expectedCwd, _ := filepath.EvalSymlinks(tmpDir)
	actualCwd, _ := filepath.EvalSymlinks(amanmcp.Cwd)
	assert.Equal(t, expectedCwd, actualCwd, "cwd should match project root (after symlink resolution)")
}

// BUG-042: Test that init validates existing config missing cwd
func TestInitCmd_ValidatesExistingConfig_MissingCwd(t *testing.T) {
	tmpDir := t.TempDir()

	// Create .mcp.json WITHOUT cwd field
	mcpConfig := `{
  "mcpServers": {
    "amanmcp": {
      "type": "stdio",
      "command": "/usr/local/bin/amanmcp",
      "args": ["serve"]
    }
  }
}`
	mcpPath := filepath.Join(tmpDir, ".mcp.json")
	err := os.WriteFile(mcpPath, []byte(mcpConfig), 0644)
	require.NoError(t, err)

	var stdout bytes.Buffer
	cmd := newInitCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--offline"})

	oldWd, _ := os.Getwd()
	err = os.Chdir(tmpDir)
	require.NoError(t, err)
	defer func() { _ = os.Chdir(oldWd) }()

	_ = cmd.Execute()

	output := stdout.String()
	// Should warn about missing cwd
	assert.Contains(t, output, "cwd", "Should warn about missing cwd field")
}

// BUG-042: Test that valid existing config passes validation
func TestInitCmd_ValidatesExistingConfig_Valid(t *testing.T) {
	tmpDir := t.TempDir()

	// Create VALID .mcp.json with cwd field
	mcpConfig := `{
  "mcpServers": {
    "amanmcp": {
      "type": "stdio",
      "command": "/usr/local/bin/amanmcp",
      "args": ["serve"],
      "cwd": "/home/user/project"
    }
  }
}`
	mcpPath := filepath.Join(tmpDir, ".mcp.json")
	err := os.WriteFile(mcpPath, []byte(mcpConfig), 0644)
	require.NoError(t, err)

	var stdout bytes.Buffer
	cmd := newInitCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--offline"})

	oldWd, _ := os.Getwd()
	err = os.Chdir(tmpDir)
	require.NoError(t, err)
	defer func() { _ = os.Chdir(oldWd) }()

	err = cmd.Execute()
	assert.NoError(t, err)

	output := stdout.String()
	// Should show "already initialized" without warnings
	assert.Contains(t, output, "already initialized")
}

// Feature: Test that init generates .amanmcp.yaml template
func TestInitCmd_GeneratesAmanmcpYAML(t *testing.T) {
	tmpDir := t.TempDir()

	var stdout bytes.Buffer
	cmd := newInitCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--offline"})

	oldWd, _ := os.Getwd()
	err := os.Chdir(tmpDir)
	require.NoError(t, err)
	defer func() { _ = os.Chdir(oldWd) }()

	_ = cmd.Execute()

	// Check .amanmcp.yaml was created
	yamlPath := filepath.Join(tmpDir, ".amanmcp.yaml")
	data, err := os.ReadFile(yamlPath)
	require.NoError(t, err, ".amanmcp.yaml should be created")

	content := string(data)
	// Should contain documented configuration options
	assert.Contains(t, content, "version:", "Should contain version field")
	assert.Contains(t, content, "paths:", "Should contain paths section")
	assert.Contains(t, content, "search:", "Should contain search section")
	assert.Contains(t, content, "embeddings:", "Should contain embeddings section")
	assert.Contains(t, content, "#", "Should contain comments")
}

// Feature: Test --config-only flag skips indexing
func TestInitCmd_ConfigOnlySkipsIndexing(t *testing.T) {
	tmpDir := t.TempDir()

	var stdout bytes.Buffer
	cmd := newInitCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--offline", "--config-only"})

	oldWd, _ := os.Getwd()
	err := os.Chdir(tmpDir)
	require.NoError(t, err)
	defer func() { _ = os.Chdir(oldWd) }()

	_ = cmd.Execute()

	output := stdout.String()
	// Should show skipping message
	assert.Contains(t, output, "Skipping indexing", "Should indicate indexing is skipped")
	// Should still create .mcp.json
	mcpPath := filepath.Join(tmpDir, ".mcp.json")
	_, err = os.Stat(mcpPath)
	assert.NoError(t, err, ".mcp.json should be created even with --config-only")
	// Should NOT create .amanmcp directory (no indexing)
	amanmcpDir := filepath.Join(tmpDir, ".amanmcp")
	_, err = os.Stat(amanmcpDir)
	assert.True(t, os.IsNotExist(err), ".amanmcp directory should NOT be created with --config-only")
}

// Feature: Test that init preserves existing .amanmcp.yaml
func TestInitCmd_PreservesExistingAmanmcpYAML(t *testing.T) {
	tmpDir := t.TempDir()

	// Create existing .amanmcp.yaml with custom content
	existingContent := "version: 1\n# My custom config\npaths:\n  exclude:\n    - vendor/**\n"
	yamlPath := filepath.Join(tmpDir, ".amanmcp.yaml")
	err := os.WriteFile(yamlPath, []byte(existingContent), 0644)
	require.NoError(t, err)

	var stdout bytes.Buffer
	cmd := newInitCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--offline", "--force"}) // --force for .mcp.json, but not yaml

	oldWd, _ := os.Getwd()
	err = os.Chdir(tmpDir)
	require.NoError(t, err)
	defer func() { _ = os.Chdir(oldWd) }()

	_ = cmd.Execute()

	// Should preserve existing .amanmcp.yaml
	data, err := os.ReadFile(yamlPath)
	require.NoError(t, err)
	assert.Equal(t, existingContent, string(data), "Existing .amanmcp.yaml should not be overwritten")
}

// Feature: CLAUDE.md usage guide - creates new file when not exists
func TestInitCmd_CreatesCLAUDEMD(t *testing.T) {
	tmpDir := t.TempDir()

	var stdout bytes.Buffer
	cmd := newInitCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--offline", "--config-only"})

	oldWd, _ := os.Getwd()
	err := os.Chdir(tmpDir)
	require.NoError(t, err)
	defer func() { _ = os.Chdir(oldWd) }()

	_ = cmd.Execute()

	// Check CLAUDE.md was created
	claudeMDPath := filepath.Join(tmpDir, "CLAUDE.md")
	data, err := os.ReadFile(claudeMDPath)
	require.NoError(t, err, "CLAUDE.md should be created")

	content := string(data)
	// Should contain start/end markers
	assert.Contains(t, content, "<!-- amanmcp:start -->", "Should contain start marker")
	assert.Contains(t, content, "<!-- amanmcp:end -->", "Should contain end marker")
	// Should contain useful content
	assert.Contains(t, content, "search", "Should mention search tool")
	assert.Contains(t, content, "search_code", "Should mention search_code tool")
	assert.Contains(t, content, "search_docs", "Should mention search_docs tool")
}

// Feature: CLAUDE.md usage guide - appends to existing file without guide
func TestInitCmd_AppendsToCLAUDEMD(t *testing.T) {
	tmpDir := t.TempDir()

	// Create existing CLAUDE.md without guide
	existingContent := "# My Project\n\nThis is my project documentation.\n\n## Rules\n\n- Follow coding standards\n"
	claudeMDPath := filepath.Join(tmpDir, "CLAUDE.md")
	err := os.WriteFile(claudeMDPath, []byte(existingContent), 0644)
	require.NoError(t, err)

	var stdout bytes.Buffer
	cmd := newInitCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--offline", "--config-only"})

	oldWd, _ := os.Getwd()
	err = os.Chdir(tmpDir)
	require.NoError(t, err)
	defer func() { _ = os.Chdir(oldWd) }()

	_ = cmd.Execute()

	// Check CLAUDE.md was updated
	data, err := os.ReadFile(claudeMDPath)
	require.NoError(t, err)

	content := string(data)
	// Should preserve existing content
	assert.Contains(t, content, "# My Project", "Should preserve existing content")
	assert.Contains(t, content, "Follow coding standards", "Should preserve existing rules")
	// Should add guide
	assert.Contains(t, content, "<!-- amanmcp:start -->", "Should contain start marker")
	assert.Contains(t, content, "<!-- amanmcp:end -->", "Should contain end marker")
	// Original content should come before guide
	startMarkerPos := bytes.Index(data, []byte("<!-- amanmcp:start -->"))
	existingContentPos := bytes.Index(data, []byte("# My Project"))
	assert.Less(t, existingContentPos, startMarkerPos, "Existing content should come before guide")
}

// Feature: CLAUDE.md usage guide - skips if guide already exists (idempotent)
func TestInitCmd_SkipsExistingCLAUDEMDGuide(t *testing.T) {
	tmpDir := t.TempDir()

	// Create existing CLAUDE.md WITH guide
	existingContent := `# My Project

<!-- amanmcp:start -->
## Custom guide content
This is user-customized.
<!-- amanmcp:end -->
`
	claudeMDPath := filepath.Join(tmpDir, "CLAUDE.md")
	err := os.WriteFile(claudeMDPath, []byte(existingContent), 0644)
	require.NoError(t, err)

	var stdout bytes.Buffer
	cmd := newInitCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--offline", "--config-only", "--force"})

	oldWd, _ := os.Getwd()
	err = os.Chdir(tmpDir)
	require.NoError(t, err)
	defer func() { _ = os.Chdir(oldWd) }()

	_ = cmd.Execute()

	// Check CLAUDE.md was NOT modified
	data, err := os.ReadFile(claudeMDPath)
	require.NoError(t, err)

	// Content should be exactly the same (no duplication)
	assert.Equal(t, existingContent, string(data), "CLAUDE.md should not be modified when guide already exists")

	// Output should indicate skipping
	output := stdout.String()
	assert.Contains(t, output, "already has", "Should indicate guide already exists")
}

// Feature: CLAUDE.md usage guide - preserves content on multiple runs
func TestInitCmd_CLAUDEMDIdempotent(t *testing.T) {
	tmpDir := t.TempDir()

	var stdout bytes.Buffer

	oldWd, _ := os.Getwd()
	err := os.Chdir(tmpDir)
	require.NoError(t, err)
	defer func() { _ = os.Chdir(oldWd) }()

	// Run init twice
	for i := 0; i < 2; i++ {
		stdout.Reset()
		cmd := newInitCmd()
		cmd.SetOut(&stdout)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--offline", "--config-only", "--force"})
		_ = cmd.Execute()
	}

	// Check CLAUDE.md
	claudeMDPath := filepath.Join(tmpDir, "CLAUDE.md")
	data, err := os.ReadFile(claudeMDPath)
	require.NoError(t, err)

	// Should have exactly one start marker (not duplicated)
	startCount := bytes.Count(data, []byte("<!-- amanmcp:start -->"))
	assert.Equal(t, 1, startCount, "Should have exactly one start marker after multiple runs")
}

// =============================================================================
// FEAT-INIT1: .gitignore auto-add tests
// =============================================================================

// TestHasAmanmcpIgnore tests the helper function for detecting existing entries
func TestHasAmanmcpIgnore(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"empty", "", false},
		{"no match", "*.log\nnode_modules/\n", false},
		{"exact .amanmcp", ".amanmcp\n", true},
		{"with slash .amanmcp/", ".amanmcp/\n", true},
		{"rooted /.amanmcp", "/.amanmcp\n", true},
		{"rooted with slash /.amanmcp/", "/.amanmcp/\n", true},
		{"commented", "# .amanmcp/\n", false},
		{"with whitespace", "  .amanmcp/  \n", true},
		{"in middle", "*.log\n.amanmcp/\nnode_modules/\n", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasAmanmcpIgnore(tt.content)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestEnsureGitignore_CreatesNewFile tests creating .gitignore when it doesn't exist
func TestEnsureGitignore_CreatesNewFile(t *testing.T) {
	tmpDir := t.TempDir()

	added, err := ensureGitignore(tmpDir)

	require.NoError(t, err)
	assert.True(t, added, "should return true when gitignore created")

	// Verify content
	content, err := os.ReadFile(filepath.Join(tmpDir, ".gitignore"))
	require.NoError(t, err)
	assert.Contains(t, string(content), ".amanmcp/")
	assert.Contains(t, string(content), "# AmanMCP")
}

// TestEnsureGitignore_AppendsToExisting tests appending to an existing .gitignore
func TestEnsureGitignore_AppendsToExisting(t *testing.T) {
	tmpDir := t.TempDir()
	gitignorePath := filepath.Join(tmpDir, ".gitignore")

	// Create existing .gitignore
	existingContent := "*.log\nnode_modules/\n"
	err := os.WriteFile(gitignorePath, []byte(existingContent), 0644)
	require.NoError(t, err)

	added, err := ensureGitignore(tmpDir)

	require.NoError(t, err)
	assert.True(t, added)

	content, _ := os.ReadFile(gitignorePath)
	assert.Contains(t, string(content), "*.log", "should preserve existing content")
	assert.Contains(t, string(content), ".amanmcp/", "should add .amanmcp")
}

// TestEnsureGitignore_IdempotentExactMatch tests that exact matches are detected
func TestEnsureGitignore_IdempotentExactMatch(t *testing.T) {
	tmpDir := t.TempDir()
	gitignorePath := filepath.Join(tmpDir, ".gitignore")

	// Create .gitignore with .amanmcp/ already present
	existingContent := "*.log\n.amanmcp/\n"
	err := os.WriteFile(gitignorePath, []byte(existingContent), 0644)
	require.NoError(t, err)

	added, err := ensureGitignore(tmpDir)

	require.NoError(t, err)
	assert.False(t, added, "should return false when already present")

	content, _ := os.ReadFile(gitignorePath)
	assert.Equal(t, existingContent, string(content), "should not modify file")
}

// TestEnsureGitignore_IdempotentVariations tests that pattern variations are detected
func TestEnsureGitignore_IdempotentVariations(t *testing.T) {
	variations := []string{".amanmcp", ".amanmcp/", "/.amanmcp", "/.amanmcp/"}

	for _, pattern := range variations {
		t.Run(pattern, func(t *testing.T) {
			tmpDir := t.TempDir()
			gitignorePath := filepath.Join(tmpDir, ".gitignore")

			existingContent := "*.log\n" + pattern + "\n"
			err := os.WriteFile(gitignorePath, []byte(existingContent), 0644)
			require.NoError(t, err)

			added, err := ensureGitignore(tmpDir)

			require.NoError(t, err)
			assert.False(t, added, "should detect variation: %s", pattern)
		})
	}
}

// TestEnsureGitignore_PreservesCRLF tests that CRLF line endings are preserved
func TestEnsureGitignore_PreservesCRLF(t *testing.T) {
	tmpDir := t.TempDir()
	gitignorePath := filepath.Join(tmpDir, ".gitignore")

	// Create .gitignore with CRLF endings
	existingContent := "*.log\r\nnode_modules/\r\n"
	err := os.WriteFile(gitignorePath, []byte(existingContent), 0644)
	require.NoError(t, err)

	added, err := ensureGitignore(tmpDir)

	require.NoError(t, err)
	assert.True(t, added)

	content, _ := os.ReadFile(gitignorePath)
	// Should use CRLF for new entry
	assert.Contains(t, string(content), ".amanmcp/\r\n")
}

// TestEnsureGitignore_HandlesNoTrailingNewline tests files without trailing newline
func TestEnsureGitignore_HandlesNoTrailingNewline(t *testing.T) {
	tmpDir := t.TempDir()
	gitignorePath := filepath.Join(tmpDir, ".gitignore")

	// Create .gitignore WITHOUT trailing newline
	existingContent := "*.log"
	err := os.WriteFile(gitignorePath, []byte(existingContent), 0644)
	require.NoError(t, err)

	added, err := ensureGitignore(tmpDir)

	require.NoError(t, err)
	assert.True(t, added)

	content, _ := os.ReadFile(gitignorePath)
	// Should add newline before entry
	assert.Contains(t, string(content), "*.log\n")
	assert.Contains(t, string(content), ".amanmcp/")
}

// TestEnsureGitignore_SkipsCommentedOut tests that commented entries don't count
func TestEnsureGitignore_SkipsCommentedOut(t *testing.T) {
	tmpDir := t.TempDir()
	gitignorePath := filepath.Join(tmpDir, ".gitignore")

	// Create .gitignore with commented .amanmcp
	existingContent := "*.log\n# .amanmcp/\n"
	err := os.WriteFile(gitignorePath, []byte(existingContent), 0644)
	require.NoError(t, err)

	added, err := ensureGitignore(tmpDir)

	require.NoError(t, err)
	assert.True(t, added, "should add entry when existing is commented")
}

// TestInitCmd_AddsGitignore tests the integration with init command
func TestInitCmd_AddsGitignore(t *testing.T) {
	tmpDir := t.TempDir()

	var stdout bytes.Buffer
	cmd := newInitCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--offline", "--config-only"})

	oldWd, _ := os.Getwd()
	err := os.Chdir(tmpDir)
	require.NoError(t, err)
	defer func() { _ = os.Chdir(oldWd) }()

	_ = cmd.Execute()

	// Check .gitignore was created with .amanmcp
	content, err := os.ReadFile(filepath.Join(tmpDir, ".gitignore"))
	require.NoError(t, err)
	assert.Contains(t, string(content), ".amanmcp/")

	// Check output shows it was added
	output := stdout.String()
	assert.Contains(t, output, ".gitignore")
}

// TestInitCmd_GitignoreIdempotent tests that multiple runs don't duplicate entry
func TestInitCmd_GitignoreIdempotent(t *testing.T) {
	tmpDir := t.TempDir()

	oldWd, _ := os.Getwd()
	err := os.Chdir(tmpDir)
	require.NoError(t, err)
	defer func() { _ = os.Chdir(oldWd) }()

	// Run init twice
	for i := 0; i < 2; i++ {
		cmd := newInitCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--offline", "--config-only", "--force"})
		_ = cmd.Execute()
	}

	// Check .gitignore has exactly one .amanmcp entry
	content, err := os.ReadFile(filepath.Join(tmpDir, ".gitignore"))
	require.NoError(t, err)

	count := bytes.Count(content, []byte(".amanmcp/"))
	assert.Equal(t, 1, count, "Should have exactly one .amanmcp/ entry after multiple runs")
}
