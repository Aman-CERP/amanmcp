// Package gitignore provides gitignore pattern matching functionality.
// It implements the gitignore pattern syntax as documented at:
// https://git-scm.com/docs/gitignore
package gitignore

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// Matcher holds compiled gitignore patterns and provides thread-safe matching.
type Matcher struct {
	rules []rule
	mu    sync.RWMutex
}

// rule represents a single compiled gitignore pattern.
type rule struct {
	pattern  string         // original pattern
	regex    *regexp.Regexp // compiled regex
	negation bool           // starts with !
	dirOnly  bool           // ends with /
	anchored bool           // contains / or starts with /
	base     string         // base directory (for nested .gitignore)
}

// New creates a new empty Matcher.
func New() *Matcher {
	return &Matcher{
		rules: make([]rule, 0),
	}
}

// AddPattern adds a gitignore pattern to the matcher.
func (m *Matcher) AddPattern(pattern string) {
	m.AddPatternWithBase(pattern, "")
}

// AddPatternWithBase adds a pattern that only applies under the given base directory.
func (m *Matcher) AddPatternWithBase(pattern, base string) {
	// Handle trailing spaces escaped with backslash BEFORE trimming
	// According to gitignore spec, "\ " at end preserves the space
	hasEscapedTrailingSpace := strings.HasSuffix(pattern, `\ `)

	// Trim whitespace (but we'll restore escaped trailing space later)
	pattern = strings.TrimSpace(pattern)

	// Skip empty lines and comments
	if pattern == "" || (strings.HasPrefix(pattern, "#") && !strings.HasPrefix(pattern, `\#`)) {
		return
	}

	r := rule{
		pattern: pattern,
		base:    base,
	}

	// Handle escaped leading # or !
	if strings.HasPrefix(pattern, `\#`) {
		pattern = strings.TrimPrefix(pattern, `\`)
		r.pattern = pattern
	}
	if strings.HasPrefix(pattern, `\!`) {
		pattern = strings.TrimPrefix(pattern, `\`)
		r.pattern = pattern
	} else if strings.HasPrefix(pattern, "!") {
		// Handle negation
		r.negation = true
		pattern = strings.TrimPrefix(pattern, "!")
	}

	// Restore escaped trailing space
	if hasEscapedTrailingSpace {
		// Pattern was "file\ " which after TrimSpace becomes "file\"
		// We need to make it "file " (with literal space at end)
		if strings.HasSuffix(pattern, `\`) {
			pattern = strings.TrimSuffix(pattern, `\`) + " "
		}
	}

	// Handle directory-only pattern (trailing /)
	if strings.HasSuffix(pattern, "/") {
		r.dirOnly = true
		pattern = strings.TrimSuffix(pattern, "/")
	}

	// Handle anchored pattern (leading /)
	if strings.HasPrefix(pattern, "/") {
		r.anchored = true
		pattern = strings.TrimPrefix(pattern, "/")
	}

	// Pattern with internal / is also anchored (but applies from root)
	// "doc/frotz" means "/doc/frotz", not "**/doc/frotz"
	if strings.Contains(pattern, "/") && !strings.HasPrefix(pattern, "**/") && !strings.HasPrefix(pattern, "*") {
		r.anchored = true
	}

	// Compile pattern to regex
	regex := patternToRegex(pattern)
	r.regex = regexp.MustCompile("^" + regex + "$")

	m.mu.Lock()
	m.rules = append(m.rules, r)
	m.mu.Unlock()
}

// AddFromFile reads patterns from a gitignore file.
func (m *Matcher) AddFromFile(path, base string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open gitignore file: %w", err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		m.AddPatternWithBase(scanner.Text(), base)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read gitignore file: %w", err)
	}

	return nil
}

// Match checks if a path matches any gitignore pattern.
// Returns true if the path should be ignored.
func (m *Matcher) Match(path string, isDir bool) bool {
	// Normalize path separators
	path = filepath.ToSlash(path)

	m.mu.RLock()
	defer m.mu.RUnlock()

	ignored := false

	for _, r := range m.rules {
		if m.matchRule(path, isDir, r) {
			ignored = !r.negation
		}
	}

	return ignored
}

// matchRule checks if a path matches a single rule.
// Note: Directory-only patterns (ending with /) can match files inside that directory.
// For pattern "temp/", path "temp/file.go" should match.
func (m *Matcher) matchRule(path string, isDir bool, r rule) bool {
	// If rule has a base, only match paths under that base
	if r.base != "" {
		if !strings.HasPrefix(path, r.base+"/") && path != r.base {
			return false
		}
		// Remove base from path for matching
		if path == r.base {
			path = filepath.Base(path)
		} else {
			path = strings.TrimPrefix(path, r.base+"/")
		}
	}

	// Get path components
	parts := strings.Split(path, "/")
	basename := parts[len(parts)-1]

	// For anchored patterns, the pattern must match from the start
	if r.anchored {
		// Anchored pattern: must match the full path or path prefix
		if r.regex.MatchString(path) {
			if r.dirOnly {
				return isDir
			}
			return true
		}
		// Also check if pattern matches as a prefix (for files inside matched dir)
		if r.dirOnly {
			// Check if path starts with the matched directory
			for i := range parts[:len(parts)-1] {
				checkPath := strings.Join(parts[:i+1], "/")
				if r.regex.MatchString(checkPath) {
					return true
				}
			}
		}
		return false
	}

	// For directory-only patterns without anchoring:
	// "temp/" should match "temp" dir anywhere and files inside
	if r.dirOnly {
		// Check if any directory component matches
		for i, part := range parts {
			if r.regex.MatchString(part) {
				// If it's the last component, it must be a directory
				if i == len(parts)-1 {
					return isDir
				}
				// Otherwise it's a parent directory, so match
				return true
			}
		}
		return false
	}

	// Non-anchored, non-dir-only pattern:
	// Check if pattern matches basename
	if r.regex.MatchString(basename) {
		return true
	}

	// Also check full path (for patterns with **)
	if r.regex.MatchString(path) {
		return true
	}

	// Check each path component
	for _, part := range parts {
		if r.regex.MatchString(part) {
			return true
		}
	}

	return false
}

// patternToRegex converts a gitignore pattern to a regex string.
func patternToRegex(pattern string) string {
	var result strings.Builder

	i := 0
	for i < len(pattern) {
		c := pattern[i]

		switch c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				// ** pattern
				if i+2 < len(pattern) && pattern[i+2] == '/' {
					// **/ - matches any number of directories
					result.WriteString("(?:.*/)?")
					i += 3
					continue
				} else if i == 0 || (i > 0 && pattern[i-1] == '/') {
					// ** at end or between slashes - matches anything
					result.WriteString(".*")
					i += 2
					continue
				}
			}
			// Single * - matches anything except /
			result.WriteString("[^/]*")
			i++

		case '?':
			// ? matches any single character except /
			result.WriteString("[^/]")
			i++

		case '[':
			// Character class - pass through with escaping
			j := i + 1
			for j < len(pattern) && pattern[j] != ']' {
				j++
			}
			if j < len(pattern) {
				result.WriteString(pattern[i : j+1])
				i = j + 1
			} else {
				result.WriteString(regexp.QuoteMeta(string(c)))
				i++
			}

		case '\\':
			// Escape sequence
			if i+1 < len(pattern) {
				result.WriteString(regexp.QuoteMeta(string(pattern[i+1])))
				i += 2
			} else {
				result.WriteString(regexp.QuoteMeta(string(c)))
				i++
			}

		case '.', '+', '^', '$', '(', ')', '{', '}', '|':
			// Regex special chars - escape them
			result.WriteString(regexp.QuoteMeta(string(c)))
			i++

		default:
			result.WriteString(string(c))
			i++
		}
	}

	return result.String()
}

// ParsePatterns extracts patterns from gitignore content.
// Returns slice of non-empty, non-comment patterns.
// Used for computing diffs between gitignore versions.
func ParsePatterns(content string) []string {
	var patterns []string
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Skip empty lines and comments (unless escaped)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") && !strings.HasPrefix(line, `\#`) {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns
}

// DiffPatterns computes added and removed patterns between old and new gitignore content.
// Used for smart gitignore reconciliation (BUG-028).
func DiffPatterns(oldContent, newContent string) (added, removed []string) {
	oldPatterns := ParsePatterns(oldContent)
	newPatterns := ParsePatterns(newContent)

	oldSet := make(map[string]bool, len(oldPatterns))
	for _, p := range oldPatterns {
		oldSet[p] = true
	}

	newSet := make(map[string]bool, len(newPatterns))
	for _, p := range newPatterns {
		newSet[p] = true
	}

	// Added: in new but not in old
	for _, p := range newPatterns {
		if !oldSet[p] {
			added = append(added, p)
		}
	}

	// Removed: in old but not in new
	for _, p := range oldPatterns {
		if !newSet[p] {
			removed = append(removed, p)
		}
	}

	return added, removed
}

// MatchesAnyPattern checks if path matches any of the provided patterns.
// Returns true if the path would be ignored by any pattern.
// Used for smart gitignore reconciliation (BUG-028).
func MatchesAnyPattern(path string, patterns []string) bool {
	if len(patterns) == 0 {
		return false
	}
	matcher := New()
	for _, p := range patterns {
		matcher.AddPattern(p)
	}
	return matcher.Match(path, false)
}
