//go:build debug

package store

import (
	"context"
	"fmt"
	"math"
	"os"
	"testing"
)

func TestDebugVectorSearch(t *testing.T) {
	if os.Getenv("DEBUG_VECTOR") != "1" {
		t.Skip("Skipping debug test (set DEBUG_VECTOR=1 to run)")
	}

	ctx := context.Background()

	// Use DEBUG_DATA_DIR env var or default to current directory's .amanmcp
	dataDir := os.Getenv("DEBUG_DATA_DIR")
	if dataDir == "" {
		dataDir = ".amanmcp"
	}

	// Check vector store dimensions
	vectorPath := dataDir + "/vectors.hnsw"
	dims, err := ReadHNSWStoreDimensions(vectorPath)
	if err != nil {
		t.Fatalf("Failed to read dimensions: %v", err)
	}
	fmt.Printf("Vector store dimensions: %d\n", dims)

	// Load vector store
	vectorConfig := DefaultVectorStoreConfig(dims)
	vector, err := NewHNSWStore(vectorConfig)
	if err != nil {
		t.Fatalf("Failed to create vector store: %v", err)
	}
	defer vector.Close()

	if err := vector.Load(vectorPath); err != nil {
		t.Fatalf("Failed to load vectors: %v", err)
	}
	fmt.Printf("Loaded %d vectors\n", vector.Count())

	// Sample some vectors to check if they're valid
	fmt.Println("\n=== Sampling vectors ===")
	allIDs := vector.AllIDs()
	if len(allIDs) < 3 {
		t.Fatalf("Not enough vectors")
	}

	// Get embeddings for specific chunks
	ollamaChunk := "bbc4c8b482e6a781" // ollama.go chunk
	configChunk := "478716532dfe2419" // F02-configuration chunk

	// Check if we can retrieve vectors
	fmt.Printf("Checking vector retrieval capability...\n")

	// Let's check vector stats from the HNSW graph
	stats := vector.Stats()
	fmt.Printf("Vector store stats: %+v\n", stats)

	// Search with ollama.go chunk vector (if we could get it)
	// Since we can't directly get vectors, let's check similarity between results
	// by doing multiple searches

	// Search with different random vectors to see score distribution
	fmt.Println("\n=== Random vector similarity test ===")
	for i := 0; i < 3; i++ {
		queryVec := make([]float32, dims)
		for j := range queryVec {
			queryVec[j] = float32(i*1000+j) / float32(dims*1000)
		}
		// Normalize
		var norm float32
		for _, v := range queryVec {
			norm += v * v
		}
		norm = float32(math.Sqrt(float64(norm)))
		for j := range queryVec {
			queryVec[j] /= norm
		}

		results, _ := vector.Search(ctx, queryVec, 3)
		fmt.Printf("Random vector %d: top scores = %.4f, %.4f, %.4f\n",
			i+1, results[0].Score, results[1].Score, results[2].Score)
	}

	// Search for known IDs
	fmt.Println("\n=== Checking specific chunks ===")
	for _, id := range []string{ollamaChunk, configChunk} {
		found := false
		for _, existID := range allIDs {
			if existID == id {
				found = true
				break
			}
		}
		fmt.Printf("  Chunk %s in store: %v\n", id, found)
	}
}
