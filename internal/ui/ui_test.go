package ui

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStage_String(t *testing.T) {
	tests := []struct {
		stage Stage
		want  string
	}{
		{StageScanning, "Scanning"},
		{StageChunking, "Chunking"},
		{StageEmbedding, "Embedding"},
		{StageIndexing, "Indexing"},
		{StageComplete, "Complete"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.stage.String())
		})
	}
}

func TestStage_Icon(t *testing.T) {
	tests := []struct {
		stage Stage
		want  string
	}{
		{StageScanning, "SCAN"},
		{StageChunking, "CHUNK"},
		{StageEmbedding, "EMBED"},
		{StageIndexing, "INDEX"},
		{StageComplete, "DONE"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.stage.Icon())
		})
	}
}

func TestIsTTY_WithBuffer_ReturnsFalse(t *testing.T) {
	// Given: a bytes.Buffer (not a TTY)
	buf := &bytes.Buffer{}

	// When: checking if it's a TTY
	result := IsTTY(buf)

	// Then: returns false
	assert.False(t, result)
}

func TestIsTTY_WithNil_ReturnsFalse(t *testing.T) {
	// Given: nil writer
	// When: checking if it's a TTY
	result := IsTTY(nil)

	// Then: returns false
	assert.False(t, result)
}

func TestNewConfig_Defaults(t *testing.T) {
	// Given: default config
	buf := &bytes.Buffer{}
	cfg := NewConfig(buf)

	// Then: has sensible defaults
	assert.NotNil(t, cfg.Output)
	assert.False(t, cfg.ForcePlain)
	assert.False(t, cfg.NoColor)
}

func TestNewConfig_WithOptions(t *testing.T) {
	// Given: config with options
	buf := &bytes.Buffer{}
	cfg := NewConfig(buf, WithForcePlain(true), WithNoColor(true))

	// Then: options are applied
	assert.True(t, cfg.ForcePlain)
	assert.True(t, cfg.NoColor)
}

func TestNewRenderer_ForcePlain_ReturnsPlainRenderer(t *testing.T) {
	// Given: config with ForcePlain
	buf := &bytes.Buffer{}
	cfg := NewConfig(buf, WithForcePlain(true))

	// When: creating renderer
	r := NewRenderer(cfg)

	// Then: returns PlainRenderer
	_, ok := r.(*PlainRenderer)
	require.True(t, ok, "expected PlainRenderer")
}

func TestNewRenderer_NonTTY_ReturnsPlainRenderer(t *testing.T) {
	// Given: non-TTY output (buffer)
	buf := &bytes.Buffer{}
	cfg := NewConfig(buf)

	// When: creating renderer
	r := NewRenderer(cfg)

	// Then: returns PlainRenderer (since buffer is not a TTY)
	_, ok := r.(*PlainRenderer)
	require.True(t, ok, "expected PlainRenderer for non-TTY")
}

func TestProgressEvent_Validation(t *testing.T) {
	// Given: a progress event
	event := ProgressEvent{
		Stage:       StageScanning,
		Current:     50,
		Total:       100,
		CurrentFile: "src/main.go",
		Message:     "Processing...",
	}

	// Then: fields are set correctly
	assert.Equal(t, StageScanning, event.Stage)
	assert.Equal(t, 50, event.Current)
	assert.Equal(t, 100, event.Total)
	assert.Equal(t, "src/main.go", event.CurrentFile)
	assert.Equal(t, "Processing...", event.Message)
}

func TestErrorEvent_IsWarning(t *testing.T) {
	// Given: warning event
	warning := ErrorEvent{
		File:   "broken.go",
		Err:    assert.AnError,
		IsWarn: true,
	}

	// Then: IsWarn is true
	assert.True(t, warning.IsWarn)

	// Given: error event
	err := ErrorEvent{
		File:   "error.go",
		Err:    assert.AnError,
		IsWarn: false,
	}

	// Then: IsWarn is false
	assert.False(t, err.IsWarn)
}

func TestCompletionStats_Zero(t *testing.T) {
	// Given: zero stats
	stats := CompletionStats{}

	// Then: all fields are zero
	assert.Equal(t, 0, stats.Files)
	assert.Equal(t, 0, stats.Chunks)
	assert.Zero(t, stats.Duration)
	assert.Equal(t, 0, stats.Errors)
	assert.Equal(t, 0, stats.Warnings)
}

func TestRenderer_Interface_Compliance(t *testing.T) {
	// This test ensures PlainRenderer implements Renderer interface
	var _ Renderer = (*PlainRenderer)(nil)
}

func TestDetectNoColor_WithEnv(t *testing.T) {
	// Given: NO_COLOR environment variable set
	_ = os.Setenv("NO_COLOR", "1")
	defer func() { _ = os.Unsetenv("NO_COLOR") }()

	// When: detecting no color
	result := DetectNoColor()

	// Then: returns true
	assert.True(t, result)
}

func TestDetectNoColor_WithoutEnv(t *testing.T) {
	// Given: NO_COLOR environment variable not set
	_ = os.Unsetenv("NO_COLOR")

	// When: detecting no color
	result := DetectNoColor()

	// Then: returns false
	assert.False(t, result)
}

func TestDetectCI_WithEnv(t *testing.T) {
	// Given: CI environment variable set
	_ = os.Setenv("CI", "true")
	defer func() { _ = os.Unsetenv("CI") }()

	// When: detecting CI
	result := DetectCI()

	// Then: returns true
	assert.True(t, result)
}

func TestDetectCI_WithoutEnv(t *testing.T) {
	// Given: CI environment variable not set
	_ = os.Unsetenv("CI")
	_ = os.Unsetenv("GITHUB_ACTIONS")
	_ = os.Unsetenv("GITLAB_CI")

	// When: detecting CI
	result := DetectCI()

	// Then: returns false
	assert.False(t, result)
}

