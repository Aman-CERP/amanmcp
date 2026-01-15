package chunk

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TS01: Chunk Go File with Functions
func TestCodeChunker_ChunkGoFile_ReturnsFunctionChunks(t *testing.T) {
	source := `package main

import "fmt"

func Hello() {
	fmt.Println("Hello")
}

func Goodbye() {
	fmt.Println("Goodbye")
}
`
	chunker := NewCodeChunker()
	defer chunker.Close()

	chunks, err := chunker.Chunk(context.Background(), &FileInput{
		Path:     "main.go",
		Content:  []byte(source),
		Language: "go",
	})

	require.NoError(t, err)
	assert.Len(t, chunks, 2, "should return 2 chunks for 2 functions")

	// Verify first chunk contains Hello
	assert.Contains(t, chunks[0].RawContent, "Hello")
	assert.Equal(t, "function", string(chunks[0].Symbols[0].Type))
	assert.Equal(t, "Hello", chunks[0].Symbols[0].Name)

	// Verify second chunk contains Goodbye
	assert.Contains(t, chunks[1].RawContent, "Goodbye")
	assert.Equal(t, "function", string(chunks[1].Symbols[0].Type))
	assert.Equal(t, "Goodbye", chunks[1].Symbols[0].Name)

	// Both chunks should include import context
	for _, chunk := range chunks {
		assert.Contains(t, chunk.Context, `import "fmt"`)
		assert.Contains(t, chunk.Context, "package main")
	}
}

// TS02: Include Doc Comments
func TestCodeChunker_ChunkGoFile_IncludesDocComments(t *testing.T) {
	source := `package main

import "fmt"

// Greet returns a greeting message for the given name.
func Greet(name string) string {
	if name == "" {
		return "Hello, stranger!"
	}
	return fmt.Sprintf("Hello, %s!", name)
}
`
	chunker := NewCodeChunker()
	defer chunker.Close()

	chunks, err := chunker.Chunk(context.Background(), &FileInput{
		Path:     "main.go",
		Content:  []byte(source),
		Language: "go",
	})

	require.NoError(t, err)
	require.Len(t, chunks, 1)

	// Chunk should include doc comment
	assert.Contains(t, chunks[0].RawContent, "Greet returns a greeting")
	assert.Equal(t, "Greet", chunks[0].Symbols[0].Name)

	// Symbol should have doc comment extracted
	assert.Contains(t, chunks[0].Symbols[0].DocComment, "Greet returns a greeting")
}

// TS03: TypeScript Class with Imports
func TestCodeChunker_ChunkTypeScript_IncludesImportContext(t *testing.T) {
	source := `import { Logger } from './logger';
import { Config } from './config';

export class UserService {
	private logger: Logger;

	constructor(config: Config) {
		this.logger = new Logger(config);
	}

	getUser(id: string): User | null {
		this.logger.info('Getting user: ' + id);
		return null;
	}
}
`
	chunker := NewCodeChunker()
	defer chunker.Close()

	chunks, err := chunker.Chunk(context.Background(), &FileInput{
		Path:     "user-service.ts",
		Content:  []byte(source),
		Language: "typescript",
	})

	require.NoError(t, err)
	require.GreaterOrEqual(t, len(chunks), 1)

	// At least one chunk should include import context
	found := false
	for _, chunk := range chunks {
		if strings.Contains(chunk.Context, "import { Logger }") &&
			strings.Contains(chunk.Context, "import { Config }") {
			found = true
			break
		}
	}
	assert.True(t, found, "chunks should include import context")
}

// TS04: Fallback for Unsupported Language
func TestCodeChunker_ChunkUnsupportedLanguage_UsesLineFallback(t *testing.T) {
	source := `defmodule HelloWorld do
  def hello do
    IO.puts("Hello, World!")
  end

  def goodbye do
    IO.puts("Goodbye!")
  end
end
`
	chunker := NewCodeChunker()
	defer chunker.Close()

	chunks, err := chunker.Chunk(context.Background(), &FileInput{
		Path:     "hello.ex",
		Content:  []byte(source),
		Language: "elixir", // Unsupported language
	})

	// Should not error - fall back to line-based chunking
	require.NoError(t, err)
	require.NotEmpty(t, chunks, "should return at least one chunk")

	// Chunks should contain the content
	combined := ""
	for _, chunk := range chunks {
		combined += chunk.Content
	}
	assert.Contains(t, combined, "defmodule HelloWorld")
}

// TS05: Split Large Function
func TestCodeChunker_ChunkLargeFunction_SplitsIntoMultipleChunks(t *testing.T) {
	// Create a large function that exceeds the default chunk size
	// DefaultMaxChunkTokens = 512, TokensPerChar = 4
	// So ~2048 characters = 512 tokens
	lines := make([]string, 200) // 200 lines should exceed the limit
	for i := 0; i < 200; i++ {
		lines[i] = "\tfmt.Println(\"Line " + string(rune('A'+i%26)) + "\")"
	}

	source := `package main

import "fmt"

func VeryLargeFunction() {
` + strings.Join(lines, "\n") + `
}
`
	chunker := NewCodeChunkerWithOptions(CodeChunkerOptions{
		MaxChunkTokens: 300, // Lower threshold to force splitting
	})
	defer chunker.Close()

	chunks, err := chunker.Chunk(context.Background(), &FileInput{
		Path:     "large.go",
		Content:  []byte(source),
		Language: "go",
	})

	require.NoError(t, err)
	assert.Greater(t, len(chunks), 1, "large function should be split into multiple chunks")

	// All chunks should be under the size limit
	for _, chunk := range chunks {
		tokens := estimateTokens(chunk.RawContent)
		assert.LessOrEqual(t, tokens, 300+DefaultOverlapTokens,
			"chunk should be under size limit (with overlap tolerance)")
	}
}

// TS05b: Parent Symbol Registration (RCA-013 fix)
// When a large symbol is split, the first chunk should contain both the
// sub-symbol (e.g., "VeryLargeFunction_part1") AND the parent symbol
// (e.g., "VeryLargeFunction") to ensure discoverability in search.
func TestCodeChunker_ChunkLargeFunction_RegistersParentSymbol(t *testing.T) {
	// Create a large function that will be split
	lines := make([]string, 200)
	for i := 0; i < 200; i++ {
		lines[i] = "\tfmt.Println(\"Line " + string(rune('A'+i%26)) + "\")"
	}

	source := `package main

import "fmt"

func LargeSearchMethod() {
` + strings.Join(lines, "\n") + `
}
`
	chunker := NewCodeChunkerWithOptions(CodeChunkerOptions{
		MaxChunkTokens: 300, // Force splitting
	})
	defer chunker.Close()

	chunks, err := chunker.Chunk(context.Background(), &FileInput{
		Path:     "search.go",
		Content:  []byte(source),
		Language: "go",
	})

	require.NoError(t, err)
	require.Greater(t, len(chunks), 1, "function should be split into multiple chunks")

	// First chunk should have BOTH the parent symbol and the part symbol
	firstChunk := chunks[0]
	require.NotEmpty(t, firstChunk.Symbols, "first chunk should have symbols")

	var hasParent, hasPart bool
	for _, sym := range firstChunk.Symbols {
		if sym.Name == "LargeSearchMethod" {
			hasParent = true
		}
		if sym.Name == "LargeSearchMethod_part1" {
			hasPart = true
		}
	}

	assert.True(t, hasParent, "first chunk should contain parent symbol 'LargeSearchMethod'")
	assert.True(t, hasPart, "first chunk should contain part symbol 'LargeSearchMethod_part1'")

	// Subsequent chunks should only have part symbols, not parent
	for i, chunk := range chunks[1:] {
		for _, sym := range chunk.Symbols {
			assert.NotEqual(t, "LargeSearchMethod", sym.Name,
				"chunk %d should not have parent symbol", i+2)
			assert.Contains(t, sym.Name, "_part",
				"chunk %d symbols should be parts", i+2)
		}
	}
}

// TS06: Symbol Extraction
func TestCodeChunker_ChunkGoFile_ExtractsSymbolMetadata(t *testing.T) {
	source := `package main

func ProcessData(input []byte) ([]byte, error) {
	return input, nil
}
`
	chunker := NewCodeChunker()
	defer chunker.Close()

	chunks, err := chunker.Chunk(context.Background(), &FileInput{
		Path:     "process.go",
		Content:  []byte(source),
		Language: "go",
	})

	require.NoError(t, err)
	require.Len(t, chunks, 1)
	require.Len(t, chunks[0].Symbols, 1)

	symbol := chunks[0].Symbols[0]
	assert.Equal(t, "ProcessData", symbol.Name)
	assert.Equal(t, SymbolTypeFunction, symbol.Type)
	assert.Equal(t, 3, symbol.StartLine) // 1-indexed
	assert.Equal(t, 5, symbol.EndLine)
}

// Additional tests for comprehensive coverage

func TestCodeChunker_ChunkGoMethod_ExtractsReceiver(t *testing.T) {
	source := `package main

type Server struct {
	addr string
}

func (s *Server) Start() error {
	return nil
}

func (s *Server) Stop() error {
	return nil
}
`
	chunker := NewCodeChunker()
	defer chunker.Close()

	chunks, err := chunker.Chunk(context.Background(), &FileInput{
		Path:     "server.go",
		Content:  []byte(source),
		Language: "go",
	})

	require.NoError(t, err)
	// Should have chunks for type declaration and methods
	require.GreaterOrEqual(t, len(chunks), 2)

	// Find method chunks
	var methodChunks []*Chunk
	for _, chunk := range chunks {
		for _, sym := range chunk.Symbols {
			if sym.Type == SymbolTypeMethod {
				methodChunks = append(methodChunks, chunk)
				break
			}
		}
	}
	assert.GreaterOrEqual(t, len(methodChunks), 2, "should have 2 method chunks")
}

func TestCodeChunker_ChunkID_IsUnique(t *testing.T) {
	source := `package main

func One() {}

func Two() {}

func Three() {}
`
	chunker := NewCodeChunker()
	defer chunker.Close()

	chunks, err := chunker.Chunk(context.Background(), &FileInput{
		Path:     "funcs.go",
		Content:  []byte(source),
		Language: "go",
	})

	require.NoError(t, err)
	require.Len(t, chunks, 3)

	// All chunk IDs should be unique and 16 chars
	ids := make(map[string]bool)
	for _, chunk := range chunks {
		assert.Len(t, chunk.ID, 16, "chunk ID should be 16 characters")
		assert.False(t, ids[chunk.ID], "chunk ID should be unique")
		ids[chunk.ID] = true
	}
}

func TestCodeChunker_Chunk_SetsMetadata(t *testing.T) {
	source := `package main

func Hello() {}
`
	chunker := NewCodeChunker()
	defer chunker.Close()

	chunks, err := chunker.Chunk(context.Background(), &FileInput{
		Path:     "hello.go",
		Content:  []byte(source),
		Language: "go",
	})

	require.NoError(t, err)
	require.Len(t, chunks, 1)

	chunk := chunks[0]
	assert.Equal(t, "hello.go", chunk.FilePath)
	assert.Equal(t, ContentTypeCode, chunk.ContentType)
	assert.Equal(t, "go", chunk.Language)
	assert.NotZero(t, chunk.CreatedAt)
	assert.NotZero(t, chunk.UpdatedAt)
}

func TestCodeChunker_ChunkPythonClass_SplitsIfLarge(t *testing.T) {
	source := `import logging

class DataProcessor:
    def __init__(self, config):
        self.config = config
        self.logger = logging.getLogger(__name__)

    def process(self, data):
        return data

    def validate(self, data):
        return True
`
	chunker := NewCodeChunker()
	defer chunker.Close()

	chunks, err := chunker.Chunk(context.Background(), &FileInput{
		Path:     "processor.py",
		Content:  []byte(source),
		Language: "python",
	})

	require.NoError(t, err)
	require.NotEmpty(t, chunks)

	// Should contain class-related content
	found := false
	for _, chunk := range chunks {
		if strings.Contains(chunk.RawContent, "DataProcessor") {
			found = true
			break
		}
	}
	assert.True(t, found, "should contain DataProcessor class")
}

func TestCodeChunker_ChunkJavaScript_HandlesArrowFunctions(t *testing.T) {
	source := `const greet = (name) => {
	return 'Hello, ' + name;
};

const farewell = function(name) {
	return 'Goodbye, ' + name;
};
`
	chunker := NewCodeChunker()
	defer chunker.Close()

	chunks, err := chunker.Chunk(context.Background(), &FileInput{
		Path:     "greetings.js",
		Content:  []byte(source),
		Language: "javascript",
	})

	require.NoError(t, err)
	require.NotEmpty(t, chunks)

	// Should extract arrow function and function expression
	names := make([]string, 0)
	for _, chunk := range chunks {
		for _, sym := range chunk.Symbols {
			names = append(names, sym.Name)
		}
	}
	assert.Contains(t, names, "greet")
	assert.Contains(t, names, "farewell")
}

func TestCodeChunker_SupportedExtensions(t *testing.T) {
	chunker := NewCodeChunker()
	defer chunker.Close()

	exts := chunker.SupportedExtensions()

	assert.Contains(t, exts, ".go")
	assert.Contains(t, exts, ".ts")
	assert.Contains(t, exts, ".tsx")
	assert.Contains(t, exts, ".js")
	assert.Contains(t, exts, ".jsx")
	assert.Contains(t, exts, ".py")
}

func TestCodeChunker_EmptyFile_ReturnsNoChunks(t *testing.T) {
	chunker := NewCodeChunker()
	defer chunker.Close()

	chunks, err := chunker.Chunk(context.Background(), &FileInput{
		Path:     "empty.go",
		Content:  []byte(""),
		Language: "go",
	})

	require.NoError(t, err)
	assert.Empty(t, chunks)
}

func TestCodeChunker_OnlyPackageDecl_ReturnsNoChunks(t *testing.T) {
	chunker := NewCodeChunker()
	defer chunker.Close()

	chunks, err := chunker.Chunk(context.Background(), &FileInput{
		Path:     "pkg.go",
		Content:  []byte("package main\n"),
		Language: "go",
	})

	require.NoError(t, err)
	// No functions or types, so no chunks expected
	assert.Empty(t, chunks)
}

func TestCodeChunker_ChunkTypeScriptInterface(t *testing.T) {
	source := `export interface User {
	id: string;
	name: string;
	email: string;
}
`
	chunker := NewCodeChunker()
	defer chunker.Close()

	chunks, err := chunker.Chunk(context.Background(), &FileInput{
		Path:     "types.ts",
		Content:  []byte(source),
		Language: "typescript",
	})

	require.NoError(t, err)
	require.Len(t, chunks, 1)

	assert.Equal(t, "User", chunks[0].Symbols[0].Name)
	assert.Equal(t, SymbolTypeInterface, chunks[0].Symbols[0].Type)
}

func TestCodeChunker_ContentIncludesContext(t *testing.T) {
	source := `package main

import (
	"fmt"
	"strings"
)

func Hello(name string) {
	fmt.Println(strings.ToUpper(name))
}
`
	chunker := NewCodeChunker()
	defer chunker.Close()

	chunks, err := chunker.Chunk(context.Background(), &FileInput{
		Path:     "hello.go",
		Content:  []byte(source),
		Language: "go",
	})

	require.NoError(t, err)
	require.Len(t, chunks, 1)

	// Content should include context
	assert.Contains(t, chunks[0].Content, "package main")
	assert.Contains(t, chunks[0].Content, "import")
	assert.Contains(t, chunks[0].Content, "func Hello")

	// RawContent should be just the function
	assert.Contains(t, chunks[0].RawContent, "func Hello")
	assert.NotContains(t, chunks[0].RawContent, "package main")

	// Context should have package and imports
	assert.Contains(t, chunks[0].Context, "package main")
	assert.Contains(t, chunks[0].Context, "import")
}

// BUG-052: Content-Addressable Chunk IDs
// These tests verify that chunk IDs are stable across line number shifts,
// which is critical for checkpoint/resume functionality.

// TestCodeChunker_StableIDsAcrossLineShifts verifies that the same function
// produces the same chunk ID regardless of where it appears in the file.
// This is critical for resume to work after files are modified.
func TestCodeChunker_StableIDsAcrossLineShifts(t *testing.T) {
	// Original file: Hello() starts at line 5
	source1 := `package main

import "fmt"

func Hello() {
	fmt.Println("Hello")
}
`
	// Modified file: NewFunc() added before Hello(), Hello() now at line 9
	source2 := `package main

import "fmt"

func NewFunc() {
	fmt.Println("New")
}

func Hello() {
	fmt.Println("Hello")
}
`
	chunker := NewCodeChunker()
	defer chunker.Close()

	chunks1, err := chunker.Chunk(context.Background(), &FileInput{
		Path:     "main.go",
		Content:  []byte(source1),
		Language: "go",
	})
	require.NoError(t, err)

	chunks2, err := chunker.Chunk(context.Background(), &FileInput{
		Path:     "main.go",
		Content:  []byte(source2),
		Language: "go",
	})
	require.NoError(t, err)

	// Find Hello chunk in both versions
	var helloID1, helloID2 string
	for _, c := range chunks1 {
		for _, sym := range c.Symbols {
			if sym.Name == "Hello" {
				helloID1 = c.ID
				break
			}
		}
	}
	for _, c := range chunks2 {
		for _, sym := range c.Symbols {
			if sym.Name == "Hello" {
				helloID2 = c.ID
				break
			}
		}
	}

	require.NotEmpty(t, helloID1, "Hello chunk should exist in source1")
	require.NotEmpty(t, helloID2, "Hello chunk should exist in source2")

	// KEY ASSERTION: Hello chunk ID should be IDENTICAL despite line shift
	// This test will FAIL with position-based IDs, PASS with content-based IDs
	assert.Equal(t, helloID1, helloID2,
		"Hello() chunk ID should be stable across line number shifts")
}

// TestCodeChunker_DifferentContentDifferentID verifies that changing
// function content produces a different chunk ID (embedding invalidation).
func TestCodeChunker_DifferentContentDifferentID(t *testing.T) {
	source1 := `package main

func Hello() {
	println("Hello")
}
`
	source2 := `package main

func Hello() {
	println("Hello World") // Changed content
}
`
	chunker := NewCodeChunker()
	defer chunker.Close()

	chunks1, err := chunker.Chunk(context.Background(), &FileInput{
		Path:     "main.go",
		Content:  []byte(source1),
		Language: "go",
	})
	require.NoError(t, err)

	chunks2, err := chunker.Chunk(context.Background(), &FileInput{
		Path:     "main.go",
		Content:  []byte(source2),
		Language: "go",
	})
	require.NoError(t, err)

	// Content changed, so chunk ID should be different
	assert.NotEqual(t, chunks1[0].ID, chunks2[0].ID,
		"Modified function content should produce different chunk ID")
}

// TestCodeChunker_SameContentDifferentFile verifies that identical content
// in different files produces different chunk IDs (file context preserved).
func TestCodeChunker_SameContentDifferentFile(t *testing.T) {
	source := `package main

func Hello() {
	println("Hello")
}
`
	chunker := NewCodeChunker()
	defer chunker.Close()

	chunks1, err := chunker.Chunk(context.Background(), &FileInput{
		Path:     "file1.go",
		Content:  []byte(source),
		Language: "go",
	})
	require.NoError(t, err)

	chunks2, err := chunker.Chunk(context.Background(), &FileInput{
		Path:     "file2.go",
		Content:  []byte(source),
		Language: "go",
	})
	require.NoError(t, err)

	// Same content but different files should have different IDs
	assert.NotEqual(t, chunks1[0].ID, chunks2[0].ID,
		"Same content in different files should produce different chunk IDs")
}

// TS07: Constant Extraction Tests

// TestCodeChunker_ChunkGoFile_ExtractsConstants verifies that Go constants
// are extracted and chunked with proper symbol metadata.
func TestCodeChunker_ChunkGoFile_ExtractsConstants(t *testing.T) {
	source := `package config

// DefaultTimeout is the default request timeout in seconds.
const DefaultTimeout = 30

// MaxRetries is the maximum number of retry attempts.
const MaxRetries = 3
`
	chunker := NewCodeChunker()
	defer chunker.Close()

	chunks, err := chunker.Chunk(context.Background(), &FileInput{
		Path:     "config.go",
		Content:  []byte(source),
		Language: "go",
	})

	require.NoError(t, err)
	require.NotEmpty(t, chunks, "should extract constants as chunks")

	// Find constant symbols
	var constNames []string
	for _, chunk := range chunks {
		for _, sym := range chunk.Symbols {
			if sym.Type == SymbolTypeConstant {
				constNames = append(constNames, sym.Name)
			}
		}
	}

	assert.Contains(t, constNames, "DefaultTimeout", "should extract DefaultTimeout constant")
	assert.Contains(t, constNames, "MaxRetries", "should extract MaxRetries constant")
}

// TestCodeChunker_ChunkGoFile_ExtractsGroupedConstants verifies that grouped
// const declarations (const block) are extracted as a single chunk.
func TestCodeChunker_ChunkGoFile_ExtractsGroupedConstants(t *testing.T) {
	source := `package status

const (
	StatusPending   = "pending"
	StatusActive    = "active"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
)
`
	chunker := NewCodeChunker()
	defer chunker.Close()

	chunks, err := chunker.Chunk(context.Background(), &FileInput{
		Path:     "status.go",
		Content:  []byte(source),
		Language: "go",
	})

	require.NoError(t, err)
	require.NotEmpty(t, chunks, "should extract grouped constants")

	// Grouped constants should be in one chunk
	var constChunk *Chunk
	for _, chunk := range chunks {
		for _, sym := range chunk.Symbols {
			if sym.Type == SymbolTypeConstant {
				constChunk = chunk
				break
			}
		}
		if constChunk != nil {
			break
		}
	}

	require.NotNil(t, constChunk, "should have a constant chunk")
	assert.Contains(t, constChunk.RawContent, "StatusPending")
	assert.Contains(t, constChunk.RawContent, "StatusFailed")
}

// TestCodeChunker_ChunkGoFile_ExtractsLongStringConstant verifies that long
// string constants (like SQL queries or templates) are extracted.
func TestCodeChunker_ChunkGoFile_ExtractsLongStringConstant(t *testing.T) {
	source := `package queries

// CreateUserTable is the SQL statement to create the users table.
const CreateUserTable = ` + "`" + `
	CREATE TABLE IF NOT EXISTS users (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		email VARCHAR(255) UNIQUE NOT NULL,
		name VARCHAR(255) NOT NULL,
		password_hash VARCHAR(255) NOT NULL,
		created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
		updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
		deleted_at TIMESTAMP WITH TIME ZONE,
		CONSTRAINT users_email_check CHECK (email ~* '^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$')
	);
	CREATE INDEX idx_users_email ON users(email);
	CREATE INDEX idx_users_created_at ON users(created_at);
` + "`"
	chunker := NewCodeChunker()
	defer chunker.Close()

	chunks, err := chunker.Chunk(context.Background(), &FileInput{
		Path:     "queries.go",
		Content:  []byte(source),
		Language: "go",
	})

	require.NoError(t, err)
	require.NotEmpty(t, chunks, "should extract long string constants")

	// Find the constant chunk
	var found bool
	for _, chunk := range chunks {
		for _, sym := range chunk.Symbols {
			if sym.Type == SymbolTypeConstant && sym.Name == "CreateUserTable" {
				found = true
				assert.Contains(t, chunk.RawContent, "CREATE TABLE")
				assert.Contains(t, chunk.RawContent, "idx_users_email")
			}
		}
	}
	assert.True(t, found, "should find CreateUserTable constant")
}

// TestCodeChunker_ChunkGoFile_ExtractsVariables verifies that Go var
// declarations are extracted with proper symbol metadata.
func TestCodeChunker_ChunkGoFile_ExtractsVariables(t *testing.T) {
	source := `package config

// DefaultConfig holds the default configuration values.
var DefaultConfig = Config{
	Timeout:    30,
	MaxRetries: 3,
	BaseURL:    "https://api.example.com",
}
`
	chunker := NewCodeChunker()
	defer chunker.Close()

	chunks, err := chunker.Chunk(context.Background(), &FileInput{
		Path:     "config.go",
		Content:  []byte(source),
		Language: "go",
	})

	require.NoError(t, err)
	require.NotEmpty(t, chunks, "should extract variables as chunks")

	// Find variable symbol
	var found bool
	for _, chunk := range chunks {
		for _, sym := range chunk.Symbols {
			if sym.Type == SymbolTypeVariable && sym.Name == "DefaultConfig" {
				found = true
				break
			}
		}
	}
	assert.True(t, found, "should extract DefaultConfig variable")
}

// TestCodeChunker_ChunkTypeScript_ExtractsConstants verifies that TypeScript
// const declarations are extracted.
func TestCodeChunker_ChunkTypeScript_ExtractsConstants(t *testing.T) {
	source := `export const API_CONFIG = {
	baseUrl: 'https://api.example.com',
	timeout: 30000,
	retryAttempts: 3,
	headers: {
		'Content-Type': 'application/json',
	},
};

export const ERROR_MESSAGES = {
	NETWORK_ERROR: 'Failed to connect to the server',
	AUTH_ERROR: 'Authentication failed',
	NOT_FOUND: 'Resource not found',
};
`
	chunker := NewCodeChunker()
	defer chunker.Close()

	chunks, err := chunker.Chunk(context.Background(), &FileInput{
		Path:     "config.ts",
		Content:  []byte(source),
		Language: "typescript",
	})

	require.NoError(t, err)
	require.NotEmpty(t, chunks, "should extract TypeScript constants")

	// Find constant symbols
	var constNames []string
	for _, chunk := range chunks {
		for _, sym := range chunk.Symbols {
			if sym.Type == SymbolTypeConstant {
				constNames = append(constNames, sym.Name)
			}
		}
	}

	assert.Contains(t, constNames, "API_CONFIG", "should extract API_CONFIG constant")
	assert.Contains(t, constNames, "ERROR_MESSAGES", "should extract ERROR_MESSAGES constant")
}

// Benchmark test
func BenchmarkCodeChunker_ChunkGoFile(b *testing.B) {
	source := `package main

import "fmt"

func One() { fmt.Println("1") }
func Two() { fmt.Println("2") }
func Three() { fmt.Println("3") }
func Four() { fmt.Println("4") }
func Five() { fmt.Println("5") }
func Six() { fmt.Println("6") }
func Seven() { fmt.Println("7") }
func Eight() { fmt.Println("8") }
func Nine() { fmt.Println("9") }
func Ten() { fmt.Println("10") }
`
	chunker := NewCodeChunker()
	defer chunker.Close()

	input := &FileInput{
		Path:     "funcs.go",
		Content:  []byte(source),
		Language: "go",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = chunker.Chunk(context.Background(), input)
	}
}
