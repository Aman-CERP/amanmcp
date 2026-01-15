package indexer

import (
	"context"

	"github.com/Aman-CERP/amanmcp/internal/store"
)

// Indexer defines the contract for indexing operations.
//
// Implementations must be thread-safe for concurrent use.
// All methods accept a context for cancellation and timeout support.
//
// The Indexer interface operates on [store.Chunk] (domain model),
// abstracting away the underlying storage mechanism.
type Indexer interface {
	// Index adds chunks to the index.
	//
	// Behavior:
	//   - Idempotent: re-indexing the same chunk ID updates the content
	//   - Thread-safe: may be called concurrently
	//   - Empty slice is a no-op (returns nil)
	//
	// Returns an error if the underlying store operation fails.
	Index(ctx context.Context, chunks []*store.Chunk) error

	// Delete removes chunks by ID.
	//
	// Behavior:
	//   - No-op for non-existent IDs (does not error)
	//   - Thread-safe: may be called concurrently
	//   - Empty slice is a no-op (returns nil)
	//
	// Returns an error if the underlying store operation fails.
	Delete(ctx context.Context, ids []string) error

	// Clear removes all indexed content.
	//
	// This is a destructive operation that cannot be undone.
	// Use with caution in production environments.
	Clear(ctx context.Context) error

	// Stats returns current index statistics.
	//
	// The returned stats are a snapshot; values may change
	// immediately after the call if other goroutines modify the index.
	Stats() IndexStats

	// Close releases all resources held by the indexer.
	//
	// Behavior:
	//   - Safe to call multiple times (idempotent)
	//   - After Close, other methods may return errors
	//   - Blocks until all pending operations complete
	Close() error
}

// IndexStats holds statistics about an index.
type IndexStats struct {
	// DocumentCount is the number of indexed documents.
	DocumentCount int

	// TermCount is the number of unique terms in the index.
	TermCount int

	// AvgDocLength is the average document length in terms.
	AvgDocLength float64
}
