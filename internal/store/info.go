// Package store provides index information utilities.
package store

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// GetIndexInfo retrieves comprehensive information about an index.
// It requires an open metadata store and the data directory path.
// embedderInfo provides current embedder details for compatibility checking.
func GetIndexInfo(ctx context.Context, metadata MetadataStore, dataDir string, embedderInfo *EmbedderInfoInput) (*IndexInfo, error) {
	info := &IndexInfo{
		Location: dataDir,
	}

	// Get project info (assumes single project per index)
	// The project root is stored in the project table
	projectID, err := metadata.GetState(ctx, "project_id")
	if err == nil && projectID != "" {
		project, err := metadata.GetProject(ctx, projectID)
		if err == nil && project != nil {
			info.ProjectRoot = project.RootPath
			info.CreatedAt = project.IndexedAt
			info.UpdatedAt = project.IndexedAt
			info.ChunkCount = project.ChunkCount
			info.DocumentCount = project.FileCount
		}
	}

	// Get embedding info from state
	if dimStr, err := metadata.GetState(ctx, StateKeyIndexDimension); err == nil && dimStr != "" {
		if dim, err := strconv.Atoi(dimStr); err == nil {
			info.IndexDimensions = dim
		}
	}
	if model, err := metadata.GetState(ctx, StateKeyIndexModel); err == nil {
		info.IndexModel = model
		// Infer backend from model name
		info.IndexBackend = inferBackendFromModel(model)
	}

	// Get file sizes - check both BM25 backends
	bm25SQLitePath := filepath.Join(dataDir, "bm25.db")
	bm25BlevePath := filepath.Join(dataDir, "bm25.bleve")
	vectorPath := filepath.Join(dataDir, "vectors.hnsw")

	// Check SQLite first, then Bleve
	if stat, err := os.Stat(bm25SQLitePath); err == nil {
		info.BM25SizeBytes = stat.Size()
		if stat.ModTime().After(info.UpdatedAt) {
			info.UpdatedAt = stat.ModTime()
		}
	} else if stat, err := os.Stat(bm25BlevePath); err == nil {
		info.BM25SizeBytes = getDirSize(bm25BlevePath)
		if stat.ModTime().After(info.UpdatedAt) {
			info.UpdatedAt = stat.ModTime()
		}
	}
	if stat, err := os.Stat(vectorPath); err == nil {
		info.VectorSizeBytes = stat.Size()
		if stat.ModTime().After(info.UpdatedAt) {
			info.UpdatedAt = stat.ModTime()
		}
	}
	info.IndexSizeBytes = info.BM25SizeBytes + info.VectorSizeBytes

	// Set current embedder info and check compatibility
	if embedderInfo != nil {
		info.CurrentModel = embedderInfo.Model
		info.CurrentBackend = embedderInfo.Backend
		info.CurrentDimensions = embedderInfo.Dimensions
		info.Compatible = info.IndexDimensions == 0 || info.IndexDimensions == embedderInfo.Dimensions
	}

	return info, nil
}

// EmbedderInfoInput provides current embedder details for GetIndexInfo.
type EmbedderInfoInput struct {
	Model      string
	Backend    string
	Dimensions int
}

// inferBackendFromModel infers the backend from the model name.
func inferBackendFromModel(model string) string {
	switch {
	case model == "static" || model == "static768":
		return "static"
	case len(model) > 0 && model[0] == '/':
		// Absolute path suggests MLX local model
		return "mlx"
	case containsAny(model, []string{"mlx-community/", "mlx-"}):
		return "mlx"
	default:
		// Default to ollama for model names like "qwen3-embedding:0.6b"
		return "ollama"
	}
}

// containsAny checks if s contains any of the substrings.
func containsAny(s string, substrings []string) bool {
	for _, sub := range substrings {
		if len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}

// getDirSize calculates the total size of a directory.
func getDirSize(path string) int64 {
	var size int64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
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

// FormatBytes formats bytes as human-readable string.
func FormatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)

	switch {
	case bytes >= GB:
		return strconv.FormatFloat(float64(bytes)/float64(GB), 'f', 1, 64) + " GB"
	case bytes >= MB:
		return strconv.FormatFloat(float64(bytes)/float64(MB), 'f', 1, 64) + " MB"
	case bytes >= KB:
		return strconv.FormatFloat(float64(bytes)/float64(KB), 'f', 1, 64) + " KB"
	default:
		return strconv.FormatInt(bytes, 10) + " B"
	}
}

// FormatTime formats a time as a human-readable string.
func FormatTime(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	return t.Format("2006-01-02 15:04:05")
}
