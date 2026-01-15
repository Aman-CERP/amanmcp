package scanner

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Aman-CERP/amanmcp/internal/config"
)

// SubmoduleInfo represents a discovered git submodule.
type SubmoduleInfo struct {
	// Name is the submodule name from .gitmodules [submodule "name"].
	Name string
	// Path is the relative path to the submodule in the parent repo.
	Path string
	// URL is the remote URL of the submodule (internal use only).
	URL string
	// Branch is the tracked branch (if any).
	Branch string
	// CommitHash is the current checked-out commit.
	CommitHash string
	// Initialized indicates if the submodule has content.
	Initialized bool
}

// ParseGitmodules parses a .gitmodules file content and returns SubmoduleInfo entries.
func ParseGitmodules(content []byte) ([]SubmoduleInfo, error) {
	var submodules []SubmoduleInfo
	var current *SubmoduleInfo

	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Check for submodule section header: [submodule "name"]
		if strings.HasPrefix(line, "[submodule") {
			// Save previous submodule if valid
			if current != nil && current.Path != "" {
				submodules = append(submodules, *current)
			}

			// Extract name from [submodule "name"]
			name := extractSubmoduleName(line)
			current = &SubmoduleInfo{
				Name: name,
			}
			continue
		}

		// Parse key = value pairs
		if current == nil {
			continue
		}

		key, value := parseKeyValue(line)
		switch key {
		case "path":
			current.Path = value
		case "url":
			current.URL = value
		case "branch":
			current.Branch = value
		}
	}

	// Don't forget the last submodule
	if current != nil && current.Path != "" {
		submodules = append(submodules, *current)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error scanning .gitmodules: %w", err)
	}

	return submodules, nil
}

// extractSubmoduleName extracts the submodule name from a section header.
// Format: [submodule "name"] or [submodule "path/to/name"]
func extractSubmoduleName(line string) string {
	// Find the quoted string
	start := strings.Index(line, "\"")
	if start == -1 {
		return ""
	}
	end := strings.LastIndex(line, "\"")
	if end <= start {
		return ""
	}
	return line[start+1 : end]
}

// parseKeyValue parses a "key = value" line.
func parseKeyValue(line string) (key, value string) {
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

// IsInitialized checks if a submodule directory has content (is initialized).
func IsInitialized(submodulePath string) bool {
	// Check if directory exists
	info, err := os.Stat(submodulePath)
	if err != nil || !info.IsDir() {
		return false
	}

	// Check if directory has any content (not just empty)
	entries, err := os.ReadDir(submodulePath)
	if err != nil {
		return false
	}

	// Must have at least one non-.git entry to be considered initialized
	for _, entry := range entries {
		if entry.Name() != ".git" {
			return true
		}
	}

	return false
}

// GetCommitHash retrieves the current commit hash for a submodule.
// It reads from .git/modules/{name}/HEAD or the submodule's .git file.
func GetCommitHash(rootPath, submodulePath string) (string, error) {
	// First, try to read the .git file in the submodule to find the gitdir
	gitFilePath := filepath.Join(submodulePath, ".git")
	gitFileContent, err := os.ReadFile(gitFilePath)
	if err != nil {
		// .git file doesn't exist, try common module location
		relPath, err := filepath.Rel(rootPath, submodulePath)
		if err != nil {
			return "", fmt.Errorf("failed to get relative path: %w", err)
		}
		modulePath := filepath.Join(rootPath, ".git", "modules", relPath, "HEAD")
		return readHEADFile(modulePath)
	}

	// Parse gitdir from .git file: "gitdir: ../path/to/.git/modules/name"
	gitdir := parseGitdir(string(gitFileContent))
	if gitdir == "" {
		return "", fmt.Errorf("invalid .git file format")
	}

	// Resolve relative path
	var headPath string
	if filepath.IsAbs(gitdir) {
		headPath = filepath.Join(gitdir, "HEAD")
	} else {
		headPath = filepath.Join(submodulePath, gitdir, "HEAD")
	}

	return readHEADFile(headPath)
}

// parseGitdir extracts the gitdir path from a .git file content.
func parseGitdir(content string) string {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "gitdir:") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(content, "gitdir:"))
}

// readHEADFile reads a commit hash from a HEAD file.
func readHEADFile(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read HEAD: %w", err)
	}

	hash := strings.TrimSpace(string(content))

	// If HEAD contains a ref (e.g., "ref: refs/heads/main"), we need to resolve it
	if strings.HasPrefix(hash, "ref:") {
		// For now, return empty - would need to resolve the ref
		return "", fmt.Errorf("HEAD is a symbolic ref, not a commit hash")
	}

	return hash, nil
}

// MatchesPattern checks if a submodule matches include/exclude patterns.
// Returns true if the submodule should be included.
func MatchesPattern(name, path string, include, exclude []string) bool {
	// First check exclude patterns - if excluded, return false
	for _, pattern := range exclude {
		if matchPattern(name, pattern) || matchPattern(path, pattern) {
			return false
		}
	}

	// If no include patterns, include all (that weren't excluded)
	if len(include) == 0 {
		return true
	}

	// Check include patterns
	for _, pattern := range include {
		if matchPattern(name, pattern) || matchPattern(path, pattern) {
			return true
		}
	}

	return false
}

// matchPattern matches a string against a simple glob pattern.
// Supports * as a wildcard for any characters.
func matchPattern(s, pattern string) bool {
	// Exact match
	if s == pattern {
		return true
	}

	// Handle prefix/* patterns
	if strings.HasSuffix(pattern, "/*") {
		prefix := strings.TrimSuffix(pattern, "/*")
		if strings.HasPrefix(s, prefix+"/") || s == prefix {
			return true
		}
	}

	// Handle */suffix patterns
	if strings.HasPrefix(pattern, "*/") {
		suffix := strings.TrimPrefix(pattern, "*/")
		if strings.HasSuffix(s, "/"+suffix) || s == suffix {
			return true
		}
	}

	// Handle *pattern* (contains)
	if strings.HasPrefix(pattern, "*") && strings.HasSuffix(pattern, "*") {
		middle := strings.TrimSuffix(strings.TrimPrefix(pattern, "*"), "*")
		if strings.Contains(s, middle) {
			return true
		}
	}

	return false
}

// DiscoverSubmodules discovers all git submodules in a project.
func DiscoverSubmodules(rootPath string, cfg config.SubmoduleConfig) ([]SubmoduleInfo, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	// Track visited paths to prevent circular references
	visited := make(map[string]bool)

	return discoverSubmodulesRecursive(rootPath, rootPath, "", cfg, visited)
}

// discoverSubmodulesRecursive handles recursive submodule discovery.
func discoverSubmodulesRecursive(
	rootPath string,
	currentPath string,
	pathPrefix string,
	cfg config.SubmoduleConfig,
	visited map[string]bool,
) ([]SubmoduleInfo, error) {
	// Check for circular reference
	absPath, err := filepath.Abs(currentPath)
	if err != nil {
		return nil, err
	}
	if visited[absPath] {
		return nil, nil // Break circular reference
	}
	visited[absPath] = true

	// Read .gitmodules file
	gitmodulesPath := filepath.Join(currentPath, ".gitmodules")
	content, err := os.ReadFile(gitmodulesPath)
	if os.IsNotExist(err) {
		return nil, nil // No submodules
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read .gitmodules: %w", err)
	}

	// Parse submodules
	parsed, err := ParseGitmodules(content)
	if err != nil {
		return nil, err
	}

	var result []SubmoduleInfo

	for _, sm := range parsed {
		// Build full path relative to root
		fullPath := sm.Path
		if pathPrefix != "" {
			fullPath = filepath.Join(pathPrefix, sm.Path)
		}

		// Check against include/exclude patterns
		if !MatchesPattern(sm.Name, fullPath, cfg.Include, cfg.Exclude) {
			continue
		}

		// Build absolute path for initialization check
		submoduleAbsPath := filepath.Join(currentPath, sm.Path)

		// Check if initialized
		sm.Initialized = IsInitialized(submoduleAbsPath)

		// Try to get commit hash (best effort)
		if sm.Initialized {
			hash, hashErr := GetCommitHash(rootPath, submoduleAbsPath)
			if hashErr == nil {
				sm.CommitHash = hash
			}
		}

		// Update path to be relative to root
		sm.Path = fullPath

		result = append(result, sm)

		// Recursively discover nested submodules if enabled
		if cfg.Recursive && sm.Initialized {
			nested, nestedErr := discoverSubmodulesRecursive(
				rootPath,
				submoduleAbsPath,
				fullPath,
				cfg,
				visited,
			)
			if nestedErr == nil && len(nested) > 0 {
				result = append(result, nested...)
			}
		}
	}

	return result, nil
}
