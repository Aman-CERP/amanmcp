package async

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBackgroundIndexer(t *testing.T) {
	// Given: indexer config
	cfg := IndexerConfig{
		DataDir: t.TempDir(),
	}

	// When: creating indexer
	indexer := NewBackgroundIndexer(cfg)

	// Then: should be initialized correctly
	require.NotNil(t, indexer)
	assert.NotNil(t, indexer.Progress())
	assert.False(t, indexer.IsRunning())
}

func TestBackgroundIndexer_Start_RunsInGoroutine(t *testing.T) {
	// Given: indexer with quick task
	cfg := IndexerConfig{
		DataDir: t.TempDir(),
	}
	indexer := NewBackgroundIndexer(cfg)

	var started atomic.Bool
	indexer.IndexFunc = func(ctx context.Context, progress *IndexProgress) error {
		started.Store(true)
		return nil
	}

	// When: starting indexer
	ctx := context.Background()
	indexer.Start(ctx)

	// Then: should run in background
	assert.True(t, indexer.IsRunning())

	// Wait for completion
	err := indexer.Wait()
	require.NoError(t, err)
	assert.True(t, started.Load())
	assert.False(t, indexer.IsRunning())
}

func TestBackgroundIndexer_Progress_UpdatesDuringRun(t *testing.T) {
	// Given: indexer that updates progress
	cfg := IndexerConfig{
		DataDir: t.TempDir(),
	}
	indexer := NewBackgroundIndexer(cfg)

	indexer.IndexFunc = func(ctx context.Context, progress *IndexProgress) error {
		progress.SetStage(StageScanning, 100)
		progress.UpdateFiles(50)
		time.Sleep(10 * time.Millisecond)
		progress.SetStage(StageChunking, 100)
		progress.UpdateFiles(100)
		return nil
	}

	// When: running indexer
	ctx := context.Background()
	indexer.Start(ctx)

	// Check progress during run
	time.Sleep(5 * time.Millisecond)
	assert.True(t, indexer.IsRunning())

	// Wait for completion
	err := indexer.Wait()
	require.NoError(t, err)

	// Then: final progress should show ready
	snap := indexer.Progress().Snapshot()
	assert.Equal(t, "ready", snap.Status)
}

func TestBackgroundIndexer_Stop_GracefulShutdown(t *testing.T) {
	// Given: indexer with long-running task
	cfg := IndexerConfig{
		DataDir: t.TempDir(),
	}
	indexer := NewBackgroundIndexer(cfg)

	var stopped atomic.Bool
	indexer.IndexFunc = func(ctx context.Context, progress *IndexProgress) error {
		progress.SetStage(StageEmbedding, 1000)
		for i := 0; i < 1000; i++ {
			select {
			case <-ctx.Done():
				stopped.Store(true)
				return ctx.Err()
			case <-time.After(1 * time.Millisecond):
				progress.UpdateFiles(i)
			}
		}
		return nil
	}

	// When: starting and stopping
	ctx := context.Background()
	indexer.Start(ctx)
	time.Sleep(10 * time.Millisecond)
	indexer.Stop()

	// Then: should stop cleanly
	assert.True(t, stopped.Load())
	assert.False(t, indexer.IsRunning())
}

func TestBackgroundIndexer_Stop_ContextCancellation(t *testing.T) {
	// Given: indexer with context
	cfg := IndexerConfig{
		DataDir: t.TempDir(),
	}
	indexer := NewBackgroundIndexer(cfg)

	var stopped atomic.Bool
	indexer.IndexFunc = func(ctx context.Context, progress *IndexProgress) error {
		<-ctx.Done()
		stopped.Store(true)
		return ctx.Err()
	}

	// When: context is canceled
	ctx, cancel := context.WithCancel(context.Background())
	indexer.Start(ctx)
	time.Sleep(5 * time.Millisecond)
	cancel()

	// Wait for shutdown
	_ = indexer.Wait()

	// Then: should stop on context cancel
	assert.True(t, stopped.Load())
	assert.False(t, indexer.IsRunning())
}

func TestBackgroundIndexer_Wait_BlocksUntilComplete(t *testing.T) {
	// Given: indexer with timed task
	cfg := IndexerConfig{
		DataDir: t.TempDir(),
	}
	indexer := NewBackgroundIndexer(cfg)

	indexer.IndexFunc = func(ctx context.Context, progress *IndexProgress) error {
		time.Sleep(50 * time.Millisecond)
		return nil
	}

	// When: waiting for completion
	ctx := context.Background()
	indexer.Start(ctx)

	start := time.Now()
	err := indexer.Wait()
	elapsed := time.Since(start)

	// Then: should block until complete
	require.NoError(t, err)
	assert.GreaterOrEqual(t, elapsed, 50*time.Millisecond)
}

func TestBackgroundIndexer_LockFile_Created(t *testing.T) {
	// Given: indexer
	dataDir := t.TempDir()
	cfg := IndexerConfig{
		DataDir: dataDir,
	}
	indexer := NewBackgroundIndexer(cfg)

	var lockExists atomic.Bool
	indexer.IndexFunc = func(ctx context.Context, progress *IndexProgress) error {
		lockPath := filepath.Join(dataDir, "indexing.lock")
		_, err := os.Stat(lockPath)
		lockExists.Store(err == nil)
		return nil
	}

	// When: running indexer
	ctx := context.Background()
	indexer.Start(ctx)
	err := indexer.Wait()

	// Then: lock file should have been created during run
	require.NoError(t, err)
	assert.True(t, lockExists.Load())

	// Lock file should be removed after completion
	lockPath := filepath.Join(dataDir, "indexing.lock")
	_, err = os.Stat(lockPath)
	assert.True(t, os.IsNotExist(err))
}

func TestBackgroundIndexer_Error_SetsProgress(t *testing.T) {
	// Given: indexer that returns error
	cfg := IndexerConfig{
		DataDir: t.TempDir(),
	}
	indexer := NewBackgroundIndexer(cfg)

	expectedErr := "embedding failed"
	indexer.IndexFunc = func(ctx context.Context, progress *IndexProgress) error {
		return &testError{message: expectedErr}
	}

	// When: running indexer
	ctx := context.Background()
	indexer.Start(ctx)
	err := indexer.Wait()

	// Then: error should be set in progress
	require.Error(t, err)
	snap := indexer.Progress().Snapshot()
	assert.Equal(t, "error", snap.Status)
	assert.Contains(t, snap.ErrorMessage, expectedErr)
}

func TestBackgroundIndexer_Start_IdempotentWhenRunning(t *testing.T) {
	// Given: running indexer
	cfg := IndexerConfig{
		DataDir: t.TempDir(),
	}
	indexer := NewBackgroundIndexer(cfg)

	var startCount atomic.Int32
	indexer.IndexFunc = func(ctx context.Context, progress *IndexProgress) error {
		startCount.Add(1)
		time.Sleep(50 * time.Millisecond)
		return nil
	}

	// When: starting multiple times
	ctx := context.Background()
	indexer.Start(ctx)
	indexer.Start(ctx) // Should be ignored
	indexer.Start(ctx) // Should be ignored
	_ = indexer.Wait()

	// Then: should only start once
	assert.Equal(t, int32(1), startCount.Load())
}

func TestHasIncompleteLock(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(dir string)
		wantResult bool
	}{
		{
			name:       "no lock file",
			setup:      func(dir string) {},
			wantResult: false,
		},
		{
			name: "lock file exists",
			setup: func(dir string) {
				_ = os.WriteFile(filepath.Join(dir, "indexing.lock"), []byte("test"), 0644)
			},
			wantResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.setup(dir)

			result := HasIncompleteLock(dir)
			assert.Equal(t, tt.wantResult, result)
		})
	}
}

// testError is a simple error type for testing
type testError struct {
	message string
}

func (e *testError) Error() string {
	return e.message
}
