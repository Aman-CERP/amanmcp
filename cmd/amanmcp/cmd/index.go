package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/embed"
	"github.com/Aman-CERP/amanmcp/internal/index"
	"github.com/Aman-CERP/amanmcp/internal/logging"
	"github.com/Aman-CERP/amanmcp/internal/store"
	"github.com/Aman-CERP/amanmcp/internal/ui"
)

func newIndexCmd() *cobra.Command {
	var (
		noTUI   bool
		resume  bool
		force   bool
		backend string
	)

	cmd := &cobra.Command{
		Use:   "index [path]",
		Short: "Index a directory for searching",
		Long: `Index a directory to enable hybrid search over its contents.

This scans files, chunks code and documents, generates embeddings,
and builds both BM25 and vector indices for fast retrieval.

Backend Selection:
  (default)          Auto-detect: MLX on Apple Silicon, Ollama otherwise
  --backend=mlx      Use MLX (Apple Silicon, ~1.7x faster)
  --backend=ollama   Use Ollama (cross-platform)

Use --resume to continue from a previous interrupted indexing operation.
Use --force to clear existing index data and rebuild from scratch.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Set up signal handling for Ctrl+C - this ensures context cancellation
			// propagates properly so GPU operations stop when user interrupts
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			path := "."
			if len(args) > 0 {
				path = args[0]
			}

			// --force and --resume are mutually exclusive
			if force && resume {
				return fmt.Errorf("--force and --resume are mutually exclusive")
			}

			// Set backend via environment variable if flag provided
			// This ensures all downstream code respects the choice
			if backend != "" {
				os.Setenv("AMANMCP_EMBEDDER", backend)
			}

			return runIndexWithResume(ctx, cmd, path, false, noTUI, resume, force)
		},
	}

	cmd.Flags().BoolVar(&noTUI, "no-tui", false, "Disable TUI mode, use plain text output")
	cmd.Flags().BoolVar(&resume, "resume", false, "Resume from previous checkpoint if available")
	cmd.Flags().BoolVar(&force, "force", false, "Clear existing index and rebuild from scratch")
	cmd.Flags().StringVar(&backend, "backend", "", "Embedding backend: auto-detect (default), mlx, ollama, or static")

	// Add subcommands
	cmd.AddCommand(newIndexInfoCmd())

	return cmd
}

func runIndexWithResume(ctx context.Context, cmd *cobra.Command, path string, offline bool, noTUI bool, resume bool, force bool) error {
	// Check for existing checkpoint before starting
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

	// Handle --force: clear all index data before proceeding
	if force {
		if err := clearIndexData(dataDir); err != nil {
			return fmt.Errorf("failed to clear index data: %w", err)
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Cleared existing index data, starting fresh...\n")
		slog.Info("index_force_clear", slog.String("data_dir", dataDir))
		return runIndexWithOptions(ctx, cmd, path, offline, noTUI, 0, "")
	}

	// resumeFromChunk tracks how many chunks to skip when resuming
	resumeFromChunk := 0

	// Check context before database operations - allows Ctrl+C to work
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Check for checkpoint if metadata exists
	// Use short timeout to prevent indefinite blocking when serve is running
	if _, err := os.Stat(metadataPath); err == nil {
		loadCtx, loadCancel := context.WithTimeout(ctx, 3*time.Second)
		defer loadCancel() // Ensure cancel is always called

		metadata, err := store.NewSQLiteStore(metadataPath)
		if err == nil {
			checkpoint, loadErr := metadata.LoadIndexCheckpoint(loadCtx)

			// If checkpoint load timed out, warn but continue (will reindex)
			if loadErr != nil {
				slog.Warn("checkpoint_load_timeout", slog.String("error", loadErr.Error()))
			}

			if checkpoint != nil {
				if resume {
					// BUG-052: Check chunk ID version before resuming
					// Legacy position-based indexes cannot reliably resume after file modifications
					chunkIDVersion, _ := metadata.GetState(loadCtx, store.StateKeyChunkIDVersion)
					if chunkIDVersion != "" && chunkIDVersion != store.ChunkIDVersionContent {
						_ = metadata.Close()
						_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
							"Warning: Index uses legacy position-based chunk IDs (version %s).\n"+
								"These cannot reliably resume if files were modified.\n"+
								"Use --force to rebuild with content-addressable IDs.\n",
							chunkIDVersion)
						return fmt.Errorf("legacy chunk ID version detected, use --force to rebuild")
					}

					// Will resume from checkpoint in runIndexWithOptions
					slog.Info("checkpoint_found",
						slog.String("stage", checkpoint.Stage),
						slog.Int("embedded", checkpoint.EmbeddedCount),
						slog.Int("total", checkpoint.Total))
					_, _ = fmt.Fprintf(cmd.OutOrStdout(),
						"Resuming from checkpoint: %d/%d chunks embedded\n",
						checkpoint.EmbeddedCount, checkpoint.Total)
					resumeFromChunk = checkpoint.EmbeddedCount
				} else {
					// Warn user about existing checkpoint
					pct := 0
					if checkpoint.Total > 0 {
						pct = checkpoint.EmbeddedCount * 100 / checkpoint.Total
					}
					_ = metadata.Close()
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
						"Warning: Previous indexing was incomplete (stopped at %d%%).\n"+
							"Use --resume to continue, or --force to start fresh.\n",
						pct)
					return fmt.Errorf("incomplete checkpoint found, use --resume to continue")
				}
			}
			_ = metadata.Close()
		}
	}

	// BUG-053: Pass checkpoint embedder model to validate on resume
	checkpointEmbedderModel := ""
	if resumeFromChunk > 0 {
		// Re-open metadata to get the checkpoint embedder model
		if metadata, err := store.NewSQLiteStore(metadataPath); err == nil {
			loadCtx, loadCancel := context.WithTimeout(ctx, 3*time.Second)
			if checkpoint, err := metadata.LoadIndexCheckpoint(loadCtx); err == nil && checkpoint != nil {
				checkpointEmbedderModel = checkpoint.EmbedderModel
			}
			loadCancel()
			_ = metadata.Close()
		}
	}

	return runIndexWithOptions(ctx, cmd, path, offline, noTUI, resumeFromChunk, checkpointEmbedderModel)
}

// clearIndexData removes all index-related files from the data directory.
// This preserves the .amanmcp.yaml config file (which is at project root, not in dataDir).
func clearIndexData(dataDir string) error {
	// Files/directories to remove
	indexFiles := []string{
		filepath.Join(dataDir, "metadata.db"),
		filepath.Join(dataDir, "metadata.db-shm"),  // SQLite WAL shared memory
		filepath.Join(dataDir, "metadata.db-wal"),  // SQLite WAL journal
		filepath.Join(dataDir, "bm25.bleve"),       // BM25 index directory (legacy Bleve)
		filepath.Join(dataDir, "bm25.db"),          // BM25 index file (SQLite FTS5)
		filepath.Join(dataDir, "bm25.db-wal"),      // SQLite WAL journal
		filepath.Join(dataDir, "bm25.db-shm"),      // SQLite shared memory
		filepath.Join(dataDir, "vectors.hnsw"),     // HNSW vector store
	}

	for _, path := range indexFiles {
		if err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove %s: %w", filepath.Base(path), err)
		}
	}

	return nil
}

func runIndexWithOptions(ctx context.Context, cmd *cobra.Command, path string, offline bool, noTUI bool, resumeFromCheckpoint int, checkpointEmbedderModel string) error {
	// Initialize logging for CLI observability (BUG-039)
	// Use file-only logging to avoid interfering with user-facing output
	logCfg := logging.DefaultConfig()
	logCfg.WriteToStderr = false
	if logger, cleanup, err := logging.Setup(logCfg); err == nil {
		slog.SetDefault(logger) // Set as default so slog.Info goes to file
		defer cleanup()
	}
	// Continue even if logging setup fails - not critical for CLI

	// Validate path exists first (needed for renderer header)
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

	// Find project root (may be different from path if path is subdirectory)
	root, err := config.FindProjectRoot(absPath)
	if err != nil {
		// Use the provided path as root if no project root found
		root = absPath
	}

	// Create renderer (auto-detects TTY/CI, respects --no-tui flag)
	// Pass project root path for header display
	uiCfg := ui.NewConfig(cmd.OutOrStdout(), ui.WithForcePlain(noTUI), ui.WithProjectDir(root))
	renderer := ui.NewRenderer(uiCfg)
	if err := renderer.Start(ctx); err != nil {
		// Fall back to basic output if renderer fails to start
		slog.Warn("failed to start progress renderer", slog.String("error", err.Error()))
	}
	defer func() { _ = renderer.Stop() }()

	// Load configuration
	cfg, err := config.Load(root)
	if err != nil {
		// Use default config if not found
		cfg = config.NewConfig()
	}

	// Create data directory
	dataDir := filepath.Join(root, ".amanmcp")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	// BUG-040: Clean up stale serve.pid if process no longer exists
	servePidPath := filepath.Join(dataDir, "serve.pid")
	if pidData, err := os.ReadFile(servePidPath); err == nil {
		var pid int
		if _, scanErr := fmt.Sscanf(string(pidData), "%d", &pid); scanErr == nil && pid > 0 {
			// Check if process exists by sending signal 0
			if process, findErr := os.FindProcess(pid); findErr == nil {
				if sigErr := process.Signal(syscall.Signal(0)); sigErr != nil {
					// Process doesn't exist, remove stale PID file
					_ = os.Remove(servePidPath)
					slog.Debug("removed stale serve.pid", slog.Int("pid", pid))
				}
			}
		}
	}

	// Initialize metadata store
	metadataPath := filepath.Join(dataDir, "metadata.db")
	metadata, err := store.NewSQLiteStore(metadataPath)
	if err != nil {
		return fmt.Errorf("failed to create metadata store: %w", err)
	}
	defer func() { _ = metadata.Close() }()

	// Initialize BM25 index using factory (SQLite default for concurrent access)
	bm25BasePath := filepath.Join(dataDir, "bm25")
	bm25, err := store.NewBM25IndexWithBackend(bm25BasePath, store.DefaultBM25Config(), cfg.Search.BM25Backend)
	if err != nil {
		return fmt.Errorf("failed to create BM25 index: %w", err)
	}
	defer func() { _ = bm25.Close() }()

	// Check context before potentially blocking embedder init
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// BUG-052: Wire thermal config from config.yaml to embedder factory
	// This ensures timeout_progression and retry_timeout_multiplier are used
	thermalCfg := embed.ThermalConfig{
		TimeoutProgression:     cfg.Embeddings.TimeoutProgression,
		RetryTimeoutMultiplier: cfg.Embeddings.RetryTimeoutMultiplier,
	}
	// Parse inter_batch_delay string (e.g., "200ms") to duration
	var interBatchDelay time.Duration
	if cfg.Embeddings.InterBatchDelay != "" {
		if delay, parseErr := time.ParseDuration(cfg.Embeddings.InterBatchDelay); parseErr == nil && delay > 0 {
			thermalCfg.InterBatchDelay = delay
			interBatchDelay = delay
		}
	}
	embed.SetThermalConfig(thermalCfg)

	// Wire MLX config from config.yaml to embedder factory
	// This ensures mlx_endpoint and mlx_model are used when MLX is selected
	embed.SetMLXConfig(embed.MLXServerConfig{
		Endpoint: cfg.Embeddings.MLXEndpoint,
		Model:    cfg.Embeddings.MLXModel,
	})

	// Initialize embedder first (to get correct dimensions)
	// BUG-040: Add timeout to prevent indefinite blocking on embedder init
	// BUG-073: No silent fallback - fail if embedder unavailable
	var embedder embed.Embedder
	if offline {
		embedder = embed.NewStaticEmbedder768()
	} else {
		provider := embed.ParseProvider(cfg.Embeddings.Provider)

		// Show progress with specific provider name
		renderer.UpdateProgress(ui.ProgressEvent{
			Stage:   ui.StageScanning,
			Message: fmt.Sprintf("Connecting to %s embedder...", provider),
		})

		// Use timeout context to prevent indefinite blocking (15s max for init)
		embedCtx, embedCancel := context.WithTimeout(ctx, 15*time.Second)
		embedder, err = embed.NewEmbedder(embedCtx, provider, cfg.Embeddings.Model)
		embedCancel()

		if err != nil {
			// BUG-073: No silent fallback - show clear error to user
			return fmt.Errorf("embedder initialization failed: %w", err)
		}
	}
	defer func() { _ = embedder.Close() }()

	// Initialize vector store with embedder's dimensions
	dimensions := embedder.Dimensions()
	vectorCfg := store.DefaultVectorStoreConfig(dimensions)
	vector, err := store.NewHNSWStore(vectorCfg)
	if err != nil {
		return fmt.Errorf("failed to create vector store: %w", err)
	}
	defer func() { _ = vector.Close() }()

	// Create Runner with injected dependencies
	runner, err := index.NewRunner(index.RunnerDependencies{
		Renderer: renderer,
		Config:   cfg,
		Metadata: metadata,
		BM25:     bm25,
		Vector:   vector,
		Embedder: embedder,
	})
	if err != nil {
		return fmt.Errorf("failed to create index runner: %w", err)
	}
	defer func() { _ = runner.Close() }()

	// Run indexing
	_, err = runner.Run(ctx, index.RunnerConfig{
		RootDir:              root,
		DataDir:              dataDir,
		Offline:              offline,
		ResumeFromCheckpoint: resumeFromCheckpoint,
		CheckpointModel:      checkpointEmbedderModel,
		InterBatchDelay:      interBatchDelay,
	})

	return err
}
