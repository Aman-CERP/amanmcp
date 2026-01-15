package watcher

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/Aman-CERP/amanmcp/internal/gitignore"
)

// HybridWatcher implements the Watcher interface using fsnotify as the primary
// watching mechanism with polling as a fallback.
type HybridWatcher struct {
	fsWatcher      *fsnotify.Watcher
	pollWatcher    *PollingWatcher
	useFsnotify    bool
	debouncer      *Debouncer
	gitignore      *gitignore.Matcher
	events         chan []FileEvent
	errors         chan error
	stopCh         chan struct{}
	rootPath       string
	opts           Options
	mu             sync.RWMutex
	stopped        bool
	droppedBatches atomic.Uint64
}

// Ensure HybridWatcher implements Watcher interface.
// Note: Events() returns batched events ([]FileEvent) due to debouncing.
var _ interface {
	Start(ctx context.Context, path string) error
	Stop() error
	Events() <-chan []FileEvent
	Errors() <-chan error
} = (*HybridWatcher)(nil)

// NewHybridWatcher creates a new hybrid watcher with the given options.
// Attempts to use fsnotify first, falls back to polling if it fails.
func NewHybridWatcher(opts Options) (*HybridWatcher, error) {
	opts = opts.WithDefaults()

	h := &HybridWatcher{
		debouncer: NewDebouncer(opts.DebounceWindow),
		gitignore: gitignore.New(),
		events:    make(chan []FileEvent, opts.EventBufferSize),
		errors:    make(chan error, 10),
		stopCh:    make(chan struct{}),
		opts:      opts,
	}

	// Add custom ignore patterns
	for _, pattern := range opts.IgnorePatterns {
		h.gitignore.AddPattern(pattern)
	}

	// Always ignore .amanmcp directory
	h.gitignore.AddPattern(".amanmcp/")
	h.gitignore.AddPattern(".amanmcp/**")

	// Try to create fsnotify watcher
	fsw, err := fsnotify.NewWatcher()
	if err == nil {
		h.fsWatcher = fsw
		h.useFsnotify = true
	} else {
		// Fall back to polling
		h.useFsnotify = false
		h.pollWatcher = NewPollingWatcher(opts.PollInterval)
	}

	return h, nil
}

// Start begins watching the given directory.
func (h *HybridWatcher) Start(ctx context.Context, path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve absolute path: %w", err)
	}
	h.rootPath = absPath

	// Load .gitignore if present
	h.loadGitignore()

	// Start debouncer forwarding
	go h.forwardDebouncedEvents(ctx)

	if h.useFsnotify {
		return h.startFsnotify(ctx)
	}
	return h.startPolling(ctx)
}

// startFsnotify starts the fsnotify-based watcher.
func (h *HybridWatcher) startFsnotify(ctx context.Context) error {
	// Recursively add all directories to watch
	if err := h.addRecursive(h.rootPath); err != nil {
		return fmt.Errorf("add directories to watcher: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			_ = h.Stop()
			return ctx.Err()
		case <-h.stopCh:
			return nil
		case event, ok := <-h.fsWatcher.Events:
			if !ok {
				return nil
			}
			h.handleFsnotifyEvent(event)
		case err, ok := <-h.fsWatcher.Errors:
			if !ok {
				return nil
			}
			h.emitError(err)
		}
	}
}

// startPolling starts the polling-based watcher.
func (h *HybridWatcher) startPolling(ctx context.Context) error {
	// Forward polling events through debouncer
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-h.stopCh:
				return
			case event, ok := <-h.pollWatcher.Events():
				if !ok {
					return
				}
				// Filter and add to debouncer
				if h.shouldIgnore(event.Path, event.IsDir) {
					continue
				}

				// Handle .gitignore changes - emit special event for index reconciliation
				if filepath.Base(event.Path) == ".gitignore" {
					h.loadGitignore()
					h.debouncer.Add(FileEvent{
						Path:      event.Path,
						Operation: OpGitignoreChange,
						IsDir:     false,
						Timestamp: time.Now(),
					})
					continue
				}

				// BUG-027 fix: Handle config file changes
				baseName := filepath.Base(event.Path)
				if baseName == ".amanmcp.yaml" || baseName == ".amanmcp.yml" {
					h.debouncer.Add(FileEvent{
						Path:      event.Path,
						Operation: OpConfigChange,
						IsDir:     false,
						Timestamp: time.Now(),
					})
					continue
				}

				h.debouncer.Add(event)
			case err, ok := <-h.pollWatcher.Errors():
				if !ok {
					return
				}
				h.emitError(err)
			}
		}
	}()

	return h.pollWatcher.Start(ctx, h.rootPath)
}

// handleFsnotifyEvent converts and filters fsnotify events.
func (h *HybridWatcher) handleFsnotifyEvent(event fsnotify.Event) {
	// Get relative path
	relPath, err := filepath.Rel(h.rootPath, event.Name)
	if err != nil {
		relPath = event.Name
	}

	// Check if this is a directory
	isDir := false
	if info, err := os.Stat(event.Name); err == nil {
		isDir = info.IsDir()
	}

	// Filter ignored paths
	if h.shouldIgnore(relPath, isDir) {
		return
	}

	// Handle .gitignore changes - emit special event for index reconciliation
	if filepath.Base(event.Name) == ".gitignore" {
		h.loadGitignore()
		// Emit special event to trigger index reconciliation
		// This removes newly-ignored files and adds newly-unignored files
		h.debouncer.Add(FileEvent{
			Path:      relPath,
			Operation: OpGitignoreChange,
			IsDir:     false,
			Timestamp: time.Now(),
		})
		return // Don't process as a normal file event
	}

	// BUG-027 fix: Handle config file changes
	baseName := filepath.Base(event.Name)
	if baseName == ".amanmcp.yaml" || baseName == ".amanmcp.yml" {
		h.debouncer.Add(FileEvent{
			Path:      relPath,
			Operation: OpConfigChange,
			IsDir:     false,
			Timestamp: time.Now(),
		})
		return // Don't process as a normal file event
	}

	// Convert fsnotify operation to our operation
	var op Operation
	switch {
	case event.Op&fsnotify.Create != 0:
		op = OpCreate
		// Add new directories to watch
		if isDir {
			_ = h.fsWatcher.Add(event.Name)
		}
	case event.Op&fsnotify.Write != 0:
		op = OpModify
	case event.Op&fsnotify.Remove != 0:
		op = OpDelete
	case event.Op&fsnotify.Rename != 0:
		op = OpRename
	case event.Op&fsnotify.Chmod != 0:
		// Ignore chmod events
		return
	default:
		return
	}

	h.debouncer.Add(FileEvent{
		Path:      relPath,
		Operation: op,
		IsDir:     isDir,
		Timestamp: time.Now(),
	})
}

// forwardDebouncedEvents forwards debounced events to the output channel.
func (h *HybridWatcher) forwardDebouncedEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.stopCh:
			return
		case events, ok := <-h.debouncer.Output():
			if !ok {
				return
			}
			if len(events) == 0 {
				continue
			}
			h.emitEvents(events)
		}
	}
}

// addRecursive adds all directories under root to the fsnotify watcher.
func (h *HybridWatcher) addRecursive(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // Skip files we can't access
		}

		if !d.IsDir() {
			return nil
		}

		relPath, _ := filepath.Rel(h.rootPath, path)

		// Always add the root directory
		if relPath == "." {
			return h.fsWatcher.Add(path)
		}

		// Skip ignored directories (but not root)
		if h.shouldIgnoreDir(relPath) {
			return filepath.SkipDir
		}

		return h.fsWatcher.Add(path)
	})
}

// shouldIgnoreDir checks if a directory should be ignored.
func (h *HybridWatcher) shouldIgnoreDir(relPath string) bool {
	// Always ignore .git directory
	if strings.HasPrefix(relPath, ".git") || relPath == ".git" {
		return true
	}

	// Always ignore .amanmcp directory
	if strings.HasPrefix(relPath, ".amanmcp") || relPath == ".amanmcp" {
		return true
	}

	// BUG-025 fix: Hold read lock while accessing gitignore matcher
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.gitignore.Match(relPath, true)
}

// shouldIgnore returns true if the path should be ignored.
func (h *HybridWatcher) shouldIgnore(relPath string, isDir bool) bool {
	if relPath == "." || relPath == "" {
		return true
	}

	// Always ignore .git directory
	if strings.HasPrefix(relPath, ".git/") || relPath == ".git" {
		return true
	}

	// Always ignore .amanmcp directory
	if strings.HasPrefix(relPath, ".amanmcp/") || relPath == ".amanmcp" {
		return true
	}

	// BUG-025 fix: Hold read lock while accessing gitignore matcher
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.gitignore.Match(relPath, isDir)
}

// loadGitignore loads .gitignore patterns from the root and subdirectories.
func (h *HybridWatcher) loadGitignore() {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Create new matcher with custom patterns
	h.gitignore = gitignore.New()
	for _, pattern := range h.opts.IgnorePatterns {
		h.gitignore.AddPattern(pattern)
	}
	h.gitignore.AddPattern(".amanmcp/")
	h.gitignore.AddPattern(".amanmcp/**")

	// Load root .gitignore
	gitignorePath := filepath.Join(h.rootPath, ".gitignore")
	if err := h.gitignore.AddFromFile(gitignorePath, ""); err != nil && !os.IsNotExist(err) {
		slog.Warn("failed to load root .gitignore",
			slog.String("path", gitignorePath),
			slog.String("error", err.Error()))
	}

	// Walk and load nested .gitignore files
	// BUG-029 fix: Log warnings for permission/read errors instead of silent skip
	_ = filepath.WalkDir(h.rootPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			slog.Warn("skipping directory in gitignore scan",
				slog.String("path", path),
				slog.String("error", err.Error()))
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() == ".gitignore" && path != gitignorePath {
			base, _ := filepath.Rel(h.rootPath, filepath.Dir(path))
			if err := h.gitignore.AddFromFile(path, base); err != nil {
				slog.Warn("failed to read nested .gitignore",
					slog.String("path", path),
					slog.String("error", err.Error()))
			}
		}
		return nil
	})
}

// emitEvents sends events to the output channel.
func (h *HybridWatcher) emitEvents(events []FileEvent) {
	h.mu.RLock()
	stopped := h.stopped
	h.mu.RUnlock()

	if stopped {
		return
	}

	select {
	case h.events <- events:
	default:
		count := h.droppedBatches.Add(1)
		slog.Warn("event buffer full, dropping batch",
			slog.Int("batch_size", len(events)),
			slog.Uint64("total_dropped_batches", count),
		)
	}
}

// DroppedBatches returns the number of event batches dropped due to buffer overflow.
func (h *HybridWatcher) DroppedBatches() uint64 {
	return h.droppedBatches.Load()
}

// emitError sends an error to the error channel.
func (h *HybridWatcher) emitError(err error) {
	h.mu.RLock()
	stopped := h.stopped
	h.mu.RUnlock()

	if stopped {
		return
	}

	select {
	case h.errors <- err:
	default:
	}
}

// Stop stops the watcher and releases resources.
func (h *HybridWatcher) Stop() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.stopped {
		return nil
	}

	h.stopped = true
	close(h.stopCh)

	// Stop debouncer
	h.debouncer.Stop()

	// Stop underlying watcher
	if h.useFsnotify && h.fsWatcher != nil {
		_ = h.fsWatcher.Close()
	}
	if h.pollWatcher != nil {
		_ = h.pollWatcher.Stop()
	}

	close(h.events)
	close(h.errors)
	return nil
}

// Events returns the channel of batched file events.
func (h *HybridWatcher) Events() <-chan []FileEvent {
	return h.events
}

// Errors returns the channel of errors.
func (h *HybridWatcher) Errors() <-chan error {
	return h.errors
}

// IsHealthy returns true if the watcher is running and hasn't stopped.
func (h *HybridWatcher) IsHealthy() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return !h.stopped
}

// WatcherType returns the type of watcher being used ("fsnotify" or "polling").
func (h *HybridWatcher) WatcherType() string {
	if h.useFsnotify {
		return "fsnotify"
	}
	return "polling"
}

// RootPath returns the root path being watched.
func (h *HybridWatcher) RootPath() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.rootPath
}

