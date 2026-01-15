package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/Aman-CERP/amanmcp/internal/chunk"
	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/daemon"
	"github.com/Aman-CERP/amanmcp/internal/embed"
	"github.com/Aman-CERP/amanmcp/internal/index"
	"github.com/Aman-CERP/amanmcp/internal/logging"
	"github.com/Aman-CERP/amanmcp/internal/mcp"
	"github.com/Aman-CERP/amanmcp/internal/scanner"
	"github.com/Aman-CERP/amanmcp/internal/search"
	"github.com/Aman-CERP/amanmcp/internal/session"
	"github.com/Aman-CERP/amanmcp/internal/store"
	"github.com/Aman-CERP/amanmcp/internal/watcher"
	"github.com/Aman-CERP/amanmcp/pkg/version"
)

// verifyStdinForMCP checks if stdin is suitable for MCP stdio transport.
// Returns nil if stdin is a pipe (usable for MCP), error if terminal or unavailable.
// BUG-035: Helps diagnose "file already closed" connection issues.
func verifyStdinForMCP() error {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return fmt.Errorf("stdin unavailable: %w", err)
	}

	mode := stat.Mode()
	slog.Debug("stdin status",
		slog.String("mode", mode.String()),
		slog.Int64("size", stat.Size()),
		slog.Bool("is_pipe", (mode&os.ModeNamedPipe) != 0),
		slog.Bool("is_char_device", (mode&os.ModeCharDevice) != 0))

	// If stdin is a terminal (not a pipe), provide helpful error
	if (mode & os.ModeCharDevice) != 0 {
		return fmt.Errorf("stdin is a terminal, not a pipe. " +
			"For MCP mode, run via Claude Code or pipe input:\n" +
			"  echo '{\"jsonrpc\":\"2.0\",\"method\":\"initialize\",\"id\":1}' | amanmcp serve")
	}

	return nil
}

func newServeCmd() *cobra.Command {
	var transport string
	var port int
	var sessionName string
	var debug bool

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the MCP server",
		Long: `Start the AmanMCP MCP server for AI coding assistants.

The server communicates via JSON-RPC over stdio (default) and provides
hybrid search capabilities to connected clients like Claude Code and Cursor.

File watching is automatically enabled for real-time index updates.

Before running serve, you need to index your project:
  amanmcp index .

Named sessions allow you to quickly switch between projects:
  amanmcp serve --session=work-api

Debug mode enables verbose logging to ~/.amanmcp/logs/server.log:
  amanmcp serve --debug

View logs with the amanmcp-logs command:
  amanmcp-logs -f --level DEBUG

Example configuration (.mcp.json in project root):
  {
    "mcpServers": {
      "amanmcp": {
        "command": "amanmcp",
        "args": ["serve"],
        "cwd": "/path/to/project"
      }
    }
  }

Note: The cwd field is required for Claude Code to start the server in the correct directory.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Initialize logging if debug mode is enabled
			if debug {
				cleanup, err := setupDebugLogging()
				if err != nil {
					return fmt.Errorf("failed to setup debug logging: %w", err)
				}
				defer cleanup()
				slog.Info("Debug logging enabled", slog.String("log_path", logging.DefaultLogPath()))
			}

			if sessionName != "" {
				root, err := config.FindProjectRoot(".")
				if err != nil {
					return fmt.Errorf("failed to find project root: %w", err)
				}
				return runServeWithSession(cmd.Context(), sessionName, root, transport, port)
			}
			return runServe(cmd.Context(), transport, port)
		},
	}

	cmd.Flags().StringVar(&transport, "transport", "stdio", "Transport type (stdio|sse)")
	cmd.Flags().IntVar(&port, "port", 8765, "Port for SSE transport")
	cmd.Flags().StringVar(&sessionName, "session", "", "Named session to create/load")
	cmd.Flags().BoolVar(&debug, "debug", false, "Enable debug logging to ~/.amanmcp/logs/server.log")

	return cmd
}

// setupDebugLogging initializes the structured logging system with debug level.
// Returns a cleanup function that must be called to close the log file.
func setupDebugLogging() (func(), error) {
	cfg := logging.DebugConfig()
	// Don't write to stderr in MCP mode (interferes with JSON-RPC)
	cfg.WriteToStderr = false

	logger, cleanup, err := logging.Setup(cfg)
	if err != nil {
		return nil, err
	}

	// Set as default logger for all slog calls
	slog.SetDefault(logger)
	return cleanup, nil
}

func runServe(ctx context.Context, transport string, port int) (err error) {
	// BUG-034: Initialize MCP-safe logging FIRST, before ANYTHING else.
	// This ensures all logs go to file, never stdout/stderr.
	// MCP protocol requires stdout to be used exclusively for JSON-RPC.
	mcpLogCleanup, logErr := logging.SetupMCPMode()
	if logErr != nil {
		// Can't log this error (no logger yet), just return it
		return fmt.Errorf("failed to setup MCP logging: %w", logErr)
	}
	defer mcpLogCleanup()

	// BUG-035: Verify stdin availability for stdio transport.
	// Helps diagnose connection issues early.
	if transport == "stdio" {
		if err := verifyStdinForMCP(); err != nil {
			slog.Warn("stdin validation failed (continuing anyway)",
				slog.String("error", err.Error()))
			// Don't fail - just warn. MCP SDK will report the actual error if stdin is unusable.
		}
	}

	// Recover from panics and convert to error (BUG-033: server resilience)
	defer func() {
		if r := recover(); r != nil {
			slog.Error("Server panic recovered",
				slog.Any("panic", r),
				slog.String("stack", string(debug.Stack())))
			err = fmt.Errorf("server panic: %v", r)
		}
	}()

	slog.Info("=== AmanMCP Server Startup ===",
		slog.String("version", version.Version),
		slog.String("transport", transport),
		slog.Int("port", port))

	// Find project root
	root, err := config.FindProjectRoot(".")
	if err != nil {
		return fmt.Errorf("failed to find project root: %w", err)
	}
	slog.Debug("Found project root", slog.String("root", root))

	// Load config
	cfg, err := config.Load(root)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	slog.Debug("Configuration loaded",
		slog.String("log_level", cfg.Server.LogLevel))

	// Override transport from config if not specified
	if transport == "" {
		transport = cfg.Server.Transport
	}

	// Data directory
	dataDir := filepath.Join(root, ".amanmcp")

	// ISSUE-01: Prevent multiple serve instances on same project
	// Use PID file to detect and block concurrent serve processes
	pidFile := daemon.NewPIDFile(filepath.Join(dataDir, "serve.pid"))
	if pidFile.IsRunning() {
		pid, _ := pidFile.Read()
		return fmt.Errorf("another serve instance is already running (PID %d). "+
			"Kill it first with: kill %d", pid, pid)
	}
	// Clean up stale PID file and write current PID
	_ = pidFile.Remove()
	if err := pidFile.Write(); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}
	defer pidFile.Remove()
	slog.Debug("PID file written", slog.String("path", pidFile.Path()), slog.Int("pid", os.Getpid()))

	// Check if index exists
	metadataPath := filepath.Join(dataDir, "metadata.db")
	if _, err := os.Stat(metadataPath); os.IsNotExist(err) {
		return fmt.Errorf("no index found. Run 'amanmcp index' first to create an index")
	}

	// Initialize stores
	slog.Debug("Opening metadata store", slog.String("path", metadataPath))
	metadata, err := store.NewSQLiteStore(metadataPath)
	if err != nil {
		return fmt.Errorf("failed to open metadata store: %w", err)
	}
	defer func() { _ = metadata.Close() }()

	// ISSUE-02: Block serve if index is incomplete (checkpoint exists)
	// Prevents race conditions between serve and index --resume
	checkpoint, checkpointErr := metadata.LoadIndexCheckpoint(ctx)
	if checkpointErr == nil && checkpoint != nil && checkpoint.Stage != "" && checkpoint.Stage != "complete" {
		return fmt.Errorf("incomplete index detected (stage=%s, %d/%d chunks embedded). "+
			"Run 'amanmcp index --resume' to complete indexing before serving",
			checkpoint.Stage, checkpoint.EmbeddedCount, checkpoint.Total)
	}

	// Use factory for BM25 backend selection (SQLite default for concurrent access)
	bm25BasePath := filepath.Join(dataDir, "bm25")
	slog.Debug("Opening BM25 index", slog.String("path", bm25BasePath), slog.String("backend", cfg.Search.BM25Backend))
	bm25, err := store.NewBM25IndexWithBackend(bm25BasePath, store.DefaultBM25Config(), cfg.Search.BM25Backend)
	if err != nil {
		return fmt.Errorf("failed to open BM25 index: %w", err)
	}
	defer func() { _ = bm25.Close() }()

	vectorPath := filepath.Join(dataDir, "vectors.hnsw")

	// Wire MLX config from config.yaml to embedder factory
	embed.SetMLXConfig(embed.MLXServerConfig{
		Endpoint: cfg.Embeddings.MLXEndpoint,
		Model:    cfg.Embeddings.MLXModel,
	})

	// Use config-based embedder selection (same as index command) - fixes BUG-039
	provider := embed.ParseProvider(cfg.Embeddings.Provider)
	embedder, err := embed.NewEmbedder(ctx, provider, cfg.Embeddings.Model)
	if err != nil {
		return fmt.Errorf("failed to create embedder: %w", err)
	}
	defer func() { _ = embedder.Close() }()

	// FEAT-RR1: Initialize reranker if MLX provider is being used
	var reranker search.Reranker
	if provider == embed.ProviderMLX {
		rerankerCfg := search.MLXRerankerConfig{
			Endpoint:        cfg.Embeddings.MLXEndpoint,
			SkipHealthCheck: true, // Don't fail startup if reranker unavailable
		}
		r, rerankErr := search.NewMLXReranker(ctx, rerankerCfg)
		if rerankErr != nil {
			slog.Warn("Reranker unavailable, search results will not be reranked",
				slog.String("error", rerankErr.Error()))
		} else {
			reranker = r
			defer func() { _ = reranker.Close() }()
			slog.Info("Reranker initialized", slog.String("endpoint", rerankerCfg.Endpoint))
		}
	}

	slog.Debug("embedder_initialized",
		slog.String("provider", provider.String()),
		slog.String("model", embedder.ModelName()),
		slog.Int("dimensions", embedder.Dimensions()))

	// BUG-054: Check if current embedder matches the stored index embedder
	// If mismatch, skip reconciliation to prevent mixed embeddings
	skipReconciliation := false
	storedModel, _ := metadata.GetState(ctx, store.StateKeyIndexModel)
	currentModel := embedder.ModelName()
	if storedModel != "" && storedModel != currentModel {
		slog.Warn("embedder_mismatch_skipping_reconciliation",
			slog.String("stored", storedModel),
			slog.String("current", currentModel),
			slog.String("note", "reconciliation skipped to prevent mixed embeddings"))
		skipReconciliation = true
	}

	// Initialize vector store with embedder's dimensions (fixes BUG-001)
	dimensions := embedder.Dimensions()
	vectorCfg := store.DefaultVectorStoreConfig(dimensions)
	vector, err := store.NewHNSWStore(vectorCfg)
	if err != nil {
		return fmt.Errorf("failed to create vector store: %w", err)
	}
	// Try to load existing vectors
	if _, err := os.Stat(vectorPath); err == nil {
		slog.Debug("Loading existing vectors", slog.String("path", vectorPath))
		if err := vector.Load(vectorPath); err != nil {
			// Log warning but continue - vector store will be empty
			slog.Warn("Failed to load vectors, starting with empty store",
				slog.String("error", err.Error()),
				slog.String("path", vectorPath))
		}
	}
	defer func() { _ = vector.Close() }()

	// DEBT-021: Check cross-store consistency on startup
	// Detects orphaned entries and logs warnings without blocking startup
	consistencyChecker := index.NewConsistencyChecker(metadata, bm25, vector)
	consistent, consistencyErr := consistencyChecker.QuickCheck(ctx)
	if consistencyErr != nil {
		slog.Warn("consistency_check_failed",
			slog.String("error", consistencyErr.Error()))
	} else if !consistent {
		slog.Warn("index_consistency_mismatch_detected",
			slog.String("note", "counts differ across stores, run full check for details"))
		// Run full check in background to avoid blocking startup
		go func() {
			result, err := consistencyChecker.Check(context.Background())
			if err != nil {
				slog.Warn("full_consistency_check_failed", slog.String("error", err.Error()))
				return
			}
			if len(result.Inconsistencies) > 0 {
				slog.Warn("index_inconsistencies_found",
					slog.Int("count", len(result.Inconsistencies)),
					slog.Duration("check_duration", result.Duration))
				// Repair orphans (best-effort, non-blocking)
				if err := consistencyChecker.Repair(context.Background(), result.Inconsistencies); err != nil {
					slog.Warn("consistency_repair_failed", slog.String("error", err.Error()))
				}
			}
		}()
	} else {
		slog.Debug("index_consistency_ok", slog.String("status", "all stores in sync"))
	}

	// Create search engine with query expander (QI-1 Lite)
	engineCfg := search.EngineConfig{
		DefaultLimit:   cfg.Search.MaxResults,
		MaxLimit:       100,
		DefaultWeights: search.Weights{BM25: cfg.Search.BM25Weight, Semantic: cfg.Search.SemanticWeight},
		RRFConstant:    cfg.Search.RRFConstant,
		SearchTimeout:  search.DefaultConfig().SearchTimeout,
	}
	// QI-1 Lite: Enable code-aware query expansion to bridge vocabulary gap
	// Research: https://arxiv.org/html/2408.11058v1 (LLM Agents for Code Search)
	queryExpander := search.NewQueryExpander()

	// Build engine options
	engineOpts := []search.EngineOption{
		search.WithQueryExpander(queryExpander),
	}
	// FEAT-RR1: Add reranker if available
	if reranker != nil {
		engineOpts = append(engineOpts, search.WithReranker(reranker))
	}
	// FEAT-QI3: Add multi-query decomposition for generic queries
	engineOpts = append(engineOpts, search.WithMultiQuerySearch(search.NewPatternDecomposer()))

	engine, err := search.NewEngine(bm25, vector, embedder, metadata, engineCfg, engineOpts...)
	if err != nil {
		return fmt.Errorf("failed to create search engine: %w", err)
	}
	defer func() { _ = engine.Close() }()

	// Create MCP server with embedder for capability signaling
	slog.Debug("Creating MCP server")
	srv, err := mcp.NewServer(engine, metadata, embedder, cfg, root)
	if err != nil {
		return fmt.Errorf("failed to create MCP server: %w", err)
	}
	defer func() { _ = srv.Close() }()

	// Handle graceful shutdown (DEBT-015: added SIGHUP for terminal close)
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer cancel()

	// BUG-035: Start file watcher in background to avoid blocking MCP handshake.
	// MCP protocol requires handshake response within 500ms. File watcher startup
	// can take 2+ seconds on slow filesystems. Make it non-blocking so the MCP
	// server can start serving immediately.
	// BUG-054: Pass skipReconciliation flag to prevent mixed embeddings
	// BUG-027: Pass exclude patterns for consistent reconciliation behavior
	excludePatterns := append(cfg.Paths.Exclude, "**/.amanmcp/**")
	go func() {
		slog.Debug("Starting file watcher in background", slog.String("root", root))
		if err := startFileWatcher(ctx, root, dataDir, engine, metadata, skipReconciliation, excludePatterns); err != nil {
			// Log but don't crash - server can still serve search without live updates
			slog.Error("File watcher failed to start (non-fatal, search still works)",
				slog.String("error", err.Error()),
				slog.String("root", root))
			return
		}
		slog.Info("File watcher running", slog.String("root", root))
	}()

	// Start server immediately - don't wait for file watcher
	slog.Info("MCP server ready",
		slog.String("transport", transport),
		slog.String("root", root))
	addr := fmt.Sprintf(":%d", port)
	return srv.Serve(ctx, transport, addr)
}

// startFileWatcher creates and starts the file watcher for incremental updates.
// Uses errgroup for proper goroutine coordination (DEBT-002 fix).
// Returns error if watcher fails to start within startup timeout (BUG-017 fix).
// BUG-054: skipReconciliation prevents adding embeddings from mismatched embedder model.
// BUG-027: excludePatterns passed to coordinator for consistent reconciliation behavior.
func startFileWatcher(ctx context.Context, root, dataDir string, engine *search.Engine, metadata store.MetadataStore, skipReconciliation bool, excludePatterns []string) error {
	// Create watcher with default options
	opts := watcher.Options{
		DebounceWindow:  200 * time.Millisecond,
		PollInterval:    5 * time.Second,
		EventBufferSize: 1000,
	}.WithDefaults()

	w, err := watcher.NewHybridWatcher(opts)
	if err != nil {
		return fmt.Errorf("failed to create watcher: %w", err)
	}

	// Create chunkers
	codeChunker := chunk.NewCodeChunker()
	mdChunker := chunk.NewMarkdownChunker()

	// Create scanner for gitignore reconciliation
	fileScanner, err := scanner.New()
	if err != nil {
		return fmt.Errorf("failed to create scanner: %w", err)
	}

	// Create coordinator (use same hash as index command)
	h := sha256.Sum256([]byte(root))
	projectID := hex.EncodeToString(h[:])[:16]
	coordinator := index.NewCoordinator(index.CoordinatorConfig{
		ProjectID:       projectID,
		RootPath:        root,
		DataDir:         dataDir,
		Engine:          engine,
		Metadata:        metadata,
		CodeChunker:     codeChunker,
		MDChunker:       mdChunker,
		Scanner:         fileScanner,
		ExcludePatterns: excludePatterns, // BUG-027: passed from caller
	})

	// BUG-054: Skip reconciliation if embedder model mismatch detected earlier
	// This prevents adding embeddings from a different model to an existing index
	if skipReconciliation {
		slog.Info("startup_reconciliation_skipped",
			slog.String("reason", "embedder model mismatch"),
			slog.String("note", "run 'amanmcp index --force' to rebuild with current embedder"))
	} else {
		// BUG-054: Log reconciliation start/completion for debugging race conditions
		// Note: MCP server may be handling queries during reconciliation (after BUG-035 background fix)
		// The checkpoint check at serve.go:236-243 ensures index is complete before serving starts
		slog.Info("startup_reconciliation_begin",
			slog.String("root", root),
			slog.String("note", "search available during reconciliation"))

		// Reconcile gitignore changes from while server was stopped
		if err := coordinator.ReconcileOnStartup(ctx); err != nil {
			slog.Warn("Failed to reconcile gitignore on startup", slog.String("error", err.Error()))
			// Non-fatal - continue anyway
		}

		// BUG-036: Reconcile file changes (new/modified/deleted) from while server was stopped
		if err := coordinator.ReconcileFilesOnStartup(ctx); err != nil {
			slog.Warn("Failed to reconcile files on startup", slog.String("error", err.Error()))
			// Non-fatal - continue anyway
		}

		slog.Info("startup_reconciliation_complete")
	}

	// Use errgroup with derived context for proper goroutine coordination
	// When either goroutine fails, the other will be signaled to stop via context cancellation
	g, gctx := errgroup.WithContext(ctx)

	// Channel to detect startup failure (BUG-017 fix)
	startupErr := make(chan error, 1)

	// Start watcher goroutine
	g.Go(func() error {
		slog.Info("Starting file watcher",
			slog.String("root", root),
			slog.String("type", w.WatcherType()))

		err := w.Start(gctx, root)

		// Report startup failure
		if err != nil && err != context.Canceled {
			select {
			case startupErr <- err:
			default:
			}
			slog.Error("File watcher failed", slog.String("error", err.Error()))
		}
		return err
	})

	// Process events goroutine
	g.Go(func() error {
		defer func() {
			_ = w.Stop()
			codeChunker.Close()
			mdChunker.Close()
		}()

		for {
			select {
			case <-gctx.Done():
				return gctx.Err()
			case events, ok := <-w.Events():
				if !ok {
					return nil
				}
				if len(events) > 0 {
					slog.Debug("Processing file events", slog.Int("count", len(events)))
					if err := coordinator.HandleEvents(gctx, events); err != nil {
						slog.Error("Failed to process file events", slog.String("error", err.Error()))
					}
				}
			case err, ok := <-w.Errors():
				if !ok {
					return nil
				}
				slog.Warn("File watcher error (non-fatal)", slog.String("error", err.Error()))
			}
		}
	})

	// Wait briefly to catch immediate startup failures (BUG-017 fix)
	// If the watcher fails during initial directory scan, we want to know about it
	// BUG-033: Increased default from 500ms to 2s for slow filesystems, configurable via env var
	startupTimeout := getWatcherStartupTimeout()
	select {
	case err := <-startupErr:
		// Watcher failed during startup - this is a critical error
		return fmt.Errorf("file watcher startup failed: %w", err)
	case <-time.After(startupTimeout):
		// No immediate failure, watcher appears to be running
		slog.Debug("File watcher started successfully",
			slog.String("type", w.WatcherType()),
			slog.Duration("startup_time", startupTimeout))
	case <-ctx.Done():
		return ctx.Err()
	}

	// Wait for goroutines in background (don't block the caller)
	go func() {
		if err := g.Wait(); err != nil && err != context.Canceled {
			slog.Error("File watcher stopped unexpectedly", slog.String("error", err.Error()))
		}
	}()

	return nil
}

// getWatcherStartupTimeout returns the watcher startup timeout from environment
// or a default of 2 seconds (increased from 500ms for slow filesystems).
// BUG-033: Configurable via AMANMCP_WATCHER_STARTUP_TIMEOUT environment variable.
func getWatcherStartupTimeout() time.Duration {
	if v := os.Getenv("AMANMCP_WATCHER_STARTUP_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		slog.Warn("Invalid AMANMCP_WATCHER_STARTUP_TIMEOUT, using default",
			slog.String("value", v),
			slog.Duration("default", 2*time.Second))
	}
	return 2 * time.Second
}

// runServeWithSession runs the server with session management.
// It creates or loads the named session and uses the session directory for index data.
func runServeWithSession(ctx context.Context, sessionName, projectPath, transport string, port int) (err error) {
	// BUG-035/BUG-034 addendum: Initialize MCP-safe logging FIRST.
	// This was a gap in BUG-034 - only runServe() had MCP logging.
	// Without this, session mode would have stdout contamination.
	mcpLogCleanup, logErr := logging.SetupMCPMode()
	if logErr != nil {
		return fmt.Errorf("failed to setup MCP logging: %w", logErr)
	}
	defer mcpLogCleanup()

	// BUG-035: Verify stdin availability for stdio transport.
	if transport == "stdio" {
		if err := verifyStdinForMCP(); err != nil {
			slog.Warn("stdin validation failed (continuing anyway)",
				slog.String("error", err.Error()))
		}
	}

	// Recover from panics and convert to error (BUG-033: server resilience)
	defer func() {
		if r := recover(); r != nil {
			slog.Error("Server panic recovered (session mode)",
				slog.Any("panic", r),
				slog.String("session", sessionName),
				slog.String("stack", string(debug.Stack())))
			err = fmt.Errorf("server panic: %v", r)
		}
	}()

	cfg := config.NewConfig()

	// Create session manager
	mgr, err := session.NewManager(session.ManagerConfig{
		StoragePath: cfg.Sessions.StoragePath,
		MaxSessions: cfg.Sessions.MaxSessions,
	})
	if err != nil {
		return fmt.Errorf("failed to create session manager: %w", err)
	}

	// Open or create session
	sess, err := mgr.Open(sessionName, projectPath)
	if err != nil {
		return fmt.Errorf("failed to open session: %w", err)
	}

	// Use session directory for data storage
	dataDir := sess.SessionDir

	// Check if index exists in project's .amanmcp
	projectDataDir := filepath.Join(projectPath, ".amanmcp")
	projectMetadataPath := filepath.Join(projectDataDir, "metadata.db")
	sessionMetadataPath := filepath.Join(dataDir, "metadata.db")

	// ISSUE-01: Prevent multiple serve instances on same project (session mode)
	// Use project directory for PID file since file watcher operates on project root
	pidFile := daemon.NewPIDFile(filepath.Join(projectDataDir, "serve.pid"))
	if pidFile.IsRunning() {
		pid, _ := pidFile.Read()
		return fmt.Errorf("another serve instance is already running (PID %d). "+
			"Kill it first with: kill %d", pid, pid)
	}
	_ = pidFile.Remove()
	if err := pidFile.Write(); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}
	defer pidFile.Remove()
	slog.Debug("PID file written (session mode)",
		slog.String("path", pidFile.Path()),
		slog.Int("pid", os.Getpid()),
		slog.String("session", sessionName))

	// If session has no index but project does, copy from project
	if _, err := os.Stat(sessionMetadataPath); os.IsNotExist(err) {
		if _, err := os.Stat(projectMetadataPath); err == nil {
			slog.Info("Copying index from project to session",
				slog.String("from", projectDataDir),
				slog.String("to", dataDir))
			if err := session.CopyIndexFiles(projectDataDir, dataDir); err != nil {
				return fmt.Errorf("failed to copy index files: %w", err)
			}
		} else {
			return fmt.Errorf("no index found. Run 'amanmcp index' first to create an index")
		}
	}

	// Load config from project
	projCfg, err := config.Load(projectPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Override transport if not specified
	if transport == "" {
		transport = projCfg.Server.Transport
	}

	// Initialize stores from session directory
	metadata, err := store.NewSQLiteStore(sessionMetadataPath)
	if err != nil {
		return fmt.Errorf("failed to open metadata store: %w", err)
	}
	defer func() { _ = metadata.Close() }()

	// ISSUE-02: Block serve if index is incomplete (checkpoint exists)
	// Prevents race conditions between serve and index --resume
	checkpoint, checkpointErr := metadata.LoadIndexCheckpoint(ctx)
	if checkpointErr == nil && checkpoint != nil && checkpoint.Stage != "" && checkpoint.Stage != "complete" {
		return fmt.Errorf("incomplete index detected (stage=%s, %d/%d chunks embedded). "+
			"Run 'amanmcp index --resume' to complete indexing before serving",
			checkpoint.Stage, checkpoint.EmbeddedCount, checkpoint.Total)
	}

	// Use factory for BM25 backend selection (SQLite default for concurrent access)
	bm25BasePath := filepath.Join(dataDir, "bm25")
	bm25, err := store.NewBM25IndexWithBackend(bm25BasePath, store.DefaultBM25Config(), projCfg.Search.BM25Backend)
	if err != nil {
		return fmt.Errorf("failed to open BM25 index: %w", err)
	}
	defer func() { _ = bm25.Close() }()

	vectorPath := filepath.Join(dataDir, "vectors.hnsw")

	// Wire MLX config from config.yaml to embedder factory
	embed.SetMLXConfig(embed.MLXServerConfig{
		Endpoint: projCfg.Embeddings.MLXEndpoint,
		Model:    projCfg.Embeddings.MLXModel,
	})

	// Use config-based embedder selection (same as index command) - fixes BUG-039
	provider := embed.ParseProvider(projCfg.Embeddings.Provider)
	embedder, err := embed.NewEmbedder(ctx, provider, projCfg.Embeddings.Model)
	if err != nil {
		return fmt.Errorf("failed to create embedder: %w", err)
	}
	defer func() { _ = embedder.Close() }()

	// FEAT-RR1: Initialize reranker if MLX provider is being used (session mode)
	var rerankerSession search.Reranker
	if provider == embed.ProviderMLX {
		rerankerCfg := search.MLXRerankerConfig{
			Endpoint:        projCfg.Embeddings.MLXEndpoint,
			SkipHealthCheck: true,
		}
		r, rerankErr := search.NewMLXReranker(ctx, rerankerCfg)
		if rerankErr != nil {
			slog.Warn("Reranker unavailable (session mode)",
				slog.String("error", rerankErr.Error()))
		} else {
			rerankerSession = r
			defer func() { _ = rerankerSession.Close() }()
			slog.Info("Reranker initialized (session mode)")
		}
	}

	slog.Debug("embedder_initialized",
		slog.String("provider", provider.String()),
		slog.String("model", embedder.ModelName()),
		slog.Int("dimensions", embedder.Dimensions()))

	// BUG-054: Check if current embedder matches the stored index embedder (session mode)
	skipReconciliationSession := false
	storedModelSession, _ := metadata.GetState(ctx, store.StateKeyIndexModel)
	currentModelSession := embedder.ModelName()
	if storedModelSession != "" && storedModelSession != currentModelSession {
		slog.Warn("embedder_mismatch_skipping_reconciliation",
			slog.String("stored", storedModelSession),
			slog.String("current", currentModelSession),
			slog.String("note", "reconciliation skipped to prevent mixed embeddings"))
		skipReconciliationSession = true
	}

	dimensions := embedder.Dimensions()
	vectorCfg := store.DefaultVectorStoreConfig(dimensions)
	vector, err := store.NewHNSWStore(vectorCfg)
	if err != nil {
		return fmt.Errorf("failed to create vector store: %w", err)
	}
	if _, err := os.Stat(vectorPath); err == nil {
		slog.Debug("Loading existing vectors", slog.String("path", vectorPath))
		if err := vector.Load(vectorPath); err != nil {
			slog.Warn("Failed to load vectors, starting with empty store",
				slog.String("error", err.Error()),
				slog.String("path", vectorPath))
		}
	}
	defer func() { _ = vector.Close() }()

	// DEBT-021: Check cross-store consistency on startup (session mode)
	sessionChecker := index.NewConsistencyChecker(metadata, bm25, vector)
	sessionConsistent, sessionCheckErr := sessionChecker.QuickCheck(ctx)
	if sessionCheckErr != nil {
		slog.Warn("consistency_check_failed",
			slog.String("error", sessionCheckErr.Error()),
			slog.String("session", sessionName))
	} else if !sessionConsistent {
		slog.Warn("index_consistency_mismatch_detected",
			slog.String("note", "counts differ across stores"),
			slog.String("session", sessionName))
		go func() {
			result, err := sessionChecker.Check(context.Background())
			if err != nil {
				slog.Warn("full_consistency_check_failed", slog.String("error", err.Error()))
				return
			}
			if len(result.Inconsistencies) > 0 {
				slog.Warn("index_inconsistencies_found",
					slog.Int("count", len(result.Inconsistencies)),
					slog.String("session", sessionName))
				if err := sessionChecker.Repair(context.Background(), result.Inconsistencies); err != nil {
					slog.Warn("consistency_repair_failed", slog.String("error", err.Error()))
				}
			}
		}()
	} else {
		slog.Debug("index_consistency_ok",
			slog.String("status", "all stores in sync"),
			slog.String("session", sessionName))
	}

	// Create search engine
	engineCfg := search.EngineConfig{
		DefaultLimit:   projCfg.Search.MaxResults,
		MaxLimit:       100,
		DefaultWeights: search.Weights{BM25: projCfg.Search.BM25Weight, Semantic: projCfg.Search.SemanticWeight},
		RRFConstant:    projCfg.Search.RRFConstant,
		SearchTimeout:  search.DefaultConfig().SearchTimeout,
	}
	// QI-1 Lite: Enable code-aware query expansion to bridge vocabulary gap
	queryExpander := search.NewQueryExpander()

	// Build engine options (session mode)
	engineOptsSession := []search.EngineOption{
		search.WithQueryExpander(queryExpander),
	}
	// FEAT-RR1: Add reranker if available
	if rerankerSession != nil {
		engineOptsSession = append(engineOptsSession, search.WithReranker(rerankerSession))
	}
	// FEAT-QI3: Add multi-query decomposition for generic queries
	engineOptsSession = append(engineOptsSession, search.WithMultiQuerySearch(search.NewPatternDecomposer()))

	engine, err := search.NewEngine(bm25, vector, embedder, metadata, engineCfg, engineOptsSession...)
	if err != nil {
		return fmt.Errorf("failed to create search engine: %w", err)
	}
	defer func() { _ = engine.Close() }()

	// Create MCP server
	srv, err := mcp.NewServer(engine, metadata, embedder, projCfg, projectPath)
	if err != nil {
		return fmt.Errorf("failed to create MCP server: %w", err)
	}
	defer func() { _ = srv.Close() }()

	// Handle graceful shutdown with session save (DEBT-015: added SIGHUP for terminal close)
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer cancel()

	// Save session on shutdown if auto_save is enabled
	if cfg.Sessions.AutoSave {
		defer func() {
			if err := mgr.Save(sess); err != nil {
				slog.Warn("Failed to save session on shutdown",
					slog.String("error", err.Error()),
					slog.String("session", sessionName))
			}
		}()
	}

	// BUG-035: Start file watcher in background (session mode).
	// Same as runServe() - don't block MCP handshake.
	// BUG-054: Pass skipReconciliation flag to prevent mixed embeddings
	// BUG-027: Pass exclude patterns for consistent reconciliation behavior
	sessionExcludePatterns := append(projCfg.Paths.Exclude, "**/.amanmcp/**")
	go func() {
		slog.Debug("Starting file watcher in background (session mode)",
			slog.String("root", projectPath),
			slog.String("session", sessionName))
		if err := startFileWatcher(ctx, projectPath, dataDir, engine, metadata, skipReconciliationSession, sessionExcludePatterns); err != nil {
			slog.Error("File watcher failed to start (non-fatal, search still works)",
				slog.String("error", err.Error()),
				slog.String("root", projectPath))
			return
		}
		slog.Info("File watcher running (session mode)",
			slog.String("root", projectPath),
			slog.String("session", sessionName))
	}()

	// Start server immediately
	addr := fmt.Sprintf(":%d", port)
	return srv.Serve(ctx, transport, addr)
}
