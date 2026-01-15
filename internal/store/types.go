// Package store provides vector storage (USearch), BM25 index, and metadata persistence (SQLite).
// This is the persistence layer for all indexed data.
package store

import (
	"context"
	"fmt"
	"time"
)

// ContentType represents the type of content in a chunk.
type ContentType string

const (
	ContentTypeCode     ContentType = "code"
	ContentTypeMarkdown ContentType = "markdown"
	ContentTypeText     ContentType = "text"
)

// State keys for metadata store (QW-5: dimension mismatch handling)
const (
	// StateKeyIndexDimension stores the embedding dimension used for the index
	StateKeyIndexDimension = "index_embedding_dimension"
	// StateKeyIndexModel stores the embedding model name used for the index
	StateKeyIndexModel = "index_embedding_model"
)

// Checkpoint state keys for resumable indexing
const (
	// StateKeyCheckpointStage stores current indexing stage: "scanning"|"chunking"|"embedding"|"indexing"|"complete"
	StateKeyCheckpointStage = "checkpoint_stage"
	// StateKeyCheckpointTotal stores total number of chunks to process
	StateKeyCheckpointTotal = "checkpoint_total"
	// StateKeyCheckpointEmbedded stores count of chunks that have been embedded
	StateKeyCheckpointEmbedded = "checkpoint_embedded"
	// StateKeyCheckpointTimestamp stores when checkpoint was last updated
	StateKeyCheckpointTimestamp = "checkpoint_timestamp"
	// StateKeyCheckpointEmbedderModel stores the embedder model used for this checkpoint (BUG-053)
	// Used to validate embedder consistency on resume to prevent dimension mismatch
	StateKeyCheckpointEmbedderModel = "checkpoint_embedder_model"
)

// Chunk ID versioning for migration support (BUG-052)
const (
	// StateKeyChunkIDVersion stores the chunk ID generation version
	// Used to detect legacy position-based indexes that need rebuild
	StateKeyChunkIDVersion = "chunk_id_version"

	// ChunkIDVersionLegacy indicates position-based chunk IDs (filePath + startLine)
	// These indexes cannot reliably resume after file modifications
	ChunkIDVersionLegacy = "1"

	// ChunkIDVersionContent indicates content-addressable chunk IDs (filePath + contentHash)
	// These indexes are stable across line number shifts
	ChunkIDVersionContent = "2"
)

// SymbolType represents the type of code symbol.
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

// Symbol represents a code symbol extracted during chunking.
type Symbol struct {
	Name       string
	Type       SymbolType
	StartLine  int
	EndLine    int
	Signature  string // For functions
	DocComment string
}

// Chunk represents a retrievable unit of content (code function, documentation section, etc.).
type Chunk struct {
	ID          string            // SHA256(file_path + start_line)
	FileID      string            // Parent file ID
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

// File represents a tracked file in the index.
type File struct {
	ID          string    // SHA256(relative_path)
	ProjectID   string    // Parent project ID
	Path        string    // Relative to project root
	Size        int64     // File size in bytes
	ModTime     time.Time // Last modification time
	ContentHash string    // SHA256 of content
	Language    string    // Detected language
	ContentType string    // code, markdown, text
	IndexedAt   time.Time // When indexed
}

// Project represents an indexed project/codebase.
type Project struct {
	ID          string    // SHA256(absolute_path)
	Name        string    // Directory name
	RootPath    string    // Absolute path
	ProjectType string    // go, node, python, etc.
	ChunkCount  int
	FileCount   int
	IndexedAt   time.Time
	Version     string // Index schema version
}

// MetadataStore persists chunk metadata in SQLite.
type MetadataStore interface {
	// Project operations
	SaveProject(ctx context.Context, project *Project) error
	GetProject(ctx context.Context, id string) (*Project, error)
	UpdateProjectStats(ctx context.Context, id string, fileCount, chunkCount int) error
	RefreshProjectStats(ctx context.Context, id string) error // Recalculates counts from DB and updates indexed_at

	// File operations
	SaveFiles(ctx context.Context, files []*File) error
	GetFileByPath(ctx context.Context, projectID, path string) (*File, error)
	GetChangedFiles(ctx context.Context, projectID string, since time.Time) ([]*File, error)
	ListFiles(ctx context.Context, projectID string, cursor string, limit int) ([]*File, string, error)
	GetFilePathsByProject(ctx context.Context, projectID string) ([]string, error)                 // For gitignore sync
	GetFilesForReconciliation(ctx context.Context, projectID string) (map[string]*File, error)    // For startup file sync (BUG-036)
	ListFilePathsUnder(ctx context.Context, projectID, dirPrefix string) ([]string, error)        // For subtree gitignore (BUG-028)
	DeleteFile(ctx context.Context, fileID string) error                                          // For gitignore sync (cascades to chunks)
	DeleteFilesByProject(ctx context.Context, projectID string) error

	// Chunk operations
	SaveChunks(ctx context.Context, chunks []*Chunk) error
	GetChunk(ctx context.Context, id string) (*Chunk, error)
	GetChunks(ctx context.Context, ids []string) ([]*Chunk, error) // Batch retrieval for performance
	GetChunksByFile(ctx context.Context, fileID string) ([]*Chunk, error)
	DeleteChunks(ctx context.Context, ids []string) error          // Delete chunks by ID
	DeleteChunksByFile(ctx context.Context, fileID string) error

	// Symbol operations
	SearchSymbols(ctx context.Context, name string, limit int) ([]*Symbol, error)

	// State operations (key-value store for runtime state)
	GetState(ctx context.Context, key string) (string, error)
	SetState(ctx context.Context, key, value string) error

	// Embedding operations (for HNSW compaction)
	SaveChunkEmbeddings(ctx context.Context, chunkIDs []string, embeddings [][]float32, model string) error
	GetAllEmbeddings(ctx context.Context) (map[string][]float32, error)
	GetEmbeddingStats(ctx context.Context) (withEmbedding, withoutEmbedding int, err error)

	// Checkpoint operations (for resumable indexing)
	SaveIndexCheckpoint(ctx context.Context, stage string, total, embeddedCount int, embedderModel string) error
	LoadIndexCheckpoint(ctx context.Context) (*IndexCheckpoint, error)
	ClearIndexCheckpoint(ctx context.Context) error

	// Lifecycle
	Close() error
}

// IndexCheckpoint represents the saved state of an indexing operation for resume.
type IndexCheckpoint struct {
	Stage         string    // "scanning", "chunking", "embedding", "indexing", "complete"
	Total         int       // Total chunks to process
	EmbeddedCount int       // Number of chunks with embeddings
	Timestamp     time.Time // When checkpoint was last updated
	EmbedderModel string    // Embedder model name used for this checkpoint
}

// IndexInfo contains comprehensive information about an index for the `amanmcp index info` command.
type IndexInfo struct {
	// Location paths
	Location    string // Index data directory (e.g., ~/.amanmcp/project-hash/)
	ProjectRoot string // Project root directory

	// Embedding configuration stored in index
	IndexModel      string // Model name used to build index
	IndexBackend    string // Backend (mlx, ollama, static)
	IndexDimensions int    // Embedding dimensions

	// Statistics
	ChunkCount    int   // Number of chunks in index
	DocumentCount int   // Number of documents (files) indexed
	IndexSizeBytes int64 // Total index size (BM25 + vector)
	BM25SizeBytes  int64 // BM25 index file size
	VectorSizeBytes int64 // Vector store file size

	// Timestamps
	CreatedAt time.Time // When index was first created
	UpdatedAt time.Time // When index was last updated

	// Current embedder (for comparison)
	CurrentModel      string // Current embedder model
	CurrentBackend    string // Current embedder backend
	CurrentDimensions int    // Current embedder dimensions
	Compatible        bool   // Whether current embedder is compatible with index
}

// CurrentSchemaVersion is the current database schema version.
const CurrentSchemaVersion = 2

// Document represents a document to be indexed in BM25.
type Document struct {
	ID      string // Chunk ID
	Content string // Text content
}

// BM25Result represents a single BM25 search result.
type BM25Result struct {
	DocID        string
	Score        float64
	MatchedTerms []string
}

// IndexStats provides statistics about the BM25 index.
type IndexStats struct {
	DocumentCount int
	TermCount     int
	AvgDocLength  float64
}

// BM25Index provides keyword search using BM25 algorithm.
type BM25Index interface {
	// Index adds documents to the index
	Index(ctx context.Context, docs []*Document) error

	// Search returns documents matching query, scored by BM25
	Search(ctx context.Context, query string, limit int) ([]*BM25Result, error)

	// Delete removes documents from index
	Delete(ctx context.Context, docIDs []string) error

	// AllIDs returns all document IDs in the index (for consistency checks)
	AllIDs() ([]string, error)

	// Stats returns index statistics
	Stats() *IndexStats

	// Persistence
	Save(path string) error
	Load(path string) error
	Close() error
}

// BM25Config configures the BM25 index.
type BM25Config struct {
	// K1 is the term frequency saturation parameter (default: 1.2)
	K1 float64

	// B is the length normalization parameter (default: 0.75)
	B float64

	// StopWords is a list of words to filter out during tokenization
	StopWords []string

	// MinTokenLength is minimum token length to index (default: 2)
	MinTokenLength int
}

// DefaultBM25Config returns default BM25 configuration.
func DefaultBM25Config() BM25Config {
	return BM25Config{
		K1:             1.2,
		B:              0.75,
		StopWords:      DefaultCodeStopWords,
		MinTokenLength: 2,
	}
}

// DefaultCodeStopWords contains programming keywords to filter out.
var DefaultCodeStopWords = []string{
	"var", "let", "const", "func", "function", "def", "class",
	"return", "if", "else", "for", "while",
	"data", "result", "value", "item", "key", "err", "ctx", "tmp",
}

// VectorResult represents a single vector search result.
type VectorResult struct {
	ID       string  // Chunk ID
	Distance float32 // Lower is more similar (0-2 for cosine)
	Score    float32 // Normalized similarity (0-1)
}

// VectorStoreConfig configures the vector store.
type VectorStoreConfig struct {
	// Dimensions is the vector dimension (768 for Hugot/EmbeddingGemma, 384 for MiniLM, 256 for static)
	Dimensions int

	// Quantization is the vector precision: "f32", "f16", "i8" (default: "f16")
	Quantization string

	// Metric is the distance metric: "cos" (cosine), "l2" (euclidean) (default: "cos")
	Metric string

	// M is HNSW max connections per layer (default: 32)
	M int

	// EfConstruction is HNSW build-time search width (default: 128)
	EfConstruction int

	// EfSearch is HNSW query-time search width (default: 64)
	EfSearch int
}

// DefaultVectorStoreConfig returns sensible defaults for vector store.
func DefaultVectorStoreConfig(dimensions int) VectorStoreConfig {
	return VectorStoreConfig{
		Dimensions:     dimensions,
		Quantization:   "f16",
		Metric:         "cos",
		M:              32,
		EfConstruction: 128,
		EfSearch:       64,
	}
}

// VectorStore provides semantic search using HNSW algorithm.
type VectorStore interface {
	// Add inserts vectors with their IDs. If an ID exists, it is replaced.
	Add(ctx context.Context, ids []string, vectors [][]float32) error

	// Search finds k nearest neighbors to query vector.
	Search(ctx context.Context, query []float32, k int) ([]*VectorResult, error)

	// Delete removes vectors by ID.
	Delete(ctx context.Context, ids []string) error

	// AllIDs returns all vector IDs in the store (for consistency checks)
	AllIDs() []string

	// Contains checks if ID exists.
	Contains(id string) bool

	// Count returns number of vectors.
	Count() int

	// Persistence
	Save(path string) error
	Load(path string) error
	Close() error
}

// ErrDimensionMismatch indicates vector dimension mismatch.
type ErrDimensionMismatch struct {
	Expected int
	Got      int
}

func (e ErrDimensionMismatch) Error() string {
	return fmt.Sprintf("dimension mismatch: expected %d, got %d (run 'amanmcp reindex --force')", e.Expected, e.Got)
}
