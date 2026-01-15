package preflight

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// MinModelDiskSpaceBytes is the minimum disk space needed for EmbeddingGemma model download (~1.5GB).
const MinModelDiskSpaceBytes = 1.5 * 1024 * 1024 * 1024 // 1.5 GB

// CheckEmbedderModel checks if the embedding model is downloaded and ready.
func (c *Checker) CheckEmbedderModel() CheckResult {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return CheckResult{
			Name:     "embedder_model",
			Status:   StatusWarn,
			Message:  fmt.Sprintf("cannot determine home directory: %v", err),
			Required: false,
		}
	}
	return c.checkEmbedderModelWithHome(homeDir)
}

// checkEmbedderModelWithHome checks embedder model with a specific home directory.
// This allows testing with temp directories.
func (c *Checker) checkEmbedderModelWithHome(homeDir string) CheckResult {
	result := CheckResult{
		Name:     "embedder_model",
		Required: false, // Non-critical - we can fall back to static
	}

	modelDir := filepath.Join(homeDir, ".amanmcp", "models")

	// Check if model directory exists and has content
	entries, err := os.ReadDir(modelDir)
	if err != nil {
		if os.IsNotExist(err) {
			result.Status = StatusWarn
			result.Message = "Model not downloaded (will download on first index)"
			result.Details = fmt.Sprintf("Model directory: %s", modelDir)
			return result
		}
		result.Status = StatusWarn
		result.Message = fmt.Sprintf("Cannot access model directory: %v", err)
		return result
	}

	if len(entries) == 0 {
		result.Status = StatusWarn
		result.Message = "Model not downloaded (will download on first index)"
		result.Details = fmt.Sprintf("Model directory: %s (empty)", modelDir)
		return result
	}

	// Count total size of model files
	var totalSize int64
	err = filepath.Walk(modelDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Ignore errors, just count what we can
		}
		if !info.IsDir() {
			totalSize += info.Size()
		}
		return nil
	})
	if err != nil {
		totalSize = 0 // Couldn't calculate, but we know files exist
	}

	result.Status = StatusPass
	if totalSize > 0 {
		result.Message = fmt.Sprintf("Model downloaded (%s)", formatBytes(uint64(totalSize)))
	} else {
		result.Message = "Model downloaded and ready"
	}
	result.Details = fmt.Sprintf("Model directory: %s", modelDir)
	return result
}

// CheckEmbedderDiskSpace checks if there's enough disk space for model download.
func (c *Checker) CheckEmbedderDiskSpace() CheckResult {
	result := CheckResult{
		Name:     "embedder_disk_space",
		Required: false, // Non-critical - we can fall back to static
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		result.Status = StatusWarn
		result.Message = fmt.Sprintf("Cannot determine home directory: %v", err)
		return result
	}

	// Check disk space in home directory (where models are stored)
	var stat syscall.Statfs_t
	if err := syscall.Statfs(homeDir, &stat); err != nil {
		result.Status = StatusWarn
		result.Message = fmt.Sprintf("Cannot check disk space: %v", err)
		return result
	}

	availableBytes := stat.Bavail * uint64(stat.Bsize)

	if availableBytes < uint64(MinModelDiskSpaceBytes) {
		result.Status = StatusWarn
		result.Message = fmt.Sprintf("%s available (model needs ~1.5 GB)", formatBytes(availableBytes))
		result.Details = "Consider freeing up disk space or use --embedder=static for offline mode"
		return result
	}

	result.Status = StatusPass
	result.Message = fmt.Sprintf("%s available for model download", formatBytes(availableBytes))
	return result
}
