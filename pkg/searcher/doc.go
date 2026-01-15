// Package searcher provides modular search components for hybrid code search.
//
// This package implements the Searcher interface with multiple implementations:
//
//   - [BM25Searcher]: Lexical search using BM25 algorithm
//   - [VectorSearcher]: Semantic search using embeddings
//   - [FusionSearcher]: Hybrid search combining BM25 and Vector with RRF
//
// # Architecture
//
// The package follows Black Box Design principles, allowing each component
// to be tested and replaced independently:
//
//	┌─────────────────────────────────────────────────────────────┐
//	│                      FusionSearcher                         │
//	│  ┌─────────────────┐              ┌─────────────────┐      │
//	│  │  BM25Searcher   │──────────────│ VectorSearcher  │      │
//	│  │                 │   RRF Fusion │                 │      │
//	│  │  store.BM25Index│              │ embed.Embedder  │      │
//	│  │                 │              │ store.VectorStore│      │
//	│  └─────────────────┘              └─────────────────┘      │
//	└─────────────────────────────────────────────────────────────┘
//
// # Usage
//
// Basic usage with all components:
//
//	// Create individual searchers
//	bm25, _ := searcher.NewBM25Searcher(
//	    searcher.WithBM25Store(bm25Index),
//	)
//	vector, _ := searcher.NewVectorSearcher(
//	    searcher.WithEmbedder(embedder),
//	    searcher.WithVectorStore(vectorStore),
//	)
//
//	// Create fusion searcher
//	fusion, _ := searcher.NewFusionSearcher(
//	    searcher.WithBM25(bm25),
//	    searcher.WithVector(vector),
//	    searcher.WithFusionConfig(searcher.FusionConfig{
//	        BM25Weight:     0.35,
//	        SemanticWeight: 0.65,
//	        RRFConstant:    60,
//	    }),
//	)
//
//	// Search
//	results, err := fusion.Search(ctx, "How does RRF fusion work", 10)
//
// # BM25-Only Mode
//
// For deployments without an embedder:
//
//	fusion, _ := searcher.NewFusionSearcher(
//	    searcher.WithBM25(bm25),
//	    // No vector searcher = BM25-only mode
//	)
//
// # Thread Safety
//
// All Searcher implementations are safe for concurrent use.
package searcher
