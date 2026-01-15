package chunk

import (
	"context"
	"time"
)

// Chunk size defaults (based on 2025 RAG research)
const (
	DefaultMaxChunkTokens = 512 // Optimal for 85-90% recall
	DefaultOverlapTokens  = 64  // ~12.5% overlap
	MinChunkTokens        = 100 // Minimum viable chunk
	TokensPerChar         = 4   // Rough approximation: 4 chars = 1 token
)

// ContentType represents the type of content in a chunk
type ContentType string

const (
	ContentTypeCode     ContentType = "code"
	ContentTypeMarkdown ContentType = "markdown"
	ContentTypeText     ContentType = "text"
)

// Chunk is a retrievable unit of content
type Chunk struct {
	ID          string            // SHA256(file_path + start_line)[:16]
	FilePath    string            // Relative to project root
	Content     string            // Full content with context
	RawContent  string            // Just the symbol, no context (code only)
	Context     string            // Imports, package decl (code only)
	ContentType ContentType       // code, markdown, text
	Language    string            // go, typescript, python, etc.
	StartLine   int               // 1-indexed
	EndLine     int               // Inclusive
	Symbols     []*Symbol         // Functions, classes, etc.
	Metadata    map[string]string // Custom metadata
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// FileInput is input for the Chunker interface
type FileInput struct {
	Path     string // Relative path
	Content  []byte // File content
	Language string // go, typescript, python, etc.
}

// Chunker is the interface for splitting files into chunks
type Chunker interface {
	// Chunk splits a file into semantic chunks
	Chunk(ctx context.Context, file *FileInput) ([]*Chunk, error)

	// SupportedExtensions returns file extensions this chunker handles
	SupportedExtensions() []string
}

// SymbolType represents the kind of code symbol
type SymbolType string

const (
	SymbolTypeFunction  SymbolType = "function"
	SymbolTypeClass     SymbolType = "class"
	SymbolTypeInterface SymbolType = "interface"
	SymbolTypeType      SymbolType = "type"
	SymbolTypeVariable  SymbolType = "variable"
	SymbolTypeConstant  SymbolType = "constant"
	SymbolTypeMethod    SymbolType = "method"
)

// Symbol represents a code symbol extracted from parsing
type Symbol struct {
	Name       string
	Type       SymbolType
	StartLine  int
	EndLine    int
	Signature  string
	DocComment string
}

// Tree represents a parsed AST
type Tree struct {
	Root     *Node
	Source   []byte
	Language string
}

// Node represents a node in the AST
type Node struct {
	Type       string
	StartByte  uint32
	EndByte    uint32
	StartPoint Point
	EndPoint   Point
	Children   []*Node
	HasError   bool
}

// Point represents a position in the source code
type Point struct {
	Row    uint32 // 0-indexed line number
	Column uint32
}

// LanguageConfig holds configuration for a supported language
type LanguageConfig struct {
	Name       string
	Extensions []string

	// Node types that indicate function declarations
	FunctionTypes []string

	// Node types that indicate class/struct definitions
	ClassTypes []string

	// Node types that indicate interface definitions
	InterfaceTypes []string

	// Node types that indicate method definitions
	MethodTypes []string

	// Node types that indicate type definitions
	TypeDefTypes []string

	// Node types that indicate constant declarations
	ConstantTypes []string

	// Node types that indicate variable declarations
	VariableTypes []string

	// Node type for name identifier
	NameField string
}
