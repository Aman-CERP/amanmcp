//go:build debug

package store

import (
	"context"
	"fmt"
	"os"
	"testing"
)

func TestDebugBM25Index(t *testing.T) {
	if os.Getenv("DEBUG_BM25") != "1" {
		t.Skip("Skipping debug test (set DEBUG_BM25=1 to run)")
	}

	ctx := context.Background()
	// Use DEBUG_DATA_DIR env var or default to current directory's .amanmcp
	dataDir := os.Getenv("DEBUG_DATA_DIR")
	if dataDir == "" {
		dataDir = ".amanmcp"
	}
	bm25BasePath := dataDir + "/bm25"
	bm25, err := NewBM25IndexWithBackend(bm25BasePath, DefaultBM25Config(), "")
	if err != nil {
		t.Fatalf("Failed to open BM25: %v", err)
	}
	defer bm25.Close()

	stats := bm25.Stats()
	fmt.Printf("BM25 Doc Count: %d\n", stats.DocumentCount)

	// Search for OllamaEmbedder
	results, err := bm25.Search(ctx, "OllamaEmbedder", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	fmt.Printf("\nSearch for 'OllamaEmbedder': %d results\n", len(results))
	for i, r := range results {
		fmt.Printf("  %d. ID=%s Score=%.4f Terms=%v\n", i+1, r.DocID, r.Score, r.MatchedTerms)
	}

	// Check AllIDs to see if ollama.go chunks are there
	allIDs, err := bm25.AllIDs()
	if err != nil {
		t.Fatalf("AllIDs failed: %v", err)
	}
	fmt.Printf("\nTotal docs in BM25: %d\n", len(allIDs))

	// Look for known ollama.go chunk IDs
	knownIDs := []string{
		"bbc4c8b482e6a781",
		"423ea91cf8b42030",
	}
	for _, known := range knownIDs {
		found := false
		for _, id := range allIDs {
			if id == known {
				found = true
				break
			}
		}
		fmt.Printf("  Chunk %s in index: %v\n", known, found)
	}

	// Check F02-configuration chunks - what do they contain?
	f02Chunks := []string{"478716532dfe2419", "a4f754ec61680b33"}
	fmt.Printf("\nF02-configuration chunk search:\n")
	for _, id := range f02Chunks {
		for _, allID := range allIDs {
			if id == allID {
				fmt.Printf("  Chunk %s exists in BM25 index\n", id)
			}
		}
	}
}
