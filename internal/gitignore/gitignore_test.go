package gitignore

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// AC01: Basic Pattern Matching
// =============================================================================

func TestMatcher_Match_SimplePatterns(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		path     string
		isDir    bool
		expected bool
	}{
		// Simple filename patterns
		{name: "exact filename match", pattern: "foo.txt", path: "foo.txt", isDir: false, expected: true},
		{name: "exact filename no match", pattern: "foo.txt", path: "bar.txt", isDir: false, expected: false},
		{name: "filename in subdir", pattern: "foo.txt", path: "src/foo.txt", isDir: false, expected: true},
		{name: "filename deep nested", pattern: "foo.txt", path: "a/b/c/foo.txt", isDir: false, expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()
			m.AddPattern(tt.pattern)
			got := m.Match(tt.path, tt.isDir)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestMatcher_Match_WildcardPatterns(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		path     string
		isDir    bool
		expected bool
	}{
		// Extension wildcards
		{name: "*.log matches .log", pattern: "*.log", path: "error.log", isDir: false, expected: true},
		{name: "*.log matches deep .log", pattern: "*.log", path: "logs/error.log", isDir: false, expected: true},
		{name: "*.log no match .txt", pattern: "*.log", path: "error.txt", isDir: false, expected: false},
		{name: "*.js matches js file", pattern: "*.js", path: "app.js", isDir: false, expected: true},

		// Prefix wildcards
		{name: "test* matches testfile", pattern: "test*", path: "testfile.go", isDir: false, expected: true},
		{name: "test* matches test_util", pattern: "test*", path: "test_util.go", isDir: false, expected: true},
		{name: "test* no match production", pattern: "test*", path: "production.go", isDir: false, expected: false},

		// Question mark (single char)
		{name: "file?.txt matches file1.txt", pattern: "file?.txt", path: "file1.txt", isDir: false, expected: true},
		{name: "file?.txt matches fileA.txt", pattern: "file?.txt", path: "fileA.txt", isDir: false, expected: true},
		{name: "file?.txt no match file12.txt", pattern: "file?.txt", path: "file12.txt", isDir: false, expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()
			m.AddPattern(tt.pattern)
			got := m.Match(tt.path, tt.isDir)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestMatcher_Match_DoubleStarPatterns(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		path     string
		isDir    bool
		expected bool
	}{
		// **/name - matches at any level
		{name: "**/node_modules at root", pattern: "**/node_modules", path: "node_modules", isDir: true, expected: true},
		{name: "**/node_modules nested", pattern: "**/node_modules", path: "packages/foo/node_modules", isDir: true, expected: true},
		{name: "**/test file at root", pattern: "**/test", path: "test", isDir: false, expected: true},
		{name: "**/test file nested", pattern: "**/test", path: "foo/bar/test", isDir: false, expected: true},

		// name/** - matches everything inside
		{name: "logs/** matches file inside", pattern: "logs/**", path: "logs/error.log", isDir: false, expected: true},
		{name: "logs/** matches nested", pattern: "logs/**", path: "logs/2024/01/error.log", isDir: false, expected: true},
		{name: "logs/** no match outside", pattern: "logs/**", path: "src/logs/error.log", isDir: false, expected: false},

		// **/*.ext - extension anywhere
		{name: "**/*.log at root", pattern: "**/*.log", path: "error.log", isDir: false, expected: true},
		{name: "**/*.log nested", pattern: "**/*.log", path: "logs/error.log", isDir: false, expected: true},
		{name: "**/*.log deep nested", pattern: "**/*.log", path: "a/b/c/d/error.log", isDir: false, expected: true},
		{name: "**/*.log no match .txt", pattern: "**/*.log", path: "error.txt", isDir: false, expected: false},

		// a/**/b - zero or more dirs between
		{name: "a/**/b direct", pattern: "a/**/b", path: "a/b", isDir: false, expected: true},
		{name: "a/**/b one level", pattern: "a/**/b", path: "a/x/b", isDir: false, expected: true},
		{name: "a/**/b two levels", pattern: "a/**/b", path: "a/x/y/b", isDir: false, expected: true},
		{name: "a/**/b no match wrong prefix", pattern: "a/**/b", path: "c/x/b", isDir: false, expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()
			m.AddPattern(tt.pattern)
			got := m.Match(tt.path, tt.isDir)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestMatcher_Match_RootedPatterns(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		path     string
		isDir    bool
		expected bool
	}{
		// /pattern - only matches at root
		{name: "/build at root", pattern: "/build", path: "build", isDir: true, expected: true},
		{name: "/build not nested", pattern: "/build", path: "src/build", isDir: true, expected: false},
		{name: "/temp/ at root dir", pattern: "/temp/", path: "temp", isDir: true, expected: true},
		{name: "/temp/ nested", pattern: "/temp/", path: "src/temp", isDir: true, expected: false},
		{name: "/config.json at root", pattern: "/config.json", path: "config.json", isDir: false, expected: true},
		{name: "/config.json nested", pattern: "/config.json", path: "src/config.json", isDir: false, expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()
			m.AddPattern(tt.pattern)
			got := m.Match(tt.path, tt.isDir)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// =============================================================================
// AC02: Negation Support
// =============================================================================

func TestMatcher_Match_Negation(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		path     string
		isDir    bool
		expected bool
	}{
		{
			name:     "negation overrides previous match",
			patterns: []string{"*.log", "!important.log"},
			path:     "important.log",
			isDir:    false,
			expected: false, // not ignored
		},
		{
			name:     "negation doesn't affect non-matching",
			patterns: []string{"*.log", "!important.log"},
			path:     "debug.log",
			isDir:    false,
			expected: true, // still ignored
		},
		{
			name:     "multiple negations",
			patterns: []string{"*", "!*.go", "!*.md"},
			path:     "main.go",
			isDir:    false,
			expected: false, // not ignored
		},
		{
			name:     "negation for dir",
			patterns: []string{"temp/", "!temp/important/"},
			path:     "temp/important",
			isDir:    true,
			expected: false, // not ignored
		},
		{
			name:     "re-ignore after negation",
			patterns: []string{"*.log", "!important.log", "really_important.log"},
			path:     "really_important.log",
			isDir:    false,
			expected: true, // ignored again
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()
			for _, p := range tt.patterns {
				m.AddPattern(p)
			}
			got := m.Match(tt.path, tt.isDir)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// =============================================================================
// AC03: Directory Patterns
// =============================================================================

func TestMatcher_Match_DirectoryPatterns(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		path     string
		isDir    bool
		expected bool
	}{
		// Trailing slash means directory only
		{name: "build/ matches directory", pattern: "build/", path: "build", isDir: true, expected: true},
		{name: "build/ not file", pattern: "build/", path: "build", isDir: false, expected: false},
		{name: "logs/ matches nested dir", pattern: "logs/", path: "src/logs", isDir: true, expected: true},
		{name: "logs/ not nested file", pattern: "logs/", path: "src/logs", isDir: false, expected: false},

		// No trailing slash matches both
		{name: "build matches dir", pattern: "build", path: "build", isDir: true, expected: true},
		{name: "build matches file", pattern: "build", path: "build", isDir: false, expected: true},

		// Directory patterns with wildcards
		{name: "temp*/ matches temp1 dir", pattern: "temp*/", path: "temp1", isDir: true, expected: true},
		{name: "temp*/ not temp1 file", pattern: "temp*/", path: "temp1", isDir: false, expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()
			m.AddPattern(tt.pattern)
			got := m.Match(tt.path, tt.isDir)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// =============================================================================
// AC04: Nested Gitignore (via AddPatternWithBase)
// =============================================================================

func TestMatcher_Match_NestedPatterns(t *testing.T) {
	tests := []struct {
		name     string
		patterns []struct {
			pattern string
			base    string
		}
		path     string
		isDir    bool
		expected bool
	}{
		{
			name: "root pattern applies everywhere",
			patterns: []struct {
				pattern string
				base    string
			}{
				{pattern: "*.tmp", base: ""},
			},
			path:     "src/data.tmp",
			isDir:    false,
			expected: true,
		},
		{
			name: "nested pattern only in subdir",
			patterns: []struct {
				pattern string
				base    string
			}{
				{pattern: "*.generated.go", base: "src"},
			},
			path:     "src/code.generated.go",
			isDir:    false,
			expected: true,
		},
		{
			name: "nested pattern not at root",
			patterns: []struct {
				pattern string
				base    string
			}{
				{pattern: "*.generated.go", base: "src"},
			},
			path:     "code.generated.go",
			isDir:    false,
			expected: false,
		},
		{
			name: "both root and nested patterns",
			patterns: []struct {
				pattern string
				base    string
			}{
				{pattern: "*.tmp", base: ""},
				{pattern: "cache/", base: "src"},
			},
			path:     "foo.tmp",
			isDir:    false,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()
			for _, p := range tt.patterns {
				m.AddPatternWithBase(p.pattern, p.base)
			}
			got := m.Match(tt.path, tt.isDir)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// =============================================================================
// AC05: Edge Cases
// =============================================================================

func TestMatcher_Parse_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectRules int
	}{
		{name: "empty line", input: "", expectRules: 0},
		{name: "whitespace only", input: "   ", expectRules: 0},
		{name: "comment", input: "# this is a comment", expectRules: 0},
		{name: "valid pattern", input: "*.log", expectRules: 1},
		{name: "pattern with trailing space", input: "*.log  ", expectRules: 1},
		{name: "pattern with leading space", input: "  *.log", expectRules: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()
			m.AddPattern(tt.input)
			assert.Equal(t, tt.expectRules, len(m.rules))
		})
	}
}

func TestMatcher_Match_EscapedHash(t *testing.T) {
	m := New()
	m.AddPattern(`\#important`)

	// Should match literal #important
	assert.True(t, m.Match("#important", false))
	assert.False(t, m.Match("important", false))
}

func TestMatcher_Match_EscapedExclamation(t *testing.T) {
	m := New()
	m.AddPattern(`\!important`)

	// Should match literal !important, not be a negation
	assert.True(t, m.Match("!important", false))
}

func TestMatcher_Match_TrailingSpacesEscaped(t *testing.T) {
	m := New()
	// A trailing backslash-space should preserve the space
	m.AddPattern(`file\ `)

	assert.True(t, m.Match("file ", false))
	assert.False(t, m.Match("file", false))
}

// =============================================================================
// F03 Bug Fixes
// =============================================================================

func TestMatcher_Match_PathPatterns_Bug1(t *testing.T) {
	// Bug #1: Path patterns like src/temp/ not matched correctly
	m := New()
	m.AddPattern("src/temp/")
	m.AddPattern("docs/internal/")

	// These SHOULD be ignored
	assert.True(t, m.Match("src/temp/cache.go", false), "src/temp/cache.go should be ignored")
	assert.True(t, m.Match("src/temp", true), "src/temp dir should be ignored")
	assert.True(t, m.Match("docs/internal/secret.md", false), "docs/internal/secret.md should be ignored")

	// These should NOT be ignored
	assert.False(t, m.Match("src/other.go", false))
	assert.False(t, m.Match("other/temp/file.go", false))
}

func TestMatcher_Match_AnchoredPatterns_Bug2(t *testing.T) {
	// Bug #2: Anchored patterns like /temp/ not supported
	m := New()
	m.AddPattern("/temp/")

	// At root - SHOULD be ignored
	assert.True(t, m.Match("temp", true), "temp dir at root should be ignored")
	assert.True(t, m.Match("temp/root.go", false), "temp/root.go should be ignored")

	// Nested - should NOT be ignored
	assert.False(t, m.Match("src/temp", true), "src/temp should NOT be ignored")
	assert.False(t, m.Match("src/temp/nested.go", false), "src/temp/nested.go should NOT be ignored")
}

func TestMatcher_Match_DoubleStarInGitignore_Bug3(t *testing.T) {
	// Bug #3: **/pattern in gitignore files not handled
	m := New()
	m.AddPattern("**/cache/")
	m.AddPattern("**/logs/*.log")

	// These SHOULD be ignored
	assert.True(t, m.Match("cache", true), "cache dir at root should be ignored")
	assert.True(t, m.Match("cache/data.go", false), "cache/data.go should be ignored")
	assert.True(t, m.Match("src/cache", true), "src/cache should be ignored")
	assert.True(t, m.Match("src/cache/store.go", false), "src/cache/store.go should be ignored")
	assert.True(t, m.Match("logs/app.log", false), "logs/app.log should be ignored")
	assert.True(t, m.Match("src/logs/debug.log", false), "src/logs/debug.log should be ignored")

	// These should NOT be ignored
	assert.False(t, m.Match("logs/app.txt", false))
}

// =============================================================================
// File Loading
// =============================================================================

func TestMatcher_AddFromFile(t *testing.T) {
	tmpDir := t.TempDir()
	gitignorePath := filepath.Join(tmpDir, ".gitignore")

	content := `# Comment
*.log
!important.log

# Another comment
build/
/temp/
`
	require.NoError(t, os.WriteFile(gitignorePath, []byte(content), 0o644))

	m := New()
	err := m.AddFromFile(gitignorePath, "")
	require.NoError(t, err)

	// Check parsed rules (comments and empty lines excluded)
	assert.Equal(t, 4, len(m.rules))

	// Verify matching behavior
	assert.True(t, m.Match("error.log", false))
	assert.False(t, m.Match("important.log", false))
	assert.True(t, m.Match("build", true))
	assert.True(t, m.Match("temp", true))
	assert.False(t, m.Match("src/temp", true))
}

func TestMatcher_AddFromFile_NonExistent(t *testing.T) {
	m := New()
	err := m.AddFromFile("/nonexistent/.gitignore", "")
	assert.Error(t, err)
}

func TestMatcher_AddFromFile_WithBase(t *testing.T) {
	tmpDir := t.TempDir()

	// Create src/.gitignore
	srcDir := filepath.Join(tmpDir, "src")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	gitignorePath := filepath.Join(srcDir, ".gitignore")

	content := `*.generated.go
temp/
`
	require.NoError(t, os.WriteFile(gitignorePath, []byte(content), 0o644))

	m := New()
	err := m.AddFromFile(gitignorePath, "src")
	require.NoError(t, err)

	// Pattern applies under src/
	assert.True(t, m.Match("src/code.generated.go", false))
	assert.True(t, m.Match("src/temp", true))

	// Pattern doesn't apply at root
	assert.False(t, m.Match("code.generated.go", false))
	assert.False(t, m.Match("temp", true))
}

// =============================================================================
// Thread Safety
// =============================================================================

func TestMatcher_ThreadSafety(t *testing.T) {
	m := New()
	m.AddPattern("*.log")
	m.AddPattern("temp/")

	var wg sync.WaitGroup
	const goroutines = 10
	const iterations = 100

	// Concurrent reads
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = m.Match("error.log", false)
				_ = m.Match("temp", true)
				_ = m.Match("main.go", false)
			}
		}()
	}

	// Concurrent writes
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				m.AddPattern("*.txt")
			}
		}(i)
	}

	wg.Wait()
}

// =============================================================================
// Complex Scenarios
// =============================================================================

func TestMatcher_Match_RealWorldScenario(t *testing.T) {
	m := New()

	// Typical .gitignore content
	patterns := []string{
		"# Dependencies",
		"node_modules/",
		"vendor/",
		"",
		"# Build outputs",
		"dist/",
		"build/",
		"*.min.js",
		"*.min.css",
		"",
		"# Logs",
		"*.log",
		"logs/",
		"!important.log",
		"",
		"# IDE",
		".idea/",
		".vscode/",
		"*.swp",
		"",
		"# OS",
		".DS_Store",
		"Thumbs.db",
		"",
		"# Project specific",
		"/config.local.json",
		"**/temp/",
		"**/*.generated.go",
	}

	for _, p := range patterns {
		m.AddPattern(p)
	}

	// Dependencies
	assert.True(t, m.Match("node_modules", true))
	assert.True(t, m.Match("node_modules/lodash/index.js", false))
	assert.True(t, m.Match("vendor", true))

	// Build outputs
	assert.True(t, m.Match("dist", true))
	assert.True(t, m.Match("dist/bundle.js", false))
	assert.True(t, m.Match("app.min.js", false))
	assert.True(t, m.Match("styles.min.css", false))

	// Logs
	assert.True(t, m.Match("error.log", false))
	assert.True(t, m.Match("logs", true))
	assert.False(t, m.Match("important.log", false)) // negated

	// IDE
	assert.True(t, m.Match(".idea", true))
	assert.True(t, m.Match(".vscode", true))
	assert.True(t, m.Match("main.go.swp", false))

	// OS
	assert.True(t, m.Match(".DS_Store", false))
	assert.True(t, m.Match("Thumbs.db", false))

	// Project specific
	assert.True(t, m.Match("config.local.json", false))
	assert.False(t, m.Match("src/config.local.json", false)) // anchored
	assert.True(t, m.Match("temp", true))
	assert.True(t, m.Match("src/temp", true))
	assert.True(t, m.Match("code.generated.go", false))
	assert.True(t, m.Match("pkg/models/user.generated.go", false))

	// Should NOT be ignored
	assert.False(t, m.Match("main.go", false))
	assert.False(t, m.Match("src/app.ts", false))
	assert.False(t, m.Match("README.md", false))
	assert.False(t, m.Match("package.json", false))
}

func TestMatcher_Match_GitSpecExamples(t *testing.T) {
	// Examples from git-scm.com/docs/gitignore
	tests := []struct {
		name     string
		patterns []string
		path     string
		isDir    bool
		expected bool
	}{
		{
			name:     "hello.* matches hello.txt",
			patterns: []string{"hello.*"},
			path:     "hello.txt",
			expected: true,
		},
		{
			name:     "foo/ matches foo directory",
			patterns: []string{"foo/"},
			path:     "foo",
			isDir:    true,
			expected: true,
		},
		{
			name:     "foo/ does not match foo file",
			patterns: []string{"foo/"},
			path:     "foo",
			isDir:    false,
			expected: false,
		},
		{
			name:     "doc/frotz/ matches only doc/frotz dir",
			patterns: []string{"doc/frotz/"},
			path:     "doc/frotz",
			isDir:    true,
			expected: true,
		},
		{
			name:     "doc/frotz/ doesn't match a/doc/frotz",
			patterns: []string{"doc/frotz/"},
			path:     "a/doc/frotz",
			isDir:    true,
			expected: false,
		},
		{
			name:     "frotz/ matches frotz anywhere",
			patterns: []string{"frotz/"},
			path:     "a/b/frotz",
			isDir:    true,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()
			for _, p := range tt.patterns {
				m.AddPattern(p)
			}
			got := m.Match(tt.path, tt.isDir)
			assert.Equal(t, tt.expected, got, "path: %s, isDir: %v", tt.path, tt.isDir)
		})
	}
}

// =============================================================================
// Pattern Diff Utilities (BUG-028)
// =============================================================================

func TestParsePatterns_SkipsCommentsAndEmpty(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected []string
	}{
		{
			name:     "empty content",
			content:  "",
			expected: nil,
		},
		{
			name:     "only comments",
			content:  "# Comment 1\n# Comment 2\n",
			expected: nil,
		},
		{
			name:     "only whitespace",
			content:  "   \n\t\n  \n",
			expected: nil,
		},
		{
			name:     "mixed content",
			content:  "# Comment\n*.log\n\nbuild/\n# Another comment\ntemp/",
			expected: []string{"*.log", "build/", "temp/"},
		},
		{
			name:     "escaped hash is a pattern",
			content:  `\#important`,
			expected: []string{`\#important`},
		},
		{
			name:     "pattern with leading/trailing spaces",
			content:  "  *.log  \n  build/  ",
			expected: []string{"*.log", "build/"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParsePatterns(tt.content)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestDiffPatterns_AddedOnly(t *testing.T) {
	oldContent := "*.log\nbuild/"
	newContent := "*.log\nbuild/\n*.tmp\nvendor/"

	added, removed := DiffPatterns(oldContent, newContent)

	assert.ElementsMatch(t, []string{"*.tmp", "vendor/"}, added)
	assert.Empty(t, removed)
}

func TestDiffPatterns_RemovedOnly(t *testing.T) {
	oldContent := "*.log\nbuild/\n*.tmp\nvendor/"
	newContent := "*.log\nbuild/"

	added, removed := DiffPatterns(oldContent, newContent)

	assert.Empty(t, added)
	assert.ElementsMatch(t, []string{"*.tmp", "vendor/"}, removed)
}

func TestDiffPatterns_Mixed(t *testing.T) {
	oldContent := "*.log\nbuild/\nold-pattern"
	newContent := "*.log\nbuild/\nnew-pattern"

	added, removed := DiffPatterns(oldContent, newContent)

	assert.ElementsMatch(t, []string{"new-pattern"}, added)
	assert.ElementsMatch(t, []string{"old-pattern"}, removed)
}

func TestDiffPatterns_NoChange(t *testing.T) {
	content := "*.log\nbuild/"

	added, removed := DiffPatterns(content, content)

	assert.Empty(t, added)
	assert.Empty(t, removed)
}

func TestDiffPatterns_OnlyCommentsChanged(t *testing.T) {
	oldContent := "# Old comment\n*.log"
	newContent := "# New comment\n# Another comment\n*.log"

	added, removed := DiffPatterns(oldContent, newContent)

	assert.Empty(t, added)
	assert.Empty(t, removed)
}

func TestDiffPatterns_EmptyToPatterns(t *testing.T) {
	oldContent := ""
	newContent := "*.log\nbuild/"

	added, removed := DiffPatterns(oldContent, newContent)

	assert.ElementsMatch(t, []string{"*.log", "build/"}, added)
	assert.Empty(t, removed)
}

func TestDiffPatterns_PatternsToEmpty(t *testing.T) {
	oldContent := "*.log\nbuild/"
	newContent := ""

	added, removed := DiffPatterns(oldContent, newContent)

	assert.Empty(t, added)
	assert.ElementsMatch(t, []string{"*.log", "build/"}, removed)
}

func TestMatchesAnyPattern(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		patterns []string
		expected bool
	}{
		{
			name:     "empty patterns",
			path:     "any/file.go",
			patterns: nil,
			expected: false,
		},
		{
			name:     "extension match",
			path:     "logs/error.log",
			patterns: []string{"*.log"},
			expected: true,
		},
		{
			name:     "no match",
			path:     "main.go",
			patterns: []string{"*.log", "*.tmp"},
			expected: false,
		},
		{
			name:     "directory pattern",
			path:     "build/output.js",
			patterns: []string{"build/"},
			expected: true,
		},
		{
			name:     "double star pattern",
			path:     "src/vendor/lib/file.go",
			patterns: []string{"**/vendor/"},
			expected: true,
		},
		{
			name:     "negation not processed in isolation",
			path:     "important.log",
			patterns: []string{"!important.log"},
			expected: false, // negation doesn't match, it un-ignores
		},
		{
			name:     "multiple patterns first matches",
			path:     "cache/data.bin",
			patterns: []string{"cache/", "*.tmp"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchesAnyPattern(tt.path, tt.patterns)
			assert.Equal(t, tt.expected, got)
		})
	}
}
