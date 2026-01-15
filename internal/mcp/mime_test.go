package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TS08: MIME Type Detection - F18 MCP Resources
func TestMimeTypeForPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		// Go
		{name: "go file", path: "main.go", expected: "text/x-go"},
		{name: "go mod", path: "go.mod", expected: "text/x-go.mod"},
		{name: "go sum", path: "go.sum", expected: "text/x-go.sum"},

		// TypeScript/JavaScript
		{name: "typescript", path: "app.ts", expected: "text/typescript"},
		{name: "tsx", path: "component.tsx", expected: "text/typescript"},
		{name: "javascript", path: "script.js", expected: "text/javascript"},
		{name: "jsx", path: "react.jsx", expected: "text/javascript"},
		{name: "mjs", path: "module.mjs", expected: "text/javascript"},

		// Python
		{name: "python", path: "app.py", expected: "text/x-python"},

		// Web
		{name: "html", path: "index.html", expected: "text/html"},
		{name: "htm", path: "page.htm", expected: "text/html"},
		{name: "css", path: "style.css", expected: "text/css"},
		{name: "scss", path: "theme.scss", expected: "text/x-scss"},

		// Data
		{name: "json", path: "config.json", expected: "application/json"},
		{name: "yaml", path: "config.yaml", expected: "text/x-yaml"},
		{name: "yml", path: "docker-compose.yml", expected: "text/x-yaml"},
		{name: "xml", path: "pom.xml", expected: "text/xml"},
		{name: "toml", path: "Cargo.toml", expected: "text/x-toml"},

		// Documentation
		{name: "markdown", path: "README.md", expected: "text/markdown"},
		{name: "mdx", path: "page.mdx", expected: "text/markdown"},
		{name: "txt", path: "notes.txt", expected: "text/plain"},
		{name: "rst", path: "docs.rst", expected: "text/x-rst"},

		// Config files
		{name: "env", path: ".env", expected: "text/plain"},
		{name: "ini", path: "config.ini", expected: "text/plain"},
		{name: "conf", path: "nginx.conf", expected: "text/plain"},

		// Shell
		{name: "sh", path: "setup.sh", expected: "text/x-sh"},
		{name: "bash", path: "build.bash", expected: "text/x-sh"},
		{name: "zsh", path: "init.zsh", expected: "text/x-sh"},

		// SQL
		{name: "sql", path: "schema.sql", expected: "text/x-sql"},

		// C/C++
		{name: "c", path: "main.c", expected: "text/x-c"},
		{name: "cpp", path: "main.cpp", expected: "text/x-c++"},
		{name: "h", path: "header.h", expected: "text/x-c"},
		{name: "hpp", path: "header.hpp", expected: "text/x-c++"},

		// Java
		{name: "java", path: "Main.java", expected: "text/x-java"},

		// Rust
		{name: "rust", path: "main.rs", expected: "text/x-rust"},

		// Ruby
		{name: "ruby", path: "app.rb", expected: "text/x-ruby"},

		// PHP
		{name: "php", path: "index.php", expected: "text/x-php"},

		// Path with directories
		{name: "nested path", path: "src/internal/mcp/server.go", expected: "text/x-go"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MimeTypeForPath(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TS10: Special Filenames
func TestMimeTypeForPath_SpecialFilenames(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{name: "Dockerfile", path: "Dockerfile", expected: "text/x-dockerfile"},
		{name: "dockerfile path", path: "docker/Dockerfile", expected: "text/x-dockerfile"},
		{name: "Makefile", path: "Makefile", expected: "text/x-makefile"},
		{name: "makefile path", path: "build/Makefile", expected: "text/x-makefile"},
		{name: "Jenkinsfile", path: "Jenkinsfile", expected: "text/x-groovy"},
		{name: "Vagrantfile", path: "Vagrantfile", expected: "text/x-ruby"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MimeTypeForPath(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TS09: Unknown Extension
func TestMimeTypeForPath_UnknownExtension(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "xyz extension", path: "file.xyz"},
		{name: "unknown extension", path: "data.abc"},
		{name: "no extension", path: "LICENSE"},
		{name: "random extension", path: "config.foobar"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MimeTypeForPath(tt.path)
			assert.Equal(t, "text/plain", result, "unknown extensions should default to text/plain")
		})
	}
}
