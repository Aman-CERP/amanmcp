package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/store"
	"github.com/Aman-CERP/amanmcp/internal/ui"
)

// hashString returns SHA256 hash of a string (first 16 chars).
func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])[:16]
}

func newStatusCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show index health and status",
		Long: `Display information about the current index including:
  - Number of indexed files and chunks
  - Last indexing time
  - Storage sizes (metadata, BM25, vectors)
  - Embedder status (type, model, availability)
  - Watcher status (if running)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(cmd.Context(), cmd, jsonOutput)
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")

	return cmd
}

func runStatus(ctx context.Context, cmd *cobra.Command, jsonOutput bool) error {
	// Find project root
	root, err := config.FindProjectRoot(".")
	if err != nil {
		cwd, _ := os.Getwd()
		root = cwd
	}

	dataDir := filepath.Join(root, ".amanmcp")

	// Check if index exists
	metadataPath := filepath.Join(dataDir, "metadata.db")
	if !fileExists(metadataPath) {
		return fmt.Errorf("no index found in %s\nRun 'amanmcp index' to create one", root)
	}

	// Collect status info
	info, err := collectStatus(ctx, root, dataDir)
	if err != nil {
		return fmt.Errorf("failed to collect status: %w", err)
	}

	// Render output
	noColor := ui.DetectNoColor()
	renderer := ui.NewStatusRenderer(cmd.OutOrStdout(), noColor)

	if jsonOutput {
		return renderer.RenderJSON(info)
	}

	return renderer.Render(info)
}

func collectStatus(ctx context.Context, root, dataDir string) (ui.StatusInfo, error) {
	info := ui.StatusInfo{
		ProjectName: filepath.Base(root),
	}

	// Open metadata store
	metadataPath := filepath.Join(dataDir, "metadata.db")
	metadata, err := store.NewSQLiteStore(metadataPath)
	if err != nil {
		return info, fmt.Errorf("failed to open metadata store: %w", err)
	}
	defer func() { _ = metadata.Close() }()

	// Get project info
	projectID := hashString(root)
	project, err := metadata.GetProject(ctx, projectID)
	if err != nil {
		// Project not found is not fatal
		project = nil
	}

	if project != nil {
		info.TotalFiles = project.FileCount
		info.TotalChunks = project.ChunkCount
		info.LastIndexed = project.IndexedAt
	}

	// Get storage sizes
	info.MetadataSize = getFileSize(metadataPath)

	// Check both BM25 backends for size calculation
	bm25SQLitePath := filepath.Join(dataDir, "bm25.db")
	bm25BlevePath := filepath.Join(dataDir, "bm25.bleve")
	if size := getFileSize(bm25SQLitePath); size > 0 {
		info.BM25Size = size
	} else {
		info.BM25Size = getDirSize(bm25BlevePath)
	}

	vectorPath := filepath.Join(dataDir, "vectors.hnsw")
	info.VectorSize = getFileSize(vectorPath)

	info.TotalSize = info.MetadataSize + info.BM25Size + info.VectorSize

	// Detect embedder type
	cfg, err := config.Load(root)
	if err != nil {
		cfg = config.NewConfig()
	}

	info.EmbedderType = cfg.Embeddings.Provider
	if info.EmbedderType == "" {
		info.EmbedderType = "hugot" // Default
	}

	// Check embedder status
	info.EmbedderStatus = "ready"
	info.EmbedderModel = cfg.Embeddings.Model
	if info.EmbedderModel == "" {
		info.EmbedderModel = "embeddinggemma" // Default for hugot
	}

	// Watcher status - check if watcher process is running
	// For now, we don't have a way to check if watcher is running
	// So we'll just report "n/a"
	info.WatcherStatus = "n/a"

	return info, nil
}

// getFileSize returns the size of a file in bytes.
func getFileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// getDirSize returns the total size of all files in a directory.
func getDirSize(path string) int64 {
	var size int64

	_ = filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})

	return size
}
