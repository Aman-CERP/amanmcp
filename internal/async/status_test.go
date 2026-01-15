package async

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewIndexProgress(t *testing.T) {
	// Given/When: creating a new progress tracker
	p := NewIndexProgress()

	// Then: should be initialized with indexing status
	require.NotNil(t, p)
	snap := p.Snapshot()
	assert.Equal(t, string(StatusIndexing), snap.Status)
	assert.Equal(t, string(StageScanning), snap.Stage)
	assert.Equal(t, 0, snap.FilesTotal)
	assert.Equal(t, 0, snap.FilesProcessed)
	assert.True(t, p.IsIndexing())
}

func TestIndexProgress_SetStage(t *testing.T) {
	tests := []struct {
		name      string
		stage     IndexingStage
		total     int
		wantStage string
		wantTotal int
	}{
		{
			name:      "scanning stage",
			stage:     StageScanning,
			total:     100,
			wantStage: "scanning",
			wantTotal: 100,
		},
		{
			name:      "chunking stage",
			stage:     StageChunking,
			total:     500,
			wantStage: "chunking",
			wantTotal: 500,
		},
		{
			name:      "embedding stage",
			stage:     StageEmbedding,
			total:     1000,
			wantStage: "embedding",
			wantTotal: 1000,
		},
		{
			name:      "indexing stage",
			stage:     StageIndexing,
			total:     1000,
			wantStage: "indexing",
			wantTotal: 1000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewIndexProgress()

			// When: setting stage
			p.SetStage(tt.stage, tt.total)

			// Then: snapshot reflects the change
			snap := p.Snapshot()
			assert.Equal(t, tt.wantStage, snap.Stage)
			assert.Equal(t, tt.wantTotal, snap.FilesTotal)
		})
	}
}

func TestIndexProgress_UpdateFiles(t *testing.T) {
	// Given: progress tracker in chunking stage
	p := NewIndexProgress()
	p.SetStage(StageChunking, 100)

	// When: updating processed files
	p.UpdateFiles(50)

	// Then: snapshot shows updated count
	snap := p.Snapshot()
	assert.Equal(t, 50, snap.FilesProcessed)
	assert.Equal(t, 100, snap.FilesTotal)
}

func TestIndexProgress_UpdateChunks(t *testing.T) {
	// Given: progress tracker in embedding stage
	p := NewIndexProgress()
	p.SetStage(StageEmbedding, 100)
	p.SetChunksTotal(500)

	// When: updating indexed chunks
	p.UpdateChunks(250)

	// Then: snapshot shows updated count
	snap := p.Snapshot()
	assert.Equal(t, 250, snap.ChunksIndexed)
	assert.Equal(t, 500, snap.ChunksTotal)
}

func TestIndexProgress_SetError(t *testing.T) {
	// Given: progress tracker
	p := NewIndexProgress()

	// When: setting an error
	p.SetError("embedding failed: connection refused")

	// Then: status changes to error
	snap := p.Snapshot()
	assert.Equal(t, string(StatusError), snap.Status)
	assert.Equal(t, "embedding failed: connection refused", snap.ErrorMessage)
	assert.False(t, p.IsIndexing())
}

func TestIndexProgress_SetReady(t *testing.T) {
	// Given: progress tracker with some progress
	p := NewIndexProgress()
	p.SetStage(StageIndexing, 100)
	p.UpdateFiles(100)

	// When: marking as ready
	p.SetReady()

	// Then: status changes to ready
	snap := p.Snapshot()
	assert.Equal(t, string(StatusReady), snap.Status)
	assert.False(t, p.IsIndexing())
}

func TestIndexProgress_ProgressPct(t *testing.T) {
	tests := []struct {
		name           string
		total          int
		processed      int
		wantProgressPc float64
	}{
		{
			name:           "zero total returns zero",
			total:          0,
			processed:      0,
			wantProgressPc: 0.0,
		},
		{
			name:           "half complete",
			total:          100,
			processed:      50,
			wantProgressPc: 50.0,
		},
		{
			name:           "fully complete",
			total:          100,
			processed:      100,
			wantProgressPc: 100.0,
		},
		{
			name:           "partial progress",
			total:          1000,
			processed:      333,
			wantProgressPc: 33.3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewIndexProgress()
			p.SetStage(StageChunking, tt.total)
			p.UpdateFiles(tt.processed)

			snap := p.Snapshot()
			assert.InDelta(t, tt.wantProgressPc, snap.ProgressPct, 0.1)
		})
	}
}

func TestIndexProgress_ElapsedSeconds(t *testing.T) {
	// Given: progress tracker created at a specific time
	p := NewIndexProgress()

	// When: some time passes
	time.Sleep(100 * time.Millisecond)

	// Then: elapsed seconds is tracked
	snap := p.Snapshot()
	assert.GreaterOrEqual(t, snap.ElapsedSeconds, 0)
}

func TestIndexProgress_Snapshot_Immutable(t *testing.T) {
	// Given: progress tracker with initial state
	p := NewIndexProgress()
	p.SetStage(StageChunking, 100)
	p.UpdateFiles(50)

	// When: taking a snapshot and modifying progress
	snap1 := p.Snapshot()
	p.UpdateFiles(75)
	snap2 := p.Snapshot()

	// Then: first snapshot is unchanged
	assert.Equal(t, 50, snap1.FilesProcessed)
	assert.Equal(t, 75, snap2.FilesProcessed)
}

func TestIndexProgress_ThreadSafe(t *testing.T) {
	// Given: progress tracker
	p := NewIndexProgress()
	p.SetStage(StageEmbedding, 1000)

	// When: concurrent reads and writes
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)

		// Writer goroutine
		go func(n int) {
			defer wg.Done()
			p.UpdateFiles(n)
		}(i)

		// Reader goroutine
		go func() {
			defer wg.Done()
			_ = p.Snapshot()
			_ = p.IsIndexing()
		}()
	}

	wg.Wait()

	// Then: no race conditions (test passes with -race flag)
	snap := p.Snapshot()
	assert.GreaterOrEqual(t, snap.FilesProcessed, 0)
	assert.LessOrEqual(t, snap.FilesProcessed, 99)
}

func TestIndexProgress_ConcurrentStageTransitions(t *testing.T) {
	// Given: progress tracker
	p := NewIndexProgress()

	// When: concurrent stage transitions
	var wg sync.WaitGroup
	stages := []IndexingStage{StageScanning, StageChunking, StageEmbedding, StageIndexing}

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			stage := stages[n%len(stages)]
			p.SetStage(stage, n*10)
			_ = p.Snapshot()
		}(i)
	}

	wg.Wait()

	// Then: no race conditions
	snap := p.Snapshot()
	assert.NotEmpty(t, snap.Stage)
}

func TestIndexingStatus_Values(t *testing.T) {
	// Verify constant values match expected strings
	assert.Equal(t, "indexing", string(StatusIndexing))
	assert.Equal(t, "ready", string(StatusReady))
	assert.Equal(t, "error", string(StatusError))
}

func TestIndexingStage_Values(t *testing.T) {
	// Verify constant values match expected strings
	assert.Equal(t, "scanning", string(StageScanning))
	assert.Equal(t, "chunking", string(StageChunking))
	assert.Equal(t, "embedding", string(StageEmbedding))
	assert.Equal(t, "indexing", string(StageIndexing))
}
