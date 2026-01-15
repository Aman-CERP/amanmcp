package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Aman-CERP/amanmcp/internal/async"
	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/embed"
	"github.com/Aman-CERP/amanmcp/internal/search"
	"github.com/Aman-CERP/amanmcp/internal/store"
	"github.com/Aman-CERP/amanmcp/internal/telemetry"
	"github.com/Aman-CERP/amanmcp/pkg/version"
)

// Server is the MCP server for AmanMCP.
// It bridges AI clients (Claude Code, Cursor) with the hybrid search engine.
type Server struct {
	mcp      *mcp.Server
	engine   search.SearchEngine
	metadata store.MetadataStore
	embedder embed.Embedder // Embedder for capability signaling
	config   *config.Config
	logger   *slog.Logger

	// Project identification for resource operations
	projectID string
	rootPath  string

	// Background indexing progress (nil if not indexing)
	indexProgress *async.IndexProgress

	// Query telemetry (optional, set via SetMetrics)
	metrics *telemetry.QueryMetrics

	mu sync.RWMutex
}

// ToolInfo contains information about a registered tool.
type ToolInfo struct {
	Name        string
	Description string
}

// ResourceInfo contains information about a resource.
type ResourceInfo struct {
	URI      string
	Name     string
	MIMEType string
}

// ResourceContent contains the content of a resource.
type ResourceContent struct {
	URI      string
	Content  string
	MIMEType string
}

// SearchInput defines the input schema for the search tool.
type SearchInput struct {
	Query    string   `json:"query" jsonschema:"the search query to execute"`
	Limit    int      `json:"limit,omitempty" jsonschema:"maximum number of results, default 10"`
	Filter   string   `json:"filter,omitempty" jsonschema:"filter by content type: all, code, docs"`
	Language string   `json:"language,omitempty" jsonschema:"filter by programming language, e.g. go, typescript"`
	Scope    []string `json:"scope,omitempty" jsonschema:"filter by path prefixes (OR logic)"`
}

// SearchOutput defines the output schema for the search tool.
type SearchOutput struct {
	Results []SearchResultOutput `json:"results" jsonschema:"list of search results"`
}

// SearchResultOutput defines a single search result with context-rich metadata.
// UX-1: Enhanced response format explaining WHY results matched.
type SearchResultOutput struct {
	FilePath     string   `json:"file_path" jsonschema:"file path relative to project root"`
	Content      string   `json:"content" jsonschema:"matched content snippet"`
	Score        float64  `json:"score" jsonschema:"relevance score between 0 and 1"`
	Language     string   `json:"language,omitempty" jsonschema:"programming language of the file"`
	MatchReason  string   `json:"match_reason,omitempty" jsonschema:"human-readable explanation of why this result matched"`
	Symbol       string   `json:"symbol,omitempty" jsonschema:"primary symbol name (function, class, type)"`
	SymbolType   string   `json:"symbol_type,omitempty" jsonschema:"type of symbol: function, class, interface, type, method"`
	Signature    string   `json:"signature,omitempty" jsonschema:"full function/method signature"`
	MatchedTerms []string `json:"matched_terms,omitempty" jsonschema:"query terms that matched this result"`
	InBothLists  bool     `json:"in_both_lists,omitempty" jsonschema:"true if result appeared in both keyword and semantic search"`
}

// NewServer creates a new MCP server.
// The embedder parameter is used for capability signaling - AI clients can query
// the actual embedder state to adjust their search strategies.
// rootPath is used for project detection (go.mod, package.json, etc.).
func NewServer(engine search.SearchEngine, metadata store.MetadataStore, embedder embed.Embedder, cfg *config.Config, rootPath string) (*Server, error) {
	if engine == nil {
		return nil, errors.New("search engine is required")
	}
	if metadata == nil {
		return nil, errors.New("metadata store is required")
	}
	if cfg == nil {
		cfg = config.NewConfig()
	}

	s := &Server{
		engine:   engine,
		metadata: metadata,
		embedder: embedder, // May be nil - will report as unavailable
		config:   cfg,
		rootPath: rootPath,
		logger:   slog.Default(),
	}

	// Create MCP server with implementation info
	s.mcp = mcp.NewServer(
		&mcp.Implementation{
			Name:    "AmanMCP",
			Version: version.Version,
		},
		nil, // ServerOptions - capabilities are inferred from registered tools/resources
	)

	// Register tools
	s.registerTools()

	return s, nil
}

// SetIndexProgress sets the index progress tracker for background indexing.
// This enables the server to report indexing progress via index_status and
// return appropriate messages when search is called during indexing.
func (s *Server) SetIndexProgress(progress *async.IndexProgress) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.indexProgress = progress
}

// SetMetrics sets the query metrics collector for telemetry.
// When set, a query_metrics resource is registered.
func (s *Server) SetMetrics(m *telemetry.QueryMetrics) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metrics = m

	// Register query_metrics resource if metrics is provided
	if m != nil {
		s.registerQueryMetricsResource()
	}
}

// MCPServer returns the underlying MCP server instance.
func (s *Server) MCPServer() *mcp.Server {
	return s.mcp
}

// Info returns the server name and version.
func (s *Server) Info() (name, ver string) {
	return "AmanMCP", version.Version
}

// Capabilities returns whether tools and resources are enabled.
func (s *Server) Capabilities() (hasTools, hasResources bool) {
	// Both are enabled for F16
	return true, true
}

// ListTools returns all registered tools.
func (s *Server) ListTools() []ToolInfo {
	// Return the tools we register
	// QW-3: Enhanced descriptions to explain WHY amanmcp > grep
	return []ToolInfo{
		{
			Name:        "search",
			Description: "Primary search tool. Instantly finds code and documentation using a full-codebase index. Use this for 95% of your search tasks - faster and smarter than grep. Understands code semantics, not just keywords.",
		},
		{
			Name:        "search_code",
			Description: "Code-specialized search. Finds functions, classes, and implementations by meaning, not just text matching. Use when you need to understand HOW something is implemented. Supports language and symbol type filtering.",
		},
		{
			Name:        "search_docs",
			Description: "Documentation search with context. Finds architecture decisions, design rationale, and guides. Preserves section hierarchy so you understand WHERE in the doc structure a match appears.",
		},
		{
			Name:        "index_status",
			Description: "Check if the codebase index is ready and which embedder is active. Use before searching to verify the index is complete.",
		},
	}
}

// CallTool invokes a tool by name with the given arguments.
func (s *Server) CallTool(ctx context.Context, name string, args map[string]any) (any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	switch name {
	case "search":
		return s.handleSearchTool(ctx, args)
	case "search_code":
		return s.handleSearchCodeTool(ctx, args)
	case "search_docs":
		return s.handleSearchDocsTool(ctx, args)
	case "index_status":
		return s.handleIndexStatusTool(ctx, args)
	default:
		return nil, NewMethodNotFoundError(name)
	}
}

// handleSearchTool handles the search tool invocation.
// Returns markdown-formatted results.
func (s *Server) handleSearchTool(ctx context.Context, args map[string]any) (string, error) {
	// Check if indexing is in progress
	s.mu.RLock()
	progress := s.indexProgress
	s.mu.RUnlock()

	if progress != nil && progress.IsIndexing() {
		snap := progress.Snapshot()
		return fmt.Sprintf("## Indexing in Progress\n\n"+
			"**Progress:** %.1f%% (%d/%d files)\n"+
			"**Stage:** %s\n\n"+
			"Search results may be incomplete or unavailable. Please try again in a moment.",
			snap.ProgressPct, snap.FilesProcessed, snap.FilesTotal, snap.Stage), nil
	}

	start := time.Now()
	requestID := generateRequestID()

	// Extract and validate query
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return "", NewInvalidParamsError("query parameter is required and must be a non-empty string")
	}

	// Validate query is not just whitespace (DEBT-019)
	if strings.TrimSpace(query) == "" {
		return "", NewInvalidParamsError("query cannot be empty or whitespace only")
	}

	// Extract optional parameters with limit clamping
	limit := clampLimit(0, 10, 1, 50) // default 10
	if l, ok := args["limit"].(float64); ok {
		limit = clampLimit(int(l), 10, 1, 50)
	}

	s.logger.Info("search started",
		slog.String("request_id", requestID),
		slog.String("query", query),
		slog.Int("limit", limit))

	opts := search.SearchOptions{
		Limit: limit,
	}

	if filter, ok := args["filter"].(string); ok {
		opts.Filter = filter
	}
	if lang, ok := args["language"].(string); ok {
		opts.Language = lang
	}
	if scope, ok := args["scope"].([]interface{}); ok {
		for _, s := range scope {
			if str, ok := s.(string); ok {
				opts.Scopes = append(opts.Scopes, str)
			}
		}
	}

	// Execute search
	results, err := s.engine.Search(ctx, query, opts)
	duration := time.Since(start)

	if err != nil {
		s.logger.Error("search failed",
			slog.String("request_id", requestID),
			slog.Duration("duration", duration),
			slog.String("error", err.Error()))
		return "", MapError(err)
	}

	s.logger.Info("search completed",
		slog.String("request_id", requestID),
		slog.Duration("duration", duration),
		slog.Int("result_count", len(results)))

	// Format as markdown
	return FormatSearchResults(query, results), nil
}

// handleSearchCodeTool handles the search_code tool invocation.
// Returns markdown-formatted code results with language and symbol filtering.
func (s *Server) handleSearchCodeTool(ctx context.Context, args map[string]any) (string, error) {
	start := time.Now()
	requestID := generateRequestID()

	// Extract and validate query
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return "", NewInvalidParamsError("query parameter is required and must be a non-empty string")
	}

	// Extract optional parameters with limit clamping
	limit := clampLimit(0, 10, 1, 50) // default 10
	if l, ok := args["limit"].(float64); ok {
		limit = clampLimit(int(l), 10, 1, 50)
	}

	s.logger.Info("search_code started",
		slog.String("request_id", requestID),
		slog.String("query", query),
		slog.Int("limit", limit))

	opts := search.SearchOptions{
		Limit:  limit,
		Filter: "code", // Always filter to code
	}

	// Language filter
	var langFilter string
	if lang, ok := args["language"].(string); ok {
		opts.Language = lang
		langFilter = lang
	}

	// Symbol type filter
	if symbolType, ok := args["symbol_type"].(string); ok {
		if symbolType != "any" {
			opts.SymbolType = symbolType
		}
	}

	// Scope filter
	if scope, ok := args["scope"].([]interface{}); ok {
		for _, s := range scope {
			if str, ok := s.(string); ok {
				opts.Scopes = append(opts.Scopes, str)
			}
		}
	}

	// Execute search
	results, err := s.engine.Search(ctx, query, opts)
	duration := time.Since(start)

	if err != nil {
		s.logger.Error("search_code failed",
			slog.String("request_id", requestID),
			slog.Duration("duration", duration),
			slog.String("error", err.Error()))
		return "", MapError(err)
	}

	s.logger.Info("search_code completed",
		slog.String("request_id", requestID),
		slog.Duration("duration", duration),
		slog.Int("result_count", len(results)))

	// Format as markdown
	return FormatCodeResults(query, results, langFilter), nil
}

// handleSearchDocsTool handles the search_docs tool invocation.
// Returns markdown-formatted documentation results.
func (s *Server) handleSearchDocsTool(ctx context.Context, args map[string]any) (string, error) {
	start := time.Now()
	requestID := generateRequestID()

	// Extract and validate query
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return "", NewInvalidParamsError("query parameter is required and must be a non-empty string")
	}

	// Extract optional parameters with limit clamping
	limit := clampLimit(0, 10, 1, 50) // default 10
	if l, ok := args["limit"].(float64); ok {
		limit = clampLimit(int(l), 10, 1, 50)
	}

	s.logger.Info("search_docs started",
		slog.String("request_id", requestID),
		slog.String("query", query),
		slog.Int("limit", limit))

	opts := search.SearchOptions{
		Limit:  limit,
		Filter: "docs", // Always filter to docs
	}

	// Scope filter
	if scope, ok := args["scope"].([]interface{}); ok {
		for _, s := range scope {
			if str, ok := s.(string); ok {
				opts.Scopes = append(opts.Scopes, str)
			}
		}
	}

	// Execute search
	results, err := s.engine.Search(ctx, query, opts)
	duration := time.Since(start)

	if err != nil {
		s.logger.Error("search_docs failed",
			slog.String("request_id", requestID),
			slog.Duration("duration", duration),
			slog.String("error", err.Error()))
		return "", MapError(err)
	}

	s.logger.Info("search_docs completed",
		slog.String("request_id", requestID),
		slog.Duration("duration", duration),
		slog.Int("result_count", len(results)))

	// Format as markdown
	return FormatDocsResults(query, results), nil
}

// handleIndexStatusTool handles the index_status tool invocation.
// Returns JSON-formatted index statistics including embedder capability info.
// AI clients can use this to adjust their search strategies based on
// whether Hugot (high quality semantic) or static (lower quality) embeddings are active.
func (s *Server) handleIndexStatusTool(ctx context.Context, _ map[string]any) (*IndexStatusOutput, error) {
	start := time.Now()
	requestID := generateRequestID()

	s.logger.Info("index_status started",
		slog.String("request_id", requestID))

	stats := s.engine.Stats()

	// Determine embedder capability state
	var actualProvider, actualModel, semanticQuality, status string
	var dimensions int
	var isFallbackActive bool

	if s.embedder != nil {
		actualModel = s.embedder.ModelName()
		dimensions = s.embedder.Dimensions()

		// Determine if using static fallback based on model name or dimensions
		isFallbackActive = actualModel == "static" || dimensions == embed.StaticDimensions

		if isFallbackActive {
			actualProvider = "static"
			semanticQuality = "low"
		} else {
			actualProvider = "hugot"
			semanticQuality = "high"
		}

		// Check runtime availability
		if s.embedder.Available(ctx) {
			status = "ready"
		} else {
			status = "unavailable"
		}
	} else {
		// No embedder configured
		actualProvider = "none"
		actualModel = "none"
		dimensions = 0
		isFallbackActive = true
		semanticQuality = "none"
		status = "unavailable"
	}

	// Detect project info
	detector := NewProjectDetector(s.rootPath, s.logger)
	projectInfo := detector.Detect()

	// Build output
	output := &IndexStatusOutput{
		Project: *projectInfo,
		Stats: IndexStats{
			FileCount:      0,
			ChunkCount:     0,
			IndexSizeBytes: 0,
			LastIndexed:    time.Now().Format(time.RFC3339),
		},
		Embeddings: EmbeddingInfo{
			// Config values
			Provider: s.config.Embeddings.Provider,
			Model:    s.config.Embeddings.Model,
			Status:   status,
			// Runtime state - AI clients use this to adjust search strategy
			ActualProvider:   actualProvider,
			ActualModel:      actualModel,
			Dimensions:       dimensions,
			IsFallbackActive: isFallbackActive,
			SemanticQuality:  semanticQuality,
		},
	}

	// Fill in stats if available
	if stats != nil {
		if stats.BM25Stats != nil {
			output.Stats.FileCount = stats.BM25Stats.DocumentCount
		}
		output.Stats.ChunkCount = stats.VectorCount
	}

	// Add indexing progress if available
	s.mu.RLock()
	progress := s.indexProgress
	s.mu.RUnlock()

	if progress != nil {
		snap := progress.Snapshot()
		output.Indexing = &IndexingProgress{
			Status:         snap.Status,
			Stage:          snap.Stage,
			FilesTotal:     snap.FilesTotal,
			FilesProcessed: snap.FilesProcessed,
			ChunksIndexed:  snap.ChunksIndexed,
			ProgressPct:    snap.ProgressPct,
			ElapsedSeconds: snap.ElapsedSeconds,
			ErrorMessage:   snap.ErrorMessage,
		}
	}

	duration := time.Since(start)
	s.logger.Info("index_status completed",
		slog.String("request_id", requestID),
		slog.Duration("duration", duration),
		slog.String("project_name", projectInfo.Name),
		slog.String("project_type", projectInfo.Type))

	return output, nil
}

// registerTools registers all tools with the MCP server.
// BUG-033: Added logging for debugging tool registration issues.
func (s *Server) registerTools() {
	s.logger.Debug("Registering MCP tools")

	// Register search tool - generic hybrid search
	// QW-3: Enhanced descriptions to explain WHY amanmcp > grep
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "search",
		Description: "Primary search tool. Instantly finds code and documentation using a full-codebase index. Use this for 95% of your search tasks - faster and smarter than grep. Understands code semantics, not just keywords.",
	}, s.mcpSearchHandler)
	s.logger.Debug("Registered tool", slog.String("name", "search"))

	// Register search_code tool - code-specific search
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "search_code",
		Description: "Code-specialized search. Finds functions, classes, and implementations by meaning, not just text matching. Use when you need to understand HOW something is implemented. Supports language and symbol type filtering.",
	}, s.mcpSearchCodeHandler)
	s.logger.Debug("Registered tool", slog.String("name", "search_code"))

	// Register search_docs tool - documentation search
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "search_docs",
		Description: "Documentation search with context. Finds architecture decisions, design rationale, and guides. Preserves section hierarchy so you understand WHERE in the doc structure a match appears.",
	}, s.mcpSearchDocsHandler)
	s.logger.Debug("Registered tool", slog.String("name", "search_docs"))

	// Register index_status tool - index diagnostics
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "index_status",
		Description: "Check if the codebase index is ready and which embedder is active. Use before searching to verify the index is complete.",
	}, s.mcpIndexStatusHandler)
	s.logger.Debug("Registered tool", slog.String("name", "index_status"))

	s.logger.Info("MCP tools registered", slog.Int("count", 4))
}

// mcpSearchHandler is the MCP SDK handler for the search tool.
func (s *Server) mcpSearchHandler(ctx context.Context, req *mcp.CallToolRequest, input SearchInput) (
	*mcp.CallToolResult,
	SearchOutput,
	error,
) {
	// Validate query
	if input.Query == "" {
		return nil, SearchOutput{}, NewInvalidParamsError("query parameter is required")
	}

	// Build search options
	opts := search.SearchOptions{
		Limit:    10,
		Filter:   input.Filter,
		Language: input.Language,
		Scopes:   input.Scope,
	}
	if input.Limit > 0 {
		opts.Limit = input.Limit
	}

	// Execute search
	results, err := s.engine.Search(ctx, input.Query, opts)
	if err != nil {
		return nil, SearchOutput{}, MapError(err)
	}

	// Convert to output format with enhanced context (UX-1)
	output := SearchOutput{
		Results: make([]SearchResultOutput, 0, len(results)),
	}

	for _, r := range results {
		if r.Chunk != nil {
			output.Results = append(output.Results, ToSearchResultOutput(r))
		}
	}

	return nil, output, nil
}

// mcpSearchCodeHandler is the MCP SDK handler for the search_code tool.
func (s *Server) mcpSearchCodeHandler(ctx context.Context, _ *mcp.CallToolRequest, input SearchCodeInput) (
	*mcp.CallToolResult,
	SearchOutput,
	error,
) {
	// Validate query
	if input.Query == "" {
		return nil, SearchOutput{}, NewInvalidParamsError("query parameter is required")
	}

	// Build search options
	opts := search.SearchOptions{
		Limit:    10,
		Filter:   "code", // Always filter to code
		Language: input.Language,
		Scopes:   input.Scope,
	}
	if input.Limit > 0 {
		opts.Limit = input.Limit
	}
	if input.SymbolType != "" && input.SymbolType != "any" {
		opts.SymbolType = input.SymbolType
	}

	// Execute search
	results, err := s.engine.Search(ctx, input.Query, opts)
	if err != nil {
		return nil, SearchOutput{}, MapError(err)
	}

	// Convert to output format with enhanced context (UX-1)
	output := SearchOutput{
		Results: make([]SearchResultOutput, 0, len(results)),
	}

	for _, r := range results {
		if r.Chunk != nil {
			output.Results = append(output.Results, ToSearchResultOutput(r))
		}
	}

	return nil, output, nil
}

// mcpSearchDocsHandler is the MCP SDK handler for the search_docs tool.
func (s *Server) mcpSearchDocsHandler(ctx context.Context, _ *mcp.CallToolRequest, input SearchDocsInput) (
	*mcp.CallToolResult,
	SearchOutput,
	error,
) {
	// Validate query
	if input.Query == "" {
		return nil, SearchOutput{}, NewInvalidParamsError("query parameter is required")
	}

	// Build search options
	opts := search.SearchOptions{
		Limit:  10,
		Filter: "docs", // Always filter to docs
		Scopes: input.Scope,
	}
	if input.Limit > 0 {
		opts.Limit = input.Limit
	}

	// Execute search
	results, err := s.engine.Search(ctx, input.Query, opts)
	if err != nil {
		return nil, SearchOutput{}, MapError(err)
	}

	// Convert to output format with enhanced context (UX-1)
	output := SearchOutput{
		Results: make([]SearchResultOutput, 0, len(results)),
	}

	for _, r := range results {
		if r.Chunk != nil {
			output.Results = append(output.Results, ToSearchResultOutput(r))
		}
	}

	return nil, output, nil
}

// mcpIndexStatusHandler is the MCP SDK handler for the index_status tool.
func (s *Server) mcpIndexStatusHandler(ctx context.Context, _ *mcp.CallToolRequest, _ IndexStatusInput) (
	*mcp.CallToolResult,
	*IndexStatusOutput,
	error,
) {
	output, err := s.handleIndexStatusTool(ctx, nil)
	if err != nil {
		return nil, nil, MapError(err)
	}
	return nil, output, nil
}

// ListResources returns all available resources.
func (s *Server) ListResources(ctx context.Context, cursor string) ([]ResourceInfo, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Get files from metadata store
	files, err := s.metadata.GetChangedFiles(ctx, "", emptyTime)
	if err != nil {
		return nil, "", err
	}

	resources := make([]ResourceInfo, 0, len(files))
	for _, f := range files {
		resources = append(resources, ResourceInfo{
			URI:      fmt.Sprintf("file://%s", f.Path),
			Name:     f.Path,
			MIMEType: mimeTypeForLanguage(f.Language),
		})
	}

	return resources, "", nil // No pagination for now
}

// ReadResource reads a resource by URI.
func (s *Server) ReadResource(ctx context.Context, uri string) (*ResourceContent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Parse URI - support chunk:// and file:// schemes
	var chunkID string
	if strings.HasPrefix(uri, "chunk://") {
		chunkID = strings.TrimPrefix(uri, "chunk://")
	} else if strings.HasPrefix(uri, "file://") {
		// For file:// URIs, we'd need to look up the file
		// For now, return not found
		return nil, NewResourceNotFoundError(uri)
	} else {
		return nil, NewResourceNotFoundError(uri)
	}

	// Get chunk from metadata store
	chunk, err := s.metadata.GetChunk(ctx, chunkID)
	if err != nil {
		return nil, err
	}
	if chunk == nil {
		return nil, NewResourceNotFoundError(uri)
	}

	return &ResourceContent{
		URI:      uri,
		Content:  chunk.Content,
		MIMEType: mimeTypeForLanguage(chunk.Language),
	}, nil
}

// Serve starts the server with the specified transport.
func (s *Server) Serve(ctx context.Context, transport, addr string) error {
	s.logger.Info("Starting MCP server",
		slog.String("transport", transport),
		slog.String("addr", addr))

	switch transport {
	case "stdio":
		s.logger.Debug("Using stdio transport for JSON-RPC")
		err := s.mcp.Run(ctx, &mcp.StdioTransport{})
		if err != nil && err != context.Canceled {
			s.logger.Error("MCP server stopped with error",
				slog.String("error", err.Error()))
		} else {
			s.logger.Info("MCP server stopped gracefully")
		}
		return err
	case "sse":
		// SSE transport not yet implemented in SDK
		return fmt.Errorf("SSE transport not yet implemented")
	default:
		return fmt.Errorf("unknown transport: %s (supported: stdio)", transport)
	}
}

// Close releases server resources.
func (s *Server) Close() error {
	// The MCP server doesn't have a Close method - it stops when context is canceled
	return nil
}

// mimeTypeForLanguage returns the MIME type for a programming language.
func mimeTypeForLanguage(lang string) string {
	switch strings.ToLower(lang) {
	case "go":
		return "text/x-go"
	case "typescript", "ts":
		return "text/typescript"
	case "javascript", "js":
		return "text/javascript"
	case "python", "py":
		return "text/x-python"
	case "rust", "rs":
		return "text/x-rust"
	case "java":
		return "text/x-java"
	case "c":
		return "text/x-c"
	case "cpp", "c++":
		return "text/x-c++"
	case "markdown", "md":
		return "text/markdown"
	default:
		return "text/plain"
	}
}

// emptyTime is a zero time value for listing all files.
var emptyTime = time.Time{}

// generateRequestID creates a short unique request ID for log correlation.
func generateRequestID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
