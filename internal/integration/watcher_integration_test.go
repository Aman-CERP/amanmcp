package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Aman-CERP/amanmcp/internal/watcher"
)

// Watcher Integration Tests - These test the file watcher behavior
// to verify it correctly detects file changes.

// TestWatcher_FileCreated_EmitsEvent tests that creating a file emits a create event.
func TestWatcher_FileCreated_EmitsEvent(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Given: a watcher watching a directory
	dir := t.TempDir()
	w, err := watcher.NewHybridWatcher(watcher.Options{
		DebounceWindow:  100 * time.Millisecond,
		EventBufferSize: 100,
	}.WithDefaults())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start watcher in background
	go func() {
		_ = w.Start(ctx, dir)
	}()
	defer func() { _ = w.Stop() }()

	// Wait for watcher to initialize
	time.Sleep(200 * time.Millisecond)

	// When: creating a new file
	testFile := filepath.Join(dir, "test.go")
	err = os.WriteFile(testFile, []byte("package test"), 0644)
	require.NoError(t, err)

	// Then: a create event should be emitted
	select {
	case events := <-w.Events():
		assert.NotEmpty(t, events, "Should receive events")
		foundCreate := false
		for _, e := range events {
			if e.Operation == watcher.OpCreate && e.Path == "test.go" {
				foundCreate = true
				break
			}
		}
		assert.True(t, foundCreate, "Should receive CREATE event for test.go")
	case <-ctx.Done():
		t.Fatal("Timed out waiting for create event")
	}
}

// TestWatcher_FileModified_EmitsEvent tests that modifying a file emits a modify event.
func TestWatcher_FileModified_EmitsEvent(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Given: a directory with an existing file
	dir := t.TempDir()
	testFile := filepath.Join(dir, "existing.go")
	err := os.WriteFile(testFile, []byte("package test"), 0644)
	require.NoError(t, err)

	w, err := watcher.NewHybridWatcher(watcher.Options{
		DebounceWindow:  100 * time.Millisecond,
		EventBufferSize: 100,
	}.WithDefaults())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_ = w.Start(ctx, dir)
	}()
	defer func() { _ = w.Stop() }()

	time.Sleep(200 * time.Millisecond)

	// When: modifying the file
	err = os.WriteFile(testFile, []byte("package test\n\nfunc main() {}"), 0644)
	require.NoError(t, err)

	// Then: a modify event should be emitted
	select {
	case events := <-w.Events():
		assert.NotEmpty(t, events, "Should receive events")
		foundModify := false
		for _, e := range events {
			if e.Operation == watcher.OpModify && e.Path == "existing.go" {
				foundModify = true
				break
			}
		}
		assert.True(t, foundModify, "Should receive MODIFY event for existing.go")
	case <-ctx.Done():
		t.Fatal("Timed out waiting for modify event")
	}
}

// TestWatcher_FileDeleted_EmitsEvent tests that deleting a file emits a delete event.
func TestWatcher_FileDeleted_EmitsEvent(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Given: a directory with an existing file
	dir := t.TempDir()
	testFile := filepath.Join(dir, "todelete.go")
	err := os.WriteFile(testFile, []byte("package test"), 0644)
	require.NoError(t, err)

	w, err := watcher.NewHybridWatcher(watcher.Options{
		DebounceWindow:  100 * time.Millisecond,
		EventBufferSize: 100,
	}.WithDefaults())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_ = w.Start(ctx, dir)
	}()
	defer func() { _ = w.Stop() }()

	time.Sleep(200 * time.Millisecond)

	// When: deleting the file
	err = os.Remove(testFile)
	require.NoError(t, err)

	// Then: a delete event should be emitted
	select {
	case events := <-w.Events():
		assert.NotEmpty(t, events, "Should receive events")
		foundDelete := false
		for _, e := range events {
			if e.Operation == watcher.OpDelete && e.Path == "todelete.go" {
				foundDelete = true
				break
			}
		}
		assert.True(t, foundDelete, "Should receive DELETE event for todelete.go")
	case <-ctx.Done():
		t.Fatal("Timed out waiting for delete event")
	}
}

// TestWatcher_IsHealthy_ReportsCorrectly tests the health check method.
func TestWatcher_IsHealthy_ReportsCorrectly(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Given: a new watcher
	w, err := watcher.NewHybridWatcher(watcher.DefaultOptions())
	require.NoError(t, err)

	// Then: should be healthy before starting (not stopped yet)
	assert.True(t, w.IsHealthy(), "New watcher should be healthy")

	// When: stopping the watcher
	err = w.Stop()
	require.NoError(t, err)

	// Then: should no longer be healthy
	assert.False(t, w.IsHealthy(), "Stopped watcher should not be healthy")
}

// TestWatcher_WatcherType_ReturnsCorrectType tests the watcher type method.
func TestWatcher_WatcherType_ReturnsCorrectType(t *testing.T) {
	// Given: a new watcher
	w, err := watcher.NewHybridWatcher(watcher.DefaultOptions())
	require.NoError(t, err)
	defer func() { _ = w.Stop() }()

	// Then: should return fsnotify or polling
	watcherType := w.WatcherType()
	assert.Contains(t, []string{"fsnotify", "polling"}, watcherType,
		"WatcherType should be fsnotify or polling")
}

// TestWatcher_IgnoresGitignored_DoesNotEmitEvents tests that gitignored files
// don't produce events.
func TestWatcher_IgnoresGitignored_DoesNotEmitEvents(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Given: a directory with .gitignore excluding *.log
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.log\n"), 0644)
	require.NoError(t, err)

	w, err := watcher.NewHybridWatcher(watcher.Options{
		DebounceWindow:  100 * time.Millisecond,
		EventBufferSize: 100,
	}.WithDefaults())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		_ = w.Start(ctx, dir)
	}()
	defer func() { _ = w.Stop() }()

	time.Sleep(200 * time.Millisecond)

	// When: creating a .log file (should be ignored)
	logFile := filepath.Join(dir, "debug.log")
	err = os.WriteFile(logFile, []byte("log content"), 0644)
	require.NoError(t, err)

	// And: creating a .go file (should not be ignored)
	goFile := filepath.Join(dir, "main.go")
	err = os.WriteFile(goFile, []byte("package main"), 0644)
	require.NoError(t, err)

	// Then: should only receive event for .go file, not .log
	select {
	case events := <-w.Events():
		for _, e := range events {
			assert.NotEqual(t, "debug.log", e.Path,
				"Should not receive events for gitignored .log files")
		}
	case <-ctx.Done():
		// Timeout is acceptable - might just not receive any events
	}
}
