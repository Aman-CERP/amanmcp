// Package async provides background processing infrastructure for AmanMCP.
package async

import (
	"sync"
	"time"
)

// IndexingStatus represents the overall indexing state.
type IndexingStatus string

const (
	// StatusIndexing indicates indexing is in progress.
	StatusIndexing IndexingStatus = "indexing"
	// StatusReady indicates indexing is complete and search is available.
	StatusReady IndexingStatus = "ready"
	// StatusError indicates indexing failed with an error.
	StatusError IndexingStatus = "error"
)

// IndexingStage represents the current stage of the indexing process.
type IndexingStage string

const (
	// StageScanning indicates the file discovery phase.
	StageScanning IndexingStage = "scanning"
	// StageChunking indicates the code/text chunking phase.
	StageChunking IndexingStage = "chunking"
	// StageEmbedding indicates the embedding generation phase.
	StageEmbedding IndexingStage = "embedding"
	// StageIndexing indicates the index building phase.
	StageIndexing IndexingStage = "indexing"
)

// IndexProgressSnapshot is an immutable snapshot of indexing progress.
type IndexProgressSnapshot struct {
	Status         string  `json:"status"`
	Stage          string  `json:"stage"`
	FilesTotal     int     `json:"files_total"`
	FilesProcessed int     `json:"files_processed"`
	ChunksTotal    int     `json:"chunks_total"`
	ChunksIndexed  int     `json:"chunks_indexed"`
	ProgressPct    float64 `json:"progress_pct"`
	ElapsedSeconds int     `json:"elapsed_seconds"`
	ErrorMessage   string  `json:"error_message,omitempty"`
}

// IndexProgress provides thread-safe tracking of indexing progress.
type IndexProgress struct {
	mu sync.RWMutex

	status         IndexingStatus
	stage          IndexingStage
	filesTotal     int
	filesProcessed int
	chunksTotal    int
	chunksIndexed  int
	startTime      time.Time
	errorMessage   string
}

// NewIndexProgress creates a new progress tracker initialized for indexing.
func NewIndexProgress() *IndexProgress {
	return &IndexProgress{
		status:    StatusIndexing,
		stage:     StageScanning,
		startTime: time.Now(),
	}
}

// SetStage updates the current indexing stage and resets the total count.
func (p *IndexProgress) SetStage(stage IndexingStage, total int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.stage = stage
	p.filesTotal = total
}

// UpdateFiles updates the number of processed files.
func (p *IndexProgress) UpdateFiles(processed int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.filesProcessed = processed
}

// SetChunksTotal sets the total number of chunks to process.
func (p *IndexProgress) SetChunksTotal(total int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.chunksTotal = total
}

// UpdateChunks updates the number of indexed chunks.
func (p *IndexProgress) UpdateChunks(indexed int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.chunksIndexed = indexed
}

// SetError marks the indexing as failed with an error message.
func (p *IndexProgress) SetError(message string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.status = StatusError
	p.errorMessage = message
}

// SetReady marks the indexing as complete and ready for search.
func (p *IndexProgress) SetReady() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.status = StatusReady
}

// IsIndexing returns true if indexing is still in progress.
func (p *IndexProgress) IsIndexing() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.status == StatusIndexing
}

// Snapshot returns an immutable copy of the current progress state.
func (p *IndexProgress) Snapshot() IndexProgressSnapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var progressPct float64
	if p.filesTotal > 0 {
		progressPct = float64(p.filesProcessed) / float64(p.filesTotal) * 100.0
	}

	return IndexProgressSnapshot{
		Status:         string(p.status),
		Stage:          string(p.stage),
		FilesTotal:     p.filesTotal,
		FilesProcessed: p.filesProcessed,
		ChunksTotal:    p.chunksTotal,
		ChunksIndexed:  p.chunksIndexed,
		ProgressPct:    progressPct,
		ElapsedSeconds: int(time.Since(p.startTime).Seconds()),
		ErrorMessage:   p.errorMessage,
	}
}
