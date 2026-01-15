package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Aman-CERP/amanmcp/internal/store"
)

// MaxResourceSize is the maximum file size for resources (1MB).
const MaxResourceSize = 1024 * 1024

// RegisterResources loads indexed files and registers them as MCP resources.
// This should be called after the server is created and before serving.
func (s *Server) RegisterResources(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.projectID == "" || s.rootPath == "" {
		return fmt.Errorf("projectID and rootPath must be set before registering resources")
	}

	// Load all indexed files
	files, _, err := s.metadata.ListFiles(ctx, s.projectID, "", 10000)
	if err != nil {
		return fmt.Errorf("failed to list files: %w", err)
	}

	// Register each file as a resource
	for _, f := range files {
		s.registerFileResource(f)
	}

	s.logger.Info("registered resources", "count", len(files))
	return nil
}

// registerFileResource registers a single file as an MCP resource.
func (s *Server) registerFileResource(f *store.File) {
	uri := fmt.Sprintf("file://%s", f.Path)
	s.mcp.AddResource(
		&mcp.Resource{
			Name:        filepath.Base(f.Path),
			URI:         uri,
			Description: fmt.Sprintf("%s (%s)", f.Path, humanSize(f.Size)),
			MIMEType:    MimeTypeForPath(f.Path),
		},
		s.makeFileHandler(f.Path),
	)
}

// makeFileHandler creates a read handler for a specific file path.
func (s *Server) makeFileHandler(path string) mcp.ResourceHandler {
	return func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return s.handleReadResource(ctx, path)
	}
}

// handleReadResource reads file content with security validation.
// Returns the file content or an error.
func (s *Server) handleReadResource(ctx context.Context, relativePath string) (*mcp.ReadResourceResult, error) {
	// Validate path security
	if !s.isValidPath(relativePath) {
		return nil, NewInvalidParamsError(fmt.Sprintf("invalid path: %s", relativePath))
	}

	// Check if file is indexed
	file, err := s.metadata.GetFileByPath(ctx, s.projectID, relativePath)
	if err != nil {
		return nil, MapError(err)
	}
	if file == nil {
		return nil, NewInvalidParamsError(fmt.Sprintf("file not indexed: %s", relativePath))
	}

	// Build full path
	fullPath := filepath.Join(s.rootPath, relativePath)

	// Check file size before reading
	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &MCPError{
				Code:    ErrCodeFileNotFound,
				Message: fmt.Sprintf("file not found: %s", relativePath),
			}
		}
		return nil, MapError(err)
	}

	if info.Size() > MaxResourceSize {
		return nil, &MCPError{
			Code:    ErrCodeFileTooLarge,
			Message: fmt.Sprintf("file too large: %d bytes (max %d)", info.Size(), MaxResourceSize),
		}
	}

	// Read file content
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, MapError(err)
	}

	// Build result
	uri := fmt.Sprintf("file://%s", relativePath)
	mimeType := MimeTypeForPath(relativePath)

	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{
			{
				URI:      uri,
				MIMEType: mimeType,
				Text:     string(content),
			},
		},
	}, nil
}

// isValidPath validates that a path is safe to access.
// Returns false for path traversal attempts or absolute paths.
func (s *Server) isValidPath(path string) bool {
	if path == "" {
		return false
	}

	// Reject absolute paths
	if filepath.IsAbs(path) {
		return false
	}

	// Check for Windows absolute paths
	if len(path) >= 2 && path[1] == ':' {
		return false
	}

	// Clean the path and check for traversal
	cleaned := filepath.Clean(path)

	// After cleaning, path should not start with ".."
	if strings.HasPrefix(cleaned, "..") {
		return false
	}

	// Path should not contain ".." components
	parts := strings.Split(cleaned, string(filepath.Separator))
	for _, part := range parts {
		if part == ".." {
			return false
		}
	}

	return true
}

// humanSize formats bytes as a human-readable string.
func humanSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// QueryMetricsOutput is the JSON structure for the query_metrics resource.
type QueryMetricsOutput struct {
	Summary             QueryMetricsSummary           `json:"summary"`
	QueryTypeCounts     map[string]int64              `json:"query_type_counts"`
	TopTerms            []QueryTermCount              `json:"top_terms"`
	ZeroResultQueries   []string                      `json:"zero_result_queries"`
	LatencyDistribution map[string]int64              `json:"latency_distribution"`
}

// QueryMetricsSummary provides overview statistics.
type QueryMetricsSummary struct {
	TotalQueries     int64   `json:"total_queries"`
	TimePeriod       string  `json:"time_period"`
	ZeroResultPct    float64 `json:"zero_result_pct"`
}

// QueryTermCount represents a term and its frequency.
type QueryTermCount struct {
	Term  string `json:"term"`
	Count int64  `json:"count"`
}

// registerQueryMetricsResource registers the query_metrics resource.
func (s *Server) registerQueryMetricsResource() {
	s.mcp.AddResource(
		&mcp.Resource{
			Name:        "query_metrics",
			URI:         "amanmcp://query_metrics",
			Description: "Query pattern telemetry for search optimization",
			MIMEType:    "application/json",
		},
		s.makeQueryMetricsHandler(),
	)
}

// makeQueryMetricsHandler creates a handler for the query_metrics resource.
func (s *Server) makeQueryMetricsHandler() mcp.ResourceHandler {
	return func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		s.mu.RLock()
		metrics := s.metrics
		s.mu.RUnlock()

		if metrics == nil {
			return nil, NewInvalidParamsError("query metrics not available")
		}

		snapshot := metrics.Snapshot()

		// Convert to output format
		output := QueryMetricsOutput{
			Summary: QueryMetricsSummary{
				TotalQueries:  snapshot.TotalQueries,
				TimePeriod:    "session",
				ZeroResultPct: snapshot.ZeroResultPercentage(),
			},
			QueryTypeCounts:     make(map[string]int64),
			TopTerms:            make([]QueryTermCount, 0, len(snapshot.TopTerms)),
			ZeroResultQueries:   snapshot.ZeroResultQueries,
			LatencyDistribution: make(map[string]int64),
		}

		// Convert query type counts
		for qt, count := range snapshot.QueryTypeCounts {
			output.QueryTypeCounts[string(qt)] = count
		}

		// Convert top terms
		for _, tc := range snapshot.TopTerms {
			output.TopTerms = append(output.TopTerms, QueryTermCount{
				Term:  tc.Term,
				Count: tc.Count,
			})
		}

		// Convert latency distribution
		for bucket, count := range snapshot.LatencyDistribution {
			output.LatencyDistribution[string(bucket)] = count
		}

		// Marshal to JSON
		content, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return nil, MapError(err)
		}

		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{
				{
					URI:      "amanmcp://query_metrics",
					MIMEType: "application/json",
					Text:     string(content),
				},
			},
		}, nil
	}
}
