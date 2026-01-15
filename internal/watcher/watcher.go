package watcher

import (
	"context"
	"time"
)

// Operation represents a file system operation type.
type Operation int

const (
	// OpCreate indicates a new file or directory was created.
	OpCreate Operation = iota
	// OpModify indicates an existing file was modified.
	OpModify
	// OpDelete indicates a file or directory was deleted.
	OpDelete
	// OpRename indicates a file or directory was renamed.
	OpRename
	// OpGitignoreChange indicates a .gitignore file was modified.
	// This triggers index reconciliation to remove newly-ignored files
	// and add newly-unignored files.
	OpGitignoreChange
	// OpConfigChange indicates the .amanmcp.yaml config file was modified.
	// This triggers reload of exclude patterns and reconciliation.
	OpConfigChange
)

// String returns a human-readable representation of the operation.
func (op Operation) String() string {
	switch op {
	case OpCreate:
		return "CREATE"
	case OpModify:
		return "MODIFY"
	case OpDelete:
		return "DELETE"
	case OpRename:
		return "RENAME"
	case OpGitignoreChange:
		return "GITIGNORE_CHANGE"
	case OpConfigChange:
		return "CONFIG_CHANGE"
	default:
		return "UNKNOWN"
	}
}

// FileEvent represents a file system event.
type FileEvent struct {
	// Path is the relative path to the file or directory.
	Path string

	// OldPath is the previous path for rename events.
	// Empty for non-rename events.
	OldPath string

	// Operation is the type of file system operation.
	Operation Operation

	// IsDir indicates if the event is for a directory.
	IsDir bool

	// Timestamp is when the event was detected.
	Timestamp time.Time
}

// Watcher defines the interface for file system watching.
type Watcher interface {
	// Start begins watching the given directory recursively.
	// Returns an error if watching fails to initialize.
	// The watcher runs until Stop is called or context is cancelled.
	Start(ctx context.Context, path string) error

	// Stop stops the watcher and releases resources.
	// Safe to call multiple times.
	Stop() error

	// Events returns a channel of file events.
	// The channel is closed when the watcher stops.
	Events() <-chan FileEvent

	// Errors returns a channel of watcher errors.
	// Non-fatal errors are sent here; the watcher continues running.
	// The channel is closed when the watcher stops.
	Errors() <-chan error
}

// Options configures the watcher behavior.
type Options struct {
	// DebounceWindow is the time to wait before emitting coalesced events.
	// Default: 200ms
	DebounceWindow time.Duration

	// PollInterval is the interval for polling mode (fallback).
	// Default: 5s
	PollInterval time.Duration

	// EventBufferSize is the size of the event channel buffer.
	// Default: 1000
	EventBufferSize int

	// IgnorePatterns are additional patterns to ignore beyond .gitignore.
	// Patterns use gitignore syntax.
	IgnorePatterns []string
}

// DefaultOptions returns the default watcher options.
func DefaultOptions() Options {
	return Options{
		DebounceWindow:  200 * time.Millisecond,
		PollInterval:    5 * time.Second,
		EventBufferSize: 1000,
		IgnorePatterns:  nil,
	}
}

// Validate validates the options and returns an error if invalid.
func (o Options) Validate() error {
	// All options have sensible defaults, no validation needed currently
	return nil
}

// WithDefaults returns options with defaults applied for zero values.
func (o Options) WithDefaults() Options {
	defaults := DefaultOptions()
	if o.DebounceWindow == 0 {
		o.DebounceWindow = defaults.DebounceWindow
	}
	if o.PollInterval == 0 {
		o.PollInterval = defaults.PollInterval
	}
	if o.EventBufferSize == 0 {
		o.EventBufferSize = defaults.EventBufferSize
	}
	return o
}
