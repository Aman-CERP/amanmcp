// Package watcher provides real-time file system watching with automatic
// debouncing and gitignore-aware filtering.
//
// The package implements a hybrid watching strategy:
//   - Primary: fsnotify for efficient event-based watching
//   - Fallback: Polling for environments where fsnotify fails (network mounts, Docker volumes)
//
// Events are debounced to coalesce rapid changes from IDEs and git operations,
// and filtered against .gitignore patterns to skip irrelevant files.
//
// Usage:
//
//	opts := watcher.DefaultOptions()
//	w, err := watcher.NewHybridWatcher(opts)
//	if err != nil {
//	    return err
//	}
//	defer w.Stop()
//
//	if err := w.Start(ctx, "/path/to/project"); err != nil {
//	    return err
//	}
//
//	for event := range w.Events() {
//	    switch event.Operation {
//	    case watcher.OpCreate:
//	        // Handle file creation
//	    case watcher.OpModify:
//	        // Handle file modification
//	    case watcher.OpDelete:
//	        // Handle file deletion
//	    }
//	}
package watcher
