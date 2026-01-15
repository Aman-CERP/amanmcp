package watcher

import (
	"log/slog"
	"sync"
	"time"
)

// Debouncer coalesces rapid file events to prevent index thrashing.
// Events for the same path within the debounce window are merged according
// to these rules:
//   - CREATE + MODIFY = CREATE (file is still new)
//   - CREATE + DELETE = nothing (file never really existed)
//   - MODIFY + DELETE = DELETE (file is gone)
//   - DELETE + CREATE = MODIFY (file was replaced)
type Debouncer struct {
	window  time.Duration
	pending map[string]*pendingEvent
	mu      sync.Mutex
	output  chan []FileEvent
	timer   *time.Timer
	stopCh  chan struct{}
	stopped bool
}

type pendingEvent struct {
	event    FileEvent
	firstOp  Operation // Track the first operation for coalescing
	lastSeen time.Time
}

// NewDebouncer creates a new debouncer with the given window duration.
// Events are coalesced within this window before being emitted.
func NewDebouncer(window time.Duration) *Debouncer {
	d := &Debouncer{
		window:  window,
		pending: make(map[string]*pendingEvent),
		output:  make(chan []FileEvent, 10),
		stopCh:  make(chan struct{}),
	}
	return d
}

// Add adds an event to be debounced.
// Events for the same path are coalesced according to the coalescing rules.
func (d *Debouncer) Add(event FileEvent) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.stopped {
		return
	}

	path := event.Path
	now := time.Now()

	if existing, ok := d.pending[path]; ok {
		// Coalesce with existing event
		coalesced := d.coalesce(existing, event)
		if coalesced == nil {
			// Events cancelled each other out (CREATE + DELETE)
			delete(d.pending, path)
		} else {
			existing.event = *coalesced
			existing.lastSeen = now
		}
	} else {
		// New event for this path
		d.pending[path] = &pendingEvent{
			event:    event,
			firstOp:  event.Operation,
			lastSeen: now,
		}
	}

	d.scheduleFlush()
}

// coalesce merges two events according to the coalescing rules.
// Returns nil if the events cancel each other out.
func (d *Debouncer) coalesce(existing *pendingEvent, new FileEvent) *FileEvent {
	// Coalescing rules based on operation sequence
	switch existing.firstOp {
	case OpCreate:
		switch new.Operation {
		case OpModify:
			// CREATE + MODIFY = CREATE (keep original)
			return &existing.event
		case OpDelete:
			// CREATE + DELETE = nothing
			return nil
		default:
			// Keep the new operation
			return &new
		}

	case OpModify:
		switch new.Operation {
		case OpModify:
			// MODIFY + MODIFY = MODIFY (keep latest)
			return &new
		case OpDelete:
			// MODIFY + DELETE = DELETE
			return &new
		default:
			return &new
		}

	case OpDelete:
		switch new.Operation {
		case OpCreate:
			// DELETE + CREATE = MODIFY (file was replaced)
			result := new
			result.Operation = OpModify
			return &result
		default:
			return &new
		}

	default:
		// For unknown or rename operations, keep the latest
		return &new
	}
}

// scheduleFlush schedules a flush after the debounce window.
func (d *Debouncer) scheduleFlush() {
	if d.timer != nil {
		d.timer.Stop()
	}

	d.timer = time.AfterFunc(d.window, func() {
		d.flush()
	})
}

// flush emits all pending events.
func (d *Debouncer) flush() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.stopped || len(d.pending) == 0 {
		return
	}

	events := make([]FileEvent, 0, len(d.pending))
	for _, pe := range d.pending {
		events = append(events, pe.event)
	}
	d.pending = make(map[string]*pendingEvent)

	// Non-blocking send
	select {
	case d.output <- events:
	default:
		slog.Warn("debouncer output full, dropping batch",
			slog.Int("batch_size", len(events)),
		)
	}
}

// Output returns the channel of debounced events.
// Events are emitted as batches after the debounce window.
func (d *Debouncer) Output() <-chan []FileEvent {
	return d.output
}

// Stop stops the debouncer and closes the output channel.
// Safe to call multiple times.
func (d *Debouncer) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.stopped {
		return
	}

	d.stopped = true
	if d.timer != nil {
		d.timer.Stop()
	}
	close(d.stopCh)
	close(d.output)
}
