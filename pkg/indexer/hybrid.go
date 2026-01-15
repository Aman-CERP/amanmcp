package indexer

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/Aman-CERP/amanmcp/internal/store"
)

// ErrNoIndexers is returned when attempting to create a HybridIndexer without any indexers.
var ErrNoIndexers = errors.New("at least one indexer is required")

// HybridIndexer composes multiple indexers for hybrid search.
//
// It coordinates BM25 (keyword) and Vector (semantic) indexers,
// fanning out operations to both. Either indexer may be nil to
// support BM25-only or Vector-only modes.
//
// HybridIndexer is safe for concurrent use. All methods may be called
// from multiple goroutines simultaneously.
type HybridIndexer struct {
	bm25   Indexer // May be nil for vector-only mode
	vector Indexer // May be nil for BM25-only mode
	mu     sync.RWMutex
	closed bool
}

// HybridOption configures a HybridIndexer.
type HybridOption func(*HybridIndexer)

// WithBM25 sets the BM25 indexer component.
//
// Pass nil to operate in vector-only mode.
func WithBM25(idx Indexer) HybridOption {
	return func(h *HybridIndexer) {
		h.bm25 = idx
	}
}

// WithVector sets the Vector indexer component.
//
// Pass nil to operate in BM25-only mode.
func WithVector(idx Indexer) HybridOption {
	return func(h *HybridIndexer) {
		h.vector = idx
	}
}

// NewHybridIndexer creates a hybrid indexer from components.
//
// At least one indexer must be provided. Example configurations:
//
//	// Full hybrid mode
//	h, err := NewHybridIndexer(WithBM25(bm25), WithVector(vector))
//
//	// BM25-only mode (e.g., when embedder unavailable)
//	h, err := NewHybridIndexer(WithBM25(bm25))
//
//	// Vector-only mode (rare)
//	h, err := NewHybridIndexer(WithVector(vector))
//
// Returns ErrNoIndexers if both indexers are nil.
func NewHybridIndexer(opts ...HybridOption) (*HybridIndexer, error) {
	h := &HybridIndexer{}

	for _, opt := range opts {
		opt(h)
	}

	if h.bm25 == nil && h.vector == nil {
		return nil, ErrNoIndexers
	}

	return h, nil
}

// Index sends chunks to both indexers sequentially.
//
// BM25 is indexed first, then Vector. If either fails, the operation
// fails fast and returns immediately. This ensures atomic behavior:
// either both succeed or neither does (for the current batch).
//
// Empty or nil slices are no-ops that return nil.
//
// This method is thread-safe.
func (h *HybridIndexer) Index(ctx context.Context, chunks []*store.Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Index BM25 first (if available)
	if h.bm25 != nil {
		if err := h.bm25.Index(ctx, chunks); err != nil {
			return fmt.Errorf("hybrid bm25 index: %w", err)
		}
	}

	// Then Vector (if available)
	if h.vector != nil {
		if err := h.vector.Index(ctx, chunks); err != nil {
			return fmt.Errorf("hybrid vector index: %w", err)
		}
	}

	return nil
}

// Delete removes chunks from both indexers.
//
// This uses a best-effort pattern: both indexers are attempted even
// if one fails. Orphaned entries in one index are harmless (filtered
// during search) and cleaned up during compaction.
//
// Empty or nil slices are no-ops that return nil.
//
// This method is thread-safe.
func (h *HybridIndexer) Delete(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	var errs []error

	// Delete from BM25 (best-effort)
	if h.bm25 != nil {
		if err := h.bm25.Delete(ctx, ids); err != nil {
			errs = append(errs, fmt.Errorf("hybrid bm25 delete: %w", err))
		}
	}

	// Delete from Vector (best-effort)
	if h.vector != nil {
		if err := h.vector.Delete(ctx, ids); err != nil {
			errs = append(errs, fmt.Errorf("hybrid vector delete: %w", err))
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

// Clear removes all content from both indexers.
//
// Uses fail-fast pattern: if BM25 clear fails, Vector clear is not attempted.
//
// This method is thread-safe.
func (h *HybridIndexer) Clear(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Clear BM25 first
	if h.bm25 != nil {
		if err := h.bm25.Clear(ctx); err != nil {
			return fmt.Errorf("hybrid bm25 clear: %w", err)
		}
	}

	// Then Vector
	if h.vector != nil {
		if err := h.vector.Clear(ctx); err != nil {
			return fmt.Errorf("hybrid vector clear: %w", err)
		}
	}

	return nil
}

// Stats returns combined statistics from both indexers.
//
// For hybrid mode, stats are aggregated:
//   - DocumentCount: Maximum of both (should be equal if consistent)
//   - TermCount: From BM25 (vectors don't have terms)
//   - AvgDocLength: From BM25 (vectors don't have doc length)
//
// For single-indexer modes, returns that indexer's stats.
//
// This method is thread-safe. The returned stats are a snapshot;
// values may change immediately after if other goroutines modify the index.
func (h *HybridIndexer) Stats() IndexStats {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var stats IndexStats

	// Get BM25 stats (has term info)
	if h.bm25 != nil {
		bm25Stats := h.bm25.Stats()
		stats.DocumentCount = bm25Stats.DocumentCount
		stats.TermCount = bm25Stats.TermCount
		stats.AvgDocLength = bm25Stats.AvgDocLength
	}

	// Get Vector stats (only document count is meaningful)
	if h.vector != nil {
		vectorStats := h.vector.Stats()
		// Use max document count (should be equal if consistent)
		if vectorStats.DocumentCount > stats.DocumentCount {
			stats.DocumentCount = vectorStats.DocumentCount
		}
	}

	return stats
}

// Close releases resources from both indexers.
//
// Both indexers are closed even if one fails. Errors are accumulated
// and returned as a joined error.
//
// This method is idempotent; calling it multiple times is safe.
//
// This method is thread-safe.
func (h *HybridIndexer) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return nil
	}

	h.closed = true

	var errs []error

	// Close BM25
	if h.bm25 != nil {
		if err := h.bm25.Close(); err != nil {
			errs = append(errs, fmt.Errorf("hybrid bm25 close: %w", err))
		}
	}

	// Close Vector
	if h.vector != nil {
		if err := h.vector.Close(); err != nil {
			errs = append(errs, fmt.Errorf("hybrid vector close: %w", err))
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

// Ensure HybridIndexer implements Indexer at compile time.
var _ Indexer = (*HybridIndexer)(nil)
