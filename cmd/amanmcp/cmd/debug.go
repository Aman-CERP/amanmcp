package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/logging"
	"github.com/Aman-CERP/amanmcp/internal/store"
	"github.com/Aman-CERP/amanmcp/internal/ui"
)

// DebugInfo holds all debug information for display.
type DebugInfo struct {
	// Index location
	IndexPath   string `json:"index_path"`
	ProjectRoot string `json:"project_root"`

	// Timestamps
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	IndexAge  string `json:"index_age"`

	// Files and chunks
	FileCount  int `json:"file_count"`
	ChunkCount int `json:"chunk_count"`

	// Language distribution
	Languages map[string]float64 `json:"languages"`

	// Embedder info
	EmbedderProvider   string `json:"embedder_provider"`
	EmbedderModel      string `json:"embedder_model"`
	EmbedderDimensions int    `json:"embedder_dimensions"`
	EmbedderAvailable  bool   `json:"embedder_available"`

	// BM25 index
	BM25Backend   string `json:"bm25_backend"`
	BM25Documents int    `json:"bm25_documents"`
	BM25SizeBytes int64  `json:"bm25_size_bytes"`

	// Vector store
	VectorCount     int   `json:"vector_count"`
	VectorSizeBytes int64 `json:"vector_size_bytes"`

	// Storage totals
	TotalSizeBytes    int64 `json:"total_size_bytes"`
	MemoryEstimate    int64 `json:"memory_estimate_bytes"`
	MetadataSizeBytes int64 `json:"metadata_size_bytes"`
}

func newDebugCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "debug",
		Short: "Show index debug information",
		Long: `Display detailed index statistics and system information for debugging.

Shows:
  - Index location and timestamps
  - File and chunk counts with language distribution
  - Embedder configuration and availability
  - BM25 and vector store statistics
  - Storage sizes and memory estimates`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDebug(cmd.Context(), cmd, jsonOutput)
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")

	return cmd
}

func runDebug(ctx context.Context, cmd *cobra.Command, jsonOutput bool) error {
	// Set up file-only logging (no stderr output to keep CLI clean)
	logCfg := logging.DefaultConfig()
	logCfg.WriteToStderr = false // File only
	logger, cleanup, err := logging.Setup(logCfg)
	if err == nil {
		defer cleanup()
		slog.SetDefault(logger)
	}

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

	// Collect debug info
	info, err := collectDebugInfo(ctx, root, dataDir)
	if err != nil {
		return fmt.Errorf("failed to collect debug info: %w", err)
	}

	// Log to file (always, for observability)
	slog.Info("Debug info collected",
		slog.String("index_path", info.IndexPath),
		slog.String("project_root", info.ProjectRoot),
		slog.Int("file_count", info.FileCount),
		slog.Int("chunk_count", info.ChunkCount),
		slog.String("embedder_provider", info.EmbedderProvider),
		slog.String("embedder_model", info.EmbedderModel),
		slog.Int("embedder_dimensions", info.EmbedderDimensions),
		slog.Bool("embedder_available", info.EmbedderAvailable),
		slog.String("bm25_backend", info.BM25Backend),
		slog.Int("bm25_documents", info.BM25Documents),
		slog.Int64("bm25_size_bytes", info.BM25SizeBytes),
		slog.Int("vector_count", info.VectorCount),
		slog.Int64("vector_size_bytes", info.VectorSizeBytes),
		slog.Int64("total_size_bytes", info.TotalSizeBytes),
		slog.Int64("memory_estimate_bytes", info.MemoryEstimate),
	)

	// Output
	if jsonOutput {
		return outputDebugJSON(cmd, info)
	}

	return outputDebugHuman(cmd, info)
}

func collectDebugInfo(ctx context.Context, root, dataDir string) (*DebugInfo, error) {
	info := &DebugInfo{
		IndexPath:   dataDir,
		ProjectRoot: root,
		Languages:   make(map[string]float64),
	}

	// Open metadata store
	metadataPath := filepath.Join(dataDir, "metadata.db")
	metadata, err := store.NewSQLiteStore(metadataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open metadata store: %w", err)
	}
	defer func() { _ = metadata.Close() }()

	// Get project info
	projectID := hashString(root)
	project, err := metadata.GetProject(ctx, projectID)
	if err == nil && project != nil {
		info.FileCount = project.FileCount
		info.ChunkCount = project.ChunkCount
		info.CreatedAt = store.FormatTime(project.IndexedAt)
		info.UpdatedAt = store.FormatTime(project.IndexedAt)
		info.IndexAge = formatAge(project.IndexedAt)
	}

	// Load configuration for embedder info
	cfg, err := config.Load(root)
	if err != nil {
		cfg = config.NewConfig()
	}

	info.EmbedderProvider = cfg.Embeddings.Provider
	if info.EmbedderProvider == "" {
		info.EmbedderProvider = "ollama"
	}
	info.EmbedderModel = cfg.Embeddings.Model
	if info.EmbedderModel == "" {
		info.EmbedderModel = "qwen3-embedding:0.6b"
	}

	// Get stored embedder dimensions from metadata state
	if dimStr, err := metadata.GetState(ctx, store.StateKeyIndexDimension); err == nil && dimStr != "" {
		fmt.Sscanf(dimStr, "%d", &info.EmbedderDimensions)
	}

	// Check embedder availability (simplified - check if model is set)
	info.EmbedderAvailable = info.EmbedderModel != ""

	// Determine BM25 backend
	bm25SQLitePath := filepath.Join(dataDir, "bm25.db")
	bm25BlevePath := filepath.Join(dataDir, "bm25.bleve")
	if fileExists(bm25SQLitePath) {
		info.BM25Backend = "sqlite"
		info.BM25SizeBytes = getFileSize(bm25SQLitePath)
	} else if fileExists(bm25BlevePath) {
		info.BM25Backend = "bleve"
		info.BM25SizeBytes = getDirSize(bm25BlevePath)
	}
	info.BM25Documents = info.ChunkCount // BM25 documents = chunks

	// Vector store
	vectorPath := filepath.Join(dataDir, "vectors.hnsw")
	info.VectorSizeBytes = getFileSize(vectorPath)
	info.VectorCount = info.ChunkCount // Vectors = chunks (1:1)

	// Metadata size
	info.MetadataSizeBytes = getFileSize(metadataPath)

	// Total storage
	info.TotalSizeBytes = info.MetadataSizeBytes + info.BM25SizeBytes + info.VectorSizeBytes

	// Memory estimate heuristic:
	// - HNSW: ~1.5x file size in memory (graph overhead)
	// - BM25: ~0.3x file size in memory (inverted index)
	// - Metadata: ~0.5x file size in memory (SQLite cache)
	info.MemoryEstimate = int64(
		float64(info.VectorSizeBytes)*1.5 +
			float64(info.BM25SizeBytes)*0.3 +
			float64(info.MetadataSizeBytes)*0.5,
	)

	// Language distribution from file paths
	filePaths, err := metadata.GetFilePathsByProject(ctx, projectID)
	if err == nil && len(filePaths) > 0 {
		langCounts := make(map[string]int)
		for _, path := range filePaths {
			ext := strings.TrimPrefix(filepath.Ext(path), ".")
			if ext == "" {
				ext = "other"
			}
			// Normalize common extensions
			ext = normalizeExtension(ext)
			langCounts[ext]++
		}

		// Convert to percentages, keeping top 4
		total := float64(len(filePaths))
		type langPair struct {
			ext   string
			count int
		}
		pairs := make([]langPair, 0, len(langCounts))
		for ext, count := range langCounts {
			pairs = append(pairs, langPair{ext, count})
		}
		sort.Slice(pairs, func(i, j int) bool {
			return pairs[i].count > pairs[j].count
		})

		otherCount := 0
		for i, p := range pairs {
			if i < 4 {
				info.Languages[p.ext] = float64(p.count) / total
			} else {
				otherCount += p.count
			}
		}
		if otherCount > 0 {
			info.Languages["other"] = float64(otherCount) / total
		}
	}

	return info, nil
}

func outputDebugJSON(cmd *cobra.Command, info *DebugInfo) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(info)
}

func outputDebugHuman(cmd *cobra.Command, info *DebugInfo) error {
	w := cmd.OutOrStdout()
	noColor := ui.DetectNoColor()

	// Header
	fmt.Fprintln(w, "AmanMCP Debug Info")
	if noColor {
		fmt.Fprintln(w, "========================================")
	} else {
		fmt.Fprintln(w, "\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550")
	}

	// Index info
	fmt.Fprintf(w, "Index Path:   %s\n", info.IndexPath)
	fmt.Fprintf(w, "Project Root: %s\n", info.ProjectRoot)
	fmt.Fprintf(w, "Index Age:    %s\n", info.IndexAge)
	fmt.Fprintf(w, "Last Update:  %s\n", info.UpdatedAt)
	fmt.Fprintln(w)

	// Files & Chunks
	fmt.Fprintln(w, "FILES & CHUNKS")
	fmt.Fprintf(w, "\u251c\u2500 Files:  %s\n", formatNumber(info.FileCount))
	fmt.Fprintf(w, "\u251c\u2500 Chunks: %s\n", formatNumber(info.ChunkCount))
	fmt.Fprintf(w, "\u2514\u2500 Languages: %s\n", formatLanguages(info.Languages))
	fmt.Fprintln(w)

	// Embedder
	fmt.Fprintln(w, "EMBEDDER")
	fmt.Fprintf(w, "\u251c\u2500 Provider:   %s\n", info.EmbedderProvider)
	fmt.Fprintf(w, "\u251c\u2500 Model:      %s\n", info.EmbedderModel)
	fmt.Fprintf(w, "\u251c\u2500 Dimensions: %d\n", info.EmbedderDimensions)
	available := "\u2717"
	if info.EmbedderAvailable {
		available = "\u2713"
	}
	fmt.Fprintf(w, "\u2514\u2500 Available:  %s\n", available)
	fmt.Fprintln(w)

	// BM25 Index
	fmt.Fprintln(w, "BM25 INDEX")
	fmt.Fprintf(w, "\u251c\u2500 Backend:   %s\n", info.BM25Backend)
	fmt.Fprintf(w, "\u251c\u2500 Documents: %s\n", formatNumber(info.BM25Documents))
	fmt.Fprintf(w, "\u2514\u2500 Size:      %s\n", store.FormatBytes(info.BM25SizeBytes))
	fmt.Fprintln(w)

	// Vector Store
	fmt.Fprintln(w, "VECTOR STORE")
	fmt.Fprintf(w, "\u251c\u2500 Vectors: %s\n", formatNumber(info.VectorCount))
	fmt.Fprintf(w, "\u2514\u2500 Size:    %s\n", store.FormatBytes(info.VectorSizeBytes))
	fmt.Fprintln(w)

	// Storage
	fmt.Fprintln(w, "STORAGE")
	fmt.Fprintf(w, "\u251c\u2500 Total Size:   %s\n", store.FormatBytes(info.TotalSizeBytes))
	fmt.Fprintf(w, "\u2514\u2500 Memory (est): ~%s\n", store.FormatBytes(info.MemoryEstimate))

	// Footer
	if noColor {
		fmt.Fprintln(w, "========================================")
	} else {
		fmt.Fprintln(w, "\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550")
	}

	return nil
}

// formatAge returns a human-readable age string.
func formatAge(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}

	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		mins := int(d.Minutes())
		if mins == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	case d < 24*time.Hour:
		hours := int(d.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}

// formatNumber formats an integer with comma separators.
func formatNumber(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}

	s := fmt.Sprintf("%d", n)
	result := make([]byte, 0, len(s)+(len(s)-1)/3)

	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}

	return string(result)
}

// formatLanguages formats language distribution as a string.
func formatLanguages(langs map[string]float64) string {
	if len(langs) == 0 {
		return "none"
	}

	// Sort by percentage descending
	type langPair struct {
		ext string
		pct float64
	}
	pairs := make([]langPair, 0, len(langs))
	for ext, pct := range langs {
		pairs = append(pairs, langPair{ext, pct})
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].pct > pairs[j].pct
	})

	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		parts = append(parts, fmt.Sprintf("%s (%d%%)", p.ext, int(p.pct*100)))
	}

	return strings.Join(parts, ", ")
}

// normalizeExtension normalizes common file extensions.
func normalizeExtension(ext string) string {
	ext = strings.ToLower(ext)
	switch ext {
	case "tsx", "ts":
		return "ts"
	case "jsx", "js", "mjs", "cjs":
		return "js"
	case "yml":
		return "yaml"
	case "htm":
		return "html"
	default:
		return ext
	}
}
