// Package scanner provides file scanning functionality for AmanMCP.
// It discovers indexable files in a project, respecting exclusion patterns,
// .gitignore rules, and sensitive file patterns.
package scanner

import (
	"time"

	"github.com/Aman-CERP/amanmcp/internal/config"
)

// ContentType represents the type of content in a file.
type ContentType string

const (
	// ContentTypeCode represents source code files.
	ContentTypeCode ContentType = "code"
	// ContentTypeMarkdown represents markdown documentation files.
	ContentTypeMarkdown ContentType = "markdown"
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
}

// ScanResult is returned from the scanner channel.
type ScanResult struct {
	File  *FileInfo
	Error error
}

// DefaultMaxFileSize is the default maximum file size (10MB).
const DefaultMaxFileSize = 10 * 1024 * 1024

// languageMap maps file extensions to programming languages.
var languageMap = map[string]string{
	// Go
	".go": "go",

	// JavaScript/TypeScript
	".js":  "javascript",
	".jsx": "javascript",
	".mjs": "javascript",
	".ts":  "typescript",
	".tsx": "typescript",

	// Python
	".py":  "python",
	".pyw": "python",
	".pyi": "python",

	// Web
	".html": "html",
	".htm":  "html",
	".css":  "css",
	".scss": "scss",
	".sass": "sass",
	".less": "less",

	// Data/Config
	".json":  "json",
	".yaml":  "yaml",
	".yml":   "yaml",
	".toml":  "toml",
	".xml":   "xml",
	".ini":   "ini",
	".conf":  "config",
	".properties": "properties",

	// Documentation
	".md":       "markdown",
	".mdx":      "markdown",
	".markdown": "markdown",
	".rst":      "rst",
	".txt":      "text",

	// Shell
	".sh":   "shell",
	".bash": "shell",
	".zsh":  "shell",
	".fish": "fish",

	// Ruby
	".rb":   "ruby",
	".rake": "ruby",
	".erb":  "erb",

	// Rust
	".rs": "rust",

	// Java/Kotlin
	".java": "java",
	".kt":   "kotlin",
	".kts":  "kotlin",

	// C/C++
	".c":   "c",
	".h":   "c",
	".cpp": "cpp",
	".hpp": "cpp",
	".cc":  "cpp",
	".cxx": "cpp",

	// C#
	".cs": "csharp",

	// Swift
	".swift": "swift",

	// PHP
	".php": "php",

	// Scala
	".scala": "scala",

	// Elixir/Erlang
	".ex":  "elixir",
	".exs": "elixir",
	".erl": "erlang",

	// Haskell
	".hs": "haskell",

	// Lua
	".lua": "lua",

	// R
	".r": "r",
	".R": "r",

	// SQL
	".sql": "sql",

	// Docker
	"Dockerfile": "dockerfile",

	// Makefile
	"Makefile":     "makefile",
	"makefile":     "makefile",
	"GNUmakefile":  "makefile",

	// Other
	".vue":   "vue",
	".svelte": "svelte",
	".graphql": "graphql",
	".gql":   "graphql",
	".proto": "protobuf",
}

// contentTypeMap maps languages to content types.
var contentTypeMap = map[string]ContentType{
	// Code
	"go":         ContentTypeCode,
	"javascript": ContentTypeCode,
	"typescript": ContentTypeCode,
	"python":     ContentTypeCode,
	"ruby":       ContentTypeCode,
	"rust":       ContentTypeCode,
	"java":       ContentTypeCode,
	"kotlin":     ContentTypeCode,
	"c":          ContentTypeCode,
	"cpp":        ContentTypeCode,
	"csharp":     ContentTypeCode,
	"swift":      ContentTypeCode,
	"php":        ContentTypeCode,
	"scala":      ContentTypeCode,
	"elixir":     ContentTypeCode,
	"erlang":     ContentTypeCode,
	"haskell":    ContentTypeCode,
	"lua":        ContentTypeCode,
	"r":          ContentTypeCode,
	"sql":        ContentTypeCode,
	"shell":      ContentTypeCode,
	"fish":       ContentTypeCode,
	"erb":        ContentTypeCode,
	"vue":        ContentTypeCode,
	"svelte":     ContentTypeCode,
	"graphql":    ContentTypeCode,
	"protobuf":   ContentTypeCode,
	"html":       ContentTypeCode,
	"css":        ContentTypeCode,
	"scss":       ContentTypeCode,
	"sass":       ContentTypeCode,
	"less":       ContentTypeCode,

	// Markdown
	"markdown": ContentTypeMarkdown,
	"rst":      ContentTypeMarkdown,

	// Text
	"text": ContentTypeText,

	// Config
	"json":       ContentTypeConfig,
	"yaml":       ContentTypeConfig,
	"toml":       ContentTypeConfig,
	"xml":        ContentTypeConfig,
	"ini":        ContentTypeConfig,
	"config":     ContentTypeConfig,
	"properties": ContentTypeConfig,
	"dockerfile": ContentTypeConfig,
	"makefile":   ContentTypeConfig,
}

// DetectLanguage detects the programming language from a file path.
func DetectLanguage(path string) string {
	// Check exact filename matches first (Dockerfile, Makefile, etc.)
	base := baseName(path)
	if lang, ok := languageMap[base]; ok {
		return lang
	}

	// Check extension
	ext := extension(path)
	if lang, ok := languageMap[ext]; ok {
		return lang
	}

	return ""
}

// DetectContentType detects the content type from a language.
func DetectContentType(language string) ContentType {
	if ct, ok := contentTypeMap[language]; ok {
		return ct
	}
	return ContentTypeText
}

// baseName returns the file name from a path.
func baseName(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[i+1:]
		}
	}
	return path
}

// extension returns the file extension from a path (including the dot).
func extension(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '.' {
			return path[i:]
		}
		if path[i] == '/' || path[i] == '\\' {
			break
		}
	}
	return ""
}
