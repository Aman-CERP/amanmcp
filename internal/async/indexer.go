package async

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// IndexFunc is the function signature for the actual indexing work.
type IndexFunc func(ctx context.Context, progress *IndexProgress) error

// IndexerConfig configures the BackgroundIndexer.
type IndexerConfig struct {
	DataDir string
}

// BackgroundIndexer runs indexing in a background goroutine with progress tracking.
type BackgroundIndexer struct {
	config   IndexerConfig
	progress *IndexProgress

	// IndexFunc is the actual indexing function to run.
	// This can be injected for testing.
	IndexFunc IndexFunc

	// Lifecycle management
	stopCh chan struct{}
	doneCh chan struct{}

	mu      sync.Mutex
	running bool
	err     error
}

// NewBackgroundIndexer creates a new background indexer.
func NewBackgroundIndexer(cfg IndexerConfig) *BackgroundIndexer {
	return &BackgroundIndexer{
		config:   cfg,
		progress: NewIndexProgress(),
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

// Progress returns the progress tracker for this indexer.
func (b *BackgroundIndexer) Progress() *IndexProgress {
	return b.progress
}

// IsRunning returns true if the indexer is currently running.
func (b *BackgroundIndexer) IsRunning() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.running
}

// Start begins indexing in a background goroutine.
// This is non-blocking and returns immediately.
// Use Wait() to block until completion.
func (b *BackgroundIndexer) Start(ctx context.Context) {
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		return
	}
	b.running = true
	b.mu.Unlock()

	go b.run(ctx)
}

// run executes the indexing in the background.
func (b *BackgroundIndexer) run(ctx context.Context) {
	defer close(b.doneCh)
	defer func() {
		b.mu.Lock()
		b.running = false
		b.mu.Unlock()
	}()

	// Create merged context that respects both parent and stop channel
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		select {
		case <-b.stopCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	// Create lock file
	lockPath := filepath.Join(b.config.DataDir, "indexing.lock")
	if err := os.MkdirAll(b.config.DataDir, 0755); err != nil {
		b.progress.SetError(err.Error())
		b.mu.Lock()
		b.err = err
		b.mu.Unlock()
		return
	}

	if err := os.WriteFile(lockPath, []byte(time.Now().Format(time.RFC3339)), 0644); err != nil {
		b.progress.SetError(err.Error())
		b.mu.Lock()
		b.err = err
		b.mu.Unlock()
		return
	}

	// Ensure lock file is removed on completion
	defer func() { _ = os.Remove(lockPath) }()

	// Run the actual indexing function
	if b.IndexFunc != nil {
		if err := b.IndexFunc(ctx, b.progress); err != nil {
			b.progress.SetError(err.Error())
			b.mu.Lock()
			b.err = err
			b.mu.Unlock()
			return
		}
	}

	// Mark as ready
	b.progress.SetReady()
}

// Stop signals the indexer to stop and waits for it to finish.
func (b *BackgroundIndexer) Stop() {
	b.mu.Lock()
	if !b.running {
		b.mu.Unlock()
		return
	}
	b.mu.Unlock()

	close(b.stopCh)
	<-b.doneCh
}

// Wait blocks until the indexer completes and returns any error.
func (b *BackgroundIndexer) Wait() error {
	<-b.doneCh
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.err
}

// HasIncompleteLock checks if there's an incomplete indexing lock file.
func HasIncompleteLock(dataDir string) bool {
	lockPath := filepath.Join(dataDir, "indexing.lock")
	_, err := os.Stat(lockPath)
	return err == nil
}
