//go:build debug

package search

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/embed"
	"github.com/Aman-CERP/amanmcp/internal/store"
)

func TestDebugFullSearchFlow(t *testing.T) {
	if os.Getenv("DEBUG_SEARCH") != "1" {
		t.Skip("Skipping debug test (set DEBUG_SEARCH=1 to run)")
	}

	ctx := context.Background()
	// Use DEBUG_DATA_DIR env var or default to current directory's .amanmcp
	dataDir := os.Getenv("DEBUG_DATA_DIR")
	if dataDir == "" {
		dataDir = ".amanmcp"
	}

	// Open all stores
	metadata, err := store.NewSQLiteStore(filepath.Join(dataDir, "metadata.db"))
	if err != nil {
		t.Fatalf("Failed to open metadata: %v", err)
	}
	defer metadata.Close()

	bm25BasePath := filepath.Join(dataDir, "bm25")
	bm25, err := store.NewBM25IndexWithBackend(bm25BasePath, store.DefaultBM25Config(), "")
	if err != nil {
		t.Fatalf("Failed to open BM25: %v", err)
	}
	defer bm25.Close()

	vectorConfig := store.DefaultVectorStoreConfig(768)
	vector, err := store.NewHNSWStore(vectorConfig)
	if err != nil {
		t.Fatalf("Failed to create vector store: %v", err)
	}
	defer vector.Close()

	vectorPath := filepath.Join(dataDir, "vectors.hnsw")
	if err := vector.Load(vectorPath); err != nil {
		t.Logf("Warning: Could not load vectors: %v", err)
	}

	embedder := embed.NewStaticEmbedder768()

	// Create search engine with BM25-only weights
	engineConfig := DefaultConfig()
	engineConfig.DefaultWeights = Weights{
		BM25:     1.0,
		Semantic: 0.0,
	}
	engine := New(bm25, vector, embedder, metadata, engineConfig)

	// Run search
	fmt.Println("\n=== Testing Full Search Flow ===")
	fmt.Println("Query: OllamaEmbedder")
	fmt.Printf("Weights: BM25=%.2f, Semantic=%.2f\n", engineConfig.DefaultWeights.BM25, engineConfig.DefaultWeights.Semantic)

	results, err := engine.Search(ctx, "OllamaEmbedder", SearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	fmt.Printf("\n=== Search Results (%d) ===\n", len(results))
	for i, r := range results {
		filePath := "unknown"
		if r.Chunk != nil {
			filePath = r.Chunk.FilePath
		}
		fmt.Printf("%d. File=%s Score=%.4f BM25=%.4f Vec=%.4f InBoth=%v\n",
			i+1, filePath, r.Score, r.BM25Score, r.VecScore, r.InBothLists)
	}

	// Also directly check BM25 results
	fmt.Println("\n=== Direct BM25 Results ===")
	bm25Results, err := bm25.Search(ctx, "OllamaEmbedder", 10)
	if err != nil {
		t.Fatalf("BM25 search failed: %v", err)
	}
	for i, r := range bm25Results {
		// Look up file path from metadata
		chunks, _ := metadata.GetChunks(ctx, []string{r.DocID})
		filePath := "not_found"
		if len(chunks) > 0 {
			filePath = chunks[0].FilePath
		}
		fmt.Printf("%d. ID=%s File=%s Score=%.4f\n", i+1, r.DocID, filePath, r.Score)
	}
}

// TestDebugCLIPath mimics the exact CLI search path
func TestDebugCLIPath(t *testing.T) {
	if os.Getenv("DEBUG_CLI") != "1" {
		t.Skip("Skipping CLI debug test (set DEBUG_CLI=1 to run)")
	}

	ctx := context.Background()
	// Use DEBUG_ROOT env var or default to current directory
	root := os.Getenv("DEBUG_ROOT")
	if root == "" {
		root = "."
	}
	dataDir := filepath.Join(root, ".amanmcp")

	// Load configuration (exactly like CLI does)
	cfg, err := config.Load(root)
	if err != nil {
		cfg = config.NewConfig()
	}
	fmt.Printf("Loaded config: BM25Weight=%.2f, SemanticWeight=%.2f\n",
		cfg.Search.BM25Weight, cfg.Search.SemanticWeight)

	// Initialize stores (exactly like CLI does)
	metadataPath := filepath.Join(dataDir, "metadata.db")
	metadata, err := store.NewSQLiteStore(metadataPath)
	if err != nil {
		t.Fatalf("Failed to open metadata: %v", err)
	}
	defer func() { _ = metadata.Close() }()

	bm25BasePath := filepath.Join(dataDir, "bm25")
	bm25Config := store.DefaultBM25Config()
	bm25, err := store.NewBM25IndexWithBackend(bm25BasePath, bm25Config, "")
	if err != nil {
		t.Fatalf("Failed to open BM25 index: %v", err)
	}
	defer func() { _ = bm25.Close() }()

	// Check existing vector store dimensions
	vectorPath := filepath.Join(dataDir, "vectors.hnsw")
	existingDims, err := store.ReadHNSWStoreDimensions(vectorPath)
	if err != nil {
		fmt.Printf("Could not read vector dimensions: %v\n", err)
		existingDims = 0
	} else {
		fmt.Printf("Existing vector dimensions: %d\n", existingDims)
	}

	// Wire MLX config from config.yaml to embedder factory
	embed.SetMLXConfig(embed.MLXServerConfig{
		Endpoint: cfg.Embeddings.MLXEndpoint,
		Model:    cfg.Embeddings.MLXModel,
	})

	// Use config-based embedder selection (same as index command) - fixes BUG-039
	provider := embed.ParseProvider(cfg.Embeddings.Provider)
	embedder, err := embed.NewEmbedder(ctx, provider, cfg.Embeddings.Model)
	if err != nil {
		t.Fatalf("Failed to create embedder: %v", err)
	}
	fmt.Printf("Embedder: provider=%s, model=%s, dims=%d\n",
		provider.String(), embedder.ModelName(), embedder.Dimensions())
	defer func() { _ = embedder.Close() }()

	// Use embedder dimensions for vector store
	dimensions := embedder.Dimensions()
	vectorConfig := store.DefaultVectorStoreConfig(dimensions)
	vector, err := store.NewHNSWStore(vectorConfig)
	if err != nil {
		t.Fatalf("Failed to create vector store: %v", err)
	}
	defer func() { _ = vector.Close() }()

	// Try to load vectors
	if _, err := os.Stat(vectorPath); err == nil {
		if loadErr := vector.Load(vectorPath); loadErr != nil {
			fmt.Printf("Vector load failed: %v\n", loadErr)
		} else {
			fmt.Printf("Vectors loaded: count=%d\n", vector.Count())
		}
	}

	// Create search engine with defaults
	engineConfig := DefaultConfig()
	if cfg.Search.MaxResults > 0 {
		engineConfig.DefaultLimit = cfg.Search.MaxResults
	}
	if cfg.Search.BM25Weight > 0 || cfg.Search.SemanticWeight > 0 {
		engineConfig.DefaultWeights = Weights{
			BM25:     cfg.Search.BM25Weight,
			Semantic: cfg.Search.SemanticWeight,
		}
	}
	fmt.Printf("Engine config: DefaultLimit=%d, BM25=%.2f, Semantic=%.2f\n",
		engineConfig.DefaultLimit, engineConfig.DefaultWeights.BM25, engineConfig.DefaultWeights.Semantic)

	engine := New(bm25, vector, embedder, metadata, engineConfig)

	// Build search options
	searchOpts := SearchOptions{
		Limit:  10,
		Filter: "all",
	}

	// Execute search
	fmt.Println("\n=== Search for 'OllamaEmbedder' ===")
	results, err := engine.Search(ctx, "OllamaEmbedder", searchOpts)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	fmt.Printf("Results: %d\n", len(results))
	for i, r := range results {
		filePath := "unknown"
		if r.Chunk != nil {
			filePath = r.Chunk.FilePath
		}
		fmt.Printf("%d. File=%s Score=%.4f BM25=%.4f Vec=%.4f InBoth=%v\n",
			i+1, filePath, r.Score, r.BM25Score, r.VecScore, r.InBothLists)
	}

	// Also check direct BM25
	fmt.Println("\n=== Direct BM25 ===")
	bm25Results, _ := bm25.Search(ctx, "OllamaEmbedder", 5)
	for i, r := range bm25Results {
		chunks, _ := metadata.GetChunks(ctx, []string{r.DocID})
		filePath := "not_found"
		if len(chunks) > 0 {
			filePath = chunks[0].FilePath
		}
		fmt.Printf("%d. ID=%s File=%s Score=%.4f\n", i+1, r.DocID, filePath, r.Score)
	}
}
