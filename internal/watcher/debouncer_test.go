package watcher

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDebouncer_SingleEvent_PassesThrough(t *testing.T) {
	// Given: a debouncer with short window
	d := NewDebouncer(50 * time.Millisecond)
	defer d.Stop()

	// When: a single event is added
	event := FileEvent{
		Path:      "test.go",
		Operation: OpCreate,
		Timestamp: time.Now(),
	}
	d.Add(event)

	// Then: the event passes through after the debounce window
	select {
	case events := <-d.Output():
		require.Len(t, events, 1)
		assert.Equal(t, "test.go", events[0].Path)
		assert.Equal(t, OpCreate, events[0].Operation)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for debounced event")
	}
}

func TestDebouncer_MultipleEventsForSameFile_Coalesces(t *testing.T) {
	// Given: a debouncer with short window
	d := NewDebouncer(100 * time.Millisecond)
	defer d.Stop()

	// When: multiple events for the same file are added rapidly
	for i := 0; i < 5; i++ {
		d.Add(FileEvent{
			Path:      "test.go",
			Operation: OpModify,
			Timestamp: time.Now(),
		})
		time.Sleep(10 * time.Millisecond)
	}

	// Then: only one event comes out
	select {
	case events := <-d.Output():
		require.Len(t, events, 1)
		assert.Equal(t, "test.go", events[0].Path)
		assert.Equal(t, OpModify, events[0].Operation)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for debounced events")
	}
}

func TestDebouncer_CreateThenDelete_NoEvent(t *testing.T) {
	// Given: a debouncer with short window
	d := NewDebouncer(50 * time.Millisecond)
	defer d.Stop()

	// When: CREATE followed by DELETE for same file
	d.Add(FileEvent{
		Path:      "temp.go",
		Operation: OpCreate,
		Timestamp: time.Now(),
	})
	d.Add(FileEvent{
		Path:      "temp.go",
		Operation: OpDelete,
		Timestamp: time.Now(),
	})

	// Then: no event is emitted (file never really existed)
	select {
	case events := <-d.Output():
		// Empty batch is acceptable
		assert.Empty(t, events)
	case <-time.After(200 * time.Millisecond):
		// No event is also acceptable
	}
}

func TestDebouncer_ModifyThenDelete_DeleteOnly(t *testing.T) {
	// Given: a debouncer with short window
	d := NewDebouncer(50 * time.Millisecond)
	defer d.Stop()

	// When: MODIFY followed by DELETE
	d.Add(FileEvent{
		Path:      "existing.go",
		Operation: OpModify,
		Timestamp: time.Now(),
	})
	d.Add(FileEvent{
		Path:      "existing.go",
		Operation: OpDelete,
		Timestamp: time.Now(),
	})

	// Then: only DELETE is emitted
	select {
	case events := <-d.Output():
		require.Len(t, events, 1)
		assert.Equal(t, OpDelete, events[0].Operation)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for debounced event")
	}
}

func TestDebouncer_DeleteThenCreate_ModifyEvent(t *testing.T) {
	// Given: a debouncer with short window
	d := NewDebouncer(50 * time.Millisecond)
	defer d.Stop()

	// When: DELETE followed by CREATE (file was replaced)
	d.Add(FileEvent{
		Path:      "replaced.go",
		Operation: OpDelete,
		Timestamp: time.Now(),
	})
	d.Add(FileEvent{
		Path:      "replaced.go",
		Operation: OpCreate,
		Timestamp: time.Now(),
	})

	// Then: MODIFY is emitted (file was replaced)
	select {
	case events := <-d.Output():
		require.Len(t, events, 1)
		assert.Equal(t, OpModify, events[0].Operation)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for debounced event")
	}
}

func TestDebouncer_DifferentFiles_IndependentEvents(t *testing.T) {
	// Given: a debouncer with short window
	d := NewDebouncer(50 * time.Millisecond)
	defer d.Stop()

	// When: events for different files are added
	d.Add(FileEvent{Path: "a.go", Operation: OpCreate, Timestamp: time.Now()})
	d.Add(FileEvent{Path: "b.go", Operation: OpModify, Timestamp: time.Now()})
	d.Add(FileEvent{Path: "c.go", Operation: OpDelete, Timestamp: time.Now()})

	// Then: all events are emitted
	select {
	case events := <-d.Output():
		require.Len(t, events, 3)

		// Check all paths are present (order may vary)
		paths := make(map[string]Operation)
		for _, e := range events {
			paths[e.Path] = e.Operation
		}
		assert.Equal(t, OpCreate, paths["a.go"])
		assert.Equal(t, OpModify, paths["b.go"])
		assert.Equal(t, OpDelete, paths["c.go"])
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for debounced events")
	}
}

func TestDebouncer_Stop_ClosesOutput(t *testing.T) {
	// Given: a debouncer
	d := NewDebouncer(50 * time.Millisecond)

	// When: stopped
	d.Stop()

	// Then: output channel is closed
	select {
	case _, ok := <-d.Output():
		assert.False(t, ok, "channel should be closed")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for channel close")
	}
}

func TestDebouncer_CreateThenModify_CreateOnly(t *testing.T) {
	// Given: a debouncer with short window
	d := NewDebouncer(50 * time.Millisecond)
	defer d.Stop()

	// When: CREATE followed by MODIFY
	d.Add(FileEvent{
		Path:      "new.go",
		Operation: OpCreate,
		Timestamp: time.Now(),
	})
	d.Add(FileEvent{
		Path:      "new.go",
		Operation: OpModify,
		Timestamp: time.Now(),
	})

	// Then: only CREATE is emitted (file is still new)
	select {
	case events := <-d.Output():
		require.Len(t, events, 1)
		assert.Equal(t, OpCreate, events[0].Operation)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for debounced event")
	}
}
