package embed

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
)

// FileLock provides cross-process file locking using gofrs/flock.
// This prevents race conditions when multiple AmanMCP instances
// try to download the embedding model simultaneously.
// Works on all platforms (Unix, Linux, macOS, Windows).
type FileLock struct {
	path   string
	flock  *flock.Flock
	locked bool // explicit state tracking for clarity
}

// NewFileLock creates a new file lock for the given directory.
// The lock file will be created at <dir>/.download.lock
func NewFileLock(dir string) *FileLock {
	lockPath := filepath.Join(dir, ".download.lock")
	return &FileLock{
		path:  lockPath,
		flock: flock.New(lockPath),
	}
}

// Lock acquires an exclusive lock on the file.
// This call blocks until the lock is available.
// If the lock file doesn't exist, it will be created.
func (l *FileLock) Lock() error {
	// Ensure directory exists
	dir := filepath.Dir(l.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create lock directory: %w", err)
	}

	// Acquire exclusive lock (blocking)
	if err := l.flock.Lock(); err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}

	l.locked = true
	return nil
}

// TryLock attempts to acquire the lock without blocking.
// Returns true if the lock was acquired, false if it's held by another process.
func (l *FileLock) TryLock() (bool, error) {
	// Ensure directory exists
	dir := filepath.Dir(l.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return false, fmt.Errorf("failed to create lock directory: %w", err)
	}

	// Try to acquire exclusive lock (non-blocking)
	acquired, err := l.flock.TryLock()
	if err != nil {
		return false, fmt.Errorf("failed to acquire lock: %w", err)
	}

	if acquired {
		l.locked = true
	}
	return acquired, nil
}

// Unlock releases the file lock.
// It's safe to call Unlock multiple times or on an unlocked FileLock.
func (l *FileLock) Unlock() error {
	if !l.locked {
		return nil
	}

	if err := l.flock.Unlock(); err != nil {
		l.locked = false
		return fmt.Errorf("failed to release lock: %w", err)
	}

	l.locked = false
	return nil
}

// Path returns the path to the lock file.
func (l *FileLock) Path() string {
	return l.path
}

// IsLocked returns true if the lock is currently held.
func (l *FileLock) IsLocked() bool {
	return l.locked
}
