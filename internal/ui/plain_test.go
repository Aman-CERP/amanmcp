package ui

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlainRenderer_UpdateProgress_OutputFormat(t *testing.T) {
	// Given: a plain renderer
	buf := &bytes.Buffer{}
	r := NewPlainRenderer(NewConfig(buf))

	// When: updating progress
	r.UpdateProgress(ProgressEvent{
		Stage:       StageScanning,
		Current:     50,
		Total:       100,
		CurrentFile: "src/main.go",
	})

	// Then: output is correctly formatted
	output := buf.String()
	assert.Contains(t, output, "[SCAN]")
	assert.Contains(t, output, "50/100")
	assert.Contains(t, output, "src/main.go")
}

func TestPlainRenderer_UpdateProgress_NoANSICodes(t *testing.T) {
	// Given: a plain renderer
	buf := &bytes.Buffer{}
	r := NewPlainRenderer(NewConfig(buf))

	// When: rendering progress through all stages
	stages := []Stage{StageScanning, StageChunking, StageEmbedding, StageIndexing, StageComplete}
	for _, stage := range stages {
		r.UpdateProgress(ProgressEvent{
			Stage:   stage,
			Current: 50,
			Total:   100,
			Message: "Processing...",
		})
	}

	// Then: output contains no ANSI escape codes
	output := buf.String()
	assert.NotContains(t, output, "\x1b[", "should not contain ANSI escape codes")
	assert.NotContains(t, output, "\033[", "should not contain ANSI escape codes")
}

func TestPlainRenderer_UpdateProgress_WithMessage(t *testing.T) {
	// Given: a plain renderer
	buf := &bytes.Buffer{}
	r := NewPlainRenderer(NewConfig(buf))

	// When: updating with message instead of file
	r.UpdateProgress(ProgressEvent{
		Stage:   StageEmbedding,
		Current: 100,
		Total:   200,
		Message: "Generating embeddings...",
	})

	// Then: message is shown
	output := buf.String()
	assert.Contains(t, output, "[EMBED]")
	assert.Contains(t, output, "Generating embeddings...")
}

func TestPlainRenderer_UpdateProgress_ZeroTotal(t *testing.T) {
	// Given: a plain renderer
	buf := &bytes.Buffer{}
	r := NewPlainRenderer(NewConfig(buf))

	// When: updating with zero total (unknown count)
	r.UpdateProgress(ProgressEvent{
		Stage:   StageScanning,
		Total:   0,
		Message: "Scanning files...",
	})

	// Then: shows message without count
	output := buf.String()
	assert.Contains(t, output, "[SCAN]")
	assert.Contains(t, output, "Scanning files...")
	assert.NotContains(t, output, "0/0")
}

func TestPlainRenderer_AddError_Error(t *testing.T) {
	// Given: a plain renderer
	buf := &bytes.Buffer{}
	r := NewPlainRenderer(NewConfig(buf))

	// When: adding an error
	r.AddError(ErrorEvent{
		File:   "broken.go",
		Err:    errors.New("syntax error at line 42"),
		IsWarn: false,
	})

	// Then: error is formatted correctly
	output := buf.String()
	assert.Contains(t, output, "ERROR:")
	assert.Contains(t, output, "broken.go")
	assert.Contains(t, output, "syntax error at line 42")
}

func TestPlainRenderer_AddError_Warning(t *testing.T) {
	// Given: a plain renderer
	buf := &bytes.Buffer{}
	r := NewPlainRenderer(NewConfig(buf))

	// When: adding a warning
	r.AddError(ErrorEvent{
		File:   "large.go",
		Err:    errors.New("file size exceeds limit"),
		IsWarn: true,
	})

	// Then: warning is formatted correctly
	output := buf.String()
	assert.Contains(t, output, "WARN:")
	assert.Contains(t, output, "large.go")
	assert.Contains(t, output, "file size exceeds limit")
}

func TestPlainRenderer_AddError_NoFile(t *testing.T) {
	// Given: a plain renderer
	buf := &bytes.Buffer{}
	r := NewPlainRenderer(NewConfig(buf))

	// When: adding error without file
	r.AddError(ErrorEvent{
		Err:    errors.New("connection failed"),
		IsWarn: false,
	})

	// Then: error shows without file prefix
	output := buf.String()
	assert.Contains(t, output, "ERROR:")
	assert.Contains(t, output, "connection failed")
}

func TestPlainRenderer_Complete_Basic(t *testing.T) {
	// Given: a plain renderer
	buf := &bytes.Buffer{}
	r := NewPlainRenderer(NewConfig(buf))

	// When: completing
	r.Complete(CompletionStats{
		Files:    100,
		Chunks:   500,
		Duration: 5 * time.Second,
		Errors:   0,
		Warnings: 0,
	})

	// Then: summary is shown
	output := buf.String()
	assert.Contains(t, output, "Complete:")
	assert.Contains(t, output, "100 files")
	assert.Contains(t, output, "500 chunks")
	assert.Contains(t, output, "5s")
}

func TestPlainRenderer_Complete_WithErrors(t *testing.T) {
	// Given: a plain renderer
	buf := &bytes.Buffer{}
	r := NewPlainRenderer(NewConfig(buf))

	// When: completing with errors
	r.Complete(CompletionStats{
		Files:    95,
		Chunks:   450,
		Duration: 10 * time.Second,
		Errors:   3,
		Warnings: 2,
	})

	// Then: error summary is included
	output := buf.String()
	assert.Contains(t, output, "Complete:")
	assert.Contains(t, output, "95 files")
	assert.Contains(t, output, "3 errors")
	assert.Contains(t, output, "2 warnings")
}

func TestPlainRenderer_Complete_NoANSICodes(t *testing.T) {
	// Given: a plain renderer
	buf := &bytes.Buffer{}
	r := NewPlainRenderer(NewConfig(buf))

	// When: completing
	r.Complete(CompletionStats{
		Files:    100,
		Chunks:   500,
		Duration: 5 * time.Second,
		Errors:   2,
		Warnings: 1,
	})

	// Then: no ANSI codes in output
	output := buf.String()
	assert.NotContains(t, output, "\x1b[")
	assert.NotContains(t, output, "\033[")
}

func TestPlainRenderer_StartStop(t *testing.T) {
	// Given: a plain renderer
	buf := &bytes.Buffer{}
	r := NewPlainRenderer(NewConfig(buf))

	// When: starting and stopping
	ctx := context.Background()
	err := r.Start(ctx)
	require.NoError(t, err)

	err = r.Stop()
	require.NoError(t, err)
}

func TestPlainRenderer_ThreadSafe(t *testing.T) {
	// Given: a plain renderer
	buf := &bytes.Buffer{}
	r := NewPlainRenderer(NewConfig(buf))

	// When: concurrent updates
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(n int) {
			r.UpdateProgress(ProgressEvent{
				Stage:   StageScanning,
				Current: n,
				Total:   100,
			})
			r.AddError(ErrorEvent{
				File:   "test.go",
				Err:    errors.New("test"),
				IsWarn: n%2 == 0,
			})
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Then: no panic, output is written
	output := buf.String()
	assert.NotEmpty(t, output)
}

func TestPlainRenderer_AllStages(t *testing.T) {
	// Given: a plain renderer
	buf := &bytes.Buffer{}
	r := NewPlainRenderer(NewConfig(buf))

	// When: going through all stages
	stages := []struct {
		stage Stage
		icon  string
	}{
		{StageScanning, "SCAN"},
		{StageChunking, "CHUNK"},
		{StageEmbedding, "EMBED"},
		{StageIndexing, "INDEX"},
	}

	for _, s := range stages {
		r.UpdateProgress(ProgressEvent{
			Stage:   s.stage,
			Current: 50,
			Total:   100,
		})
	}

	// Then: all stage icons appear
	output := buf.String()
	for _, s := range stages {
		assert.Contains(t, output, "["+s.icon+"]")
	}
}

func TestPlainRenderer_LongFilePath(t *testing.T) {
	// Given: a plain renderer
	buf := &bytes.Buffer{}
	r := NewPlainRenderer(NewConfig(buf))

	// When: updating with long file path
	longPath := strings.Repeat("very/", 20) + "deep/file.go"
	r.UpdateProgress(ProgressEvent{
		Stage:       StageScanning,
		Current:     1,
		Total:       10,
		CurrentFile: longPath,
	})

	// Then: full path is shown (no truncation in plain mode)
	output := buf.String()
	assert.Contains(t, output, "file.go")
}
