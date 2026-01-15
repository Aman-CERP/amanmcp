package indexer

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/Aman-CERP/amanmcp/internal/embed"
	"github.com/Aman-CERP/amanmcp/internal/store"
)

// ErrNilEmbedder is returned when attempting to create a VectorIndexer without an embedder.
var ErrNilEmbedder = errors.New("embedder is required")

// ErrNilVectorStore is returned when attempting to create a VectorIndexer without a vector store.
var ErrNilVectorStore = errors.New("vector store is required")

// VectorIndexer provides semantic indexing for code chunks.
//
// It generates embeddings via an [embed.Embedder] and stores them in a
// [store.VectorStore]. This enables semantic similarity search over indexed content.
//
// VectorIndexer is safe for concurrent use. All methods may be called
// from multiple goroutines simultaneously.
type VectorIndexer struct {
	embedder embed.Embedder
	store    store.VectorStore
	mu       sync.RWMutex
	closed   bool
}

// VectorOption configures a VectorIndexer.
// Note: We use a separate type to avoid conflicts with BM25Indexer options.
type VectorOption func(*VectorIndexer)

// WithEmbedder sets the embedder for generating embeddings.
//
// This is a required option; NewVectorIndexer will return an error
// if no embedder is provided.
func WithEmbedder(e embed.Embedder) VectorOption {
	return func(v *VectorIndexer) {
		v.embedder = e
	}
}

// WithVectorStore sets the vector store backend.
//
// This is a required option; NewVectorIndexer will return an error
// if no store is provided.
func WithVectorStore(s store.VectorStore) VectorOption {
	return func(v *VectorIndexer) {
		v.store = s
	}
}

// NewVectorIndexer creates a new vector indexer with the given options.
//
// At minimum, WithEmbedder and WithVectorStore must be provided:
//
//	indexer, err := NewVectorIndexer(
//	    WithEmbedder(embedder),
//	    WithVectorStore(vectorStore),
//	)
//
// Returns ErrNilEmbedder if no embedder is provided.
// Returns ErrNilVectorStore if no store is provided.
func NewVectorIndexer(opts ...VectorOption) (*VectorIndexer, error) {
	v := &VectorIndexer{}

	for _, opt := range opts {
		opt(v)
	}

	if v.embedder == nil {
		return nil, ErrNilEmbedder
	}
	if v.store == nil {
		return nil, ErrNilVectorStore
	}

	return v, nil
}

// Index generates embeddings for chunks and stores them in the vector store.
//
// The process:
//  1. Extract text content from chunks
//  2. Generate embeddings via embedder.EmbedBatch()
//  3. Store embeddings via vectorStore.Add()
//
// Empty or nil slices are no-ops that return nil.
//
// This method is thread-safe.
func (v *VectorIndexer) Index(ctx context.Context, chunks []*store.Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	// Extract texts and IDs from chunks
	texts := make([]string, len(chunks))
	ids := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Content
		ids[i] = c.ID
	}

	// Generate embeddings
	embeddings, err := v.embedder.EmbedBatch(ctx, texts)
	if err != nil {
		return fmt.Errorf("vector embed: %w", err)
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	// Store embeddings
	if err := v.store.Add(ctx, ids, embeddings); err != nil {
		return fmt.Errorf("vector store add: %w", err)
	}

	return nil
}

// Delete removes vectors by ID from the vector store.
//
// Non-existent IDs are silently ignored (no error).
// Empty or nil slices are no-ops that return nil.
//
// This method is thread-safe.
func (v *VectorIndexer) Delete(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	if err := v.store.Delete(ctx, ids); err != nil {
		return fmt.Errorf("vector delete: %w", err)
	}

	return nil
}

// Clear removes all vectors from the store.
//
// This retrieves all vector IDs and deletes them.
// An empty store is a no-op.
//
// This method is thread-safe.
func (v *VectorIndexer) Clear(ctx context.Context) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	ids := v.store.AllIDs()
	if len(ids) == 0 {
		return nil
	}

	if err := v.store.Delete(ctx, ids); err != nil {
		return fmt.Errorf("vector clear: %w", err)
	}

	return nil
}

// Stats returns current index statistics.
//
// For vector stores, only DocumentCount is meaningful (number of vectors).
// TermCount and AvgDocLength are not applicable and return 0.
//
// This method is thread-safe. The returned stats are a snapshot;
// values may change immediately after if other goroutines modify the index.
func (v *VectorIndexer) Stats() IndexStats {
	v.mu.RLock()
	defer v.mu.RUnlock()

	return IndexStats{
		DocumentCount: v.store.Count(),
		TermCount:     0, // N/A for vectors
		AvgDocLength:  0, // N/A for vectors
	}
}

// Close releases all resources held by the indexer.
//
// This method is idempotent; calling it multiple times is safe.
// After Close, other methods may return errors.
//
// This method is thread-safe.
func (v *VectorIndexer) Close() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.closed {
		return nil
	}

	v.closed = true

	if err := v.store.Close(); err != nil {
		return fmt.Errorf("vector close: %w", err)
	}

	return nil
}

// Ensure VectorIndexer implements Indexer at compile time.
var _ Indexer = (*VectorIndexer)(nil)
