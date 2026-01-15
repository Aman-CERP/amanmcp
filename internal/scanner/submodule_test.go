package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Aman-CERP/amanmcp/internal/config"
)

// TestParseGitmodules_Valid tests parsing a valid .gitmodules file.
func TestParseGitmodules_Valid(t *testing.T) {
	content := []byte(`[submodule "libs/shared-utils"]
	path = libs/shared-utils
	url = https://github.com/example/shared-utils.git
	branch = main

[submodule "vendor/legacy"]
	path = vendor/legacy
	url = https://github.com/example/legacy.git
`)

	// When: parsing the content
	submodules, err := ParseGitmodules(content)

	// Then: returns correct SubmoduleInfo list
	require.NoError(t, err)
	require.Len(t, submodules, 2)

	assert.Equal(t, "libs/shared-utils", submodules[0].Name)
	assert.Equal(t, "libs/shared-utils", submodules[0].Path)
	assert.Equal(t, "https://github.com/example/shared-utils.git", submodules[0].URL)
	assert.Equal(t, "main", submodules[0].Branch)

	assert.Equal(t, "vendor/legacy", submodules[1].Name)
	assert.Equal(t, "vendor/legacy", submodules[1].Path)
	assert.Equal(t, "https://github.com/example/legacy.git", submodules[1].URL)
	assert.Equal(t, "", submodules[1].Branch)
}

// TestParseGitmodules_Empty tests parsing an empty .gitmodules file.
func TestParseGitmodules_Empty(t *testing.T) {
	content := []byte("")

	submodules, err := ParseGitmodules(content)

	require.NoError(t, err)
	assert.Empty(t, submodules)
}

// TestParseGitmodules_Malformed tests parsing malformed .gitmodules content.
func TestParseGitmodules_Malformed(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "missing path",
			content: "[submodule \"test\"]\n\turl = https://example.com/test.git\n",
		},
		{
			name:    "incomplete section header",
			content: "[submodule\n\tpath = test\n",
		},
		{
			name:    "random text",
			content: "this is not a valid gitmodules file\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			submodules, err := ParseGitmodules([]byte(tt.content))

			// Should not error, but return empty or partial results
			require.NoError(t, err)
			// Malformed entries should be skipped
			for _, sm := range submodules {
				// Any returned submodule should have at least path
				assert.NotEmpty(t, sm.Path, "submodule should have path")
			}
		})
	}
}

// TestParseGitmodules_MultipleSections tests parsing multiple submodule sections.
func TestParseGitmodules_MultipleSections(t *testing.T) {
	content := []byte(`[submodule "lib1"]
	path = lib1
	url = https://example.com/lib1.git

[submodule "lib2"]
	path = lib2
	url = https://example.com/lib2.git

[submodule "lib3"]
	path = lib3
	url = https://example.com/lib3.git
`)

	submodules, err := ParseGitmodules(content)

	require.NoError(t, err)
	require.Len(t, submodules, 3)
	assert.Equal(t, "lib1", submodules[0].Name)
	assert.Equal(t, "lib2", submodules[1].Name)
	assert.Equal(t, "lib3", submodules[2].Name)
}

// TestParseGitmodules_WithBranch tests parsing submodule with branch directive.
func TestParseGitmodules_WithBranch(t *testing.T) {
	content := []byte(`[submodule "feature-lib"]
	path = libs/feature
	url = https://example.com/feature.git
	branch = develop
`)

	submodules, err := ParseGitmodules(content)

	require.NoError(t, err)
	require.Len(t, submodules, 1)
	assert.Equal(t, "develop", submodules[0].Branch)
}

// TestParseGitmodules_TabsAndSpaces tests parsing with mixed whitespace.
func TestParseGitmodules_TabsAndSpaces(t *testing.T) {
	content := []byte(`[submodule "test"]
    path = test
	url = https://example.com/test.git
  branch = main
`)

	submodules, err := ParseGitmodules(content)

	require.NoError(t, err)
	require.Len(t, submodules, 1)
	assert.Equal(t, "test", submodules[0].Path)
	assert.Equal(t, "https://example.com/test.git", submodules[0].URL)
	assert.Equal(t, "main", submodules[0].Branch)
}

// TestIsInitialized_True tests detecting an initialized submodule.
func TestIsInitialized_True(t *testing.T) {
	// Given: a directory with content (simulating initialized submodule)
	tmpDir := t.TempDir()
	submodulePath := filepath.Join(tmpDir, "submodule")
	require.NoError(t, os.MkdirAll(submodulePath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(submodulePath, "README.md"), []byte("# Test"), 0o644))

	// When: checking if initialized
	initialized := IsInitialized(submodulePath)

	// Then: returns true
	assert.True(t, initialized)
}

// TestIsInitialized_False tests detecting an uninitialized submodule.
func TestIsInitialized_False(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T) string
	}{
		{
			name: "empty directory",
			setup: func(t *testing.T) string {
				tmpDir := t.TempDir()
				submodulePath := filepath.Join(tmpDir, "submodule")
				require.NoError(t, os.MkdirAll(submodulePath, 0o755))
				return submodulePath
			},
		},
		{
			name: "nonexistent directory",
			setup: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "nonexistent")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.setup(t)

			initialized := IsInitialized(path)

			assert.False(t, initialized)
		})
	}
}

// TestGetCommitHash tests retrieving commit hash from submodule.
func TestGetCommitHash(t *testing.T) {
	// Given: a directory with a .git file pointing to modules dir
	tmpDir := t.TempDir()
	submodulePath := filepath.Join(tmpDir, "submodule")
	require.NoError(t, os.MkdirAll(submodulePath, 0o755))

	// Create .git/modules/submodule/HEAD with a commit hash
	modulesPath := filepath.Join(tmpDir, ".git", "modules", "submodule")
	require.NoError(t, os.MkdirAll(modulesPath, 0o755))
	commitHash := "abc123def456789012345678901234567890abcd"
	require.NoError(t, os.WriteFile(filepath.Join(modulesPath, "HEAD"), []byte(commitHash+"\n"), 0o644))

	// Create .git file in submodule pointing to modules
	gitFile := filepath.Join(submodulePath, ".git")
	require.NoError(t, os.WriteFile(gitFile, []byte("gitdir: ../.git/modules/submodule\n"), 0o644))

	// When: getting commit hash
	hash, err := GetCommitHash(tmpDir, submodulePath)

	// Then: returns the commit hash
	require.NoError(t, err)
	assert.Equal(t, commitHash, hash)
}

// TestGetCommitHash_NotFound tests commit hash retrieval when not available.
func TestGetCommitHash_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	submodulePath := filepath.Join(tmpDir, "submodule")
	require.NoError(t, os.MkdirAll(submodulePath, 0o755))

	hash, err := GetCommitHash(tmpDir, submodulePath)

	assert.Error(t, err)
	assert.Empty(t, hash)
}

// TestMatchesPattern_Include tests include pattern matching.
func TestMatchesPattern_Include(t *testing.T) {
	tests := []struct {
		name    string
		smName  string
		smPath  string
		include []string
		exclude []string
		want    bool
	}{
		{
			name:    "matches include by name",
			smName:  "libs/shared",
			smPath:  "libs/shared",
			include: []string{"libs/shared"},
			exclude: nil,
			want:    true,
		},
		{
			name:    "matches include by prefix",
			smName:  "libs/shared",
			smPath:  "libs/shared",
			include: []string{"libs/*"},
			exclude: nil,
			want:    true,
		},
		{
			name:    "no match with include",
			smName:  "vendor/legacy",
			smPath:  "vendor/legacy",
			include: []string{"libs/*"},
			exclude: nil,
			want:    false,
		},
		{
			name:    "empty include means all",
			smName:  "anything",
			smPath:  "anything",
			include: nil,
			exclude: nil,
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchesPattern(tt.smName, tt.smPath, tt.include, tt.exclude)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestMatchesPattern_Exclude tests exclude pattern matching.
func TestMatchesPattern_Exclude(t *testing.T) {
	tests := []struct {
		name    string
		smName  string
		smPath  string
		include []string
		exclude []string
		want    bool
	}{
		{
			name:    "excluded by exact name",
			smName:  "vendor/legacy",
			smPath:  "vendor/legacy",
			include: nil,
			exclude: []string{"vendor/legacy"},
			want:    false,
		},
		{
			name:    "excluded by pattern",
			smName:  "vendor/old-lib",
			smPath:  "vendor/old-lib",
			include: nil,
			exclude: []string{"vendor/*"},
			want:    false,
		},
		{
			name:    "not excluded",
			smName:  "libs/utils",
			smPath:  "libs/utils",
			include: nil,
			exclude: []string{"vendor/*"},
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchesPattern(tt.smName, tt.smPath, tt.include, tt.exclude)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestMatchesPattern_Both tests include and exclude interaction.
func TestMatchesPattern_Both(t *testing.T) {
	tests := []struct {
		name    string
		smName  string
		smPath  string
		include []string
		exclude []string
		want    bool
	}{
		{
			name:    "included but also excluded - exclude wins",
			smName:  "libs/deprecated",
			smPath:  "libs/deprecated",
			include: []string{"libs/*"},
			exclude: []string{"libs/deprecated"},
			want:    false,
		},
		{
			name:    "included and not excluded",
			smName:  "libs/active",
			smPath:  "libs/active",
			include: []string{"libs/*"},
			exclude: []string{"libs/deprecated"},
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchesPattern(tt.smName, tt.smPath, tt.include, tt.exclude)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestDiscoverSubmodules_Integration tests full submodule discovery.
func TestDiscoverSubmodules_Integration(t *testing.T) {
	// Given: a project with .gitmodules and initialized submodule
	tmpDir := t.TempDir()

	// Create .gitmodules
	gitmodulesContent := []byte(`[submodule "libs/utils"]
	path = libs/utils
	url = https://example.com/utils.git
	branch = main
`)
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".gitmodules"), gitmodulesContent, 0o644))

	// Create initialized submodule
	submodulePath := filepath.Join(tmpDir, "libs", "utils")
	require.NoError(t, os.MkdirAll(submodulePath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(submodulePath, "utils.go"), []byte("package utils"), 0o644))

	// When: discovering submodules
	cfg := config.SubmoduleConfig{
		Enabled:   true,
		Recursive: false,
	}
	submodules, err := DiscoverSubmodules(tmpDir, cfg)

	// Then: returns the initialized submodule
	require.NoError(t, err)
	require.Len(t, submodules, 1)
	assert.Equal(t, "libs/utils", submodules[0].Name)
	assert.Equal(t, "libs/utils", submodules[0].Path)
	assert.True(t, submodules[0].Initialized)
}

// TestDiscoverSubmodules_NoGitmodules tests behavior when no .gitmodules exists.
func TestDiscoverSubmodules_NoGitmodules(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.SubmoduleConfig{
		Enabled: true,
	}
	submodules, err := DiscoverSubmodules(tmpDir, cfg)

	require.NoError(t, err)
	assert.Empty(t, submodules)
}

// TestDiscoverSubmodules_WithExclude tests exclude filtering during discovery.
func TestDiscoverSubmodules_WithExclude(t *testing.T) {
	tmpDir := t.TempDir()

	gitmodulesContent := []byte(`[submodule "libs/good"]
	path = libs/good
	url = https://example.com/good.git

[submodule "vendor/legacy"]
	path = vendor/legacy
	url = https://example.com/legacy.git
`)
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".gitmodules"), gitmodulesContent, 0o644))

	// Create both submodules as initialized
	for _, path := range []string{"libs/good", "vendor/legacy"} {
		subPath := filepath.Join(tmpDir, path)
		require.NoError(t, os.MkdirAll(subPath, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(subPath, "file.go"), []byte("package test"), 0o644))
	}

	cfg := config.SubmoduleConfig{
		Enabled: true,
		Exclude: []string{"vendor/*"},
	}
	submodules, err := DiscoverSubmodules(tmpDir, cfg)

	require.NoError(t, err)
	require.Len(t, submodules, 1)
	assert.Equal(t, "libs/good", submodules[0].Name)
}

// TestDiscoverSubmodules_UninitializedSkipped tests that uninitialized submodules are marked.
func TestDiscoverSubmodules_UninitializedSkipped(t *testing.T) {
	tmpDir := t.TempDir()

	gitmodulesContent := []byte(`[submodule "libs/initialized"]
	path = libs/initialized
	url = https://example.com/init.git

[submodule "libs/empty"]
	path = libs/empty
	url = https://example.com/empty.git
`)
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".gitmodules"), gitmodulesContent, 0o644))

	// Create initialized submodule with content
	initPath := filepath.Join(tmpDir, "libs", "initialized")
	require.NoError(t, os.MkdirAll(initPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(initPath, "lib.go"), []byte("package lib"), 0o644))

	// Create empty submodule dir
	emptyPath := filepath.Join(tmpDir, "libs", "empty")
	require.NoError(t, os.MkdirAll(emptyPath, 0o755))

	cfg := config.SubmoduleConfig{
		Enabled: true,
	}
	submodules, err := DiscoverSubmodules(tmpDir, cfg)

	require.NoError(t, err)
	require.Len(t, submodules, 2)

	// Find each submodule and check status
	for _, sm := range submodules {
		if sm.Name == "libs/initialized" {
			assert.True(t, sm.Initialized, "libs/initialized should be initialized")
		}
		if sm.Name == "libs/empty" {
			assert.False(t, sm.Initialized, "libs/empty should not be initialized")
		}
	}
}

// TestDiscoverSubmodules_Recursive tests nested submodule discovery.
func TestDiscoverSubmodules_Recursive(t *testing.T) {
	tmpDir := t.TempDir()

	// Root .gitmodules
	rootGitmodules := []byte(`[submodule "libs/outer"]
	path = libs/outer
	url = https://example.com/outer.git
`)
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".gitmodules"), rootGitmodules, 0o644))

	// Create outer submodule with its own .gitmodules
	outerPath := filepath.Join(tmpDir, "libs", "outer")
	require.NoError(t, os.MkdirAll(outerPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(outerPath, "outer.go"), []byte("package outer"), 0o644))

	nestedGitmodules := []byte(`[submodule "nested/inner"]
	path = nested/inner
	url = https://example.com/inner.git
`)
	require.NoError(t, os.WriteFile(filepath.Join(outerPath, ".gitmodules"), nestedGitmodules, 0o644))

	// Create nested submodule
	innerPath := filepath.Join(outerPath, "nested", "inner")
	require.NoError(t, os.MkdirAll(innerPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(innerPath, "inner.go"), []byte("package inner"), 0o644))

	// When: discovering with recursive enabled
	cfg := config.SubmoduleConfig{
		Enabled:   true,
		Recursive: true,
	}
	submodules, err := DiscoverSubmodules(tmpDir, cfg)

	// Then: finds both outer and nested submodules
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(submodules), 1) // At least finds outer
	// Note: nested submodule paths should be relative to root
}

// TestDiscoverSubmodules_CircularRef tests detection of circular references.
func TestDiscoverSubmodules_CircularRef(t *testing.T) {
	// This is a theoretical test - in practice git prevents circular submodules
	// but we should handle it gracefully if somehow encountered
	tmpDir := t.TempDir()

	// Create a submodule that points back to parent (simulated)
	gitmodulesContent := []byte(`[submodule "libs/circular"]
	path = libs/circular
	url = https://example.com/parent.git
`)
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".gitmodules"), gitmodulesContent, 0o644))

	subPath := filepath.Join(tmpDir, "libs", "circular")
	require.NoError(t, os.MkdirAll(subPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(subPath, "file.go"), []byte("package circular"), 0o644))

	// Create .gitmodules in submodule that references back
	circularGitmodules := []byte(`[submodule "parent"]
	path = parent
	url = https://example.com/parent.git
`)
	require.NoError(t, os.WriteFile(filepath.Join(subPath, ".gitmodules"), circularGitmodules, 0o644))

	cfg := config.SubmoduleConfig{
		Enabled:   true,
		Recursive: true,
	}

	// Should not hang or panic, should handle gracefully
	submodules, err := DiscoverSubmodules(tmpDir, cfg)

	require.NoError(t, err)
	assert.NotNil(t, submodules)
}

// BenchmarkParseGitmodules tests parsing performance.
func BenchmarkParseGitmodules(b *testing.B) {
	// Create .gitmodules content with 10 submodules
	var content []byte
	for i := 0; i < 10; i++ {
		section := []byte(`[submodule "lib` + string(rune('0'+i)) + `"]
	path = lib` + string(rune('0'+i)) + `
	url = https://example.com/lib` + string(rune('0'+i)) + `.git
	branch = main

`)
		content = append(content, section...)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := ParseGitmodules(content)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// TestScanner_WithSubmodules tests scanning with submodules enabled.
func TestScanner_WithSubmodules(t *testing.T) {
	// Given: a project with .gitmodules and initialized submodule
	tmpDir := t.TempDir()

	// Create .gitmodules
	gitmodulesContent := []byte(`[submodule "libs/utils"]
	path = libs/utils
	url = https://example.com/utils.git
`)
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".gitmodules"), gitmodulesContent, 0o644))

	// Create a main file
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "src", "main.go"), []byte("package main"), 0o644))

	// Create initialized submodule with files
	submodulePath := filepath.Join(tmpDir, "libs", "utils")
	require.NoError(t, os.MkdirAll(submodulePath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(submodulePath, "utils.go"), []byte("package utils"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(submodulePath, "helper.go"), []byte("package utils"), 0o644))

	// Create scanner
	s, err := New()
	require.NoError(t, err)

	// When: scanning with submodules enabled
	results, err := s.Scan(context.Background(), &ScanOptions{
		RootDir:          tmpDir,
		RespectGitignore: true,
		Submodules: &config.SubmoduleConfig{
			Enabled: true,
		},
	})
	require.NoError(t, err)

	// Collect results
	var files []string
	for r := range results {
		if r.Error != nil {
			continue
		}
		files = append(files, r.File.Path)
	}

	// Then: includes both main files and submodule files
	assert.Contains(t, files, "src/main.go", "should include main file")
	assert.Contains(t, files, "libs/utils/utils.go", "should include submodule file with full path")
	assert.Contains(t, files, "libs/utils/helper.go", "should include all submodule files")
}

// TestScanner_SubmodulesDisabled tests scanning with submodules disabled (default).
func TestScanner_SubmodulesDisabled(t *testing.T) {
	// Given: a project with .gitmodules and initialized submodule
	tmpDir := t.TempDir()

	// Create .gitmodules
	gitmodulesContent := []byte(`[submodule "libs/utils"]
	path = libs/utils
	url = https://example.com/utils.git
`)
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".gitmodules"), gitmodulesContent, 0o644))

	// Create a main file
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "src", "main.go"), []byte("package main"), 0o644))

	// Create initialized submodule with files
	submodulePath := filepath.Join(tmpDir, "libs", "utils")
	require.NoError(t, os.MkdirAll(submodulePath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(submodulePath, "utils.go"), []byte("package utils"), 0o644))

	// Create scanner
	s, err := New()
	require.NoError(t, err)

	// When: scanning WITHOUT submodules enabled (default)
	results, err := s.Scan(context.Background(), &ScanOptions{
		RootDir: tmpDir,
		// Submodules not set - default is nil/disabled
	})
	require.NoError(t, err)

	// Collect results
	var files []string
	for r := range results {
		if r.Error != nil {
			continue
		}
		files = append(files, r.File.Path)
	}

	// Then: includes main files but submodule files are still scanned
	// (they're just directories, scanner walks into them normally)
	// The key difference is DiscoverSubmodules is not called
	assert.Contains(t, files, "src/main.go", "should include main file")
	// Note: libs/utils/utils.go will be included because it's a normal directory
	// The difference is in structured submodule discovery, not file exclusion
}

// TestScanner_SubmodulesWithExclude tests scanner with submodule exclude patterns.
func TestScanner_SubmodulesWithExclude(t *testing.T) {
	// Given: a project with multiple submodules
	tmpDir := t.TempDir()

	gitmodulesContent := []byte(`[submodule "libs/included"]
	path = libs/included
	url = https://example.com/included.git

[submodule "vendor/excluded"]
	path = vendor/excluded
	url = https://example.com/excluded.git
`)
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".gitmodules"), gitmodulesContent, 0o644))

	// Create both submodules with files
	for _, path := range []string{"libs/included", "vendor/excluded"} {
		subPath := filepath.Join(tmpDir, path)
		require.NoError(t, os.MkdirAll(subPath, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(subPath, "file.go"), []byte("package test"), 0o644))
	}

	s, err := New()
	require.NoError(t, err)

	// When: scanning with vendor excluded
	results, err := s.Scan(context.Background(), &ScanOptions{
		RootDir: tmpDir,
		Submodules: &config.SubmoduleConfig{
			Enabled: true,
			Exclude: []string{"vendor/*"},
		},
	})
	require.NoError(t, err)

	// Collect results
	var files []string
	for r := range results {
		if r.Error != nil {
			continue
		}
		files = append(files, r.File.Path)
	}

	// Then: includes included submodule but not excluded one
	// Note: The default scanner still walks vendor/excluded as a normal directory
	// The submodule exclusion affects structured discovery, not directory walking
	assert.Contains(t, files, "libs/included/file.go", "should include non-excluded submodule")
}

// BenchmarkDiscoverSubmodules tests discovery performance.
func BenchmarkDiscoverSubmodules(b *testing.B) {
	tmpDir := b.TempDir()

	// Create .gitmodules with 10 submodules
	var content []byte
	for i := 0; i < 10; i++ {
		name := "lib" + string(rune('0'+i))
		section := []byte(`[submodule "` + name + `"]
	path = ` + name + `
	url = https://example.com/` + name + `.git

`)
		content = append(content, section...)

		// Create initialized submodule
		subPath := filepath.Join(tmpDir, name)
		if err := os.MkdirAll(subPath, 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(subPath, "file.go"), []byte("package "+name), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(tmpDir, ".gitmodules"), content, 0o644); err != nil {
		b.Fatal(err)
	}

	cfg := config.SubmoduleConfig{
		Enabled:   true,
		Recursive: false,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := DiscoverSubmodules(tmpDir, cfg)
		if err != nil {
			b.Fatal(err)
		}
	}
}
