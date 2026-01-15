package ui

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProgressTracker(t *testing.T) {
	// When: creating a new tracker
	tracker := NewProgressTracker()

	// Then: starts at StageScanning with zero progress
	stats := tracker.Stats()
	assert.Equal(t, StageScanning, stats.Stage)
	assert.Equal(t, 0, stats.Current)
	assert.Equal(t, 0, stats.Total)
}

func TestProgressTracker_SetStage(t *testing.T) {
	// Given: a new tracker
	tracker := NewProgressTracker()

	// When: setting stage with total
	tracker.SetStage(StageChunking, 100)

	// Then: stage and total are updated
	stats := tracker.Stats()
	assert.Equal(t, StageChunking, stats.Stage)
	assert.Equal(t, 100, stats.Total)
	assert.Equal(t, 0, stats.Current) // Current resets on stage change
}

func TestProgressTracker_Update(t *testing.T) {
	// Given: a tracker in chunking stage
	tracker := NewProgressTracker()
	tracker.SetStage(StageChunking, 100)

	// When: updating progress
	tracker.Update(50, "src/main.go")

	// Then: current and file are updated
	stats := tracker.Stats()
	assert.Equal(t, 50, stats.Current)
	assert.Equal(t, "src/main.go", stats.CurrentFile)
}

func TestProgressTracker_Progress_Percentage(t *testing.T) {
	tests := []struct {
		name     string
		current  int
		total    int
		expected float64
	}{
		{"zero total", 0, 0, 0.0},
		{"zero current", 0, 100, 0.0},
		{"half done", 50, 100, 0.5},
		{"complete", 100, 100, 1.0},
		{"over 100%", 150, 100, 1.0}, // Capped at 1.0
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := NewProgressTracker()
			tracker.SetStage(StageScanning, tt.total)
			tracker.Update(tt.current, "")

			assert.InDelta(t, tt.expected, tracker.Progress(), 0.01)
		})
	}
}

func TestProgressTracker_AddError(t *testing.T) {
	// Given: a tracker
	tracker := NewProgressTracker()

	// When: adding an error
	tracker.AddError(ErrorEvent{
		File:   "broken.go",
		Err:    assert.AnError,
		IsWarn: false,
	})

	// Then: error count increases
	stats := tracker.Stats()
	assert.Equal(t, 1, stats.ErrorCount)
	assert.Equal(t, 0, stats.WarnCount)

	// When: adding a warning
	tracker.AddError(ErrorEvent{
		File:   "warning.go",
		Err:    assert.AnError,
		IsWarn: true,
	})

	// Then: warning count increases
	stats = tracker.Stats()
	assert.Equal(t, 1, stats.ErrorCount)
	assert.Equal(t, 1, stats.WarnCount)
}

func TestProgressTracker_ETA_ZeroProgress(t *testing.T) {
	// Given: a tracker with no progress
	tracker := NewProgressTracker()
	tracker.SetStage(StageScanning, 100)

	// When: calculating ETA
	eta := tracker.ETA()

	// Then: returns 0 (unknown)
	assert.Equal(t, time.Duration(0), eta)
}

func TestProgressTracker_ETA_PartialProgress(t *testing.T) {
	// Given: a tracker with some progress
	tracker := NewProgressTracker()
	tracker.SetStage(StageScanning, 100)

	// Simulate some time passing
	time.Sleep(50 * time.Millisecond)

	// Update to 50%
	tracker.Update(50, "file.go")

	// When: calculating ETA
	eta := tracker.ETA()

	// Then: ETA should be roughly equal to elapsed time (50% done in ~50ms, so ~50ms remaining)
	// Allow some variance for test execution time
	assert.True(t, eta >= 0, "ETA should be non-negative")
	assert.True(t, eta < 500*time.Millisecond, "ETA should be reasonable")
}

func TestProgressTracker_ThreadSafety(t *testing.T) {
	// Given: a tracker
	tracker := NewProgressTracker()
	tracker.SetStage(StageScanning, 1000)

	// When: concurrent updates
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			tracker.Update(n, "file.go")
			tracker.Progress()
			tracker.Stats()
		}(i)
	}
	wg.Wait()

	// Then: no panic, data is consistent
	stats := tracker.Stats()
	require.NotNil(t, stats)
}

func TestProgressTracker_StageTransition(t *testing.T) {
	// Given: a tracker progressing through stages
	tracker := NewProgressTracker()

	// Stage 1: Scanning
	tracker.SetStage(StageScanning, 100)
	tracker.Update(100, "last.go")
	assert.Equal(t, StageScanning, tracker.Stats().Stage)

	// Stage 2: Chunking
	tracker.SetStage(StageChunking, 500)
	assert.Equal(t, StageChunking, tracker.Stats().Stage)
	assert.Equal(t, 0, tracker.Stats().Current) // Reset on stage change
	assert.Equal(t, 500, tracker.Stats().Total)

	// Stage 3: Embedding
	tracker.SetStage(StageEmbedding, 500)
	tracker.Update(250, "embedding...")
	assert.Equal(t, StageEmbedding, tracker.Stats().Stage)

	// Stage 4: Indexing
	tracker.SetStage(StageIndexing, 500)
	tracker.Update(500, "")
	assert.Equal(t, StageIndexing, tracker.Stats().Stage)

	// Complete
	tracker.SetStage(StageComplete, 0)
	assert.Equal(t, StageComplete, tracker.Stats().Stage)
}

func TestProgressTracker_ElapsedTime(t *testing.T) {
	// Given: a tracker
	tracker := NewProgressTracker()

	// When: some time passes
	time.Sleep(10 * time.Millisecond)

	// Then: elapsed time is tracked
	elapsed := tracker.Elapsed()
	assert.True(t, elapsed >= 10*time.Millisecond)
}

func TestProgressStats_Fields(t *testing.T) {
	// Given: a configured tracker
	tracker := NewProgressTracker()
	tracker.SetStage(StageEmbedding, 200)
	tracker.Update(100, "current.go")
	tracker.AddError(ErrorEvent{File: "err.go", Err: assert.AnError, IsWarn: false})
	tracker.AddError(ErrorEvent{File: "warn.go", Err: assert.AnError, IsWarn: true})

	// When: getting stats
	stats := tracker.Stats()

	// Then: all fields are populated
	assert.Equal(t, StageEmbedding, stats.Stage)
	assert.Equal(t, 100, stats.Current)
	assert.Equal(t, 200, stats.Total)
	assert.InDelta(t, 0.5, stats.Progress, 0.01)
	assert.Equal(t, "current.go", stats.CurrentFile)
	assert.Equal(t, 1, stats.ErrorCount)
	assert.Equal(t, 1, stats.WarnCount)
}
