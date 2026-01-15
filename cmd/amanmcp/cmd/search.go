package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/daemon"
	"github.com/Aman-CERP/amanmcp/internal/embed"
	"github.com/Aman-CERP/amanmcp/internal/logging"
	"github.com/Aman-CERP/amanmcp/internal/output"
	"github.com/Aman-CERP/amanmcp/internal/search"
	"github.com/Aman-CERP/amanmcp/internal/store"
)

// searchOptions holds CLI flags for search.
type searchOptions struct {
	limit    int
	filter   string   // "all", "code", "docs"
	language string
	format   string   // "text", "json"
	scopes   []string // path prefixes for filtering
	bm25Only bool     // FEAT-DIM1: skip semantic search, use BM25 only
	local    bool     // Force local search (bypass daemon)
	explain  bool     // FEAT-UNIX3: show search decision process
}

func newSearchCmd() *cobra.Command {
	var opts searchOptions

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search the indexed codebase",
		Long: `Search the indexed codebase using hybrid search.

Combines BM25 (keyword) and semantic (embedding) search
with Reciprocal Rank Fusion for optimal results.

Examples:
  amanmcp search "authentication middleware"
  amanmcp search "handleRequest" --type code --limit 5
  amanmcp search "setup instructions" --type docs
  amanmcp search "error handling" --format json`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := strings.Join(args, " ")
			return runSearch(cmd.Context(), cmd, query, opts)
		},
	}

	cmd.Flags().IntVarP(&opts.limit, "limit", "n", 10, "Maximum number of results")
	cmd.Flags().StringVarP(&opts.filter, "type", "t", "all", "Filter by type: all, code, docs")
	cmd.Flags().StringVarP(&opts.language, "language", "l", "", "Filter by language (e.g., go, python)")
	cmd.Flags().StringVarP(&opts.format, "format", "f", "text", "Output format: text, json")
	cmd.Flags().StringSliceVarP(&opts.scopes, "scope", "s", nil, "Filter by path scope (repeatable, e.g., --scope services/api)")
	cmd.Flags().BoolVar(&opts.bm25Only, "bm25-only", false, "Use keyword search only (skip semantic search)")
	cmd.Flags().BoolVar(&opts.local, "local", false, "Force local search (bypass daemon)")
	cmd.Flags().BoolVar(&opts.explain, "explain", false, "Show search decision process (BM25/vector results, weights, RRF fusion)")

	return cmd
}

func runSearch(ctx context.Context, cmd *cobra.Command, query string, opts searchOptions) error {
	// Initialize logging for CLI observability (BUG-039)
	logCfg := logging.DefaultConfig()
	logCfg.WriteToStderr = false
	if _, cleanup, err := logging.Setup(logCfg); err == nil {
		defer cleanup()
	}

	slog.Info("search_started", slog.String("query", query), slog.Int("limit", opts.limit))
	out := output.New(cmd.OutOrStdout())

	// Find project root
	root, err := config.FindProjectRoot(".")
	if err != nil {
		root, _ = os.Getwd()
	}

	// Check for index
	dataDir := filepath.Join(root, ".amanmcp")
	metadataPath := filepath.Join(dataDir, "metadata.db")
	if _, err := os.Stat(metadataPath); os.IsNotExist(err) {
		return fmt.Errorf("no index found. Run 'amanmcp index' first")
	}

	// Try daemon-based search first (fast, keeps embedder loaded)
	// Skip daemon if --local flag is set
	daemonCfg := daemon.DefaultConfig()
	client := daemon.NewClient(daemonCfg)
	if !opts.local && client.IsRunning() {
		slog.Info("search_using_daemon")
		results, err := client.Search(ctx, daemon.SearchParams{
			Query:    query,
			RootPath: root,
			Limit:    opts.limit,
			Filter:   opts.filter,
			Language: opts.language,
			Scopes:   opts.scopes,
			BM25Only: opts.bm25Only,
			Explain:  opts.explain, // FEAT-UNIX3
		})
		if err != nil {
			// Daemon error - log warning and fall through to local search
			slog.Warn("Daemon search failed, falling back to local",
				slog.String("error", err.Error()))
		} else {
			slog.Info("search_complete", slog.String("mode", "daemon"), slog.Int("results", len(results)))
			return formatDaemonResults(cmd, out, query, results, opts.format)
		}
	}

	// Fallback: Local search with dimension-compatible StaticEmbedder
	slog.Info("search_using_local")
	return runLocalSearch(ctx, cmd, root, query, opts)
}

// runLocalSearch performs search without daemon using StaticEmbedder.
// This is fast but has lower semantic quality than Hugot embeddings.
func runLocalSearch(ctx context.Context, cmd *cobra.Command, root, query string, opts searchOptions) error {
	out := output.New(cmd.OutOrStdout())
	dataDir := filepath.Join(root, ".amanmcp")

	// Load configuration
	cfg, err := config.Load(root)
	if err != nil {
		cfg = config.NewConfig()
	}

	// Initialize stores
	metadataPath := filepath.Join(dataDir, "metadata.db")
	metadata, err := store.NewSQLiteStore(metadataPath)
	if err != nil {
		return fmt.Errorf("failed to open metadata: %w", err)
	}
	defer func() { _ = metadata.Close() }()

	// Use factory for BM25 backend selection (SQLite default for concurrent access)
	bm25BasePath := filepath.Join(dataDir, "bm25")
	bm25Config := store.DefaultBM25Config()
	bm25, err := store.NewBM25IndexWithBackend(bm25BasePath, bm25Config, cfg.Search.BM25Backend)
	if err != nil {
		return fmt.Errorf("failed to open BM25 index: %w", err)
	}
	defer func() { _ = bm25.Close() }()

	// Check existing vector store dimensions
	vectorPath := filepath.Join(dataDir, "vectors.hnsw")
	existingDims, err := store.ReadHNSWStoreDimensions(vectorPath)
	if err != nil {
		slog.Debug("Could not read vector dimensions", slog.String("error", err.Error()))
		existingDims = 0
	}

	// BUG-073: Only create embedder when not using --bm25-only
	var embedder embed.Embedder
	var dimensions int

	if opts.bm25Only {
		// Use static embedder for BM25-only mode (no network calls needed)
		embedder = embed.NewStaticEmbedder768()
		dimensions = embedder.Dimensions()
		slog.Debug("bm25_only_mode", slog.Int("dimensions", dimensions))
	} else {
		// Wire MLX config from config.yaml to embedder factory
		embed.SetMLXConfig(embed.MLXServerConfig{
			Endpoint: cfg.Embeddings.MLXEndpoint,
			Model:    cfg.Embeddings.MLXModel,
		})

		// Use config-based embedder selection (same as index command) - fixes BUG-039
		provider := embed.ParseProvider(cfg.Embeddings.Provider)
		embedder, err = embed.NewEmbedder(ctx, provider, cfg.Embeddings.Model)
		if err != nil {
			return fmt.Errorf("failed to create embedder: %w", err)
		}
		dimensions = embedder.Dimensions()
		slog.Debug("embedder_initialized",
			slog.String("provider", provider.String()),
			slog.String("model", embedder.ModelName()),
			slog.Int("dimensions", dimensions),
			slog.Int("existing_dims", existingDims))
	}
	defer func() { _ = embedder.Close() }()
	vectorConfig := store.DefaultVectorStoreConfig(dimensions)
	vector, err := store.NewHNSWStore(vectorConfig)
	if err != nil {
		return fmt.Errorf("failed to create vector store: %w", err)
	}
	defer func() { _ = vector.Close() }()

	// Try to load vectors
	if _, err := os.Stat(vectorPath); err == nil {
		if loadErr := vector.Load(vectorPath); loadErr != nil {
			slog.Debug("vector_load_failed", slog.String("error", loadErr.Error()))
		}
	}

	// Create search engine with defaults
	engineConfig := search.DefaultConfig()
	if cfg.Search.MaxResults > 0 {
		engineConfig.DefaultLimit = cfg.Search.MaxResults
	}
	if cfg.Search.BM25Weight > 0 || cfg.Search.SemanticWeight > 0 {
		engineConfig.DefaultWeights = search.Weights{
			BM25:     cfg.Search.BM25Weight,
			Semantic: cfg.Search.SemanticWeight,
		}
	}
	// FEAT-QI3: Add multi-query decomposition for generic queries
	engine := search.New(bm25, vector, embedder, metadata, engineConfig,
		search.WithMultiQuerySearch(search.NewPatternDecomposer()))

	// Build search options
	searchOpts := search.SearchOptions{
		Limit:    opts.limit,
		Filter:   opts.filter,
		Language: opts.language,
		Scopes:   opts.scopes,
		BM25Only: opts.bm25Only,
		Explain:  opts.explain, // FEAT-UNIX3
	}

	// Execute search
	results, err := engine.Search(ctx, query, searchOpts)
	if err != nil {
		return fmt.Errorf("search failed: %w", err)
	}
	slog.Info("search_complete", slog.String("mode", "local"), slog.Int("results", len(results)))

	// Format and output results
	if len(results) == 0 {
		out.Status("", fmt.Sprintf("No results found for %q", query))
		return nil
	}

	switch opts.format {
	case "json":
		return formatJSON(cmd, results)
	default:
		return formatText(out, query, results)
	}
}

// formatDaemonResults formats search results from daemon.
func formatDaemonResults(cmd *cobra.Command, out *output.Writer, query string, results []daemon.SearchResult, format string) error {
	if len(results) == 0 {
		out.Status("", fmt.Sprintf("No results found for %q", query))
		return nil
	}

	switch format {
	case "json":
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(results)
	default:
		// FEAT-UNIX3: Show explain header if first result has explain data
		if len(results) > 0 && results[0].Explain != nil {
			formatDaemonExplainHeader(out, results[0].Explain)
		}

		out.Statusf("🔍", "Found %d results for %q:", len(results), query)
		out.Newline()

		hasExplain := len(results) > 0 && results[0].Explain != nil
		for i, r := range results {
			location := r.FilePath
			if r.StartLine > 0 {
				location = fmt.Sprintf("%s:%d", r.FilePath, r.StartLine)
			}

			// FEAT-UNIX3: Include BM25/Vector ranks in explain mode
			if hasExplain {
				out.Statusf("", "%d. %s (score: %.3f)", i+1, location, r.Score)
				out.Status("", fmt.Sprintf("      BM25: rank %d (score: %.3f) | Vector: rank %d (score: %.3f)",
					r.BM25Rank, r.BM25Score, r.VecRank, r.VecScore))
			} else {
				out.Statusf("", "%d. %s (score: %.2f)", i+1, location, r.Score)
			}

			// Show snippet (first 3 lines)
			snippet := getSnippet(r.Content, 3)
			for _, line := range snippet {
				out.Status("", "   "+line)
			}
			out.Newline()
		}
		return nil
	}
}

// formatDaemonExplainHeader outputs the explain summary for daemon results.
// FEAT-UNIX3: Implements Unix Rule of Transparency for search debugging.
func formatDaemonExplainHeader(out *output.Writer, explain *daemon.ExplainData) {
	out.Status("", "════════════════════════════════════════")
	out.Status("", "SEARCH EXPLANATION")
	out.Status("", "════════════════════════════════════════")
	out.Status("", fmt.Sprintf("Query: %q", explain.Query))
	out.Newline()

	// Show search mode
	if explain.BM25Only {
		out.Status("", "Mode: BM25-only (--bm25-only flag)")
	} else if explain.DimensionMismatch {
		out.Status("", "Mode: BM25-only (dimension mismatch - run 'amanmcp reindex --force')")
	} else if explain.MultiQueryDecomposed {
		out.Status("", "Mode: Multi-query decomposition")
		out.Status("", "Sub-queries:")
		for _, sq := range explain.SubQueries {
			out.Status("", fmt.Sprintf("  - %q", sq))
		}
	} else {
		out.Status("", "Mode: Hybrid (BM25 + Vector)")
	}
	out.Newline()

	// Show result counts and weights
	out.Status("", fmt.Sprintf("BM25 Results: %d (weight: %.2f)", explain.BM25ResultCount, explain.BM25Weight))
	out.Status("", fmt.Sprintf("Vector Results: %d (weight: %.2f)", explain.VectorResultCount, explain.SemanticWeight))
	out.Status("", fmt.Sprintf("RRF Constant: k=%d", explain.RRFConstant))
	out.Status("", "════════════════════════════════════════")
	out.Newline()
}

// formatText outputs results in human-readable format.
func formatText(out *output.Writer, query string, results []*search.SearchResult) error {
	// FEAT-UNIX3: Show explain header if first result has explain data
	if len(results) > 0 && results[0].Explain != nil {
		formatExplainHeader(out, results[0].Explain)
	}

	out.Statusf("🔍", "Found %d results for %q:", len(results), query)
	out.Newline()

	for i, r := range results {
		if r.Chunk == nil {
			continue
		}

		// Format: 1. path/to/file.go:42 (score: 0.89)
		location := r.Chunk.FilePath
		if r.Chunk.StartLine > 0 {
			location = fmt.Sprintf("%s:%d", r.Chunk.FilePath, r.Chunk.StartLine)
		}

		// FEAT-UNIX3: Include BM25/Vector ranks in explain mode
		if results[0].Explain != nil {
			out.Statusf("", "%d. %s (score: %.3f)", i+1, location, r.Score)
			out.Status("", fmt.Sprintf("      BM25: rank %d (score: %.3f) | Vector: rank %d (score: %.3f)",
				r.BM25Rank, r.BM25Score, r.VecRank, r.VecScore))
		} else {
			out.Statusf("", "%d. %s (score: %.2f)", i+1, location, r.Score)
		}

		// Show snippet (first 3 lines)
		snippet := getSnippet(r.Chunk.Content, 3)
		for _, line := range snippet {
			out.Status("", "   "+line)
		}
		out.Newline()
	}

	return nil
}

// formatExplainHeader outputs the explain summary for a search.
// FEAT-UNIX3: Implements Unix Rule of Transparency for search debugging.
func formatExplainHeader(out *output.Writer, explain *search.ExplainData) {
	out.Status("", "════════════════════════════════════════")
	out.Status("", "SEARCH EXPLANATION")
	out.Status("", "════════════════════════════════════════")
	out.Status("", fmt.Sprintf("Query: %q", explain.Query))
	out.Newline()

	// Show search mode
	if explain.BM25Only {
		out.Status("", "Mode: BM25-only (--bm25-only flag)")
	} else if explain.DimensionMismatch {
		out.Status("", "Mode: BM25-only (dimension mismatch - run 'amanmcp reindex --force')")
	} else if explain.MultiQueryDecomposed {
		out.Status("", "Mode: Multi-query decomposition")
		out.Status("", "Sub-queries:")
		for _, sq := range explain.SubQueries {
			out.Status("", fmt.Sprintf("  - %q", sq))
		}
	} else {
		out.Status("", "Mode: Hybrid (BM25 + Vector)")
	}
	out.Newline()

	// Show result counts and weights
	out.Status("", fmt.Sprintf("BM25 Results: %d (weight: %.2f)", explain.BM25ResultCount, explain.Weights.BM25))
	out.Status("", fmt.Sprintf("Vector Results: %d (weight: %.2f)", explain.VectorResultCount, explain.Weights.Semantic))
	out.Status("", fmt.Sprintf("RRF Constant: k=%d", explain.RRFConstant))
	out.Status("", "════════════════════════════════════════")
	out.Newline()
}

// formatJSON outputs results in JSON format.
func formatJSON(cmd *cobra.Command, results []*search.SearchResult) error {
	type jsonResult struct {
		FilePath  string  `json:"file_path"`
		StartLine int     `json:"start_line"`
		EndLine   int     `json:"end_line"`
		Score     float64 `json:"score"`
		Content   string  `json:"content"`
		Language  string  `json:"language,omitempty"`
	}

	var output []jsonResult
	for _, r := range results {
		if r.Chunk == nil {
			continue
		}
		output = append(output, jsonResult{
			FilePath:  r.Chunk.FilePath,
			StartLine: r.Chunk.StartLine,
			EndLine:   r.Chunk.EndLine,
			Score:     r.Score,
			Content:   r.Chunk.Content,
			Language:  r.Chunk.Language,
		})
	}

	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}

// getSnippet returns the first n lines of content.
func getSnippet(content string, n int) []string {
	lines := strings.Split(content, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	// Trim trailing empty lines
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
