// Package indexer provides modular indexing components for AmanMCP.
//
// This package follows Black Box Design principles (Eskil Steenberg):
//   - Clean interfaces that hide implementation details
//   - Replaceable components (swap backends without code changes)
//   - Single responsibility per module
//
// # Architecture
//
// The indexer package separates indexing concerns from the search engine:
//
//	┌─────────────────┐
//	│  Search Engine  │  (orchestrates search)
//	└────────┬────────┘
//	         │
//	┌────────▼────────┐
//	│    Indexer      │  ← This package
//	│   (interface)   │
//	└────────┬────────┘
//	         │
//	    ┌────┴────┐
//	    │         │
//	┌───▼───┐ ┌───▼───┐
//	│ BM25  │ │Vector │   (future: FEAT-BB3)
//	└───────┘ └───────┘
//
// # Usage
//
// Create a BM25 indexer:
//
//	store, _ := store.NewBM25IndexWithBackend(path, config, "sqlite")
//	indexer, err := indexer.NewBM25Indexer(indexer.WithStore(store))
//	if err != nil {
//	    return err
//	}
//	defer indexer.Close()
//
//	// Index chunks
//	err = indexer.Index(ctx, chunks)
//
// # Thread Safety
//
// All Indexer implementations are safe for concurrent use.
// Multiple goroutines may call Index, Delete, etc. simultaneously.
//
// # Related
//
//   - FEAT-BB2: BM25Indexer module extraction
//   - FEAT-BB3: VectorIndexer module extraction (future)
//   - FEAT-BB4: HybridIndexer composition (future)
package indexer
