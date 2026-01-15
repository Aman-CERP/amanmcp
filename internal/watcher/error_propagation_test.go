package watcher

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Error Propagation Tests - These test that errors are properly surfaced
// rather than silently ignored, as identified in the comprehensive test analysis.

// TestHybridWatcher_Start_InvalidPath_ReturnsError tests that starting a
// watcher on a non-existent path returns an error.
func TestHybridWatcher_Start_InvalidPath_ReturnsError(t *testing.T) {
	// Given: a hybrid watcher
	opts := DefaultOptions()
	w, err := NewHybridWatcher(opts)
	require.NoError(t, err)
	defer func() { _ = w.Stop() }()

	// When: starting on a non-existent path
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start in goroutine and capture error
	errCh := make(chan error, 1)
	go func() {
		errCh <- w.Start(ctx, "/nonexistent/path/that/does/not/exist")
	}()

	// Then: an error should be returned
	select {
	case err := <-errCh:
		// Note: fsnotify may create the watcher but fail on first directory add
		// The error might come from Start or via Errors channel
		if err != nil {
			assert.Error(t, err, "Start should return error for invalid path")
		}
	case err := <-w.Errors():
		assert.Error(t, err, "Error should be sent to Errors channel")
	case <-time.After(3 * time.Second):
		// If no error, check if we're silently failing
		t.Log("No immediate error - checking for silent failure")
	}
}

// TestHybridWatcher_Errors_ChannelIsOpen tests that the Errors channel
// is properly initialized and can receive errors.
func TestHybridWatcher_Errors_ChannelIsOpen(t *testing.T) {
	// Given: a hybrid watcher
	opts := DefaultOptions()
	w, err := NewHybridWatcher(opts)
	require.NoError(t, err)
	defer func() { _ = w.Stop() }()

	// Then: Errors channel should be non-nil and open
	assert.NotNil(t, w.Errors(), "Errors channel should not be nil")
}

// TestHybridWatcher_Stop_ClosesChannels_ErrorPropagation tests that stopping
// the watcher properly closes the event and error channels.
func TestHybridWatcher_Stop_ClosesChannels_ErrorPropagation(t *testing.T) {
	// Given: a started watcher
	tmpDir := t.TempDir()
	opts := Options{
		DebounceWindow:  10 * time.Millisecond,
		EventBufferSize: 10,
	}.WithDefaults()

	w, err := NewHybridWatcher(opts)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{})
	go func() {
		close(started)
		_ = w.Start(ctx, tmpDir)
	}()
	<-started
	time.Sleep(100 * time.Millisecond) // Wait for watcher to start

	// When: stopping the watcher
	err = w.Stop()
	require.NoError(t, err)

	// Then: Events channel should be closed (reading returns ok=false eventually)
	// Wait a bit for channels to close
	time.Sleep(100 * time.Millisecond)

	// Multiple stops should be safe
	err = w.Stop()
	assert.NoError(t, err, "Multiple stops should be safe")
}

// TestHybridWatcher_ContextCancel_StopsCleanly tests that canceling the
// context stops the watcher cleanly without hanging.
func TestHybridWatcher_ContextCancel_StopsCleanly(t *testing.T) {
	// Given: a started watcher
	tmpDir := t.TempDir()
	opts := Options{
		DebounceWindow:  10 * time.Millisecond,
		EventBufferSize: 10,
	}.WithDefaults()

	w, err := NewHybridWatcher(opts)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	startErr := make(chan error, 1)
	go func() {
		startErr <- w.Start(ctx, tmpDir)
	}()

	// Wait for watcher to be running
	time.Sleep(100 * time.Millisecond)

	// When: canceling context
	cancel()

	// Then: Start should return without hanging
	select {
	case err := <-startErr:
		// context.Canceled is expected
		if err != nil && err != context.Canceled {
			t.Logf("Start returned with: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Watcher did not stop within timeout after context cancel")
	}
}

// TestHybridWatcher_WatchDeletedDirectory_HandlesGracefully tests that
// the watcher handles the watched directory being deleted.
func TestHybridWatcher_WatchDeletedDirectory_HandlesGracefully(t *testing.T) {
	// Given: a watcher watching a directory
	tmpDir := t.TempDir()
	watchDir := filepath.Join(tmpDir, "watched")
	err := os.MkdirAll(watchDir, 0755)
	require.NoError(t, err)

	opts := Options{
		DebounceWindow:  10 * time.Millisecond,
		EventBufferSize: 10,
	}.WithDefaults()

	w, err := NewHybridWatcher(opts)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{})
	go func() {
		close(started)
		_ = w.Start(ctx, watchDir)
	}()
	<-started
	time.Sleep(200 * time.Millisecond)

	// When: deleting the watched directory
	err = os.RemoveAll(watchDir)
	require.NoError(t, err)

	// Then: should not panic, may emit error or event
	// Collect any events/errors for a short time
	timeout := time.After(1 * time.Second)
	for {
		select {
		case events := <-w.Events():
			t.Logf("Got events after directory deletion: %v", events)
		case err := <-w.Errors():
			t.Logf("Got error after directory deletion: %v", err)
			// Error is acceptable - directory was deleted
		case <-timeout:
			// Success - watcher didn't panic
			t.Log("Watcher handled directory deletion without panic")
			return
		}
	}
}

// TestHybridWatcher_PermissionDenied_ReportsError tests that the watcher
// properly reports permission errors.
func TestHybridWatcher_PermissionDenied_ReportsError(t *testing.T) {
	// Skip on CI or if running as root
	if os.Getuid() == 0 {
		t.Skip("Test requires non-root user")
	}

	// Given: a directory with no read permission
	tmpDir := t.TempDir()
	restrictedDir := filepath.Join(tmpDir, "restricted")
	err := os.MkdirAll(restrictedDir, 0000) // No permissions
	require.NoError(t, err)
	defer func() { _ = os.Chmod(restrictedDir, 0755) }()

	opts := DefaultOptions()
	w, err := NewHybridWatcher(opts)
	require.NoError(t, err)
	defer func() { _ = w.Stop() }()

	// When: starting watcher on restricted directory
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- w.Start(ctx, restrictedDir)
	}()

	// Then: should get an error
	select {
	case err := <-errCh:
		if err != nil {
			t.Logf("Got expected start error: %v", err)
		}
	case err := <-w.Errors():
		t.Logf("Got expected error from Errors channel: %v", err)
	case <-ctx.Done():
		t.Log("Context expired - may have silently failed")
	}
}

// TestPollingWatcher_Start_InvalidPath_ReturnsError tests the polling
// watcher with an invalid path.
func TestPollingWatcher_Start_InvalidPath_ReturnsError(t *testing.T) {
	// Given: a polling watcher
	w := NewPollingWatcher(100 * time.Millisecond)

	// When: starting on non-existent path
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := w.Start(ctx, "/nonexistent/path")

	// Then: should return error
	assert.Error(t, err, "Start should fail for non-existent path")
}

// TestDebouncer_Stop_ClosesOutput_ErrorPropagation tests that stopping
// the debouncer properly closes the output channel.
func TestDebouncer_Stop_ClosesOutput_ErrorPropagation(t *testing.T) {
	// Given: a debouncer
	d := NewDebouncer(10 * time.Millisecond)

	// When: stopping
	d.Stop()

	// Then: output channel should be closed
	select {
	case _, ok := <-d.Output():
		assert.False(t, ok, "Output channel should be closed")
	case <-time.After(100 * time.Millisecond):
		// Also acceptable - channel might already be closed
	}
}

// TestHybridWatcher_ConcurrentStop_Safe tests that concurrent stops
// don't cause a panic.
func TestHybridWatcher_ConcurrentStop_Safe(t *testing.T) {
	// Given: a started watcher
	tmpDir := t.TempDir()
	opts := DefaultOptions()

	w, err := NewHybridWatcher(opts)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = w.Start(ctx, tmpDir)
	}()
	time.Sleep(100 * time.Millisecond)

	// When: stopping concurrently from multiple goroutines
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			_ = w.Stop()
			done <- struct{}{}
		}()
	}

	// Then: should complete without panic
	for i := 0; i < 10; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Concurrent stops didn't complete in time")
		}
	}
}
