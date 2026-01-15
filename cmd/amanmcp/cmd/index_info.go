package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/embed"
	"github.com/Aman-CERP/amanmcp/internal/store"
)

func newIndexInfoCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "info [path]",
		Short: "Show index configuration and statistics",
		Long: `Display detailed information about the search index including embedding
model, dimensions, chunk counts, and file sizes.

This command helps you:
- Check which model the current index uses
- Debug dimension mismatch errors
- Verify index was built correctly after reindex
- Compare index configurations across projects`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "."
			if len(args) > 0 {
				path = args[0]
			}

			return runIndexInfo(cmd.Context(), cmd, path, jsonOutput)
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	return cmd
}

func runIndexInfo(ctx context.Context, cmd *cobra.Command, path string, jsonOutput bool) error {
	// Resolve path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	root, err := config.FindProjectRoot(absPath)
	if err != nil {
		root = absPath
	}

	dataDir := filepath.Join(root, ".amanmcp")
	metadataPath := filepath.Join(dataDir, "metadata.db")

	// Check if index exists
	if _, err := os.Stat(metadataPath); os.IsNotExist(err) {
		return fmt.Errorf("no index found at %s\nRun 'amanmcp index %s' to create one", dataDir, path)
	}

	// Open metadata store
	metadata, err := store.NewSQLiteStore(metadataPath)
	if err != nil {
		return fmt.Errorf("failed to open metadata: %w", err)
	}
	defer metadata.Close()

	// Get current embedder info for compatibility checking
	cfg, err := config.Load(root)
	if err != nil {
		cfg = config.NewConfig()
	}

	// Apply config to embedder factory
	embed.SetMLXConfig(embed.MLXServerConfig{
		Endpoint: cfg.Embeddings.MLXEndpoint,
		Model:    cfg.Embeddings.MLXModel,
	})

	// Create embedder to get current config
	provider := embed.ParseProvider(cfg.Embeddings.Provider)
	embedder, err := embed.NewEmbedder(ctx, provider, cfg.Embeddings.Model)
	var embedderInput *store.EmbedderInfoInput
	if err == nil {
		embedInfo := embed.GetInfo(ctx, embedder)
		embedderInput = &store.EmbedderInfoInput{
			Model:      embedInfo.Model,
			Backend:    string(embedInfo.Provider),
			Dimensions: embedInfo.Dimensions,
		}
		embedder.Close()
	}

	// Get index info
	info, err := store.GetIndexInfo(ctx, metadata, dataDir, embedderInput)
	if err != nil {
		return fmt.Errorf("failed to get index info: %w", err)
	}

	// Output
	if jsonOutput {
		return outputIndexInfoJSON(cmd, info)
	}
	return outputIndexInfoHuman(cmd, info)
}

func outputIndexInfoJSON(cmd *cobra.Command, info *store.IndexInfo) error {
	output := map[string]interface{}{
		"location": info.Location,
		"project":  info.ProjectRoot,
		"embedding": map[string]interface{}{
			"model":      info.IndexModel,
			"backend":    info.IndexBackend,
			"dimensions": info.IndexDimensions,
		},
		"statistics": map[string]interface{}{
			"chunks":           info.ChunkCount,
			"documents":        info.DocumentCount,
			"index_size_bytes": info.IndexSizeBytes,
			"bm25_size_bytes":  info.BM25SizeBytes,
			"vector_size_bytes": info.VectorSizeBytes,
		},
		"timestamps": map[string]interface{}{
			"created":     info.CreatedAt,
			"last_update": info.UpdatedAt,
		},
		"current_embedder": map[string]interface{}{
			"model":      info.CurrentModel,
			"backend":    info.CurrentBackend,
			"dimensions": info.CurrentDimensions,
			"compatible": info.Compatible,
		},
	}

	encoder := json.NewEncoder(cmd.OutOrStdout())
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}

func outputIndexInfoHuman(cmd *cobra.Command, info *store.IndexInfo) error {
	out := cmd.OutOrStdout()

	fmt.Fprintln(out, "Index Information")
	fmt.Fprintln(out, "=================")
	fmt.Fprintln(out)

	fmt.Fprintf(out, "Location:    %s\n", info.Location)
	fmt.Fprintf(out, "Project:     %s\n", info.ProjectRoot)
	fmt.Fprintln(out)

	fmt.Fprintln(out, "Embedding Configuration:")
	if info.IndexModel != "" {
		fmt.Fprintf(out, "  Model:       %s\n", info.IndexModel)
		fmt.Fprintf(out, "  Backend:     %s\n", info.IndexBackend)
		fmt.Fprintf(out, "  Dimensions:  %d\n", info.IndexDimensions)
	} else {
		fmt.Fprintln(out, "  (not stored - legacy index)")
	}
	fmt.Fprintln(out)

	fmt.Fprintln(out, "Index Statistics:")
	fmt.Fprintf(out, "  Chunks:      %d\n", info.ChunkCount)
	fmt.Fprintf(out, "  Documents:   %d\n", info.DocumentCount)
	fmt.Fprintf(out, "  Index Size:  %s\n", store.FormatBytes(info.IndexSizeBytes))
	fmt.Fprintf(out, "  BM25 Size:   %s\n", store.FormatBytes(info.BM25SizeBytes))
	fmt.Fprintf(out, "  Vector Size: %s\n", store.FormatBytes(info.VectorSizeBytes))
	fmt.Fprintln(out)

	fmt.Fprintln(out, "Timestamps:")
	fmt.Fprintf(out, "  Created:     %s\n", store.FormatTime(info.CreatedAt))
	fmt.Fprintf(out, "  Last Update: %s\n", store.FormatTime(info.UpdatedAt))
	fmt.Fprintln(out)

	if info.CurrentModel != "" {
		fmt.Fprintln(out, "Current Embedder:")
		fmt.Fprintf(out, "  Model:       %s\n", info.CurrentModel)
		fmt.Fprintf(out, "  Backend:     %s\n", info.CurrentBackend)
		fmt.Fprintf(out, "  Dimensions:  %d\n", info.CurrentDimensions)

		if info.Compatible {
			fmt.Fprintln(out, "  Status:      Compatible")
		} else {
			fmt.Fprintln(out, "  Status:      INCOMPATIBLE")
			fmt.Fprintln(out)
			fmt.Fprintln(out, "  Dimension mismatch detected!")
			fmt.Fprintf(out, "    Index: %d dims (%s)\n", info.IndexDimensions, info.IndexModel)
			fmt.Fprintf(out, "    Current: %d dims (%s)\n", info.CurrentDimensions, info.CurrentModel)
			fmt.Fprintln(out)
			fmt.Fprintln(out, "    Semantic search will be disabled until reindex.")
			fmt.Fprintf(out, "    Run 'amanmcp reindex --force' to rebuild with %s.\n", info.CurrentModel)
		}
	}

	return nil
}
