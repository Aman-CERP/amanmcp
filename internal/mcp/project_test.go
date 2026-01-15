package mcp

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectProjectInfo_GoProject(t *testing.T) {
	// Given: a directory with go.mod
	tmpDir := t.TempDir()
	goMod := `module github.com/test/myapp

go 1.21
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644))

	// When: detecting project info
	detector := NewProjectDetector(tmpDir, slog.Default())
	info := detector.Detect()

	// Then: project info is correctly detected
	assert.Equal(t, "myapp", info.Name)
	assert.Equal(t, tmpDir, info.RootPath)
	assert.Equal(t, "go", info.Type)
}

func TestDetectProjectInfo_NodeProject(t *testing.T) {
	// Given: a directory with package.json
	tmpDir := t.TempDir()
	packageJSON := `{
  "name": "my-node-app",
  "version": "1.0.0"
}
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(packageJSON), 0644))

	// When: detecting project info
	detector := NewProjectDetector(tmpDir, slog.Default())
	info := detector.Detect()

	// Then: project info is correctly detected
	assert.Equal(t, "my-node-app", info.Name)
	assert.Equal(t, tmpDir, info.RootPath)
	assert.Equal(t, "node", info.Type)
}

func TestDetectProjectInfo_PythonProject(t *testing.T) {
	// Given: a directory with pyproject.toml
	tmpDir := t.TempDir()
	pyproject := `[project]
name = "my-python-app"
version = "0.1.0"
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "pyproject.toml"), []byte(pyproject), 0644))

	// When: detecting project info
	detector := NewProjectDetector(tmpDir, slog.Default())
	info := detector.Detect()

	// Then: project info is correctly detected
	assert.Equal(t, "my-python-app", info.Name)
	assert.Equal(t, tmpDir, info.RootPath)
	assert.Equal(t, "python", info.Type)
}

func TestDetectProjectInfo_UnknownProject(t *testing.T) {
	// Given: a directory with no recognizable project files
	tmpDir := t.TempDir()

	// When: detecting project info
	detector := NewProjectDetector(tmpDir, slog.Default())
	info := detector.Detect()

	// Then: falls back to directory name
	assert.Equal(t, filepath.Base(tmpDir), info.Name)
	assert.Equal(t, tmpDir, info.RootPath)
	assert.Equal(t, "unknown", info.Type)
}

func TestDetectProjectInfo_GoModPriority(t *testing.T) {
	// Given: a directory with both go.mod and package.json
	tmpDir := t.TempDir()
	goMod := `module github.com/test/go-priority

go 1.21
`
	packageJSON := `{"name": "node-app"}`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(packageJSON), 0644))

	// When: detecting project info
	detector := NewProjectDetector(tmpDir, slog.Default())
	info := detector.Detect()

	// Then: go.mod takes priority
	assert.Equal(t, "go-priority", info.Name)
	assert.Equal(t, "go", info.Type)
}

func TestDetectProjectInfo_ScopedNpmPackage(t *testing.T) {
	// Given: a directory with scoped npm package
	tmpDir := t.TempDir()
	packageJSON := `{"name": "@myorg/my-package"}`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(packageJSON), 0644))

	// When: detecting project info
	detector := NewProjectDetector(tmpDir, slog.Default())
	info := detector.Detect()

	// Then: scoped package name is extracted
	assert.Equal(t, "my-package", info.Name)
	assert.Equal(t, "node", info.Type)
}
