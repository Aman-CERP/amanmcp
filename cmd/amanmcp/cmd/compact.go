package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/logging"
	"github.com/Aman-CERP/amanmcp/internal/store"
)

func newCompactCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "compact [path]",
		Short: "Compact the vector index by removing orphaned nodes",
		Long: `Rebuilds the HNSW vector index from stored embeddings.

This reclaims memory from orphaned nodes created by lazy deletion during
file updates. The command uses embeddings stored in SQLite, so no
re-embedding is required (zero Ollama API calls).

Note: Only indexes created after v0.1.43 have stored embeddings.
Older indexes will show an error and need to be rebuilt with 'amanmcp index'.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "."
			if len(args) > 0 {
				path = args[0]
			}
			return runCompact(cmd.Context(), path)
		},
	}

	return cmd
}

func runCompact(ctx context.Context, path string) error {
	// Initialize logging for CLI observability
	logCfg := logging.DefaultConfig()
	logCfg.WriteToStderr = false
	if _, cleanup, err := logging.Setup(logCfg); err == nil {
		defer cleanup()
	}

	// Validate path exists
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("failed to access path: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", absPath)
	}

	// Find project root
	root, err := config.FindProjectRoot(absPath)
	if err != nil {
		root = absPath
	}

	dataDir := filepath.Join(root, ".amanmcp")

	// Check if index exists
	metadataPath := filepath.Join(dataDir, "metadata.db")
	if !fileExists(metadataPath) {
		return fmt.Errorf("no index found at %s - run 'amanmcp index' first", dataDir)
	}

	vectorPath := filepath.Join(dataDir, "vectors.hnsw")
	if !fileExists(vectorPath) {
		return fmt.Errorf("no vector index found at %s - run 'amanmcp index' first", vectorPath)
	}

	fmt.Println("Compacting vector index...")
	startTime := time.Now()

	// Open metadata store to get embeddings
	metadata, err := store.NewSQLiteStore(metadataPath)
	if err != nil {
		return fmt.Errorf("failed to open metadata store: %w", err)
	}
	defer func() { _ = metadata.Close() }()

	// Check embedding stats
	withEmb, withoutEmb, err := metadata.GetEmbeddingStats(ctx)
	if err != nil {
		return fmt.Errorf("failed to get embedding stats: %w", err)
	}

	totalChunks := withEmb + withoutEmb
	if withEmb == 0 {
		return fmt.Errorf("no stored embeddings found (%d chunks without embeddings)\n"+
			"This index was created before embedding storage was added.\n"+
			"Run 'amanmcp index --reindex' to rebuild with stored embeddings", totalChunks)
	}

	if withoutEmb > 0 {
		fmt.Printf("Warning: %d of %d chunks have no stored embeddings\n", withoutEmb, totalChunks)
		fmt.Printf("These chunks will be excluded from the compacted index.\n")
		fmt.Printf("Run 'amanmcp index --reindex' to include all chunks.\n\n")
	}

	// Load all embeddings from SQLite
	fmt.Printf("Loading %d embeddings from SQLite...\n", withEmb)
	embeddings, err := metadata.GetAllEmbeddings(ctx)
	if err != nil {
		return fmt.Errorf("failed to load embeddings: %w", err)
	}

	if len(embeddings) == 0 {
		return fmt.Errorf("no embeddings retrieved - index may be corrupted")
	}

	// Determine embedding dimensions from first entry
	var dims int
	for _, emb := range embeddings {
		dims = len(emb)
		break
	}

	fmt.Printf("Creating fresh HNSW graph (dims=%d)...\n", dims)

	// Create fresh HNSW store
	vectorCfg := store.DefaultVectorStoreConfig(dims)
	newVector, err := store.NewHNSWStore(vectorCfg)
	if err != nil {
		return fmt.Errorf("failed to create vector store: %w", err)
	}
	defer func() { _ = newVector.Close() }()

	// Prepare batch add
	ids := make([]string, 0, len(embeddings))
	vecs := make([][]float32, 0, len(embeddings))
	for id, vec := range embeddings {
		ids = append(ids, id)
		vecs = append(vecs, vec)
	}

	// Add all embeddings to new store
	fmt.Printf("Adding %d vectors to new graph...\n", len(ids))
	if err := newVector.Add(ctx, ids, vecs); err != nil {
		return fmt.Errorf("failed to add vectors: %w", err)
	}

	// Get old store stats for comparison
	oldVector, err := store.NewHNSWStore(vectorCfg)
	if err != nil {
		slog.Warn("failed to open old vector store for comparison", slog.String("error", err.Error()))
	} else {
		if err := oldVector.Load(vectorPath); err != nil {
			slog.Warn("failed to load old vector store for comparison", slog.String("error", err.Error()))
		} else {
			oldCount := oldVector.Count()
			newCount := newVector.Count()
			orphansRemoved := oldCount - newCount
			if orphansRemoved > 0 {
				fmt.Printf("Orphaned nodes removed: %d\n", orphansRemoved)
			}
		}
		_ = oldVector.Close()
	}

	// Save new vector store (overwrites old)
	fmt.Println("Saving compacted index...")
	if err := newVector.Save(vectorPath); err != nil {
		return fmt.Errorf("failed to save vector store: %w", err)
	}

	elapsed := time.Since(startTime)
	fmt.Printf("Compaction complete in %v\n", elapsed.Round(time.Millisecond))
	fmt.Printf("Vector count: %d\n", newVector.Count())

	return nil
}
