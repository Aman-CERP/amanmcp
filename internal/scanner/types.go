// Package scanner provides file scanning functionality for AmanMCP.
// It discovers indexable files in a project, respecting exclusion patterns,
// .gitignore rules, and sensitive file patterns.
package scanner

import (
	"time"

	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/language"
)

// ContentType represents the type of content in a file.
type ContentType string

const (
	// ContentTypeCode represents source code files.
	ContentTypeCode ContentType = "code"
	// ContentTypeMarkdown represents markdown documentation files.
	ContentTypeMarkdown ContentType = "markdown"
	// ContentTypePDF represents PDF document files.
	ContentTypePDF ContentType = "pdf"
	// ContentTypeText represents plain text files.
	ContentTypeText ContentType = "text"
	// ContentTypeConfig represents configuration files.
	ContentTypeConfig ContentType = "config"
)

// FileInfo contains metadata about a discovered file.
type FileInfo struct {
	Path        string      // Relative path to project root
	AbsPath     string      // Absolute path
	Size        int64       // File size in bytes
	ModTime     time.Time   // Last modification time
	ContentType ContentType // code, markdown, text, config
	Language    string      // go, typescript, python, etc.
	IsGenerated bool        // Detected as generated file
}

// ScanOptions configures the scanner behavior.
type ScanOptions struct {
	// RootDir is the project root directory to scan.
	RootDir string

	// IncludePatterns specifies patterns to include (empty = all).
	IncludePatterns []string

	// ExcludePatterns specifies patterns to exclude.
	ExcludePatterns []string

	// RespectGitignore enables .gitignore parsing.
	RespectGitignore bool

	// Workers is the number of concurrent workers (0 = NumCPU).
	Workers int

	// MaxFileSize is the maximum file size to include in bytes (0 = 10MB default).
	MaxFileSize int64

	// FollowSymlinks enables following symbolic links (default: false).
	FollowSymlinks bool

	// ProgressFunc is called with progress updates during scanning.
	ProgressFunc func(scanned, total int)

	// Submodules configures git submodule discovery.
	// If nil or Enabled is false, submodules are not scanned.
	Submodules *config.SubmoduleConfig

	// LanguageRegistry resolves language detection and content type.
	// Nil uses the built-in default registry.
	LanguageRegistry *language.Registry
}

// ScanResult is returned from the scanner channel.
type ScanResult struct {
	File  *FileInfo
	Error error
}

// DefaultMaxFileSize is the default maximum file size (10MB).
const DefaultMaxFileSize = 10 * 1024 * 1024

// DetectLanguage detects the programming language from a file path.
func DetectLanguage(path string) string {
	return DetectLanguageWithRegistry(path, nil)
}

// DetectLanguageWithRegistry detects the language from a path using registry.
func DetectLanguageWithRegistry(path string, registry *language.Registry) string {
	if registry == nil {
		registry = language.DefaultRegistry()
	}
	return registry.Detect(path)
}

// DetectContentType detects the content type from a language.
func DetectContentType(languageName string) ContentType {
	return DetectContentTypeWithRegistry(languageName, nil)
}

// DetectContentTypeWithRegistry detects content type using registry.
func DetectContentTypeWithRegistry(languageName string, registry *language.Registry) ContentType {
	if registry == nil {
		registry = language.DefaultRegistry()
	}
	switch registry.ContentType(languageName) {
	case language.ContentTypeCode:
		return ContentTypeCode
	case language.ContentTypeMarkdown:
		return ContentTypeMarkdown
	case language.ContentTypePDF:
		return ContentTypePDF
	case language.ContentTypeConfig:
		return ContentTypeConfig
	default:
		return ContentTypeText
	}
}
