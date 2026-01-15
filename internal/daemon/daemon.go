package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/embed"
	"github.com/Aman-CERP/amanmcp/internal/search"
	"github.com/Aman-CERP/amanmcp/internal/store"
)

// Daemon manages the background search service.
type Daemon struct {
	config  Config
	server  *Server
	pidFile *PIDFile

	// Shared resources (created once at startup)
	embedder   embed.Embedder        // Shared across all projects
	expander   *search.QueryExpander // QI-1 Lite query expansion
	reranker   search.Reranker       // FEAT-RR1: Cross-encoder reranker (optional)
	compaction *CompactionManager    // FEAT-AI3: Background compaction

	// Per-project state (lazy loaded)
	mu       sync.RWMutex
	projects map[string]*projectState
	started  time.Time

	// Lifecycle
	stopOnce sync.Once
	done     chan struct{}
}

// projectState holds per-project search engine state.
type projectState struct {
	rootPath string
	loadedAt time.Time
	lastUsed time.Time

	// Stores (owned by this project)
	metadata store.MetadataStore
	bm25     store.BM25Index
	vector   store.VectorStore

	// Engine (uses shared embedder from Daemon)
	engine *search.Engine

	// Configuration used to create stores
	cfg *config.Config
}

// Close releases all resources held by this project state.
func (p *projectState) Close() error {
	var errs []error

	// Note: Don't close engine - it doesn't own resources, just references them

	if p.metadata != nil {
		if err := p.metadata.Close(); err != nil {
			errs = append(errs, fmt.Errorf("metadata close: %w", err))
		}
	}
	if p.bm25 != nil {
		if err := p.bm25.Close(); err != nil {
			errs = append(errs, fmt.Errorf("bm25 close: %w", err))
		}
	}
	if p.vector != nil {
		if err := p.vector.Close(); err != nil {
			errs = append(errs, fmt.Errorf("vector close: %w", err))
		}
	}

	return errors.Join(errs...)
}

// DaemonOption configures a Daemon.
type DaemonOption func(*Daemon)

// WithEmbedder sets a custom embedder (useful for testing).
func WithEmbedder(e embed.Embedder) DaemonOption {
	return func(d *Daemon) {
		d.embedder = e
	}
}

// NewDaemon creates a new daemon instance.
func NewDaemon(cfg Config, opts ...DaemonOption) (*Daemon, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	d := &Daemon{
		config:   cfg,
		pidFile:  NewPIDFile(cfg.PIDPath),
		projects: make(map[string]*projectState),
		done:     make(chan struct{}),
		expander: search.NewQueryExpander(), // Always create expander for QI-1 Lite
	}

	for _, opt := range opts {
		opt(d)
	}

	// FEAT-AI3: Initialize compaction manager with default config
	// Config can be customized via .amanmcp.yaml or env vars
	compactionCfg := config.NewConfig().Compaction
	d.compaction = NewCompactionManager(d, compactionCfg)

	return d, nil
}

// initEmbedder creates the shared embedder at daemon startup.
func (d *Daemon) initEmbedder(ctx context.Context) error {
	if d.embedder != nil {
		return nil // Already initialized (e.g., via WithEmbedder option)
	}

	// Use default config to determine provider
	cfg := config.NewConfig()
	provider := embed.ParseProvider(cfg.Embeddings.Provider)

	// Wire MLX config from config to embedder factory
	embed.SetMLXConfig(embed.MLXServerConfig{
		Endpoint: cfg.Embeddings.MLXEndpoint,
		Model:    cfg.Embeddings.MLXModel,
	})

	slog.Info("Initializing embedder",
		slog.String("provider", provider.String()),
		slog.String("model", cfg.Embeddings.Model))

	embedder, err := embed.NewEmbedder(ctx, provider, cfg.Embeddings.Model)
	if err != nil {
		return fmt.Errorf("failed to create embedder: %w", err)
	}

	d.embedder = embedder
	slog.Info("Embedder initialized",
		slog.String("provider", provider.String()),
		slog.String("model", embedder.ModelName()),
		slog.Int("dimensions", embedder.Dimensions()))

	// FEAT-RR1: Initialize reranker if MLX provider is being used
	// Reranker uses the same MLX server as embeddings
	if provider == embed.ProviderMLX {
		rerankerCfg := search.MLXRerankerConfig{
			Endpoint:        cfg.Embeddings.MLXEndpoint,
			SkipHealthCheck: true, // Don't fail startup if reranker unavailable
		}
		reranker, rerankErr := search.NewMLXReranker(ctx, rerankerCfg)
		if rerankErr != nil {
			// Graceful degradation: log warning but don't fail
			slog.Warn("Reranker unavailable, search results will not be reranked",
				slog.String("error", rerankErr.Error()))
		} else {
			d.reranker = reranker
			slog.Info("Reranker initialized",
				slog.String("endpoint", rerankerCfg.Endpoint),
				slog.String("model", rerankerCfg.Model))
		}
	}

	return nil
}

// Start begins the daemon and blocks until context is cancelled.
func (d *Daemon) Start(ctx context.Context) error {
	// Check for stale PID
	if d.pidFile.IsRunning() {
		// Try to read PID and check if it's really running
		pid, err := d.pidFile.Read()
		if err == nil && pid != os.Getpid() {
			return fmt.Errorf("daemon already running (pid: %d)", pid)
		}
	}

	// Clean up stale files
	_ = d.pidFile.Remove()
	_ = os.Remove(d.config.SocketPath)

	// Ensure directory exists
	if err := d.config.EnsureDir(); err != nil {
		return fmt.Errorf("failed to create daemon directory: %w", err)
	}

	// Write PID file
	if err := d.pidFile.Write(); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}
	defer d.pidFile.Remove()

	// Create server
	server, err := NewServer(d.config.SocketPath)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}
	d.server = server
	d.server.SetHandler(d)
	d.started = time.Now()

	// Initialize embedder early for fast first search
	if err := d.initEmbedder(ctx); err != nil {
		slog.Warn("Embedder initialization failed, will retry on first search",
			slog.String("error", err.Error()))
		// Continue - we can try again on first search
	}

	// FEAT-AI3: Start background compaction manager
	if d.compaction != nil {
		d.compaction.Start(ctx)
	}

	// Handle shutdown signals
	sigCtx, sigCancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer sigCancel()

	embedderStatus := "unavailable"
	if d.embedder != nil {
		embedderStatus = d.embedder.ModelName()
	}
	slog.Info("Daemon starting",
		slog.String("socket", d.config.SocketPath),
		slog.String("pid_file", d.config.PIDPath),
		slog.Int("max_projects", d.config.MaxProjects),
		slog.String("embedder", embedderStatus))

	// Run server (blocks until context cancelled)
	err = d.server.ListenAndServe(sigCtx)

	// Cleanup
	d.cleanup()

	return err
}

// Stop gracefully stops the daemon.
func (d *Daemon) Stop() error {
	d.stopOnce.Do(func() {
		if d.server != nil {
			_ = d.server.Close()
		}
		close(d.done)
	})
	return nil
}

// cleanup releases all resources.
func (d *Daemon) cleanup() {
	// FEAT-AI3: Stop compaction manager first (before closing project states)
	if d.compaction != nil {
		d.compaction.Stop()
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Close all project states
	for path, state := range d.projects {
		slog.Debug("Closing project state", slog.String("path", path))
		if err := state.Close(); err != nil {
			slog.Warn("Error closing project state",
				slog.String("path", path),
				slog.String("error", err.Error()))
		}
	}
	d.projects = make(map[string]*projectState)

	// Close shared embedder
	if d.embedder != nil {
		if err := d.embedder.Close(); err != nil {
			slog.Warn("Error closing embedder", slog.String("error", err.Error()))
		}
		d.embedder = nil
	}

	// FEAT-RR1: Close reranker
	if d.reranker != nil {
		if err := d.reranker.Close(); err != nil {
			slog.Warn("Error closing reranker", slog.String("error", err.Error()))
		}
		d.reranker = nil
	}

	slog.Info("Daemon stopped")
}

// HandleSearch implements RequestHandler interface.
func (d *Daemon) HandleSearch(ctx context.Context, params SearchParams) ([]SearchResult, error) {
	// FEAT-AI3: Interrupt any ongoing compaction for this project
	if d.compaction != nil {
		d.compaction.InterruptCompaction(params.RootPath)
	}

	// Get or create project state
	state, err := d.getOrCreateProject(ctx, params.RootPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load project: %w", err)
	}

	// Update last used time
	d.mu.Lock()
	state.lastUsed = time.Now()
	d.mu.Unlock()

	// Build search options
	limit := params.Limit
	if limit <= 0 {
		limit = 10
	}

	searchOpts := search.SearchOptions{
		Limit:    limit,
		Filter:   params.Filter,
		Language: params.Language,
		Scopes:   params.Scopes,
		BM25Only: params.BM25Only,
		Explain:  params.Explain, // FEAT-UNIX3
	}

	slog.Debug("Executing search",
		slog.String("query", params.Query),
		slog.String("root_path", params.RootPath),
		slog.Int("limit", limit),
		slog.Bool("explain", params.Explain))

	// Execute search via engine
	results, err := state.engine.Search(ctx, params.Query, searchOpts)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	// Convert to daemon SearchResult format
	daemonResults := make([]SearchResult, 0, len(results))
	for i, r := range results {
		if r.Chunk == nil {
			continue
		}
		result := SearchResult{
			FilePath:  r.Chunk.FilePath,
			StartLine: r.Chunk.StartLine,
			EndLine:   r.Chunk.EndLine,
			Score:     r.Score,
			Content:   r.Chunk.Content,
			Language:  r.Chunk.Language,
		}

		// FEAT-UNIX3: Include explain data when requested
		if params.Explain {
			result.BM25Score = r.BM25Score
			result.VecScore = r.VecScore
			result.BM25Rank = r.BM25Rank
			result.VecRank = r.VecRank

			// Only attach ExplainData to first result (avoid duplication)
			if i == 0 && r.Explain != nil {
				result.Explain = &ExplainData{
					Query:                r.Explain.Query,
					BM25ResultCount:      r.Explain.BM25ResultCount,
					VectorResultCount:    r.Explain.VectorResultCount,
					BM25Weight:           r.Explain.Weights.BM25,
					SemanticWeight:       r.Explain.Weights.Semantic,
					RRFConstant:          r.Explain.RRFConstant,
					BM25Only:             r.Explain.BM25Only,
					DimensionMismatch:    r.Explain.DimensionMismatch,
					MultiQueryDecomposed: r.Explain.MultiQueryDecomposed,
					SubQueries:           r.Explain.SubQueries,
				}
			}
		}

		daemonResults = append(daemonResults, result)
	}

	slog.Debug("Search complete", slog.Int("results", len(daemonResults)))

	// FEAT-AI3: Notify compaction manager of search completion (for idle tracking)
	if d.compaction != nil {
		d.compaction.OnSearchComplete(params.RootPath)
	}

	return daemonResults, nil
}

// GetStatus implements RequestHandler interface.
func (d *Daemon) GetStatus() StatusResult {
	d.mu.RLock()
	projectCount := len(d.projects)
	d.mu.RUnlock()

	embedderType := "unavailable"
	embedderStatus := "unavailable"

	if d.embedder != nil {
		embedderType = d.embedder.ModelName()
		if d.embedder.Available(context.Background()) {
			embedderStatus = "ready"
		} else {
			embedderStatus = "unavailable"
		}
	}

	return StatusResult{
		Running:        true,
		PID:            os.Getpid(),
		Uptime:         time.Since(d.started).Round(time.Second).String(),
		EmbedderType:   embedderType,
		EmbedderStatus: embedderStatus,
		ProjectsLoaded: projectCount,
	}
}

// getOrCreateProject lazily loads project state.
func (d *Daemon) getOrCreateProject(ctx context.Context, rootPath string) (*projectState, error) {
	d.mu.RLock()
	state, exists := d.projects[rootPath]
	d.mu.RUnlock()

	if exists {
		return state, nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Double-check after acquiring write lock
	if state, exists = d.projects[rootPath]; exists {
		return state, nil
	}

	// Ensure embedder is ready
	if d.embedder == nil {
		if err := d.initEmbedder(ctx); err != nil {
			return nil, fmt.Errorf("embedder unavailable: %w", err)
		}
	}

	// Check if we need to evict
	if len(d.projects) >= d.config.MaxProjects {
		d.evictLRU()
	}

	// Load project stores and create engine
	state, err := d.loadProject(ctx, rootPath)
	if err != nil {
		return nil, err
	}

	d.projects[rootPath] = state
	slog.Info("Loaded project",
		slog.String("path", rootPath),
		slog.Int("total_projects", len(d.projects)))

	return state, nil
}

// loadProject loads stores and creates a search engine for a project.
func (d *Daemon) loadProject(ctx context.Context, rootPath string) (*projectState, error) {
	dataDir := filepath.Join(rootPath, ".amanmcp")

	// Check if index exists
	metadataPath := filepath.Join(dataDir, "metadata.db")
	if _, err := os.Stat(metadataPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("no index found at %s - run 'amanmcp index' first", rootPath)
	}

	// Load configuration
	cfg, err := config.Load(rootPath)
	if err != nil {
		cfg = config.NewConfig()
	}

	// Open metadata store
	metadata, err := store.NewSQLiteStore(metadataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open metadata: %w", err)
	}

	// Open BM25 index using factory (SQLite default for concurrent access)
	bm25BasePath := filepath.Join(dataDir, "bm25")
	bm25, err := store.NewBM25IndexWithBackend(bm25BasePath, store.DefaultBM25Config(), cfg.Search.BM25Backend)
	if err != nil {
		_ = metadata.Close()
		return nil, fmt.Errorf("failed to open BM25 index: %w", err)
	}

	// Open vector store with embedder dimensions
	dimensions := d.embedder.Dimensions()
	vectorCfg := store.DefaultVectorStoreConfig(dimensions)
	vector, err := store.NewHNSWStore(vectorCfg)
	if err != nil {
		_ = bm25.Close()
		_ = metadata.Close()
		return nil, fmt.Errorf("failed to create vector store: %w", err)
	}

	// Load vectors if they exist
	vectorPath := filepath.Join(dataDir, "vectors.hnsw")
	if _, err := os.Stat(vectorPath); err == nil {
		if loadErr := vector.Load(vectorPath); loadErr != nil {
			slog.Warn("Failed to load vectors, starting with empty store",
				slog.String("error", loadErr.Error()),
				slog.String("path", vectorPath))
		}
	}

	// Create search engine with shared embedder and expander
	engineCfg := search.EngineConfig{
		DefaultLimit: cfg.Search.MaxResults,
		MaxLimit:     100,
		DefaultWeights: search.Weights{
			BM25:     cfg.Search.BM25Weight,
			Semantic: cfg.Search.SemanticWeight,
		},
		RRFConstant:   cfg.Search.RRFConstant,
		SearchTimeout: search.DefaultConfig().SearchTimeout,
	}

	// Build engine options
	engineOpts := []search.EngineOption{
		search.WithQueryExpander(d.expander),
	}
	// FEAT-RR1: Add reranker if available
	if d.reranker != nil {
		engineOpts = append(engineOpts, search.WithReranker(d.reranker))
	}
	// FEAT-QI3: Add multi-query decomposition for generic queries
	engineOpts = append(engineOpts, search.WithMultiQuerySearch(search.NewPatternDecomposer()))

	engine, err := search.NewEngine(bm25, vector, d.embedder, metadata, engineCfg, engineOpts...)
	if err != nil {
		_ = vector.Close()
		_ = bm25.Close()
		_ = metadata.Close()
		return nil, fmt.Errorf("failed to create search engine: %w", err)
	}

	return &projectState{
		rootPath: rootPath,
		loadedAt: time.Now(),
		lastUsed: time.Now(),
		metadata: metadata,
		bm25:     bm25,
		vector:   vector,
		engine:   engine,
		cfg:      cfg,
	}, nil
}

// evictLRU removes the least recently used project.
func (d *Daemon) evictLRU() {
	var oldestPath string
	var oldestTime time.Time

	for path, state := range d.projects {
		if oldestPath == "" || state.lastUsed.Before(oldestTime) {
			oldestPath = path
			oldestTime = state.lastUsed
		}
	}

	if oldestPath != "" {
		state := d.projects[oldestPath]
		slog.Info("Evicting project",
			slog.String("path", oldestPath),
			slog.Duration("idle_for", time.Since(state.lastUsed)))

		if err := state.Close(); err != nil {
			slog.Warn("Error closing evicted project",
				slog.String("path", oldestPath),
				slog.String("error", err.Error()))
		}
		delete(d.projects, oldestPath)
	}
}
