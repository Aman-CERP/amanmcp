// Package embed provides embedding functionality for AmanMCP.
// This file implements model downloading and caching for GGUF embedding models.
package embed

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// DefaultModelName is the default embedding model to use.
	DefaultModelName = "nomic-embed-text-v1.5"

	// DefaultModelFile is the quantized model file to download.
	DefaultModelFile = "nomic-embed-text-v1.5.Q8_0.gguf"

	// DefaultModelURL is the HuggingFace URL for the model.
	DefaultModelURL = "https://huggingface.co/nomic-ai/nomic-embed-text-v1.5-GGUF/resolve/main/nomic-embed-text-v1.5.Q8_0.gguf"

	// DefaultModelSize is the approximate size of the Q8_0 model in bytes (~146MB).
	DefaultModelSize = 146 * 1024 * 1024

	// NomicEmbedDimensions is the output dimension of nomic-embed-text-v1.5.
	NomicEmbedDimensions = 768

	// ModelDownloadTimeout is the maximum time to wait for model download.
	ModelDownloadTimeout = 30 * time.Minute
)

// ModelManager handles downloading and caching of embedding models.
type ModelManager struct {
	modelsDir string
	lock      *FileLock
	mu        sync.Mutex
}

// NewModelManager creates a new model manager.
// modelsDir is typically ~/.amanmcp/models/
func NewModelManager(modelsDir string) *ModelManager {
	return &ModelManager{
		modelsDir: modelsDir,
	}
}

// ModelPath returns the path to the model file.
func (m *ModelManager) ModelPath() string {
	return filepath.Join(m.modelsDir, DefaultModelFile)
}

// EnsureModel ensures the embedding model is available, downloading if necessary.
// Returns the path to the model file.
func (m *ModelManager) EnsureModel(ctx context.Context, progressFn func(downloaded, total int64)) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	modelPath := m.ModelPath()

	// Check if model already exists
	if info, err := os.Stat(modelPath); err == nil && info.Size() > 0 {
		return modelPath, nil
	}

	// Create models directory
	if err := os.MkdirAll(m.modelsDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create models directory: %w", err)
	}

	// Acquire file lock to prevent concurrent downloads
	m.lock = NewFileLock(m.modelsDir)
	if err := m.lock.Lock(); err != nil {
		return "", fmt.Errorf("failed to acquire download lock: %w", err)
	}
	defer func() {
		if err := m.lock.Unlock(); err != nil {
			// Log but don't fail
			_ = err
		}
	}()

	// Check again after acquiring lock (another process may have downloaded)
	if info, err := os.Stat(modelPath); err == nil && info.Size() > 0 {
		return modelPath, nil
	}

	// Download the model
	if err := m.downloadModel(ctx, modelPath, progressFn); err != nil {
		return "", fmt.Errorf("failed to download model: %w", err)
	}

	return modelPath, nil
}

// downloadModel downloads the model from HuggingFace.
func (m *ModelManager) downloadModel(ctx context.Context, destPath string, progressFn func(downloaded, total int64)) error {
	// Create temp file for atomic download
	tmpPath := destPath + ".tmp"
	defer os.Remove(tmpPath) // Clean up on failure

	// Create HTTP request with context
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, DefaultModelURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Add user agent
	req.Header.Set("User-Agent", "amanmcp/1.0")

	// Execute request
	client := &http.Client{
		Timeout: ModelDownloadTimeout,
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status: %s", resp.Status)
	}

	// Create temp file
	file, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer file.Close()

	// Get content length for progress
	totalSize := resp.ContentLength
	if totalSize <= 0 {
		totalSize = DefaultModelSize
	}

	// Download with progress tracking
	var downloaded int64
	buf := make([]byte, 32*1024) // 32KB buffer
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := file.Write(buf[:n]); writeErr != nil {
				return fmt.Errorf("failed to write: %w", writeErr)
			}
			downloaded += int64(n)
			if progressFn != nil {
				progressFn(downloaded, totalSize)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read: %w", err)
		}
	}

	// Sync and close
	if err := file.Sync(); err != nil {
		return fmt.Errorf("failed to sync: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("failed to close: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("failed to rename: %w", err)
	}

	return nil
}

// ModelExists checks if the model file exists.
func (m *ModelManager) ModelExists() bool {
	info, err := os.Stat(m.ModelPath())
	return err == nil && info.Size() > 0
}

// DeleteModel removes the cached model file.
func (m *ModelManager) DeleteModel() error {
	return os.Remove(m.ModelPath())
}

// DefaultModelsDir returns the default models directory path.
func DefaultModelsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".amanmcp", "models")
}
