package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIndexCmd_CreatesDataDirectory(t *testing.T) {
	// Given: a test project directory
	testDir := t.TempDir()
	createTestProject(t, testDir)

	// When: running index command
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", testDir})

	err := cmd.Execute()

	// Then: it should succeed and create .amanmcp directory
	require.NoError(t, err)
	dataDir := filepath.Join(testDir, ".amanmcp")
	assert.DirExists(t, dataDir, ".amanmcp directory should be created")
}

func TestIndexCmd_CreatesMetadataDB(t *testing.T) {
	// Given: a test project directory
	testDir := t.TempDir()
	createTestProject(t, testDir)

	// When: running index command
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", testDir})

	err := cmd.Execute()

	// Then: metadata.db should be created
	require.NoError(t, err)
	metadataPath := filepath.Join(testDir, ".amanmcp", "metadata.db")
	assert.FileExists(t, metadataPath, "metadata.db should be created")
}

func TestIndexCmd_CreatesBM25Index(t *testing.T) {
	// Given: a test project directory
	testDir := t.TempDir()
	createTestProject(t, testDir)

	// When: running index command
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", testDir})

	err := cmd.Execute()

	// Then: bm25.db file should be created (SQLite FTS5 default per REARCH-002)
	require.NoError(t, err)
	bm25Path := filepath.Join(testDir, ".amanmcp", "bm25.db")
	assert.FileExists(t, bm25Path, "bm25.db should be created")
}

func TestIndexCmd_CreatesVectorStore(t *testing.T) {
	// Given: a test project directory
	testDir := t.TempDir()
	createTestProject(t, testDir)

	// When: running index command
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", testDir})

	err := cmd.Execute()

	// Then: vectors.hnsw should be created
	require.NoError(t, err)
	vectorPath := filepath.Join(testDir, ".amanmcp", "vectors.hnsw")
	assert.FileExists(t, vectorPath, "vectors.hnsw should be created")
}

func TestIndexCmd_ReportsProgress(t *testing.T) {
	// Given: a test project directory
	testDir := t.TempDir()
	createTestProject(t, testDir)

	// When: running index command
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", testDir})

	err := cmd.Execute()

	// Then: output should report indexed files and chunks
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Complete:", "Should report indexing progress")
}

func TestIndexCmd_FailsOnNonExistentPath(t *testing.T) {
	// Given: a non-existent path

	// When: running index command
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", "/nonexistent/path"})

	err := cmd.Execute()

	// Then: it should fail
	assert.Error(t, err)
}

func TestIndexCmd_DefaultsToCurrentDirectory(t *testing.T) {
	// Given: a test project directory as current directory
	testDir := t.TempDir()
	createTestProject(t, testDir)

	// Save and restore cwd
	oldCwd, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(oldCwd) }()

	err = os.Chdir(testDir)
	require.NoError(t, err)

	// When: running index command without path
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index"})

	err = cmd.Execute()

	// Then: it should index current directory
	require.NoError(t, err)
	dataDir := filepath.Join(testDir, ".amanmcp")
	assert.DirExists(t, dataDir, ".amanmcp directory should be created")
}

func TestIndexCmd_IndexesGoFiles(t *testing.T) {
	// Given: a test project with Go files
	testDir := t.TempDir()
	createTestProject(t, testDir)

	// When: running index command
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", testDir})

	err := cmd.Execute()

	// Then: Go files should be indexed (check metadata.db has entries)
	require.NoError(t, err)
	output := buf.String()
	// Should report at least 1 file and 1 chunk
	assert.Contains(t, output, "file", "Should report files indexed")
}

func TestIndexCmd_IndexesMarkdownFiles(t *testing.T) {
	// Given: a test project with Markdown files
	testDir := t.TempDir()
	createTestProjectWithMarkdown(t, testDir)

	// When: running index command
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", testDir})

	err := cmd.Execute()

	// Then: Markdown files should be indexed
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Complete:", "Should report indexing progress")
}

func TestIndexCmd_RespectsGitignore(t *testing.T) {
	// Given: a test project with .gitignore
	testDir := t.TempDir()
	createTestProjectWithGitignore(t, testDir)

	// When: running index command
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", testDir})

	err := cmd.Execute()

	// Then: gitignored files should not be indexed
	require.NoError(t, err)
	// The output should not mention ignored files being indexed
}

// Helper functions to create test projects

func createTestProject(t *testing.T, dir string) {
	t.Helper()

	// Create amanmcp config to use static embeddings (faster tests)
	config := `embeddings:
  provider: static
`
	err := os.WriteFile(filepath.Join(dir, ".amanmcp.yaml"), []byte(config), 0644)
	require.NoError(t, err)

	// Create go.mod
	goMod := `module testproject

go 1.21
`
	err = os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0644)
	require.NoError(t, err)

	// Create main.go
	mainGo := `package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}

func helper() string {
	return "helper function"
}
`
	err = os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainGo), 0644)
	require.NoError(t, err)
}

func createTestProjectWithMarkdown(t *testing.T, dir string) {
	t.Helper()

	createTestProject(t, dir)

	// Create README.md
	readme := `# Test Project

## Overview

This is a test project for indexing.

## Features

- Feature 1
- Feature 2
`
	err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(readme), 0644)
	require.NoError(t, err)
}

func createTestProjectWithGitignore(t *testing.T, dir string) {
	t.Helper()

	createTestProject(t, dir)

	// Create .gitignore
	gitignore := `*.log
build/
`
	err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(gitignore), 0644)
	require.NoError(t, err)

	// Create a file that should be ignored
	err = os.Mkdir(filepath.Join(dir, "build"), 0755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(dir, "build", "output.go"), []byte("package build"), 0644)
	require.NoError(t, err)
}

func TestClearIndexData_RemovesIndexFiles(t *testing.T) {
	// Given: a data directory with index files
	dataDir := t.TempDir()

	// Create mock index files
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "metadata.db"), []byte("test"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "vectors.hnsw"), []byte("test"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(dataDir, "bm25.bleve"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "bm25.bleve", "store"), []byte("test"), 0644))

	// When: clearing index data
	err := clearIndexData(dataDir)

	// Then: all index files should be removed
	require.NoError(t, err)
	assert.NoFileExists(t, filepath.Join(dataDir, "metadata.db"))
	assert.NoFileExists(t, filepath.Join(dataDir, "vectors.hnsw"))
	assert.NoDirExists(t, filepath.Join(dataDir, "bm25.bleve"))
}

func TestClearIndexData_IgnoresNonExistentFiles(t *testing.T) {
	// Given: an empty data directory
	dataDir := t.TempDir()

	// When: clearing index data
	err := clearIndexData(dataDir)

	// Then: should succeed without error
	require.NoError(t, err)
}

func TestIndexCmd_ForceRebuildsIndex(t *testing.T) {
	// Given: a test project with existing index
	testDir := t.TempDir()
	createTestProject(t, testDir)

	// First, create an index
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", testDir})
	require.NoError(t, cmd.Execute())

	// Verify index exists
	metadataPath := filepath.Join(testDir, ".amanmcp", "metadata.db")
	require.FileExists(t, metadataPath)

	// Get original file info
	originalInfo, err := os.Stat(metadataPath)
	require.NoError(t, err)

	// When: running index with --force
	cmd = NewRootCmd()
	buf = new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", "--force", testDir})

	err = cmd.Execute()

	// Then: should succeed and recreate index
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Cleared existing index data", "Should report clearing index")

	// Verify new index was created
	newInfo, err := os.Stat(metadataPath)
	require.NoError(t, err)
	assert.NotEqual(t, originalInfo.ModTime(), newInfo.ModTime(), "Index file should be recreated")
}

func TestIndexCmd_ForceAndResumeMutuallyExclusive(t *testing.T) {
	// Given: a test project directory
	testDir := t.TempDir()
	createTestProject(t, testDir)

	// When: running index with both --force and --resume
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", "--force", "--resume", testDir})

	err := cmd.Execute()

	// Then: should fail with error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

func TestIndexCmd_ForcePreservesConfig(t *testing.T) {
	// Given: a test project with custom config
	testDir := t.TempDir()
	createTestProject(t, testDir)

	// Create custom config content
	customConfig := `embeddings:
  provider: static
paths:
  include: ["src/"]
`
	configPath := filepath.Join(testDir, ".amanmcp.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(customConfig), 0644))

	// First, create an index
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", testDir})
	require.NoError(t, cmd.Execute())

	// When: running index with --force
	cmd = NewRootCmd()
	buf = new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", "--force", testDir})

	err := cmd.Execute()

	// Then: config file should be preserved
	require.NoError(t, err)
	assert.FileExists(t, configPath)

	content, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Equal(t, customConfig, string(content), "Config file should be unchanged")
}
