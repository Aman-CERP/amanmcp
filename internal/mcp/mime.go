package mcp

import (
	"path/filepath"
	"strings"
)

// mimeTypes maps file extensions to MIME types.
var mimeTypes = map[string]string{
	// Go
	".go":  "text/x-go",
	".mod": "text/x-go.mod",
	".sum": "text/x-go.sum",

	// TypeScript/JavaScript
	".ts":  "text/typescript",
	".tsx": "text/typescript",
	".js":  "text/javascript",
	".jsx": "text/javascript",
	".mjs": "text/javascript",

	// Python
	".py": "text/x-python",

	// Web
	".html": "text/html",
	".htm":  "text/html",
	".css":  "text/css",
	".scss": "text/x-scss",

	// Data
	".json": "application/json",
	".yaml": "text/x-yaml",
	".yml":  "text/x-yaml",
	".xml":  "text/xml",
	".toml": "text/x-toml",

	// Documentation
	".md":  "text/markdown",
	".mdx": "text/markdown",
	".txt": "text/plain",
	".rst": "text/x-rst",

	// Config
	".env":  "text/plain",
	".ini":  "text/plain",
	".conf": "text/plain",

	// Shell
	".sh":   "text/x-sh",
	".bash": "text/x-sh",
	".zsh":  "text/x-sh",

	// SQL
	".sql": "text/x-sql",

	// C/C++
	".c":   "text/x-c",
	".cpp": "text/x-c++",
	".h":   "text/x-c",
	".hpp": "text/x-c++",

	// Java
	".java": "text/x-java",

	// Rust
	".rs": "text/x-rust",

	// Ruby
	".rb": "text/x-ruby",

	// PHP
	".php": "text/x-php",
}

// specialFilenames maps specific filenames to MIME types.
var specialFilenames = map[string]string{
	"Dockerfile":   "text/x-dockerfile",
	"Makefile":     "text/x-makefile",
	"Jenkinsfile":  "text/x-groovy",
	"Vagrantfile":  "text/x-ruby",
	"Gemfile":      "text/x-ruby",
	"Rakefile":     "text/x-ruby",
	"CMakeLists.txt": "text/x-cmake",
}

// MimeTypeForPath returns the MIME type for a file path.
// It checks file extension first, then special filenames.
// Returns "text/plain" for unknown types.
func MimeTypeForPath(path string) string {
	// Get the base filename
	base := filepath.Base(path)

	// Check for special filenames first
	if mime, ok := specialFilenames[base]; ok {
		return mime
	}

	// Check file extension
	ext := strings.ToLower(filepath.Ext(path))
	if ext != "" {
		if mime, ok := mimeTypes[ext]; ok {
			return mime
		}
	}

	// Default to text/plain
	return "text/plain"
}
