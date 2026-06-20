package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Aman-CERP/amanmcp/internal/graph"
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

func TestIndexCmd_PopulatesGraphDBByDefault(t *testing.T) {
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

	// Then: graph.db should exist with the current schema and extracted edges.
	require.NoError(t, err)
	graphPath := filepath.Join(testDir, ".amanmcp", "graph.db")
	assert.FileExists(t, graphPath, "graph.db should be populated")

	repo, err := graph.OpenSQLiteRepository(graphPath)
	require.NoError(t, err)
	defer func() { require.NoError(t, repo.Close()) }()

	snapshot, err := repo.Snapshot(t.Context(), graph.StatusOptions{ProjectID: testGraphProjectID(t, testDir)})
	require.NoError(t, err)
	assert.Equal(t, graph.SchemaVersion, snapshot.SchemaVersion)
	assert.Equal(t, graph.GraphStatusFresh, snapshot.Status)
	assert.Greater(t, snapshot.Nodes.Total, 0)
	assert.Greater(t, snapshot.Edges.Total, 0)
	assert.Greater(t, snapshot.Edges.ByKind[string(graph.EdgeKindFileDeclaresPackage)], 0)
}

func TestIndexCmd_SkipGraphLeavesGraphAbsent(t *testing.T) {
	// Given: a test project directory
	testDir := t.TempDir()
	createTestProject(t, testDir)

	// When: running index command with graph extraction disabled
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", "--skip-graph", testDir})

	err := cmd.Execute()

	// Then: search indexing succeeds and graph.db is not created or mutated.
	require.NoError(t, err)
	assert.NoFileExists(t, filepath.Join(testDir, ".amanmcp", "graph.db"))
}

func TestIndexCmd_SkipGraphContinuesWithCorruptGraphDB(t *testing.T) {
	// Given: a project with a corrupt disposable graph overlay
	testDir := t.TempDir()
	createTestProject(t, testDir)
	dataDir := filepath.Join(testDir, ".amanmcp")
	require.NoError(t, os.MkdirAll(dataDir, 0755))
	graphPath := filepath.Join(dataDir, "graph.db")
	require.NoError(t, os.WriteFile(graphPath, []byte("not sqlite"), 0644))

	// When: running a search-only index
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", "--skip-graph", testDir})

	err := cmd.Execute()

	// Then: corrupt graph state is left untouched and search artifacts are still built.
	require.NoError(t, err)
	content, err := os.ReadFile(graphPath)
	require.NoError(t, err)
	assert.Equal(t, []byte("not sqlite"), content)
	assert.FileExists(t, filepath.Join(dataDir, "metadata.db"))
	assert.FileExists(t, filepath.Join(dataDir, "bm25.db"))
	assert.FileExists(t, filepath.Join(dataDir, "vectors.hnsw"))
}

func TestIndexCmd_GraphOnlyRebuildsGraphFromExistingIndex(t *testing.T) {
	// Given: a project with an existing search index and a removed graph overlay
	testDir := t.TempDir()
	createTestProject(t, testDir)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", "--skip-graph", testDir})
	require.NoError(t, cmd.Execute())

	dataDir := filepath.Join(testDir, ".amanmcp")

	// When: rebuilding only the graph
	cmd = NewRootCmd()
	buf = new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", "--graph-only", testDir})

	err := cmd.Execute()

	// Then: graph.db is rebuilt from the existing index without a full reindex.
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Graph build complete")

	repo, err := graph.OpenSQLiteRepository(filepath.Join(dataDir, "graph.db"))
	require.NoError(t, err)
	defer func() { require.NoError(t, repo.Close()) }()

	snapshot, err := repo.Snapshot(t.Context(), graph.StatusOptions{ProjectID: testGraphProjectID(t, testDir)})
	require.NoError(t, err)
	assert.Equal(t, graph.GraphStatusFresh, snapshot.Status)
	assert.Greater(t, snapshot.Edges.Total, 0)
}

func TestIndexCmd_ZeroChunkReindexLeavesExistingGraphSnapshot(t *testing.T) {
	// Given: a project with a healthy graph from a prior code index
	testDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(testDir, ".amanmcp.yaml"), []byte("embeddings:\n  provider: static\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "main.go"), []byte(`package main

func main() {}
`), 0644))

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", testDir})
	require.NoError(t, cmd.Execute())

	require.NoError(t, os.Remove(filepath.Join(testDir, "main.go")))

	// When: re-indexing reaches the zero-search-chunk path
	cmd = NewRootCmd()
	buf = new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", testDir})
	err := cmd.Execute()

	// Then: graph is not advanced ahead of the unchanged search index snapshot.
	require.NoError(t, err)
	repo, err := graph.OpenSQLiteRepository(filepath.Join(testDir, ".amanmcp", "graph.db"))
	require.NoError(t, err)
	defer func() { require.NoError(t, repo.Close()) }()

	nodes, err := repo.ListNodes(t.Context(), graph.NodeQuery{ProjectID: testGraphProjectID(t, testDir)})
	require.NoError(t, err)
	sourcePaths := make(map[string]bool)
	for _, node := range nodes {
		sourcePaths[node.SourcePath] = true
	}
	assert.True(t, sourcePaths["main.go"], "existing graph snapshot should remain intact")
}

func TestIndexCmd_ReindexPrunesGraphForDeletedFiles(t *testing.T) {
	// Given: a project whose previous graph contains a file that is later deleted
	testDir := t.TempDir()
	createTestProject(t, testDir)
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "stale.go"), []byte(`package main

func stale() {}
`), 0644))

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", testDir})
	require.NoError(t, cmd.Execute())

	require.NoError(t, os.Remove(filepath.Join(testDir, "stale.go")))

	// When: running a normal index again
	cmd = NewRootCmd()
	buf = new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", testDir})
	err := cmd.Execute()

	// Then: graph rows derived from the deleted file are removed.
	require.NoError(t, err)
	repo, err := graph.OpenSQLiteRepository(filepath.Join(testDir, ".amanmcp", "graph.db"))
	require.NoError(t, err)
	defer func() { require.NoError(t, repo.Close()) }()

	staleEdges, err := repo.ListEdges(t.Context(), graph.EdgeQuery{
		ProjectID:  testGraphProjectID(t, testDir),
		SourcePath: "stale.go",
	})
	require.NoError(t, err)
	assert.Empty(t, staleEdges)

	nodes, err := repo.ListNodes(t.Context(), graph.NodeQuery{ProjectID: testGraphProjectID(t, testDir)})
	require.NoError(t, err)
	for _, node := range nodes {
		assert.NotEqual(t, "stale.go", node.SourcePath)
	}
}

func TestIndexCmd_ForceGraphRebuildDuringNormalIndex(t *testing.T) {
	// Given: a corrupt graph overlay and no existing search index requirement
	testDir := t.TempDir()
	createTestProject(t, testDir)
	dataDir := filepath.Join(testDir, ".amanmcp")
	require.NoError(t, os.MkdirAll(dataDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "graph.db"), []byte("not sqlite"), 0644))

	// When: forcing only the graph overlay to rebuild during normal indexing
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", "--force-graph-rebuild", testDir})
	err := cmd.Execute()

	// Then: search indexing succeeds and the graph overlay is rebuilt fresh.
	require.NoError(t, err)
	repo, err := graph.OpenSQLiteRepository(filepath.Join(dataDir, "graph.db"))
	require.NoError(t, err)
	defer func() { require.NoError(t, repo.Close()) }()

	snapshot, err := repo.Snapshot(t.Context(), graph.StatusOptions{ProjectID: testGraphProjectID(t, testDir)})
	require.NoError(t, err)
	assert.Equal(t, graph.GraphStatusFresh, snapshot.Status)
	assert.Greater(t, snapshot.Nodes.Total, 0)
	assert.Greater(t, snapshot.Edges.Total, 0)
}

func TestIndexCmd_GraphOnlySkipsDeletedFilesFromStaleMetadata(t *testing.T) {
	// Given: an existing metadata index that still names a file deleted from disk
	testDir := t.TempDir()
	createTestProject(t, testDir)
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "stale.go"), []byte(`package main

func stale() {}
`), 0644))

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", "--skip-graph", testDir})
	require.NoError(t, cmd.Execute())
	require.NoError(t, os.Remove(filepath.Join(testDir, "stale.go")))

	// When: rebuilding only the graph from existing metadata
	cmd = NewRootCmd()
	buf = new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", "--graph-only", "--force-graph-rebuild", testDir})
	err := cmd.Execute()

	// Then: the missing file is skipped and the graph rebuild still succeeds.
	require.NoError(t, err)
	repo, err := graph.OpenSQLiteRepository(filepath.Join(testDir, ".amanmcp", "graph.db"))
	require.NoError(t, err)
	defer func() { require.NoError(t, repo.Close()) }()

	snapshot, err := repo.Snapshot(t.Context(), graph.StatusOptions{ProjectID: testGraphProjectID(t, testDir)})
	require.NoError(t, err)
	assert.Equal(t, graph.GraphStatusFresh, snapshot.Status)
	assert.Greater(t, snapshot.Edges.Total, 0)

	staleEdges, err := repo.ListEdges(t.Context(), graph.EdgeQuery{
		ProjectID:  testGraphProjectID(t, testDir),
		SourcePath: "stale.go",
	})
	require.NoError(t, err)
	assert.Empty(t, staleEdges)
}

func TestIndexCmd_GraphOnlyForceRebuildRecoversCorruptGraphDB(t *testing.T) {
	// Given: an existing search index and a corrupt graph overlay
	testDir := t.TempDir()
	createTestProject(t, testDir)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", "--skip-graph", testDir})
	require.NoError(t, cmd.Execute())

	dataDir := filepath.Join(testDir, ".amanmcp")
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "graph.db"), []byte("not sqlite"), 0644))

	// When: forcing a graph-only rebuild
	cmd = NewRootCmd()
	buf = new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", "--graph-only", "--force-graph-rebuild", testDir})
	err := cmd.Execute()

	// Then: the corrupt graph overlay is replaced without rebuilding vectors.
	require.NoError(t, err)
	repo, err := graph.OpenSQLiteRepository(filepath.Join(dataDir, "graph.db"))
	require.NoError(t, err)
	defer func() { require.NoError(t, repo.Close()) }()

	snapshot, err := repo.Snapshot(t.Context(), graph.StatusOptions{ProjectID: testGraphProjectID(t, testDir)})
	require.NoError(t, err)
	assert.Equal(t, graph.GraphStatusFresh, snapshot.Status)
	assert.Greater(t, snapshot.Nodes.Total, 0)
	assert.Greater(t, snapshot.Edges.Total, 0)
}

func TestIndexCmd_ForceGraphRebuildClearsExistingGraphOnly(t *testing.T) {
	// Given: an existing search index and graph overlay
	testDir := t.TempDir()
	createTestProject(t, testDir)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", testDir})
	require.NoError(t, cmd.Execute())

	vectorPath := filepath.Join(testDir, ".amanmcp", "vectors.hnsw")
	vectorInfo, err := os.Stat(vectorPath)
	require.NoError(t, err)

	// When: forcing only the graph overlay to rebuild
	cmd = NewRootCmd()
	buf = new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", "--graph-only", "--force-graph-rebuild", testDir})

	err = cmd.Execute()

	// Then: the graph is present and the vector store was not rewritten.
	require.NoError(t, err)
	newVectorInfo, err := os.Stat(vectorPath)
	require.NoError(t, err)
	assert.Equal(t, vectorInfo.ModTime(), newVectorInfo.ModTime())

	repo, err := graph.OpenSQLiteRepository(filepath.Join(testDir, ".amanmcp", "graph.db"))
	require.NoError(t, err)
	defer func() { require.NoError(t, repo.Close()) }()

	snapshot, err := repo.Snapshot(t.Context(), graph.StatusOptions{ProjectID: testGraphProjectID(t, testDir)})
	require.NoError(t, err)
	assert.Equal(t, graph.GraphStatusFresh, snapshot.Status)
	assert.Greater(t, snapshot.Nodes.Total, 0)
	assert.Greater(t, snapshot.Edges.Total, 0)
}

func TestIndexCmd_GraphFlagConflicts(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "skip graph and graph only",
			args: []string{"index", "--skip-graph", "--graph-only"},
			want: "--skip-graph and --graph-only are mutually exclusive",
		},
		{
			name: "skip graph and force graph rebuild",
			args: []string{"index", "--skip-graph", "--force-graph-rebuild"},
			want: "--skip-graph and --force-graph-rebuild are mutually exclusive",
		},
		{
			name: "force index and graph only",
			args: []string{"index", "--force", "--graph-only"},
			want: "--force and --graph-only are mutually exclusive",
		},
		{
			name: "resume and graph only",
			args: []string{"index", "--resume", "--graph-only"},
			want: "--resume and --graph-only are mutually exclusive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewRootCmd()
			buf := new(bytes.Buffer)
			cmd.SetOut(buf)
			cmd.SetErr(buf)
			cmd.SetArgs(tt.args)

			err := cmd.Execute()

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestIndexCmd_GraphOnlyRequiresExistingIndex(t *testing.T) {
	// Given: a project that has not been indexed yet
	testDir := t.TempDir()
	createTestProject(t, testDir)

	// When: rebuilding only the graph
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", "--graph-only", testDir})

	err := cmd.Execute()

	// Then: the command should fail clearly without initializing embedding.
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--graph-only requires an existing index")
	assert.Contains(t, err.Error(), "amanmcp index")
}

func TestIndexCmd_FailsOnCorruptGraphDBWithImplementedRecoveryHint(t *testing.T) {
	// Given: a project with a corrupt disposable graph overlay
	testDir := t.TempDir()
	createTestProject(t, testDir)
	dataDir := filepath.Join(testDir, ".amanmcp")
	require.NoError(t, os.MkdirAll(dataDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "graph.db"), []byte("not sqlite"), 0644))

	// When: running index without an explicit force recovery
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", testDir})

	err := cmd.Execute()

	// Then: the failure should name recovery commands that exist today.
	require.Error(t, err)
	assert.Contains(t, err.Error(), "corrupt graph database")
	assert.Contains(t, err.Error(), "amanmcp index --force")
	assert.NotContains(t, err.Error(), "amanmcp graph rebuild")
}

func TestIndexCmd_ForceRecoversFromCorruptGraphDB(t *testing.T) {
	// Given: a project with a corrupt disposable graph overlay
	testDir := t.TempDir()
	createTestProject(t, testDir)
	dataDir := filepath.Join(testDir, ".amanmcp")
	require.NoError(t, os.MkdirAll(dataDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "graph.db"), []byte("not sqlite"), 0644))

	// When: force reindexing
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", "--force", testDir})

	err := cmd.Execute()

	// Then: the corrupt graph DB is removed and recreated with populated graph data.
	require.NoError(t, err)
	repo, err := graph.OpenSQLiteRepository(filepath.Join(dataDir, "graph.db"))
	require.NoError(t, err)
	defer func() { require.NoError(t, repo.Close()) }()

	snapshot, err := repo.Snapshot(t.Context(), graph.StatusOptions{ProjectID: testGraphProjectID(t, testDir)})
	require.NoError(t, err)
	assert.Equal(t, graph.SchemaVersion, snapshot.SchemaVersion)
	assert.Equal(t, graph.GraphStatusFresh, snapshot.Status)
	assert.Greater(t, snapshot.Nodes.Total, 0)
	assert.Greater(t, snapshot.Edges.Total, 0)
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
	assert.Contains(t, output, "[GRAPH]", "Should report graph build progress")
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

func testGraphProjectID(t *testing.T, dir string) string {
	t.Helper()

	absDir, err := filepath.Abs(dir)
	require.NoError(t, err)
	return hashString(absDir)
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
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "metadata.db-wal"), []byte("test"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "metadata.db-shm"), []byte("test"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "vectors.hnsw"), []byte("test"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(dataDir, "bm25.bleve"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "bm25.bleve", "store"), []byte("test"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "bm25.db"), []byte("test"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "bm25.db-wal"), []byte("test"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "bm25.db-shm"), []byte("test"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "graph.db"), []byte("test"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "graph.db-wal"), []byte("test"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "graph.db-shm"), []byte("test"), 0644))

	// When: clearing index data
	err := clearIndexData(dataDir)

	// Then: all index files should be removed
	require.NoError(t, err)
	assert.NoFileExists(t, filepath.Join(dataDir, "metadata.db"))
	assert.NoFileExists(t, filepath.Join(dataDir, "metadata.db-wal"))
	assert.NoFileExists(t, filepath.Join(dataDir, "metadata.db-shm"))
	assert.NoFileExists(t, filepath.Join(dataDir, "vectors.hnsw"))
	assert.NoDirExists(t, filepath.Join(dataDir, "bm25.bleve"))
	assert.NoFileExists(t, filepath.Join(dataDir, "bm25.db"))
	assert.NoFileExists(t, filepath.Join(dataDir, "bm25.db-wal"))
	assert.NoFileExists(t, filepath.Join(dataDir, "bm25.db-shm"))
	assert.NoFileExists(t, filepath.Join(dataDir, "graph.db"))
	assert.NoFileExists(t, filepath.Join(dataDir, "graph.db-wal"))
	assert.NoFileExists(t, filepath.Join(dataDir, "graph.db-shm"))
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
