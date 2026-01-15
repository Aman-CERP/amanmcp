package indexer

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/Aman-CERP/amanmcp/internal/store"
)

// ErrNilStore is returned when attempting to create a BM25Indexer without a store.
var ErrNilStore = errors.New("BM25 store is required")

// BM25Indexer provides BM25-based keyword indexing for code chunks.
//
// It wraps a [store.BM25Index] and provides a higher-level interface
// that operates on [store.Chunk] objects (domain model) rather than
// raw documents (storage model).
//
// BM25Indexer is safe for concurrent use. All methods may be called
// from multiple goroutines simultaneously.
type BM25Indexer struct {
	store  store.BM25Index
	mu     sync.RWMutex
	closed bool
}

// Option configures a BM25Indexer.
type Option func(*BM25Indexer)

// WithStore sets the BM25 store backend.
//
// This is a required option; NewBM25Indexer will return an error
// if no store is provided.
func WithStore(s store.BM25Index) Option {
	return func(i *BM25Indexer) {
		i.store = s
	}
}

// NewBM25Indexer creates a new BM25 indexer with the given options.
//
// At minimum, WithStore must be provided:
//
//	indexer, err := NewBM25Indexer(WithStore(bm25Store))
//
// Returns ErrNilStore if no store is provided.
func NewBM25Indexer(opts ...Option) (*BM25Indexer, error) {
	i := &BM25Indexer{}

	for _, opt := range opts {
		opt(i)
	}

	if i.store == nil {
		return nil, ErrNilStore
	}

	return i, nil
}

// Index adds chunks to the BM25 index.
//
// Chunks are converted to documents with ID and Content fields.
// Empty or nil slices are no-ops that return nil.
//
// This method is thread-safe.
func (i *BM25Indexer) Index(ctx context.Context, chunks []*store.Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	// Convert chunks to documents
	docs := make([]*store.Document, len(chunks))
	for j, c := range chunks {
		docs[j] = &store.Document{
			ID:      c.ID,
			Content: c.Content,
		}
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	if err := i.store.Index(ctx, docs); err != nil {
		return fmt.Errorf("BM25 index: %w", err)
	}

	return nil
}

// Delete removes chunks by ID from the BM25 index.
//
// Non-existent IDs are silently ignored (no error).
// Empty or nil slices are no-ops that return nil.
//
// This method is thread-safe.
func (i *BM25Indexer) Delete(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	if err := i.store.Delete(ctx, ids); err != nil {
		return fmt.Errorf("BM25 delete: %w", err)
	}

	return nil
}

// Clear removes all content from the BM25 index.
//
// This retrieves all document IDs and deletes them.
// An empty index is a no-op.
//
// This method is thread-safe.
func (i *BM25Indexer) Clear(ctx context.Context) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	ids, err := i.store.AllIDs()
	if err != nil {
		return fmt.Errorf("BM25 get all IDs: %w", err)
	}

	if len(ids) == 0 {
		return nil
	}

	if err := i.store.Delete(ctx, ids); err != nil {
		return fmt.Errorf("BM25 clear: %w", err)
	}

	return nil
}

// Stats returns current index statistics.
//
// This method is thread-safe. The returned stats are a snapshot;
// values may change immediately after if other goroutines modify the index.
func (i *BM25Indexer) Stats() IndexStats {
	i.mu.RLock()
	defer i.mu.RUnlock()

	storeStats := i.store.Stats()
	return IndexStats{
		DocumentCount: storeStats.DocumentCount,
		TermCount:     storeStats.TermCount,
		AvgDocLength:  storeStats.AvgDocLength,
	}
}

// Close releases all resources held by the indexer.
//
// This method is idempotent; calling it multiple times is safe.
// After Close, other methods may return errors.
//
// This method is thread-safe.
func (i *BM25Indexer) Close() error {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.closed {
		return nil
	}

	i.closed = true

	if err := i.store.Close(); err != nil {
		return fmt.Errorf("BM25 close: %w", err)
	}

	return nil
}

// Ensure BM25Indexer implements Indexer at compile time.
var _ Indexer = (*BM25Indexer)(nil)
