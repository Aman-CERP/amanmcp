package ui

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewTUIRenderer_ReturnsNilForNonTTY(t *testing.T) {
	// Given: a non-TTY buffer
	buf := &bytes.Buffer{}
	cfg := NewConfig(buf)

	// When: creating TUI renderer
	r, err := NewTUIRenderer(cfg)

	// Then: returns error (can't create TUI for non-TTY)
	assert.Error(t, err)
	assert.Nil(t, r)
}

func TestIndexingModel_InitialView(t *testing.T) {
	// Given: a new indexing model with properly initialized tracker
	tracker := NewProgressTracker()
	model := newIndexingModel(tracker, "")

	// When: getting initial view
	view := model.View()

	// Then: view contains stage indicators
	assert.Contains(t, view, "Scan")
}

func TestIndexingModel_StageIndicators(t *testing.T) {
	// Given: a model at different stages
	tracker := NewProgressTracker()
	model := newIndexingModel(tracker, "")

	// When: rendering at scanning stage
	tracker.SetStage(StageScanning, 100)
	view := model.View()

	// Then: all stage indicators are shown (short names)
	assert.Contains(t, view, "Scan")
	assert.Contains(t, view, "Chunk")
	assert.Contains(t, view, "Embed")
	assert.Contains(t, view, "Index")
}

func TestIndexingModel_ProgressDisplay(t *testing.T) {
	// Given: a model with progress
	tracker := NewProgressTracker()
	tracker.SetStage(StageScanning, 100)
	tracker.Update(50, "src/main.go")

	model := newIndexingModel(tracker, "")

	// When: rendering view
	view := model.View()

	// Then: progress is shown
	assert.Contains(t, view, "50")
	assert.Contains(t, view, "100")
}

func TestIndexingModel_FileDisplay(t *testing.T) {
	// Given: a model with current file
	tracker := NewProgressTracker()
	tracker.SetStage(StageScanning, 100)
	tracker.Update(1, "src/components/Button.tsx")

	model := newIndexingModel(tracker, "")

	// When: rendering view
	view := model.View()

	// Then: file path is shown (possibly truncated)
	assert.Contains(t, view, "Button.tsx")
}

func TestIndexingModel_ErrorDisplay(t *testing.T) {
	// Given: a model with errors
	tracker := NewProgressTracker()
	tracker.AddError(ErrorEvent{
		File:   "broken.go",
		Err:    assert.AnError,
		IsWarn: false,
	})
	tracker.AddError(ErrorEvent{
		File:   "warning.go",
		Err:    assert.AnError,
		IsWarn: true,
	})

	model := newIndexingModel(tracker, "")

	// When: rendering view
	view := model.View()

	// Then: error count is shown
	assert.Contains(t, view, "1")
}

func TestIndexingModel_CompletionState(t *testing.T) {
	// Given: a completed model
	tracker := NewProgressTracker()
	tracker.SetStage(StageComplete, 0)

	model := newIndexingModel(tracker, "")
	model.complete = true
	model.stats = CompletionStats{
		Files:  100,
		Chunks: 500,
	}

	// When: rendering view
	view := model.View()

	// Then: shows completion
	assert.Contains(t, view, "Complete")
}

func TestTruncateFilePath_Short(t *testing.T) {
	// Given: a short path
	path := "src/main.go"

	// When: truncating
	result := truncateFilePath(path, 50)

	// Then: unchanged
	assert.Equal(t, path, result)
}

func TestTruncateFilePath_Long(t *testing.T) {
	// Given: a long path
	path := "src/components/very/deeply/nested/directory/file.go"

	// When: truncating to 30 chars
	result := truncateFilePath(path, 30)

	// Then: truncated with ellipsis
	assert.LessOrEqual(t, len(result), 30)
	assert.Contains(t, result, "...")
	assert.Contains(t, result, "file.go") // Keeps filename
}

func TestTruncateFilePath_Empty(t *testing.T) {
	// Given: empty path
	path := ""

	// When: truncating
	result := truncateFilePath(path, 50)

	// Then: returns empty
	assert.Equal(t, "", result)
}

func TestTUIRenderer_InterfaceCompliance(t *testing.T) {
	// Ensure TUIRenderer implements Renderer
	var _ Renderer = (*TUIRenderer)(nil)
}
