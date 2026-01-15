package watcher

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"sync"
	"time"
)

// PollingWatcher watches for file changes by periodically scanning the directory.
// Used as a fallback when fsnotify is not available or fails.
type PollingWatcher struct {
	interval  time.Duration
	fileState map[string]fileSnapshot
	events    chan FileEvent
	errors    chan error
	stopCh    chan struct{}
	mu        sync.RWMutex
	stopped   bool
	rootPath  string
}

type fileSnapshot struct {
	modTime time.Time
	size    int64
	isDir   bool
}

// NewPollingWatcher creates a new polling watcher with the given interval.
func NewPollingWatcher(interval time.Duration) *PollingWatcher {
	return &PollingWatcher{
		interval:  interval,
		fileState: make(map[string]fileSnapshot),
		events:    make(chan FileEvent, 100),
		errors:    make(chan error, 10),
		stopCh:    make(chan struct{}),
	}
}

// Start begins watching the given directory by polling.
func (p *PollingWatcher) Start(ctx context.Context, path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve absolute path: %w", err)
	}
	p.rootPath = absPath

	// Initial scan to establish baseline
	if err := p.scan(); err != nil {
		return fmt.Errorf("perform initial scan: %w", err)
	}

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = p.Stop()
			return ctx.Err()
		case <-p.stopCh:
			return nil
		case <-ticker.C:
			if err := p.detectChanges(); err != nil {
				// Non-fatal error, send to error channel
				select {
				case p.errors <- err:
				default:
				}
			}
		}
	}
}

// Stop stops the polling watcher.
func (p *PollingWatcher) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.stopped {
		return nil
	}

	p.stopped = true
	close(p.stopCh)
	close(p.events)
	close(p.errors)
	return nil
}

// Events returns the channel of file events.
func (p *PollingWatcher) Events() <-chan FileEvent {
	return p.events
}

// Errors returns the channel of errors.
func (p *PollingWatcher) Errors() <-chan error {
	return p.errors
}

// scan walks the directory and records file state.
func (p *PollingWatcher) scan() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	return filepath.WalkDir(p.rootPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // Skip files we can't access
		}

		// Get relative path
		relPath, err := filepath.Rel(p.rootPath, path)
		if err != nil {
			return nil
		}
		if relPath == "." {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		p.fileState[relPath] = fileSnapshot{
			modTime: info.ModTime(),
			size:    info.Size(),
			isDir:   d.IsDir(),
		}

		return nil
	})
}

// detectChanges compares current state with previous state and emits events.
func (p *PollingWatcher) detectChanges() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Track current files
	currentFiles := make(map[string]fileSnapshot)

	err := filepath.WalkDir(p.rootPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		relPath, err := filepath.Rel(p.rootPath, path)
		if err != nil {
			return nil
		}
		if relPath == "." {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		snapshot := fileSnapshot{
			modTime: info.ModTime(),
			size:    info.Size(),
			isDir:   d.IsDir(),
		}
		currentFiles[relPath] = snapshot

		// Check for new or modified files
		if prev, exists := p.fileState[relPath]; !exists {
			// New file
			p.emitEvent(FileEvent{
				Path:      relPath,
				Operation: OpCreate,
				IsDir:     d.IsDir(),
				Timestamp: time.Now(),
			})
		} else if prev.modTime != snapshot.modTime || prev.size != snapshot.size {
			// Modified file
			p.emitEvent(FileEvent{
				Path:      relPath,
				Operation: OpModify,
				IsDir:     d.IsDir(),
				Timestamp: time.Now(),
			})
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("walk directory for changes: %w", err)
	}

	// Check for deleted files
	for path, snapshot := range p.fileState {
		if _, exists := currentFiles[path]; !exists {
			p.emitEvent(FileEvent{
				Path:      path,
				Operation: OpDelete,
				IsDir:     snapshot.isDir,
				Timestamp: time.Now(),
			})
		}
	}

	// Update state
	p.fileState = currentFiles
	return nil
}

// emitEvent sends an event to the events channel.
// Must be called with lock held.
func (p *PollingWatcher) emitEvent(event FileEvent) {
	if p.stopped {
		return
	}

	select {
	case p.events <- event:
	default:
		slog.Warn("polling watcher buffer full, dropping event",
			slog.String("path", event.Path),
			slog.String("op", event.Operation.String()),
		)
	}
}
