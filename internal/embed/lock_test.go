package embed

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestFileLock_LockUnlock(t *testing.T) {
	// Create temp directory
	dir := t.TempDir()

	lock := NewFileLock(dir)

	// Lock should succeed
	if err := lock.Lock(); err != nil {
		t.Fatalf("Lock() failed: %v", err)
	}

	// Verify lock file exists
	if _, err := os.Stat(lock.Path()); os.IsNotExist(err) {
		t.Error("Lock file was not created")
	}

	// Unlock should succeed
	if err := lock.Unlock(); err != nil {
		t.Fatalf("Unlock() failed: %v", err)
	}
}

func TestFileLock_UnlockWithoutLock(t *testing.T) {
	dir := t.TempDir()
	lock := NewFileLock(dir)

	// Unlock without Lock should not error
	if err := lock.Unlock(); err != nil {
		t.Errorf("Unlock() without Lock() should not error: %v", err)
	}
}

func TestFileLock_DoubleUnlock(t *testing.T) {
	dir := t.TempDir()
	lock := NewFileLock(dir)

	if err := lock.Lock(); err != nil {
		t.Fatalf("Lock() failed: %v", err)
	}

	if err := lock.Unlock(); err != nil {
		t.Fatalf("First Unlock() failed: %v", err)
	}

	// Second unlock should not error
	if err := lock.Unlock(); err != nil {
		t.Errorf("Second Unlock() should not error: %v", err)
	}
}

func TestFileLock_TryLock_Success(t *testing.T) {
	dir := t.TempDir()
	lock := NewFileLock(dir)

	acquired, err := lock.TryLock()
	if err != nil {
		t.Fatalf("TryLock() failed: %v", err)
	}
	if !acquired {
		t.Error("TryLock() should return true when lock is available")
	}

	if err := lock.Unlock(); err != nil {
		t.Fatalf("Unlock() failed: %v", err)
	}
}

func TestFileLock_TryLock_AlreadyLocked(t *testing.T) {
	dir := t.TempDir()

	// First lock
	lock1 := NewFileLock(dir)
	if err := lock1.Lock(); err != nil {
		t.Fatalf("Lock() failed: %v", err)
	}
	defer func() { _ = lock1.Unlock() }()

	// Second lock should fail with TryLock
	lock2 := NewFileLock(dir)
	acquired, err := lock2.TryLock()
	if err != nil {
		t.Fatalf("TryLock() error: %v", err)
	}
	if acquired {
		t.Error("TryLock() should return false when lock is held")
		_ = lock2.Unlock()
	}
}

func TestFileLock_Path(t *testing.T) {
	dir := "/some/dir"
	lock := NewFileLock(dir)

	expected := filepath.Join(dir, ".download.lock")
	if lock.Path() != expected {
		t.Errorf("Path() = %q, want %q", lock.Path(), expected)
	}
}

func TestFileLock_ConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	counter := 0
	var mu sync.Mutex

	// Run multiple goroutines trying to increment counter
	// With proper locking, the final count should equal numGoroutines
	numGoroutines := 10
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			lock := NewFileLock(dir)
			if err := lock.Lock(); err != nil {
				t.Errorf("Lock() failed: %v", err)
				return
			}
			defer func() { _ = lock.Unlock() }()

			// Critical section
			mu.Lock()
			counter++
			mu.Unlock()

			// Simulate some work
			time.Sleep(10 * time.Millisecond)
		}()
	}

	wg.Wait()

	if counter != numGoroutines {
		t.Errorf("counter = %d, want %d", counter, numGoroutines)
	}
}

func TestFileLock_CreatesDirectory(t *testing.T) {
	// Use a nested directory that doesn't exist
	baseDir := t.TempDir()
	nestedDir := filepath.Join(baseDir, "nested", "dir", "for", "lock")

	lock := NewFileLock(nestedDir)

	if err := lock.Lock(); err != nil {
		t.Fatalf("Lock() failed to create nested directory: %v", err)
	}
	defer func() { _ = lock.Unlock() }()

	// Verify directory was created
	if _, err := os.Stat(nestedDir); os.IsNotExist(err) {
		t.Error("Lock() did not create the nested directory")
	}
}

// ============================================================================
// DEBT-007: Lock Lifecycle State Tests
// ============================================================================

func TestFileLock_IsLocked(t *testing.T) {
	lock := NewFileLock(t.TempDir())

	// Initially not locked
	if lock.IsLocked() {
		t.Error("New lock should not be locked")
	}

	// After Lock()
	if err := lock.Lock(); err != nil {
		t.Fatalf("Lock() failed: %v", err)
	}
	if !lock.IsLocked() {
		t.Error("Lock should be locked after Lock()")
	}

	// After Unlock()
	if err := lock.Unlock(); err != nil {
		t.Fatalf("Unlock() failed: %v", err)
	}
	if lock.IsLocked() {
		t.Error("Lock should not be locked after Unlock()")
	}
}

func TestFileLock_IsLocked_TryLock(t *testing.T) {
	lock := NewFileLock(t.TempDir())

	// Initially not locked
	if lock.IsLocked() {
		t.Error("New lock should not be locked")
	}

	// After TryLock()
	acquired, err := lock.TryLock()
	if err != nil {
		t.Fatalf("TryLock() failed: %v", err)
	}
	if !acquired {
		t.Fatal("TryLock() should have acquired the lock")
	}
	if !lock.IsLocked() {
		t.Error("Lock should be locked after TryLock()")
	}

	// After Unlock()
	if err := lock.Unlock(); err != nil {
		t.Fatalf("Unlock() failed: %v", err)
	}
	if lock.IsLocked() {
		t.Error("Lock should not be locked after Unlock()")
	}
}

func TestFileLock_IsLocked_FailedTryLock(t *testing.T) {
	dir := t.TempDir()

	// First lock holds the file
	lock1 := NewFileLock(dir)
	if err := lock1.Lock(); err != nil {
		t.Fatalf("Lock() failed: %v", err)
	}
	defer func() { _ = lock1.Unlock() }()

	// Second lock fails to acquire
	lock2 := NewFileLock(dir)
	acquired, err := lock2.TryLock()
	if err != nil {
		t.Fatalf("TryLock() error: %v", err)
	}
	if acquired {
		t.Fatal("TryLock() should have failed")
	}

	// lock2 should NOT be marked as locked
	if lock2.IsLocked() {
		t.Error("Failed TryLock() should not mark lock as locked")
	}
}
