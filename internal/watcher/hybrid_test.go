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

func TestHybridWatcher_NewHybridWatcher(t *testing.T) {
	// Given: default options
	opts := DefaultOptions()

	// When: creating a hybrid watcher
	w, err := NewHybridWatcher(opts)

	// Then: no error and watcher is valid
	require.NoError(t, err)
	require.NotNil(t, w)
	defer func() { _ = w.Stop() }()
}

func TestHybridWatcher_SimpleCreate(t *testing.T) {
	// This is a minimal test to verify event flow
	tempDir := t.TempDir()
	t.Logf("TempDir: %s", tempDir)

	opts := Options{
		DebounceWindow:  10 * time.Millisecond, // Very short for testing
		EventBufferSize: 100,
	}.WithDefaults()

	w, err := NewHybridWatcher(opts)
	require.NoError(t, err)
	t.Log("Watcher created")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{})
	go func() {
		close(started)
		if err := w.Start(ctx, tempDir); err != nil && err != context.Canceled {
			t.Logf("Start error: %v", err)
		}
	}()

	<-started
	time.Sleep(200 * time.Millisecond) // Wait for watcher to be ready
	t.Log("Watcher started")

	// Create a file
	testFile := filepath.Join(tempDir, "test.go")
	t.Logf("Creating file: %s", testFile)
	err = os.WriteFile(testFile, []byte("package main"), 0o644)
	require.NoError(t, err)
	t.Log("File created")

	// Wait for event
	select {
	case events := <-w.Events():
		t.Logf("Got %d events", len(events))
		for _, e := range events {
			t.Logf("  Event: %s %s", e.Operation, e.Path)
		}
		require.NotEmpty(t, events, "expected at least one event")
	case err := <-w.Errors():
		t.Fatalf("Got error: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout - no events received")
	}

	require.NoError(t, w.Stop())
}

func TestHybridWatcher_DetectsFileCreation(t *testing.T) {
	// Given: a temp directory and hybrid watcher
	tempDir := t.TempDir()
	opts := Options{
		DebounceWindow:  50 * time.Millisecond,
		EventBufferSize: 100,
	}.WithDefaults()

	w, err := NewHybridWatcher(opts)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = w.Start(ctx, tempDir)
	}()

	// Wait for watcher to initialize
	time.Sleep(100 * time.Millisecond)

	// When: a new file is created
	testFile := filepath.Join(tempDir, "newfile.go")
	require.NoError(t, os.WriteFile(testFile, []byte("package main"), 0o644))

	// Then: a CREATE event is detected
	select {
	case events := <-w.Events():
		require.NotEmpty(t, events)
		// Find the create event
		var found bool
		for _, e := range events {
			if e.Operation == OpCreate && filepath.Base(e.Path) == "newfile.go" {
				found = true
				break
			}
		}
		assert.True(t, found, "expected CREATE event for newfile.go")
	case err := <-w.Errors():
		t.Fatalf("unexpected error: %v", err)
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for create event")
	}

	require.NoError(t, w.Stop())
}

func TestHybridWatcher_DetectsFileModification(t *testing.T) {
	// Given: a temp directory with an existing file
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "existing.go")
	require.NoError(t, os.WriteFile(testFile, []byte("package main"), 0o644))

	opts := Options{
		DebounceWindow:  50 * time.Millisecond,
		EventBufferSize: 100,
	}.WithDefaults()

	w, err := NewHybridWatcher(opts)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = w.Start(ctx, tempDir)
	}()

	// Wait for watcher to initialize
	time.Sleep(100 * time.Millisecond)

	// When: the file is modified
	require.NoError(t, os.WriteFile(testFile, []byte("package main\nfunc main() {}"), 0o644))

	// Then: a MODIFY or CREATE event is detected (fsnotify may report as Write)
	select {
	case events := <-w.Events():
		require.NotEmpty(t, events)
		// File modification detected
		var found bool
		for _, e := range events {
			if (e.Operation == OpModify || e.Operation == OpCreate) &&
				filepath.Base(e.Path) == "existing.go" {
				found = true
				break
			}
		}
		assert.True(t, found, "expected modify event for existing.go")
	case err := <-w.Errors():
		t.Fatalf("unexpected error: %v", err)
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for modify event")
	}

	require.NoError(t, w.Stop())
}

func TestHybridWatcher_DetectsFileDeletion(t *testing.T) {
	// Given: a temp directory with an existing file
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "todelete.go")
	require.NoError(t, os.WriteFile(testFile, []byte("package main"), 0o644))

	opts := Options{
		DebounceWindow:  50 * time.Millisecond,
		EventBufferSize: 100,
	}.WithDefaults()

	w, err := NewHybridWatcher(opts)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = w.Start(ctx, tempDir)
	}()

	// Wait for watcher to initialize
	time.Sleep(100 * time.Millisecond)

	// When: the file is deleted
	require.NoError(t, os.Remove(testFile))

	// Then: a DELETE event is detected
	select {
	case events := <-w.Events():
		require.NotEmpty(t, events)
		var found bool
		for _, e := range events {
			if e.Operation == OpDelete && filepath.Base(e.Path) == "todelete.go" {
				found = true
				break
			}
		}
		assert.True(t, found, "expected DELETE event for todelete.go")
	case err := <-w.Errors():
		t.Fatalf("unexpected error: %v", err)
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for delete event")
	}

	require.NoError(t, w.Stop())
}

func TestHybridWatcher_IgnoresGitignorePatterns(t *testing.T) {
	// Given: a temp directory with .gitignore
	tempDir := t.TempDir()
	gitignore := filepath.Join(tempDir, ".gitignore")
	require.NoError(t, os.WriteFile(gitignore, []byte("*.tmp\n"), 0o644))

	opts := Options{
		DebounceWindow:  50 * time.Millisecond,
		EventBufferSize: 100,
	}.WithDefaults()

	w, err := NewHybridWatcher(opts)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = w.Start(ctx, tempDir)
	}()

	// Wait for watcher to initialize
	time.Sleep(100 * time.Millisecond)

	// When: a file matching .gitignore is created
	tmpFile := filepath.Join(tempDir, "ignored.tmp")
	require.NoError(t, os.WriteFile(tmpFile, []byte("temp"), 0o644))

	// And: a non-ignored file is created
	goFile := filepath.Join(tempDir, "included.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main"), 0o644))

	// Then: only the .go file event is received
	var gotGoFile bool
	timeout := time.After(1 * time.Second)
loop:
	for {
		select {
		case events := <-w.Events():
			for _, e := range events {
				if filepath.Base(e.Path) == "included.go" {
					gotGoFile = true
				}
				// tmp files should not appear
				assert.NotEqual(t, ".tmp", filepath.Ext(e.Path),
					"should not receive events for .tmp files")
			}
		case <-timeout:
			break loop
		}
	}

	assert.True(t, gotGoFile, "should have received event for .go file")
	require.NoError(t, w.Stop())
}

func TestHybridWatcher_IgnoresAmanmcpDirectory(t *testing.T) {
	// Given: a temp directory
	tempDir := t.TempDir()

	// Create .amanmcp directory
	amanmcpDir := filepath.Join(tempDir, ".amanmcp")
	require.NoError(t, os.MkdirAll(amanmcpDir, 0o755))

	opts := Options{
		DebounceWindow:  50 * time.Millisecond,
		EventBufferSize: 100,
	}.WithDefaults()

	w, err := NewHybridWatcher(opts)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = w.Start(ctx, tempDir)
	}()

	// Wait for watcher to initialize
	time.Sleep(100 * time.Millisecond)

	// When: files in .amanmcp are created
	indexFile := filepath.Join(amanmcpDir, "index.db")
	require.NoError(t, os.WriteFile(indexFile, []byte("data"), 0o644))

	// And: a regular file is created
	goFile := filepath.Join(tempDir, "main.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main"), 0o644))

	// Then: only the regular file event is received
	var gotGoFile bool
	timeout := time.After(1 * time.Second)
loop:
	for {
		select {
		case events := <-w.Events():
			for _, e := range events {
				if filepath.Base(e.Path) == "main.go" {
					gotGoFile = true
				}
				// .amanmcp files should not appear
				assert.NotContains(t, e.Path, ".amanmcp",
					"should not receive events for .amanmcp directory")
			}
		case <-timeout:
			break loop
		}
	}

	assert.True(t, gotGoFile, "should have received event for .go file")
	require.NoError(t, w.Stop())
}

func TestHybridWatcher_DetectsNewSubdirectory(t *testing.T) {
	// Given: a temp directory and hybrid watcher
	tempDir := t.TempDir()
	opts := Options{
		DebounceWindow:  50 * time.Millisecond,
		EventBufferSize: 100,
	}.WithDefaults()

	w, err := NewHybridWatcher(opts)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = w.Start(ctx, tempDir)
	}()

	// Wait for watcher to initialize
	time.Sleep(100 * time.Millisecond)

	// When: a new subdirectory with files is created
	subDir := filepath.Join(tempDir, "subdir")
	require.NoError(t, os.MkdirAll(subDir, 0o755))
	subFile := filepath.Join(subDir, "sub.go")
	require.NoError(t, os.WriteFile(subFile, []byte("package subdir"), 0o644))

	// Then: events are detected (may need longer timeout for recursive watch)
	var gotEvent bool
	timeout := time.After(2 * time.Second)
loop:
	for {
		select {
		case events := <-w.Events():
			for _, e := range events {
				if e.Operation == OpCreate {
					gotEvent = true
				}
			}
		case <-timeout:
			break loop
		}
	}

	assert.True(t, gotEvent, "should have received create event for subdirectory or file")
	require.NoError(t, w.Stop())
}

func TestHybridWatcher_Stop_ClosesChannels(t *testing.T) {
	// Given: a hybrid watcher
	opts := DefaultOptions()
	w, err := NewHybridWatcher(opts)
	require.NoError(t, err)

	// When: stopped
	require.NoError(t, w.Stop())

	// Then: events channel is closed
	select {
	case _, ok := <-w.Events():
		assert.False(t, ok, "events channel should be closed")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for channel close")
	}
}

func TestHybridWatcher_DroppedBatches_InitiallyZero(t *testing.T) {
	// Given: a new hybrid watcher
	opts := DefaultOptions()
	w, err := NewHybridWatcher(opts)
	require.NoError(t, err)
	defer func() { _ = w.Stop() }()

	// Then: dropped batches count is zero
	assert.Equal(t, uint64(0), w.DroppedBatches())
}

func TestHybridWatcher_DroppedBatches_IncrementsOnOverflow(t *testing.T) {
	// Given: a hybrid watcher with a tiny buffer
	opts := Options{
		EventBufferSize: 1, // Very small buffer to trigger overflow
	}.WithDefaults()

	w, err := NewHybridWatcher(opts)
	require.NoError(t, err)
	defer func() { _ = w.Stop() }()

	// When: we emit more batches than the buffer can hold
	// Fill the buffer first
	w.emitEvents([]FileEvent{{Path: "/test1.go", Operation: OpCreate}})

	// Now emit more - these should be dropped
	w.emitEvents([]FileEvent{{Path: "/test2.go", Operation: OpCreate}})
	w.emitEvents([]FileEvent{{Path: "/test3.go", Operation: OpCreate}})

	// Then: dropped batches count reflects the drops
	assert.Equal(t, uint64(2), w.DroppedBatches())
}
