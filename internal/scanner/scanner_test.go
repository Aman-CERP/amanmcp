package scanner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		wantLang string
	}{
		// Go
		{name: "go file", path: "main.go", wantLang: "go"},
		{name: "go test file", path: "main_test.go", wantLang: "go"},
		{name: "go in directory", path: "pkg/lib/utils.go", wantLang: "go"},

		// JavaScript/TypeScript
		{name: "javascript", path: "app.js", wantLang: "javascript"},
		{name: "jsx", path: "Component.jsx", wantLang: "javascript"},
		{name: "mjs", path: "module.mjs", wantLang: "javascript"},
		{name: "typescript", path: "app.ts", wantLang: "typescript"},
		{name: "tsx", path: "Component.tsx", wantLang: "typescript"},

		// Python
		{name: "python", path: "script.py", wantLang: "python"},
		{name: "python pyw", path: "gui.pyw", wantLang: "python"},
		{name: "python stub", path: "types.pyi", wantLang: "python"},

		// Web
		{name: "html", path: "index.html", wantLang: "html"},
		{name: "htm", path: "page.htm", wantLang: "html"},
		{name: "css", path: "styles.css", wantLang: "css"},
		{name: "scss", path: "styles.scss", wantLang: "scss"},

		// Config/Data
		{name: "json", path: "config.json", wantLang: "json"},
		{name: "yaml", path: "config.yaml", wantLang: "yaml"},
		{name: "yml", path: "config.yml", wantLang: "yaml"},
		{name: "toml", path: "Cargo.toml", wantLang: "toml"},

		// Markdown
		{name: "markdown", path: "README.md", wantLang: "markdown"},
		{name: "mdx", path: "docs.mdx", wantLang: "markdown"},

		// Special files (exact match)
		{name: "Dockerfile", path: "Dockerfile", wantLang: "dockerfile"},
		{name: "Makefile", path: "Makefile", wantLang: "makefile"},
		{name: "makefile lowercase", path: "makefile", wantLang: "makefile"},

		// Other languages
		{name: "rust", path: "main.rs", wantLang: "rust"},
		{name: "java", path: "Main.java", wantLang: "java"},
		{name: "kotlin", path: "Main.kt", wantLang: "kotlin"},
		{name: "c", path: "main.c", wantLang: "c"},
		{name: "c header", path: "header.h", wantLang: "c"},
		{name: "cpp", path: "main.cpp", wantLang: "cpp"},
		{name: "ruby", path: "app.rb", wantLang: "ruby"},
		{name: "swift", path: "App.swift", wantLang: "swift"},
		{name: "php", path: "index.php", wantLang: "php"},
		{name: "shell", path: "script.sh", wantLang: "shell"},
		{name: "sql", path: "query.sql", wantLang: "sql"},

		// Unknown
		{name: "unknown extension", path: "file.xyz", wantLang: ""},
		{name: "no extension", path: "LICENSE", wantLang: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectLanguage(tt.path)
			assert.Equal(t, tt.wantLang, got)
		})
	}
}

func TestDetectContentType(t *testing.T) {
	tests := []struct {
		name     string
		language string
		wantType ContentType
	}{
		// Code languages
		{name: "go", language: "go", wantType: ContentTypeCode},
		{name: "javascript", language: "javascript", wantType: ContentTypeCode},
		{name: "typescript", language: "typescript", wantType: ContentTypeCode},
		{name: "python", language: "python", wantType: ContentTypeCode},
		{name: "rust", language: "rust", wantType: ContentTypeCode},
		{name: "java", language: "java", wantType: ContentTypeCode},
		{name: "html", language: "html", wantType: ContentTypeCode},
		{name: "css", language: "css", wantType: ContentTypeCode},

		// Markdown
		{name: "markdown", language: "markdown", wantType: ContentTypeMarkdown},
		{name: "rst", language: "rst", wantType: ContentTypeMarkdown},

		// Config
		{name: "json", language: "json", wantType: ContentTypeConfig},
		{name: "yaml", language: "yaml", wantType: ContentTypeConfig},
		{name: "toml", language: "toml", wantType: ContentTypeConfig},
		{name: "dockerfile", language: "dockerfile", wantType: ContentTypeConfig},
		{name: "makefile", language: "makefile", wantType: ContentTypeConfig},

		// Text (fallback)
		{name: "text", language: "text", wantType: ContentTypeText},
		{name: "unknown", language: "unknown", wantType: ContentTypeText},
		{name: "empty", language: "", wantType: ContentTypeText},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectContentType(tt.language)
			assert.Equal(t, tt.wantType, got)
		})
	}
}

func TestScanner_Scan_BasicFiles(t *testing.T) {
	// Create temp directory with test files
	tmpDir := t.TempDir()

	// Create test files
	files := map[string]string{
		"main.go":           "package main\n\nfunc main() {}\n",
		"pkg/lib.go":        "package pkg\n\nfunc Helper() {}\n",
		"README.md":         "# Test Project\n",
		"config.yaml":       "version: 1\n",
		"src/app.ts":        "export const app = {};\n",
	}

	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0o644))
	}

	// Scan
	scanner, err := New()
	require.NoError(t, err)
	results, err := scanner.Scan(context.Background(), &ScanOptions{
		RootDir: tmpDir,
	})
	require.NoError(t, err)

	// Collect results
	var fileInfos []*FileInfo
	for result := range results {
		require.NoError(t, result.Error)
		fileInfos = append(fileInfos, result.File)
	}

	// Verify all files found
	assert.Len(t, fileInfos, 5)

	// Verify file metadata
	filesByPath := make(map[string]*FileInfo)
	for _, fi := range fileInfos {
		filesByPath[fi.Path] = fi
	}

	// Check main.go
	mainGo := filesByPath["main.go"]
	require.NotNil(t, mainGo, "main.go should be found")
	assert.Equal(t, "go", mainGo.Language)
	assert.Equal(t, ContentTypeCode, mainGo.ContentType)
	assert.False(t, mainGo.IsGenerated)

	// Check README.md
	readme := filesByPath["README.md"]
	require.NotNil(t, readme, "README.md should be found")
	assert.Equal(t, "markdown", readme.Language)
	assert.Equal(t, ContentTypeMarkdown, readme.ContentType)

	// Check config.yaml
	config := filesByPath["config.yaml"]
	require.NotNil(t, config, "config.yaml should be found")
	assert.Equal(t, "yaml", config.Language)
	assert.Equal(t, ContentTypeConfig, config.ContentType)
}

func TestScanner_Scan_ExcludesNodeModules(t *testing.T) {
	tmpDir := t.TempDir()

	// Create files including node_modules
	files := map[string]string{
		"index.js":                 "console.log('hello');\n",
		"node_modules/lodash/index.js": "module.exports = {};\n",
		"node_modules/react/index.js":  "module.exports = {};\n",
	}

	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0o644))
	}

	scanner, err := New()
	require.NoError(t, err)
	results, err := scanner.Scan(context.Background(), &ScanOptions{
		RootDir: tmpDir,
	})
	require.NoError(t, err)

	var fileInfos []*FileInfo
	for result := range results {
		require.NoError(t, result.Error)
		fileInfos = append(fileInfos, result.File)
	}

	// Only index.js should be found
	assert.Len(t, fileInfos, 1)
	assert.Equal(t, "index.js", fileInfos[0].Path)
}

func TestScanner_Scan_ExcludesGitDir(t *testing.T) {
	tmpDir := t.TempDir()

	files := map[string]string{
		"main.go":             "package main\n",
		".git/config":         "[core]\n",
		".git/objects/abc123": "blob\n",
	}

	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0o644))
	}

	scanner, err := New()
	require.NoError(t, err)
	results, err := scanner.Scan(context.Background(), &ScanOptions{
		RootDir: tmpDir,
	})
	require.NoError(t, err)

	var fileInfos []*FileInfo
	for result := range results {
		require.NoError(t, result.Error)
		fileInfos = append(fileInfos, result.File)
	}

	// Only main.go should be found
	assert.Len(t, fileInfos, 1)
	assert.Equal(t, "main.go", fileInfos[0].Path)
}

func TestScanner_Scan_ExcludesVendor(t *testing.T) {
	tmpDir := t.TempDir()

	files := map[string]string{
		"main.go":                     "package main\n",
		"vendor/github.com/foo/bar.go": "package foo\n",
	}

	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0o644))
	}

	scanner, err := New()
	require.NoError(t, err)
	results, err := scanner.Scan(context.Background(), &ScanOptions{
		RootDir: tmpDir,
	})
	require.NoError(t, err)

	var fileInfos []*FileInfo
	for result := range results {
		require.NoError(t, result.Error)
		fileInfos = append(fileInfos, result.File)
	}

	assert.Len(t, fileInfos, 1)
	assert.Equal(t, "main.go", fileInfos[0].Path)
}

func TestScanner_Scan_ExcludesSensitiveFiles(t *testing.T) {
	tmpDir := t.TempDir()

	files := map[string]string{
		"main.go":           "package main\n",
		".env":              "SECRET=xyz\n",
		".env.local":        "SECRET=abc\n",
		".env.production":   "SECRET=prod\n",
		"credentials.json":  `{"key": "secret"}` + "\n",
		"secrets.yaml":      "password: secret\n",
		"private.key":       "-----BEGIN RSA PRIVATE KEY-----\n",
		"server.pem":        "-----BEGIN CERTIFICATE-----\n",
		"id_rsa":            "-----BEGIN OPENSSH PRIVATE KEY-----\n",
		".aws/credentials":  "[default]\n",
		".ssh/id_rsa":       "-----BEGIN OPENSSH PRIVATE KEY-----\n",
		".netrc":            "machine github.com\n",
		".npmrc":            "//registry.npmjs.org/:_authToken=token\n",
	}

	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0o644))
	}

	scanner, err := New()
	require.NoError(t, err)
	results, err := scanner.Scan(context.Background(), &ScanOptions{
		RootDir: tmpDir,
	})
	require.NoError(t, err)

	var fileInfos []*FileInfo
	for result := range results {
		require.NoError(t, result.Error)
		fileInfos = append(fileInfos, result.File)
	}

	// Only main.go should be found
	assert.Len(t, fileInfos, 1)
	assert.Equal(t, "main.go", fileInfos[0].Path)
}

func TestScanner_Scan_RespectsGitignore(t *testing.T) {
	tmpDir := t.TempDir()

	files := map[string]string{
		".gitignore":       "ignored/\n*.log\nbuild/\n",
		"main.go":          "package main\n",
		"ignored/secret.txt": "secret data\n",
		"debug.log":        "debug output\n",
		"build/output.js":  "compiled code\n",
		"src/app.go":       "package src\n",
	}

	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0o644))
	}

	scanner, err := New()
	require.NoError(t, err)
	results, err := scanner.Scan(context.Background(), &ScanOptions{
		RootDir:          tmpDir,
		RespectGitignore: true,
	})
	require.NoError(t, err)

	var fileInfos []*FileInfo
	for result := range results {
		require.NoError(t, result.Error)
		fileInfos = append(fileInfos, result.File)
	}

	// Should find main.go and src/app.go (not ignored/, *.log, or build/)
	paths := make([]string, len(fileInfos))
	for i, fi := range fileInfos {
		paths[i] = fi.Path
	}

	assert.Contains(t, paths, "main.go")
	assert.Contains(t, paths, "src/app.go")
	assert.NotContains(t, paths, "ignored/secret.txt")
	assert.NotContains(t, paths, "debug.log")
	assert.NotContains(t, paths, "build/output.js")
}

func TestScanner_Scan_NestedGitignore(t *testing.T) {
	tmpDir := t.TempDir()

	files := map[string]string{
		".gitignore":           "*.log\n",
		"main.go":              "package main\n",
		"app.log":              "root log\n",
		"src/.gitignore":       "temp/\n",
		"src/app.go":           "package src\n",
		"src/temp/cache.txt":   "cache\n",
		"src/other.log":        "src log\n",
	}

	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0o644))
	}

	scanner, err := New()
	require.NoError(t, err)
	results, err := scanner.Scan(context.Background(), &ScanOptions{
		RootDir:          tmpDir,
		RespectGitignore: true,
	})
	require.NoError(t, err)

	var fileInfos []*FileInfo
	for result := range results {
		require.NoError(t, result.Error)
		fileInfos = append(fileInfos, result.File)
	}

	paths := make([]string, len(fileInfos))
	for i, fi := range fileInfos {
		paths[i] = fi.Path
	}

	assert.Contains(t, paths, "main.go")
	assert.Contains(t, paths, "src/app.go")
	// *.log should be excluded by root .gitignore
	assert.NotContains(t, paths, "app.log")
	assert.NotContains(t, paths, "src/other.log")
	// temp/ should be excluded by src/.gitignore
	assert.NotContains(t, paths, "src/temp/cache.txt")
}

func TestScanner_Scan_GitignoreNegation(t *testing.T) {
	tmpDir := t.TempDir()

	files := map[string]string{
		".gitignore":      "*.log\n!important.log\n",
		"debug.log":       "debug\n",
		"important.log":   "important\n",
		"main.go":         "package main\n",
	}

	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0o644))
	}

	scanner, err := New()
	require.NoError(t, err)
	results, err := scanner.Scan(context.Background(), &ScanOptions{
		RootDir:          tmpDir,
		RespectGitignore: true,
	})
	require.NoError(t, err)

	var fileInfos []*FileInfo
	for result := range results {
		require.NoError(t, result.Error)
		fileInfos = append(fileInfos, result.File)
	}

	paths := make([]string, len(fileInfos))
	for i, fi := range fileInfos {
		paths[i] = fi.Path
	}

	assert.Contains(t, paths, "main.go")
	assert.Contains(t, paths, "important.log", "negated pattern should include file")
	assert.NotContains(t, paths, "debug.log")
}

func TestScanner_Scan_DetectsGeneratedFiles(t *testing.T) {
	tmpDir := t.TempDir()

	files := map[string]string{
		"main.go":           "package main\n\nfunc main() {}\n",
		"generated.go":      "// Code generated by tool. DO NOT EDIT.\npackage main\n",
		"mock_service.go":   "// DO NOT EDIT\npackage mock\n",
		"parser.go":         "// Generated by yacc\npackage parser\n",
		"proto.pb.go":       "// Code generated by protoc-gen-go. DO NOT EDIT.\npackage proto\n",
	}

	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0o644))
	}

	scanner, err := New()
	require.NoError(t, err)
	results, err := scanner.Scan(context.Background(), &ScanOptions{
		RootDir: tmpDir,
	})
	require.NoError(t, err)

	var fileInfos []*FileInfo
	for result := range results {
		require.NoError(t, result.Error)
		fileInfos = append(fileInfos, result.File)
	}

	filesByPath := make(map[string]*FileInfo)
	for _, fi := range fileInfos {
		filesByPath[fi.Path] = fi
	}

	assert.False(t, filesByPath["main.go"].IsGenerated)
	assert.True(t, filesByPath["generated.go"].IsGenerated)
	assert.True(t, filesByPath["mock_service.go"].IsGenerated)
	assert.True(t, filesByPath["parser.go"].IsGenerated)
	assert.True(t, filesByPath["proto.pb.go"].IsGenerated)
}

func TestScanner_Scan_SkipsSymlinks(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a real file
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "real.go"), []byte("package main\n"), 0o644))

	// Create a subdirectory with a file
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "subdir", "sub.go"), []byte("package sub\n"), 0o644))

	// Create a symlink to the file
	err := os.Symlink(filepath.Join(tmpDir, "real.go"), filepath.Join(tmpDir, "link.go"))
	if err != nil {
		t.Skip("symlinks not supported on this platform")
	}

	// Create a symlink to parent (would cause infinite loop)
	err = os.Symlink(tmpDir, filepath.Join(tmpDir, "subdir", "parent"))
	require.NoError(t, err)

	scanner, err := New()
	require.NoError(t, err)
	results, err := scanner.Scan(context.Background(), &ScanOptions{
		RootDir:        tmpDir,
		FollowSymlinks: false, // Default
	})
	require.NoError(t, err)

	var fileInfos []*FileInfo
	for result := range results {
		require.NoError(t, result.Error)
		fileInfos = append(fileInfos, result.File)
	}

	paths := make([]string, len(fileInfos))
	for i, fi := range fileInfos {
		paths[i] = fi.Path
	}

	// Should find real files but not symlinks
	assert.Contains(t, paths, "real.go")
	assert.Contains(t, paths, "subdir/sub.go")
	assert.NotContains(t, paths, "link.go")
	// Should not have traversed into the symlink
	assert.Len(t, fileInfos, 2)
}

func TestScanner_Scan_SkipsBinaryFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a text file
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\n"), 0o644))

	// Create a binary file (contains null bytes)
	binaryContent := []byte{0x00, 0x01, 0x02, 0x03, 0x04}
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "binary.dat"), binaryContent, 0o644))

	// Create a file that looks like it has extension but is binary
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "image.png"), binaryContent, 0o644))

	scanner, err := New()
	require.NoError(t, err)
	results, err := scanner.Scan(context.Background(), &ScanOptions{
		RootDir: tmpDir,
	})
	require.NoError(t, err)

	var fileInfos []*FileInfo
	for result := range results {
		require.NoError(t, result.Error)
		fileInfos = append(fileInfos, result.File)
	}

	// Only main.go should be found
	assert.Len(t, fileInfos, 1)
	assert.Equal(t, "main.go", fileInfos[0].Path)
}

func TestScanner_Scan_SkipsLargeFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a small file
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "small.go"), []byte("package main\n"), 0o644))

	// Create a "large" file (for testing, we'll use a smaller limit)
	largeContent := make([]byte, 1024*1024) // 1MB
	for i := range largeContent {
		largeContent[i] = 'a'
	}
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "large.go"), largeContent, 0o644))

	scanner, err := New()
	require.NoError(t, err)
	results, err := scanner.Scan(context.Background(), &ScanOptions{
		RootDir:     tmpDir,
		MaxFileSize: 100 * 1024, // 100KB limit
	})
	require.NoError(t, err)

	var fileInfos []*FileInfo
	for result := range results {
		require.NoError(t, result.Error)
		fileInfos = append(fileInfos, result.File)
	}

	// Only small.go should be found
	assert.Len(t, fileInfos, 1)
	assert.Equal(t, "small.go", fileInfos[0].Path)
}

func TestScanner_Scan_CustomExcludePatterns(t *testing.T) {
	tmpDir := t.TempDir()

	files := map[string]string{
		"main.go":          "package main\n",
		"test_data/file.go": "package test\n",
		"fixtures/data.go":  "package fixtures\n",
	}

	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0o644))
	}

	scanner, err := New()
	require.NoError(t, err)
	results, err := scanner.Scan(context.Background(), &ScanOptions{
		RootDir:         tmpDir,
		ExcludePatterns: []string{"**/test_data/**", "**/fixtures/**"},
	})
	require.NoError(t, err)

	var fileInfos []*FileInfo
	for result := range results {
		require.NoError(t, result.Error)
		fileInfos = append(fileInfos, result.File)
	}

	assert.Len(t, fileInfos, 1)
	assert.Equal(t, "main.go", fileInfos[0].Path)
}

func TestScanner_Scan_IncludePatterns(t *testing.T) {
	tmpDir := t.TempDir()

	files := map[string]string{
		"main.go":       "package main\n",
		"app.ts":        "const app = {};\n",
		"README.md":     "# README\n",
		"config.yaml":   "version: 1\n",
	}

	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0o644))
	}

	scanner, err := New()
	require.NoError(t, err)
	results, err := scanner.Scan(context.Background(), &ScanOptions{
		RootDir:         tmpDir,
		IncludePatterns: []string{"*.go", "*.ts"},
	})
	require.NoError(t, err)

	var fileInfos []*FileInfo
	for result := range results {
		require.NoError(t, result.Error)
		fileInfos = append(fileInfos, result.File)
	}

	paths := make([]string, len(fileInfos))
	for i, fi := range fileInfos {
		paths[i] = fi.Path
	}

	assert.Len(t, fileInfos, 2)
	assert.Contains(t, paths, "main.go")
	assert.Contains(t, paths, "app.ts")
}

func TestScanner_Scan_ReturnsCorrectMetadata(t *testing.T) {
	tmpDir := t.TempDir()

	content := "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"
	filePath := filepath.Join(tmpDir, "main.go")
	require.NoError(t, os.WriteFile(filePath, []byte(content), 0o644))

	// Get file info for comparison
	stat, err := os.Stat(filePath)
	require.NoError(t, err)

	scanner, err := New()
	require.NoError(t, err)
	results, err := scanner.Scan(context.Background(), &ScanOptions{
		RootDir: tmpDir,
	})
	require.NoError(t, err)

	var fileInfo *FileInfo
	for result := range results {
		require.NoError(t, result.Error)
		fileInfo = result.File
	}

	require.NotNil(t, fileInfo)
	assert.Equal(t, "main.go", fileInfo.Path)
	assert.Equal(t, filePath, fileInfo.AbsPath)
	assert.Equal(t, stat.Size(), fileInfo.Size)
	assert.WithinDuration(t, stat.ModTime(), fileInfo.ModTime, time.Second)
	assert.Equal(t, "go", fileInfo.Language)
	assert.Equal(t, ContentTypeCode, fileInfo.ContentType)
	assert.False(t, fileInfo.IsGenerated)
}

func TestScanner_Scan_ContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()

	// Create many files
	for i := 0; i < 100; i++ {
		path := filepath.Join(tmpDir, "dir", "subdir", "file"+string(rune('0'+i%10))+".go")
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte("package main\n"), 0o644))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Ensure cancel is called on all paths

	scanner, err := New()
	require.NoError(t, err)
	results, err := scanner.Scan(ctx, &ScanOptions{
		RootDir: tmpDir,
	})
	require.NoError(t, err)

	// Read a few results then cancel
	count := 0
	for result := range results {
		if result.Error != nil {
			break
		}
		count++
		if count >= 5 {
			cancel()
		}
	}

	// Should have stopped early
	assert.Less(t, count, 100)
}

func TestScanner_Scan_EmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	scanner, err := New()
	require.NoError(t, err)
	results, err := scanner.Scan(context.Background(), &ScanOptions{
		RootDir: tmpDir,
	})
	require.NoError(t, err)

	var fileInfos []*FileInfo
	for result := range results {
		require.NoError(t, result.Error)
		fileInfos = append(fileInfos, result.File)
	}

	assert.Empty(t, fileInfos)
}

func TestScanner_Scan_NonExistentDirectory(t *testing.T) {
	scanner, err := New()
	require.NoError(t, err)
	_, err = scanner.Scan(context.Background(), &ScanOptions{
		RootDir: "/nonexistent/path/that/does/not/exist",
	})
	require.Error(t, err)
}

func TestScanner_Scan_ExcludesMinifiedFiles(t *testing.T) {
	tmpDir := t.TempDir()

	files := map[string]string{
		"app.js":     "function app() {}\n",
		"app.min.js": "function a(){}\n",
		"styles.css": "body { margin: 0; }\n",
		"styles.min.css": "body{margin:0}\n",
	}

	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0o644))
	}

	scanner, err := New()
	require.NoError(t, err)
	results, err := scanner.Scan(context.Background(), &ScanOptions{
		RootDir: tmpDir,
	})
	require.NoError(t, err)

	var fileInfos []*FileInfo
	for result := range results {
		require.NoError(t, result.Error)
		fileInfos = append(fileInfos, result.File)
	}

	paths := make([]string, len(fileInfos))
	for i, fi := range fileInfos {
		paths[i] = fi.Path
	}

	assert.Contains(t, paths, "app.js")
	assert.Contains(t, paths, "styles.css")
	assert.NotContains(t, paths, "app.min.js")
	assert.NotContains(t, paths, "styles.min.css")
}

func TestScanner_Scan_ExcludesLockFiles(t *testing.T) {
	tmpDir := t.TempDir()

	files := map[string]string{
		"main.go":           "package main\n",
		"package-lock.json": "{}\n",
		"yarn.lock":         "# yarn\n",
		"pnpm-lock.yaml":    "lockfileVersion: 5\n",
		"go.sum":            "module v0.0.0\n",
	}

	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0o644))
	}

	scanner, err := New()
	require.NoError(t, err)
	results, err := scanner.Scan(context.Background(), &ScanOptions{
		RootDir: tmpDir,
	})
	require.NoError(t, err)

	var fileInfos []*FileInfo
	for result := range results {
		require.NoError(t, result.Error)
		fileInfos = append(fileInfos, result.File)
	}

	assert.Len(t, fileInfos, 1)
	assert.Equal(t, "main.go", fileInfos[0].Path)
}

func TestScanner_Scan_ExcludesPycache(t *testing.T) {
	tmpDir := t.TempDir()

	files := map[string]string{
		"app.py":                    "print('hello')\n",
		"__pycache__/app.cpython-39.pyc": "binary\n",
	}

	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0o644))
	}

	scanner, err := New()
	require.NoError(t, err)
	results, err := scanner.Scan(context.Background(), &ScanOptions{
		RootDir: tmpDir,
	})
	require.NoError(t, err)

	var fileInfos []*FileInfo
	for result := range results {
		require.NoError(t, result.Error)
		fileInfos = append(fileInfos, result.File)
	}

	assert.Len(t, fileInfos, 1)
	assert.Equal(t, "app.py", fileInfos[0].Path)
}

func TestScanner_Scan_ExcludesDistBuild(t *testing.T) {
	tmpDir := t.TempDir()

	files := map[string]string{
		"src/app.ts":      "export const app = {};\n",
		"dist/app.js":     "var app = {};\n",
		"build/output.js": "compiled\n",
	}

	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0o644))
	}

	scanner, err := New()
	require.NoError(t, err)
	results, err := scanner.Scan(context.Background(), &ScanOptions{
		RootDir: tmpDir,
	})
	require.NoError(t, err)

	var fileInfos []*FileInfo
	for result := range results {
		require.NoError(t, result.Error)
		fileInfos = append(fileInfos, result.File)
	}

	assert.Len(t, fileInfos, 1)
	assert.Equal(t, "src/app.ts", fileInfos[0].Path)
}

func TestScanner_Scan_ExcludesAmanPMDir(t *testing.T) {
	// BUG-062 FIX: PM docs excluded via config pattern (.aman-pm/**), not hardcoded defaults
	// This tests that .amanmcp.yaml exclusions work correctly (Unix Philosophy: no hardcoded values)
	tmpDir := t.TempDir()

	files := map[string]string{
		"main.go":                      "package main\n",
		".aman-pm/index.yaml":          "version: 1\n",
		".aman-pm/backlog/FEAT-001.md": "# Feature\n",
	}

	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0o644))
	}

	scanner, err := New()
	require.NoError(t, err)
	results, err := scanner.Scan(context.Background(), &ScanOptions{
		RootDir: tmpDir,
		// Config-based exclusion (from .amanmcp.yaml), not hardcoded defaults
		ExcludePatterns: []string{".aman-pm/**"},
	})
	require.NoError(t, err)

	var fileInfos []*FileInfo
	for result := range results {
		require.NoError(t, result.Error)
		fileInfos = append(fileInfos, result.File)
	}

	// Only main.go should be found - .aman-pm/** excluded via config pattern
	assert.Len(t, fileInfos, 1)
	assert.Equal(t, "main.go", fileInfos[0].Path)
}

// =============================================================================
// F04: Gitignore Parser Bug Fixes from F03 Validation
// =============================================================================

func TestScanner_Scan_GitignorePathPatterns(t *testing.T) {
	// Bug #1 from F03: Path patterns like src/temp/ not matched correctly
	tmpDir := t.TempDir()

	files := map[string]string{
		".gitignore":              "src/temp/\ndocs/internal/\n",
		"main.go":                 "package main\n",
		"src/app.go":              "package src\n",
		"src/temp/cache.go":       "package temp\n",
		"docs/public/readme.md":   "public docs\n",
		"docs/internal/secret.md": "secret docs\n",
	}

	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0o644))
	}

	scanner, err := New()
	require.NoError(t, err)
	results, err := scanner.Scan(context.Background(), &ScanOptions{
		RootDir:          tmpDir,
		RespectGitignore: true,
	})
	require.NoError(t, err)

	var fileInfos []*FileInfo
	for result := range results {
		require.NoError(t, result.Error)
		fileInfos = append(fileInfos, result.File)
	}

	paths := make([]string, len(fileInfos))
	for i, fi := range fileInfos {
		paths[i] = fi.Path
	}

	// Should include
	assert.Contains(t, paths, "main.go")
	assert.Contains(t, paths, "src/app.go")
	assert.Contains(t, paths, "docs/public/readme.md")

	// Should exclude (Bug #1 fix)
	assert.NotContains(t, paths, "src/temp/cache.go", "src/temp/ pattern should exclude src/temp/cache.go")
	assert.NotContains(t, paths, "docs/internal/secret.md", "docs/internal/ pattern should exclude docs/internal/secret.md")
}

func TestScanner_Scan_GitignoreAnchoredPatterns(t *testing.T) {
	// Bug #2 from F03: Anchored patterns like /temp/ not supported
	tmpDir := t.TempDir()

	files := map[string]string{
		".gitignore":         "/temp/\n",
		"main.go":            "package main\n",
		"temp/root.go":       "package temp\n",
		"src/temp/nested.go": "package nested\n",
	}

	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0o644))
	}

	scanner, err := New()
	require.NoError(t, err)
	results, err := scanner.Scan(context.Background(), &ScanOptions{
		RootDir:          tmpDir,
		RespectGitignore: true,
	})
	require.NoError(t, err)

	var fileInfos []*FileInfo
	for result := range results {
		require.NoError(t, result.Error)
		fileInfos = append(fileInfos, result.File)
	}

	paths := make([]string, len(fileInfos))
	for i, fi := range fileInfos {
		paths[i] = fi.Path
	}

	// Should include
	assert.Contains(t, paths, "main.go")
	assert.Contains(t, paths, "src/temp/nested.go", "nested temp should NOT be excluded by /temp/")

	// Should exclude (Bug #2 fix)
	assert.NotContains(t, paths, "temp/root.go", "/temp/ pattern should exclude temp/ at root")
}

// =============================================================================
// F22.5: Scanner.New() returns error (not panic)
// =============================================================================

func TestScanner_New_ReturnsScanner(t *testing.T) {
	// Given: nothing special
	// When: creating a new scanner
	s, err := New()

	// Then: returns scanner without error
	require.NoError(t, err)
	require.NotNil(t, s)
	require.NotNil(t, s.gitignoreCache)
}

// =============================================================================
// DEBT-001: Gitignore Cache LRU Eviction
// =============================================================================

func TestScanner_GitignoreCache_HasBoundedSize(t *testing.T) {
	// Given: a new scanner
	s, err := New()
	require.NoError(t, err)

	// Then: gitignore cache should be initialized with bounded size
	// The cache uses hashicorp/golang-lru with gitignoreCacheSize (1000) limit
	assert.NotNil(t, s.gitignoreCache, "gitignore cache should be initialized")

	// Verify the cache can store entries and has LRU behavior
	// Add entries up to capacity - cache should accept them
	for i := 0; i < 100; i++ {
		key := filepath.Join("/test/path", string(rune('a'+i%26)), "dir"+string(rune('0'+i%10)))
		s.gitignoreCache.Add(key, nil)
	}

	// Verify entries were added
	assert.Equal(t, 100, s.gitignoreCache.Len(), "cache should contain 100 entries")
}

func TestScanner_GitignoreCache_EvictsOldEntries(t *testing.T) {
	// Given: a scanner with some cached entries
	s, err := New()
	require.NoError(t, err)

	// When: we add more entries than the cache can hold
	// gitignoreCacheSize is 1000, so add 1100 entries
	for i := 0; i < 1100; i++ {
		key := filepath.Join("/test/path", string(rune('a'+i%26)), string(rune('a'+(i/26)%26)), "dir"+string(rune('0'+i%10)))
		s.gitignoreCache.Add(key, nil)
	}

	// Then: cache should have evicted old entries (LRU behavior)
	// Cache size should be at most gitignoreCacheSize (1000)
	assert.LessOrEqual(t, s.gitignoreCache.Len(), gitignoreCacheSize,
		"cache should not exceed gitignoreCacheSize limit")
	assert.Equal(t, gitignoreCacheSize, s.gitignoreCache.Len(),
		"cache should be at capacity after adding 1100 entries")
}

// =============================================================================
// BUG-022: Scanner Gitignore Cache Invalidation
// =============================================================================

func TestScanner_InvalidateGitignoreCache(t *testing.T) {
	// Given: scanner with populated cache
	s, err := New()
	require.NoError(t, err)

	// Populate cache with test entries
	for i := 0; i < 50; i++ {
		key := filepath.Join("/test/path", fmt.Sprintf("dir%d", i))
		s.gitignoreCache.Add(key, nil)
	}
	assert.Equal(t, 50, s.gitignoreCache.Len(), "cache should have 50 entries")

	// When: invalidating cache
	s.InvalidateGitignoreCache()

	// Then: cache is empty
	assert.Equal(t, 0, s.gitignoreCache.Len(), "cache should be empty after invalidation")
}

func TestScanner_InvalidateGitignoreCache_ThreadSafe(t *testing.T) {
	// Given: scanner with cache
	s, err := New()
	require.NoError(t, err)

	// Populate cache
	for i := 0; i < 100; i++ {
		s.gitignoreCache.Add(fmt.Sprintf("dir%d", i), nil)
	}

	// When: concurrent invalidation and access
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.InvalidateGitignoreCache()
		}()
	}
	wg.Wait()

	// Then: no panic and cache is empty
	assert.Equal(t, 0, s.gitignoreCache.Len())
}

func TestScanner_Scan_GitignoreDoubleStarPatterns(t *testing.T) {
	// Bug #3 from F03: **/pattern in gitignore files not handled
	tmpDir := t.TempDir()

	files := map[string]string{
		".gitignore":          "**/cache/\n**/logs/*.log\n",
		"main.go":             "package main\n",
		"cache/data.go":       "package cache\n",
		"src/cache/store.go":  "package cache\n",
		"logs/app.log":        "log content\n",
		"src/logs/debug.log":  "debug content\n",
		"logs/app.txt":        "text content\n",
	}

	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0o644))
	}

	scanner, err := New()
	require.NoError(t, err)
	results, err := scanner.Scan(context.Background(), &ScanOptions{
		RootDir:          tmpDir,
		RespectGitignore: true,
	})
	require.NoError(t, err)

	var fileInfos []*FileInfo
	for result := range results {
		require.NoError(t, result.Error)
		fileInfos = append(fileInfos, result.File)
	}

	paths := make([]string, len(fileInfos))
	for i, fi := range fileInfos {
		paths[i] = fi.Path
	}

	// Should include
	assert.Contains(t, paths, "main.go")
	assert.Contains(t, paths, "logs/app.txt", "*.txt should not be excluded by *.log pattern")

	// Should exclude (Bug #3 fix)
	assert.NotContains(t, paths, "cache/data.go", "**/cache/ should exclude cache/data.go")
	assert.NotContains(t, paths, "src/cache/store.go", "**/cache/ should exclude src/cache/store.go")
	assert.NotContains(t, paths, "logs/app.log", "**/logs/*.log should exclude logs/app.log")
	assert.NotContains(t, paths, "src/logs/debug.log", "**/logs/*.log should exclude src/logs/debug.log")
}

// =============================================================================
// DEBT-003: Scanner Channel Abandonment Edge Cases
// =============================================================================

// drainWithTimeout drains a channel until closed or timeout.
// Returns true if channel was closed, false if timeout.
func drainWithTimeout(ch <-chan ScanResult, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return true // Channel closed
			}
		case <-timer.C:
			return false // Timeout
		}
	}
}

func TestScanner_Scan_ImmediateCancellation(t *testing.T) {
	// Given: a directory with many files
	tmpDir := t.TempDir()
	for i := 0; i < 50; i++ {
		path := filepath.Join(tmpDir, "dir", "subdir", "file"+string(rune('a'+i%26))+".go")
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte("package main\n"), 0o644))
	}

	ctx, cancel := context.WithCancel(context.Background())

	scanner, err := New()
	require.NoError(t, err)

	baseGoroutines := runtime.NumGoroutine()

	// When: we start a scan and immediately cancel before reading
	results, err := scanner.Scan(ctx, &ScanOptions{RootDir: tmpDir})
	require.NoError(t, err)

	cancel() // Cancel immediately, don't read any results

	// Then: channel should close and goroutine should terminate
	closed := drainWithTimeout(results, 2*time.Second)
	assert.True(t, closed, "channel should close after context cancellation")

	// Verify goroutine cleanup
	assert.Eventually(t, func() bool {
		return runtime.NumGoroutine() <= baseGoroutines+2
	}, 2*time.Second, 50*time.Millisecond, "scanner goroutine should terminate")
}

func TestScanner_Scan_PreCancelledContext(t *testing.T) {
	// Given: a directory with files and a pre-cancelled context
	tmpDir := t.TempDir()
	for i := 0; i < 20; i++ {
		require.NoError(t, os.WriteFile(
			filepath.Join(tmpDir, "file"+string(rune('a'+i))+".go"),
			[]byte("package main\n"), 0o644))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Pre-cancel the context

	scanner, err := New()
	require.NoError(t, err)

	baseGoroutines := runtime.NumGoroutine()

	// When: we scan with a pre-cancelled context
	results, err := scanner.Scan(ctx, &ScanOptions{RootDir: tmpDir})
	require.NoError(t, err)

	// Then: channel should close quickly with zero or minimal results
	count := 0
	timeout := time.After(500 * time.Millisecond)
loop:
	for {
		select {
		case _, ok := <-results:
			if !ok {
				break loop
			}
			count++
		case <-timeout:
			t.Fatal("timeout waiting for channel close with pre-cancelled context")
		}
	}

	// Should have very few or zero results (race with initial context check)
	assert.LessOrEqual(t, count, 5, "pre-cancelled context should yield minimal results")

	// Verify goroutine cleanup
	assert.Eventually(t, func() bool {
		return runtime.NumGoroutine() <= baseGoroutines+2
	}, time.Second, 50*time.Millisecond, "scanner goroutine should terminate")
}

func TestScanner_Scan_GoroutineLeakVerification(t *testing.T) {
	// Given: a directory with many files
	tmpDir := t.TempDir()
	for i := 0; i < 200; i++ {
		dir := filepath.Join(tmpDir, "pkg"+string(rune('a'+i%10)))
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "file"+string(rune('0'+i%10))+".go"),
			[]byte("package main\n"), 0o644))
	}

	// Force GC and settle
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	baseGoroutines := runtime.NumGoroutine()

	scanner, err := New()
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	// When: we start a scan, read a few results, then abandon the channel
	results, err := scanner.Scan(ctx, &ScanOptions{RootDir: tmpDir})
	require.NoError(t, err)

	// Read only 3 results
	for i := 0; i < 3; i++ {
		<-results
	}

	// Cancel and abandon channel (intentionally don't drain)
	cancel()

	// Then: goroutine should terminate even without draining
	assert.Eventually(t, func() bool {
		runtime.GC()
		return runtime.NumGoroutine() <= baseGoroutines+2
	}, 3*time.Second, 100*time.Millisecond,
		"scanner goroutine should terminate after context cancel without draining")
}

func TestScanner_Scan_CancellationWithFullBuffer(t *testing.T) {
	// Given: enough files to fill the channel buffer
	tmpDir := t.TempDir()

	// Buffer is workers*10, default workers = NumCPU
	bufferSize := runtime.NumCPU() * 10
	fileCount := bufferSize * 3 // Ensure buffer will definitely fill

	for i := 0; i < fileCount; i++ {
		require.NoError(t, os.WriteFile(
			filepath.Join(tmpDir, "file"+string(rune('a'+i%26))+string(rune('0'+i%10))+".go"),
			[]byte("package main\n"), 0o644))
	}

	ctx, cancel := context.WithCancel(context.Background())

	scanner, err := New()
	require.NoError(t, err)

	baseGoroutines := runtime.NumGoroutine()

	// When: we start a scan but don't consume, letting buffer fill
	results, err := scanner.Scan(ctx, &ScanOptions{RootDir: tmpDir})
	require.NoError(t, err)

	// Wait for buffer to likely fill
	time.Sleep(200 * time.Millisecond)

	// Cancel while buffer is full
	cancel()

	// Then: should not deadlock, channel should close
	closed := drainWithTimeout(results, 2*time.Second)
	assert.True(t, closed, "channel should close even when buffer was full")

	// Verify cleanup
	assert.Eventually(t, func() bool {
		return runtime.NumGoroutine() <= baseGoroutines+2
	}, 2*time.Second, 50*time.Millisecond, "scanner goroutine should terminate")
}

func TestScanner_Scan_SubmoduleCancellation(t *testing.T) {
	// Given: a project with a submodule structure
	tmpDir := t.TempDir()

	// Create main project files
	for i := 0; i < 10; i++ {
		require.NoError(t, os.WriteFile(
			filepath.Join(tmpDir, "main"+string(rune('0'+i))+".go"),
			[]byte("package main\n"), 0o644))
	}

	// Create submodule structure
	submodulePath := filepath.Join(tmpDir, "libs", "utils")
	require.NoError(t, os.MkdirAll(submodulePath, 0o755))

	// Create .gitmodules file
	gitmodules := `[submodule "libs/utils"]
	path = libs/utils
	url = https://github.com/example/utils.git
`
	require.NoError(t, os.WriteFile(
		filepath.Join(tmpDir, ".gitmodules"), []byte(gitmodules), 0o644))

	// Create .git directory in submodule to mark it as initialized
	require.NoError(t, os.MkdirAll(filepath.Join(submodulePath, ".git"), 0o755))

	// Create many files in submodule
	for i := 0; i < 100; i++ {
		require.NoError(t, os.WriteFile(
			filepath.Join(submodulePath, "util"+string(rune('a'+i%26))+string(rune('0'+i%10))+".go"),
			[]byte("package utils\n"), 0o644))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Ensure cleanup on all paths

	scanner, err := New()
	require.NoError(t, err)

	baseGoroutines := runtime.NumGoroutine()

	// When: we scan with submodules enabled and cancel mid-scan
	results, err := scanner.Scan(ctx, &ScanOptions{
		RootDir: tmpDir,
		Submodules: &config.SubmoduleConfig{
			Enabled: true,
		},
	})
	require.NoError(t, err)

	// Read a few results then cancel
	count := 0
	for result := range results {
		if result.Error != nil {
			break
		}
		count++
		if count >= 5 {
			cancel() // Cancel explicitly mid-scan
			break
		}
	}

	// Drain remaining
	drainWithTimeout(results, 2*time.Second)

	// Then: goroutine should terminate
	assert.Eventually(t, func() bool {
		return runtime.NumGoroutine() <= baseGoroutines+2
	}, 2*time.Second, 50*time.Millisecond, "submodule scanner goroutine should terminate")
}

func TestScanner_Scan_MultipleConcurrentScansCancel(t *testing.T) {
	// Given: a directory with files
	tmpDir := t.TempDir()
	for i := 0; i < 50; i++ {
		require.NoError(t, os.WriteFile(
			filepath.Join(tmpDir, "file"+string(rune('a'+i%26))+".go"),
			[]byte("package main\n"), 0o644))
	}

	scanner, err := New()
	require.NoError(t, err)

	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	baseGoroutines := runtime.NumGoroutine()

	const numScans = 5
	contexts := make([]context.Context, numScans)
	cancels := make([]context.CancelFunc, numScans)
	resultChans := make([]<-chan ScanResult, numScans)

	// When: we start multiple concurrent scans
	for i := 0; i < numScans; i++ {
		contexts[i], cancels[i] = context.WithCancel(context.Background())
		resultChans[i], err = scanner.Scan(contexts[i], &ScanOptions{RootDir: tmpDir})
		require.NoError(t, err)
	}

	// Read a few results from each
	for i := 0; i < numScans; i++ {
		<-resultChans[i]
		<-resultChans[i]
	}

	// Cancel all scans
	for i := 0; i < numScans; i++ {
		cancels[i]()
	}

	// Drain all channels concurrently
	done := make(chan bool, numScans)
	for i := 0; i < numScans; i++ {
		go func(ch <-chan ScanResult) {
			drainWithTimeout(ch, 2*time.Second)
			done <- true
		}(resultChans[i])
	}

	// Wait for all drains with timeout
	timeout := time.After(3 * time.Second)
	for i := 0; i < numScans; i++ {
		select {
		case <-done:
		case <-timeout:
			t.Fatal("timeout waiting for channels to close")
		}
	}

	// Then: all goroutines should clean up
	assert.Eventually(t, func() bool {
		runtime.GC()
		return runtime.NumGoroutine() <= baseGoroutines+2
	}, 3*time.Second, 100*time.Millisecond,
		"all scanner goroutines should terminate after cancellation")
}

// TestMatchDirPattern_DirGlob tests directory pattern matching for dir/** patterns.
// BUG-062 FIX: This tests that .aman-pm/** style patterns work from config.
func TestMatchDirPattern_DirGlob(t *testing.T) {
	tests := []struct {
		name    string
		relPath string
		pattern string
		want    bool
	}{
		// dir/** patterns (no leading **/) - used in .amanmcp.yaml
		{
			name:    ".aman-pm/** matches root dir",
			relPath: ".aman-pm",
			pattern: ".aman-pm/**",
			want:    true,
		},
		{
			name:    ".aman-pm/** matches nested path",
			relPath: ".aman-pm/backlog",
			pattern: ".aman-pm/**",
			want:    true,
		},
		{
			name:    ".aman-pm/** matches deeply nested",
			relPath: ".aman-pm/backlog/features",
			pattern: ".aman-pm/**",
			want:    true,
		},
		{
			name:    "archive/** matches root dir",
			relPath: "archive",
			pattern: "archive/**",
			want:    true,
		},
		{
			name:    "archive/** matches nested",
			relPath: "archive/old",
			pattern: "archive/**",
			want:    true,
		},
		{
			name:    ".aman-pm/** should NOT match other dirs",
			relPath: "other",
			pattern: ".aman-pm/**",
			want:    false,
		},
		{
			name:    ".aman-pm/** should NOT match similar names",
			relPath: "aman-pm",
			pattern: ".aman-pm/**",
			want:    false,
		},
		{
			name:    ".aman-pm/** should NOT match .aman-pm-backup",
			relPath: ".aman-pm-backup",
			pattern: ".aman-pm/**",
			want:    false,
		},
		// **/ prefix patterns should still work
		{
			name:    "**/node_modules/** matches anywhere",
			relPath: "node_modules",
			pattern: "**/node_modules/**",
			want:    true,
		},
		{
			name:    "**/node_modules/** matches nested",
			relPath: "packages/api/node_modules",
			pattern: "**/node_modules/**",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchDirPattern(tt.relPath, tt.pattern)
			assert.Equal(t, tt.want, got, "matchDirPattern(%q, %q)", tt.relPath, tt.pattern)
		})
	}
}

// TestMatchFilePattern_DirGlob tests pattern matching for dir/** and dir/prefix*.ext patterns.
// These patterns are commonly used in .amanmcp.yaml exclude configurations.
func TestMatchFilePattern_DirGlob(t *testing.T) {
	tests := []struct {
		name     string
		baseName string
		relPath  string
		pattern  string
		want     bool
	}{
		// dir/** patterns (no leading **/) - used in .amanmcp.yaml
		{
			name:     "archive/** matches file in archive dir",
			baseName: "file.md",
			relPath:  "archive/file.md",
			pattern:  "archive/**",
			want:     true,
		},
		{
			name:     "archive/** matches nested file",
			baseName: "file.md",
			relPath:  "archive/analysis/file.md",
			pattern:  "archive/**",
			want:     true,
		},
		{
			name:     "archive/** matches deeply nested file",
			baseName: "file.md",
			relPath:  "archive/sub/deep/file.md",
			pattern:  "archive/**",
			want:     true,
		},
		{
			name:     "archive/** should NOT match archive2 dir",
			baseName: "file.md",
			relPath:  "archive2/file.md",
			pattern:  "archive/**",
			want:     false,
		},
		{
			name:     "archive/** should NOT match file named archive",
			baseName: "archive",
			relPath:  "archive",
			pattern:  "archive/**",
			want:     false,
		},
		{
			name:     "docs/postmortems/** matches nested",
			baseName: "rca.md",
			relPath:  "docs/postmortems/rca.md",
			pattern:  "docs/postmortems/**",
			want:     true,
		},
		{
			name:     "docs/sessions/** matches",
			baseName: "session.md",
			relPath:  "docs/sessions/2024/session.md",
			pattern:  "docs/sessions/**",
			want:     true,
		},

		// dir/prefix*.ext patterns - used for selective file exclusion
		{
			name:     "docs/bugs/BUG-0*.md matches BUG-001.md",
			baseName: "BUG-001.md",
			relPath:  "docs/bugs/BUG-001.md",
			pattern:  "docs/bugs/BUG-0*.md",
			want:     true,
		},
		{
			name:     "docs/bugs/BUG-0*.md matches BUG-099.md",
			baseName: "BUG-099.md",
			relPath:  "docs/bugs/BUG-099.md",
			pattern:  "docs/bugs/BUG-0*.md",
			want:     true,
		},
		{
			name:     "docs/bugs/BUG-0*.md should NOT match BUG-100.md",
			baseName: "BUG-100.md",
			relPath:  "docs/bugs/BUG-100.md",
			pattern:  "docs/bugs/BUG-0*.md",
			want:     false,
		},
		{
			name:     "docs/tech-debt/DEBT-*.md matches DEBT-001.md",
			baseName: "DEBT-001.md",
			relPath:  "docs/tech-debt/DEBT-001.md",
			pattern:  "docs/tech-debt/DEBT-*.md",
			want:     true,
		},

		// Character class patterns [0-2]
		{
			name:     "BUG-0[0-2]*.md matches BUG-001.md",
			baseName: "BUG-001.md",
			relPath:  "docs/bugs/BUG-001.md",
			pattern:  "docs/bugs/BUG-0[0-2]*.md",
			want:     true,
		},
		{
			name:     "BUG-0[0-2]*.md matches BUG-029.md",
			baseName: "BUG-029.md",
			relPath:  "docs/bugs/BUG-029.md",
			pattern:  "docs/bugs/BUG-0[0-2]*.md",
			want:     true,
		},
		{
			name:     "BUG-0[0-2]*.md should NOT match BUG-037.md",
			baseName: "BUG-037.md",
			relPath:  "docs/bugs/BUG-037.md",
			pattern:  "docs/bugs/BUG-0[0-2]*.md",
			want:     false,
		},

		// Existing patterns should still work
		{
			name:     "**/node_modules/** still works",
			baseName: "index.js",
			relPath:  "node_modules/lodash/index.js",
			pattern:  "**/node_modules/**",
			want:     true,
		},
		{
			name:     "**/*.min.js still works",
			baseName: "app.min.js",
			relPath:  "dist/app.min.js",
			pattern:  "**/*.min.js",
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchFilePattern(tt.baseName, tt.relPath, tt.pattern)
			assert.Equal(t, tt.want, got, "matchFilePattern(%q, %q, %q)", tt.baseName, tt.relPath, tt.pattern)
		})
	}
}

// TestScanner_Scan_DirGlobExcludePatterns tests that dir/** patterns work in scan exclusions.
func TestScanner_Scan_DirGlobExcludePatterns(t *testing.T) {
	tmpDir := t.TempDir()

	// Create files mimicking .amanmcp.yaml exclude patterns
	files := map[string]string{
		"main.go":                       "package main\n",
		"README.md":                     "# README\n",
		"archive/old.md":                "# Old\n",
		"archive/analysis/report.md":    "# Report\n",
		"docs/postmortems/rca.md":       "# RCA\n",
		"docs/bugs/BUG-001.md":          "# BUG-001\n",
		"docs/bugs/BUG-037.md":          "# BUG-037\n",
		"docs/tech-debt/DEBT-001.md":    "# DEBT-001\n",
		"docs/specs/features/F01-core.md": "# F01\n",
	}

	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0o644))
	}

	scanner, err := New()
	require.NoError(t, err)
	results, err := scanner.Scan(context.Background(), &ScanOptions{
		RootDir: tmpDir,
		ExcludePatterns: []string{
			"archive/**",
			"docs/postmortems/**",
			"docs/bugs/BUG-0[0-2]*.md",
			"docs/tech-debt/DEBT-*.md",
		},
	})
	require.NoError(t, err)

	var paths []string
	for result := range results {
		require.NoError(t, result.Error)
		paths = append(paths, result.File.Path)
	}

	// These should be included (not excluded)
	assert.Contains(t, paths, "main.go", "main.go should be included")
	assert.Contains(t, paths, "README.md", "README.md should be included")
	assert.Contains(t, paths, "docs/bugs/BUG-037.md", "BUG-037.md should NOT be excluded (> BUG-029)")
	assert.Contains(t, paths, "docs/specs/features/F01-core.md", "F01-core.md should be included")

	// These should be excluded
	assert.NotContains(t, paths, "archive/old.md", "archive/** should exclude archive/old.md")
	assert.NotContains(t, paths, "archive/analysis/report.md", "archive/** should exclude nested files")
	assert.NotContains(t, paths, "docs/postmortems/rca.md", "docs/postmortems/** should exclude")
	assert.NotContains(t, paths, "docs/bugs/BUG-001.md", "BUG-0[0-2]*.md should exclude BUG-001.md")
	assert.NotContains(t, paths, "docs/tech-debt/DEBT-001.md", "DEBT-*.md should exclude DEBT-001.md")
}
