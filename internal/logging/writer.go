package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// RotatingWriter implements io.Writer with size-based rotation.
type RotatingWriter struct {
	path     string
	maxSize  int64
	maxFiles int

	mu            sync.Mutex
	file          *os.File
	written       int64
	immediateSync bool // Sync after each write for real-time visibility
}

// NewRotatingWriter creates a new rotating log writer.
// maxSizeMB is the maximum size in megabytes before rotation.
// maxFiles is the maximum number of rotated files to keep.
// Immediate sync is enabled by default for real-time log visibility.
func NewRotatingWriter(path string, maxSizeMB, maxFiles int) (*RotatingWriter, error) {
	w := &RotatingWriter{
		path:          path,
		maxSize:       int64(maxSizeMB) * 1024 * 1024,
		maxFiles:      maxFiles,
		immediateSync: true, // Enable by default for amanmcp-logs -f visibility
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	// Open or create the log file
	if err := w.openFile(); err != nil {
		return nil, err
	}

	return w, nil
}

// SetImmediateSync enables or disables immediate sync after each write.
// When enabled, logs are immediately visible to `amanmcp-logs -f`.
// When disabled, logs may be buffered for better performance.
func (w *RotatingWriter) SetImmediateSync(enabled bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.immediateSync = enabled
}

// Write implements io.Writer with automatic rotation.
// If immediateSync is enabled, syncs to disk after each write for real-time visibility.
func (w *RotatingWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Check if rotation is needed
	if w.written+int64(len(p)) > w.maxSize {
		if err := w.rotate(); err != nil {
			// Continue writing to current file if rotation fails
			_, _ = fmt.Fprintf(os.Stderr, "log rotation failed: %v\n", err)
		}
	}

	n, err = w.file.Write(p)
	w.written += int64(n)

	// Sync to disk for immediate visibility in amanmcp-logs -f
	if w.immediateSync && err == nil {
		_ = w.file.Sync()
	}

	return
}

// Close closes the underlying file.
func (w *RotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file != nil {
		return w.file.Close()
	}
	return nil
}

// Sync flushes the file to disk.
func (w *RotatingWriter) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file != nil {
		return w.file.Sync()
	}
	return nil
}

// openFile opens or creates the log file.
func (w *RotatingWriter) openFile() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	// Get current file size
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("failed to stat log file: %w", err)
	}

	w.file = f
	w.written = info.Size()
	return nil
}

// rotate performs log rotation.
// server.log -> server.log.1 -> server.log.2 -> ... -> delete oldest
func (w *RotatingWriter) rotate() error {
	// Close current file
	if w.file != nil {
		if err := w.file.Close(); err != nil {
			return fmt.Errorf("failed to close log file: %w", err)
		}
		w.file = nil
	}

	// Find existing rotated files
	dir := filepath.Dir(w.path)
	base := filepath.Base(w.path)
	pattern := base + ".*"

	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		return fmt.Errorf("failed to find rotated files: %w", err)
	}

	// Sort by number (highest first) to rename in correct order
	type rotatedFile struct {
		path string
		num  int
	}
	var files []rotatedFile
	for _, m := range matches {
		// Extract number from filename
		suffix := strings.TrimPrefix(filepath.Base(m), base+".")
		num, err := strconv.Atoi(suffix)
		if err != nil {
			continue // Skip files that don't match pattern
		}
		files = append(files, rotatedFile{path: m, num: num})
	}

	// Sort by number descending
	sort.Slice(files, func(i, j int) bool {
		return files[i].num > files[j].num
	})

	// Delete files beyond maxFiles
	for _, f := range files {
		if f.num >= w.maxFiles {
			_ = os.Remove(f.path)
		}
	}

	// Rename existing files (start from highest to avoid overwriting)
	for _, f := range files {
		if f.num < w.maxFiles {
			newPath := fmt.Sprintf("%s.%d", w.path, f.num+1)
			_ = os.Rename(f.path, newPath)
		}
	}

	// Rename current log to .1
	if _, err := os.Stat(w.path); err == nil {
		newPath := w.path + ".1"
		if err := os.Rename(w.path, newPath); err != nil {
			return fmt.Errorf("failed to rotate log file: %w", err)
		}
	}

	// Open new log file
	w.written = 0
	return w.openFile()
}
