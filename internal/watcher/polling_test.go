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

func TestPollingWatcher_DetectsFileCreation(t *testing.T) {
	// Given: a temp directory and polling watcher
	tempDir := t.TempDir()
	w := NewPollingWatcher(50 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = w.Start(ctx, tempDir)
	}()

	// Wait for initial scan
	time.Sleep(100 * time.Millisecond)

	// When: a new file is created
	testFile := filepath.Join(tempDir, "new.go")
	require.NoError(t, os.WriteFile(testFile, []byte("package main"), 0o644))

	// Then: a CREATE event is detected
	select {
	case event := <-w.Events():
		assert.Equal(t, OpCreate, event.Operation)
		assert.Contains(t, event.Path, "new.go")
	case err := <-w.Errors():
		t.Fatalf("unexpected error: %v", err)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for create event")
	}

	require.NoError(t, w.Stop())
}

func TestPollingWatcher_DetectsFileModification(t *testing.T) {
	// Given: a temp directory with an existing file
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "existing.go")
	require.NoError(t, os.WriteFile(testFile, []byte("package main"), 0o644))

	w := NewPollingWatcher(50 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = w.Start(ctx, tempDir)
	}()

	// Wait for initial scan
	time.Sleep(100 * time.Millisecond)

	// When: the file is modified
	time.Sleep(50 * time.Millisecond) // Ensure different mtime
	require.NoError(t, os.WriteFile(testFile, []byte("package main\nfunc main() {}"), 0o644))

	// Then: a MODIFY event is detected
	select {
	case event := <-w.Events():
		assert.Equal(t, OpModify, event.Operation)
		assert.Contains(t, event.Path, "existing.go")
	case err := <-w.Errors():
		t.Fatalf("unexpected error: %v", err)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for modify event")
	}

	require.NoError(t, w.Stop())
}

func TestPollingWatcher_DetectsFileDeletion(t *testing.T) {
	// Given: a temp directory with an existing file
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "todelete.go")
	require.NoError(t, os.WriteFile(testFile, []byte("package main"), 0o644))

	w := NewPollingWatcher(50 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = w.Start(ctx, tempDir)
	}()

	// Wait for initial scan
	time.Sleep(100 * time.Millisecond)

	// When: the file is deleted
	require.NoError(t, os.Remove(testFile))

	// Then: a DELETE event is detected
	select {
	case event := <-w.Events():
		assert.Equal(t, OpDelete, event.Operation)
		assert.Contains(t, event.Path, "todelete.go")
	case err := <-w.Errors():
		t.Fatalf("unexpected error: %v", err)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for delete event")
	}

	require.NoError(t, w.Stop())
}

func TestPollingWatcher_DetectsNewDirectory(t *testing.T) {
	// Given: a temp directory and polling watcher
	tempDir := t.TempDir()
	w := NewPollingWatcher(50 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = w.Start(ctx, tempDir)
	}()

	// Wait for initial scan
	time.Sleep(100 * time.Millisecond)

	// When: a new directory with a file is created
	subDir := filepath.Join(tempDir, "subdir")
	require.NoError(t, os.MkdirAll(subDir, 0o755))
	testFile := filepath.Join(subDir, "file.go")
	require.NoError(t, os.WriteFile(testFile, []byte("package subdir"), 0o644))

	// Then: CREATE events for both are detected
	events := collectEvents(w.Events(), 2, 500*time.Millisecond)
	require.GreaterOrEqual(t, len(events), 1, "expected at least one event")

	// Check we got at least the file creation
	hasFileEvent := false
	for _, e := range events {
		if e.Operation == OpCreate && !e.IsDir {
			hasFileEvent = true
		}
	}
	assert.True(t, hasFileEvent, "expected file create event")

	require.NoError(t, w.Stop())
}

func TestPollingWatcher_Stop_HaltsPolling(t *testing.T) {
	// Given: a polling watcher
	tempDir := t.TempDir()
	w := NewPollingWatcher(50 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = w.Start(ctx, tempDir)
	}()

	time.Sleep(100 * time.Millisecond)

	// When: stopped
	require.NoError(t, w.Stop())

	// Then: channels are closed
	select {
	case _, ok := <-w.Events():
		assert.False(t, ok, "events channel should be closed")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for channel close")
	}
}

func TestPollingWatcher_ContextCancellation(t *testing.T) {
	// Given: a polling watcher
	tempDir := t.TempDir()
	w := NewPollingWatcher(50 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())

	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		close(started)
		_ = w.Start(ctx, tempDir)
		close(done)
	}()

	<-started
	time.Sleep(100 * time.Millisecond)

	// When: context is cancelled
	cancel()

	// Then: Start returns
	select {
	case <-done:
		// Success
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for Start to return after context cancel")
	}
}

// collectEvents collects up to n events or until timeout.
func collectEvents(ch <-chan FileEvent, n int, timeout time.Duration) []FileEvent {
	var events []FileEvent
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for len(events) < n {
		select {
		case e, ok := <-ch:
			if !ok {
				return events
			}
			events = append(events, e)
		case <-timer.C:
			return events
		}
	}
	return events
}
