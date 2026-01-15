package scanner

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	lru "github.com/hashicorp/golang-lru/v2"

	"github.com/Aman-CERP/amanmcp/internal/gitignore"
)

// gitignoreCacheSize is the maximum number of gitignore matchers to cache.
// This prevents unbounded memory growth in long-running processes.
const gitignoreCacheSize = 1000

// Scanner discovers indexable files in a project directory.
type Scanner struct {
	// gitignoreCache caches parsed gitignore matchers by directory.
	// Uses LRU eviction to prevent unbounded memory growth (DEBT-001).
	gitignoreCache *lru.Cache[string, *gitignore.Matcher]
	cacheMu        sync.RWMutex
}

// New creates a new Scanner instance.
// Returns error if initialization fails (e.g., LRU cache creation).
func New() (*Scanner, error) {
	// Create LRU cache with fixed size to prevent unbounded growth
	cache, err := lru.New[string, *gitignore.Matcher](gitignoreCacheSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create gitignore cache: %w", err)
	}
	return &Scanner{
		gitignoreCache: cache,
	}, nil
}

// Scan discovers all indexable files in the project directory.
// It returns a channel of ScanResult that streams files as they are discovered.
// The channel is closed when scanning is complete.
func (s *Scanner) Scan(ctx context.Context, opts *ScanOptions) (<-chan ScanResult, error) {
	if opts == nil {
		opts = &ScanOptions{}
	}

	// Validate root directory
	rootDir := opts.RootDir
	if rootDir == "" {
		rootDir = "."
	}

	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	info, err := os.Stat(absRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to stat root directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("root path is not a directory: %s", absRoot)
	}

	// Set defaults
	maxFileSize := opts.MaxFileSize
	if maxFileSize <= 0 {
		maxFileSize = DefaultMaxFileSize
	}

	workers := opts.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	// Create result channel
	results := make(chan ScanResult, workers*10)

	// Discover submodules if enabled
	var submodulePaths []string
	if opts.Submodules != nil && opts.Submodules.Enabled {
		submodules, discoverErr := DiscoverSubmodules(absRoot, *opts.Submodules)
		if discoverErr != nil {
			// Log warning but continue (graceful degradation)
			slog.Warn("failed to discover submodules", slog.String("error", discoverErr.Error()))
		} else {
			for _, sm := range submodules {
				if sm.Initialized {
					submodulePaths = append(submodulePaths, sm.Path)
					slog.Debug("discovered initialized submodule",
						slog.String("name", sm.Name),
						slog.String("path", sm.Path))
				} else {
					slog.Warn("skipping uninitialized submodule",
						slog.String("name", sm.Name),
						slog.String("path", sm.Path))
				}
			}
		}
	}

	// Start scanning in background
	go func() {
		defer close(results)
		s.scan(ctx, absRoot, opts, maxFileSize, results)

		// Scan submodule directories
		for _, smPath := range submodulePaths {
			s.scanSubmodule(ctx, absRoot, smPath, opts, maxFileSize, results)
		}
	}()

	return results, nil
}

// ScanSubtree scans only a specific subtree of the project directory.
// Used for differential gitignore reconciliation (BUG-028).
// Paths in results are relative to the project root, not the subtree root.
func (s *Scanner) ScanSubtree(ctx context.Context, opts *ScanOptions, subtreePath string) (<-chan ScanResult, error) {
	if opts == nil {
		opts = &ScanOptions{}
	}

	// Validate root directory
	rootDir := opts.RootDir
	if rootDir == "" {
		rootDir = "."
	}

	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Validate subtree path
	subtreePath = strings.TrimPrefix(subtreePath, "/")
	subtreePath = strings.TrimSuffix(subtreePath, "/")
	if subtreePath == "" {
		// Empty subtree means scan everything - use regular Scan
		return s.Scan(ctx, opts)
	}

	absSubtree := filepath.Join(absRoot, subtreePath)

	// Security check: ensure subtree is within root
	if !strings.HasPrefix(absSubtree, absRoot) {
		return nil, fmt.Errorf("subtree path outside root: %s", subtreePath)
	}

	// Verify subtree exists
	info, err := os.Stat(absSubtree)
	if err != nil {
		if os.IsNotExist(err) {
			// Subtree doesn't exist - return empty channel
			results := make(chan ScanResult)
			close(results)
			return results, nil
		}
		return nil, fmt.Errorf("failed to stat subtree: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("subtree path is not a directory: %s", absSubtree)
	}

	// Set defaults
	maxFileSize := opts.MaxFileSize
	if maxFileSize <= 0 {
		maxFileSize = DefaultMaxFileSize
	}

	workers := opts.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	// Create result channel
	results := make(chan ScanResult, workers*10)

	// Start scanning subtree in background
	go func() {
		defer close(results)
		s.scanSubtreeInternal(ctx, absRoot, absSubtree, opts, maxFileSize, results)
	}()

	return results, nil
}

// scanSubtreeInternal performs directory traversal starting from a subtree.
// Paths in results are relative to absRoot, not absSubtree.
func (s *Scanner) scanSubtreeInternal(ctx context.Context, absRoot, absSubtree string, opts *ScanOptions, maxFileSize int64, results chan<- ScanResult) {
	err := filepath.WalkDir(absSubtree, func(path string, d fs.DirEntry, err error) error {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err != nil {
			return nil // Skip files we can't access
		}

		// Get relative path from PROJECT ROOT (not subtree root)
		relPath, err := filepath.Rel(absRoot, path)
		if err != nil {
			return nil
		}

		// Handle directories
		if d.IsDir() {
			if s.shouldExcludeDir(relPath, opts) {
				return filepath.SkipDir
			}
			return nil
		}

		// Handle symlinks
		if d.Type()&fs.ModeSymlink != 0 && !opts.FollowSymlinks {
			return nil
		}

		// Check if file should be excluded
		if s.shouldExcludeFile(relPath, absRoot, opts) {
			return nil
		}

		// Get file info
		info, err := d.Info()
		if err != nil {
			return nil
		}

		// Skip large files
		if info.Size() > maxFileSize {
			return nil
		}

		// Skip binary files
		if s.isBinaryFile(path) {
			return nil
		}

		// Detect language and content type
		language := DetectLanguage(relPath)
		contentType := DetectContentType(language)

		// Check if file matches include patterns
		if len(opts.IncludePatterns) > 0 && !s.matchesAnyPattern(relPath, opts.IncludePatterns) {
			return nil
		}

		// Check for generated file
		isGenerated := s.isGeneratedFile(path)

		fileInfo := &FileInfo{
			Path:        relPath,
			AbsPath:     path,
			Size:        info.Size(),
			ModTime:     info.ModTime(),
			ContentType: contentType,
			Language:    language,
			IsGenerated: isGenerated,
		}

		select {
		case results <- ScanResult{File: fileInfo}:
		case <-ctx.Done():
			return ctx.Err()
		}

		return nil
	})

	if err != nil && err != context.Canceled {
		select {
		case results <- ScanResult{Error: err}:
		case <-ctx.Done():
		}
	}
}

// scan performs the actual directory traversal.
func (s *Scanner) scan(ctx context.Context, absRoot string, opts *ScanOptions, maxFileSize int64, results chan<- ScanResult) {
	err := filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, err error) error {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err != nil {
			return nil // Skip files we can't access
		}

		// Get relative path
		relPath, err := filepath.Rel(absRoot, path)
		if err != nil {
			return nil
		}

		// Skip root directory itself
		if relPath == "." {
			return nil
		}

		// Handle directories
		if d.IsDir() {
			if s.shouldExcludeDir(relPath, opts) {
				return filepath.SkipDir
			}
			return nil
		}

		// Handle symlinks
		if d.Type()&fs.ModeSymlink != 0 && !opts.FollowSymlinks {
			return nil
		}

		// Check if file should be excluded
		if s.shouldExcludeFile(relPath, absRoot, opts) {
			return nil
		}

		// Get file info
		info, err := d.Info()
		if err != nil {
			return nil
		}

		// Skip large files
		if info.Size() > maxFileSize {
			return nil
		}

		// Skip binary files
		if s.isBinaryFile(path) {
			return nil
		}

		// Detect language and content type
		language := DetectLanguage(relPath)
		contentType := DetectContentType(language)

		// Check if file matches include patterns
		if len(opts.IncludePatterns) > 0 && !s.matchesAnyPattern(relPath, opts.IncludePatterns) {
			return nil
		}

		// Check for generated file
		isGenerated := s.isGeneratedFile(path)

		fileInfo := &FileInfo{
			Path:        relPath,
			AbsPath:     path,
			Size:        info.Size(),
			ModTime:     info.ModTime(),
			ContentType: contentType,
			Language:    language,
			IsGenerated: isGenerated,
		}

		select {
		case results <- ScanResult{File: fileInfo}:
		case <-ctx.Done():
			return ctx.Err()
		}

		return nil
	})

	if err != nil && err != context.Canceled {
		select {
		case results <- ScanResult{Error: err}:
		case <-ctx.Done():
		}
	}
}

// scanSubmodule scans files within a submodule directory.
// Files are indexed with their full path relative to the root (e.g., "libs/utils/file.go").
func (s *Scanner) scanSubmodule(ctx context.Context, absRoot, submodulePath string, opts *ScanOptions, maxFileSize int64, results chan<- ScanResult) {
	submoduleAbsPath := filepath.Join(absRoot, submodulePath)

	err := filepath.WalkDir(submoduleAbsPath, func(path string, d fs.DirEntry, walkErr error) error {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if walkErr != nil {
			return nil // Skip files we can't access
		}

		// Get relative path from submodule root
		relFromSubmodule, err := filepath.Rel(submoduleAbsPath, path)
		if err != nil {
			return nil
		}

		// Skip submodule root itself
		if relFromSubmodule == "." {
			return nil
		}

		// Build full relative path from project root
		relPath := filepath.Join(submodulePath, relFromSubmodule)

		// Handle directories
		if d.IsDir() {
			// Skip .git directories within submodules
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			if s.shouldExcludeDir(relFromSubmodule, opts) {
				return filepath.SkipDir
			}
			return nil
		}

		// Handle symlinks
		if d.Type()&fs.ModeSymlink != 0 && !opts.FollowSymlinks {
			return nil
		}

		// Check if file should be excluded (using path relative to submodule for pattern matching)
		if s.shouldExcludeFile(relFromSubmodule, submoduleAbsPath, opts) {
			return nil
		}

		// Get file info
		info, err := d.Info()
		if err != nil {
			return nil
		}

		// Skip large files
		if info.Size() > maxFileSize {
			return nil
		}

		// Skip binary files
		if s.isBinaryFile(path) {
			return nil
		}

		// Detect language and content type
		language := DetectLanguage(relFromSubmodule)
		contentType := DetectContentType(language)

		// Check if file matches include patterns (using submodule-relative path)
		if len(opts.IncludePatterns) > 0 && !s.matchesAnyPattern(relFromSubmodule, opts.IncludePatterns) {
			return nil
		}

		// Check for generated file
		isGenerated := s.isGeneratedFile(path)

		fileInfo := &FileInfo{
			Path:        relPath, // Full path from project root
			AbsPath:     path,
			Size:        info.Size(),
			ModTime:     info.ModTime(),
			ContentType: contentType,
			Language:    language,
			IsGenerated: isGenerated,
		}

		select {
		case results <- ScanResult{File: fileInfo}:
		case <-ctx.Done():
			return ctx.Err()
		}

		return nil
	})

	if err != nil && err != context.Canceled {
		slog.Warn("error scanning submodule",
			slog.String("submodule", submodulePath),
			slog.String("error", err.Error()))
	}
}

// shouldExcludeDir checks if a directory should be excluded.
func (s *Scanner) shouldExcludeDir(relPath string, opts *ScanOptions) bool {
	// Check default exclusions
	for _, pattern := range defaultExcludeDirs {
		if matchDirPattern(relPath, pattern) {
			return true
		}
	}

	// Check custom exclusions
	for _, pattern := range opts.ExcludePatterns {
		if matchDirPattern(relPath, pattern) {
			return true
		}
	}

	return false
}

// shouldExcludeFile checks if a file should be excluded.
func (s *Scanner) shouldExcludeFile(relPath, absRoot string, opts *ScanOptions) bool {
	baseName := filepath.Base(relPath)

	// Check sensitive file patterns
	for _, pattern := range sensitiveFilePatterns {
		if matchFilePattern(baseName, relPath, pattern) {
			return true
		}
	}

	// Check default file exclusions
	for _, pattern := range defaultExcludeFiles {
		if matchFilePattern(baseName, relPath, pattern) {
			return true
		}
	}

	// Check custom exclusions
	for _, pattern := range opts.ExcludePatterns {
		if matchFilePattern(baseName, relPath, pattern) {
			return true
		}
	}

	// Check gitignore
	if opts.RespectGitignore {
		if s.isGitignored(relPath, absRoot) {
			return true
		}
	}

	return false
}

// matchDirPattern checks if a directory path matches a pattern.
func matchDirPattern(relPath, pattern string) bool {
	// Handle **/ prefix patterns (e.g., **/node_modules/**)
	if strings.HasPrefix(pattern, "**/") {
		suffix := strings.TrimPrefix(pattern, "**/")
		suffix = strings.TrimSuffix(suffix, "/**")
		parts := strings.Split(relPath, string(filepath.Separator))
		for _, part := range parts {
			if part == suffix {
				return true
			}
		}
		return false
	}

	// Handle dir/** patterns (no leading **/)
	// BUG-062 FIX: Pattern like ".aman-pm/**" should match ".aman-pm" and ".aman-pm/anything"
	// This enables config-based exclusions from .amanmcp.yaml to work correctly
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		// Match the directory itself or any path starting with it
		if relPath == prefix || strings.HasPrefix(relPath, prefix+string(filepath.Separator)) {
			return true
		}
		return false
	}

	// Handle exact match
	return relPath == pattern || strings.HasPrefix(relPath, pattern+string(filepath.Separator))
}

// matchFilePattern checks if a file matches a pattern.
func matchFilePattern(baseName, relPath, pattern string) bool {
	// Handle dir/** patterns (no leading **/)
	// Pattern like "archive/**" should match "archive/anything/here.md"
	if strings.HasSuffix(pattern, "/**") && !strings.HasPrefix(pattern, "**/") {
		prefix := strings.TrimSuffix(pattern, "/**")
		// relPath could be "archive/file.md" or "archive/sub/file.md"
		if strings.HasPrefix(relPath, prefix+string(filepath.Separator)) {
			return true
		}
		return false
	}

	// Handle dir/prefix*.ext patterns like "docs/bugs/BUG-0*.md"
	// These patterns have a directory component and a glob in the filename
	if strings.Contains(pattern, string(filepath.Separator)) && strings.Contains(pattern, "*") && !strings.HasPrefix(pattern, "**/") {
		dir := filepath.Dir(pattern)
		filePattern := filepath.Base(pattern)
		relDir := filepath.Dir(relPath)

		// Check if directory matches exactly
		if relDir == dir {
			// Use filepath.Match for glob matching (supports *, ?, [])
			matched, err := filepath.Match(filePattern, baseName)
			if err == nil && matched {
				return true
			}
		}
		return false
	}

	// Handle **/ prefix patterns
	if strings.HasPrefix(pattern, "**/") {
		suffix := strings.TrimPrefix(pattern, "**/")
		if strings.HasPrefix(suffix, "*.") {
			// Extension pattern like **/*.min.js
			ext := strings.TrimPrefix(suffix, "*")
			return strings.HasSuffix(baseName, ext)
		}
		// Directory pattern
		parts := strings.Split(relPath, string(filepath.Separator))
		for i, part := range parts {
			if part == suffix || (i < len(parts)-1 && matchDirPattern(strings.Join(parts[:i+1], string(filepath.Separator)), pattern)) {
				return true
			}
		}
		return false
	}

	// Handle *pattern* (contains pattern)
	if strings.HasPrefix(pattern, "*") && strings.HasSuffix(pattern, "*") {
		middle := strings.TrimSuffix(strings.TrimPrefix(pattern, "*"), "*")
		return strings.Contains(strings.ToLower(baseName), strings.ToLower(middle))
	}

	// Handle .env* pattern (starts with .env)
	if strings.HasSuffix(pattern, "*") && strings.HasPrefix(pattern, ".") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(baseName, prefix)
	}

	// Handle *pattern (glob prefix - ends with pattern)
	if strings.HasPrefix(pattern, "*") {
		suffix := strings.TrimPrefix(pattern, "*")
		return strings.HasSuffix(baseName, suffix)
	}

	// Handle pattern* (glob suffix - starts with pattern)
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(baseName, prefix)
	}

	// Exact match
	return baseName == pattern
}

// matchesAnyPattern checks if a path matches any of the given patterns.
func (s *Scanner) matchesAnyPattern(relPath string, patterns []string) bool {
	baseName := filepath.Base(relPath)
	for _, pattern := range patterns {
		if matchFilePattern(baseName, relPath, pattern) {
			return true
		}
	}
	return false
}

// isBinaryFile checks if a file is binary by looking for null bytes.
func (s *Scanner) isBinaryFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	// Read first 512 bytes
	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil {
		return false
	}

	// Check for null bytes
	return bytes.Contains(buf[:n], []byte{0})
}

// isGeneratedFile checks if a file is auto-generated.
func (s *Scanner) isGeneratedFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	// Read first 1KB
	buf := make([]byte, 1024)
	n, err := f.Read(buf)
	if err != nil {
		return false
	}

	content := string(buf[:n])

	// Check for generated file markers
	markers := []string{
		"// Code generated",
		"// DO NOT EDIT",
		"/* DO NOT EDIT",
		"# Generated by",
		"<!-- AUTO-GENERATED -->",
		"// Generated by",
		"/* Generated by",
	}

	for _, marker := range markers {
		if strings.Contains(content, marker) {
			return true
		}
	}

	return false
}

// isGitignored checks if a file is ignored by gitignore.
func (s *Scanner) isGitignored(relPath, absRoot string) bool {
	// Build a composite matcher that includes all relevant .gitignore files
	// First check root .gitignore
	rootMatcher := s.getGitignoreMatcher(absRoot, "")
	if rootMatcher != nil && rootMatcher.Match(relPath, false) {
		return true
	}

	// Check nested .gitignore files
	parts := strings.Split(filepath.Dir(relPath), string(filepath.Separator))
	currentDir := absRoot
	currentBase := ""

	for _, part := range parts {
		if part == "." {
			continue
		}
		currentDir = filepath.Join(currentDir, part)
		if currentBase == "" {
			currentBase = part
		} else {
			currentBase = filepath.Join(currentBase, part)
		}

		matcher := s.getGitignoreMatcher(currentDir, currentBase)
		if matcher != nil && matcher.Match(relPath, false) {
			return true
		}
	}

	return false
}

// getGitignoreMatcher gets or creates a gitignore matcher for a directory.
func (s *Scanner) getGitignoreMatcher(dir, base string) *gitignore.Matcher {
	s.cacheMu.RLock()
	matcher, ok := s.gitignoreCache.Get(dir)
	s.cacheMu.RUnlock()
	if ok {
		return matcher
	}

	// Parse gitignore file
	gitignorePath := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(gitignorePath); os.IsNotExist(err) {
		return nil
	}

	matcher = gitignore.New()
	if err := matcher.AddFromFile(gitignorePath, base); err != nil {
		return nil
	}

	s.cacheMu.Lock()
	s.gitignoreCache.Add(dir, matcher)
	s.cacheMu.Unlock()

	return matcher
}

// InvalidateGitignoreCache clears the gitignore matcher cache.
// Call this when .gitignore files change to ensure fresh patterns are used.
// This is thread-safe and can be called concurrently.
func (s *Scanner) InvalidateGitignoreCache() {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.gitignoreCache.Purge()
}

// Default directories to exclude.
var defaultExcludeDirs = []string{
	"**/node_modules/**",
	"**/.git/**",
	"**/vendor/**",
	"**/__pycache__/**",
	"**/dist/**",
	"**/build/**",
	"**/.aws/**",
	"**/.gcp/**",
	"**/.azure/**",
	"**/.ssh/**",
}

// Default files to exclude.
var defaultExcludeFiles = []string{
	"**/*.min.js",
	"**/*.min.css",
	"**/package-lock.json",
	"**/yarn.lock",
	"**/pnpm-lock.yaml",
	"**/go.sum",
}

// Sensitive file patterns that are never indexed.
var sensitiveFilePatterns = []string{
	".env",
	".env.*",
	"*.pem",
	"*.key",
	"*.p12",
	"*.pfx",
	"*credentials*",
	"*secrets*",
	"*password*",
	".netrc",
	".npmrc",
	".pypirc",
	"id_rsa",
	"id_dsa",
	"id_ecdsa",
	"id_ed25519",
}
