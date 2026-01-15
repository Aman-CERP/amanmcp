package ui

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatusInfo_Zero(t *testing.T) {
	// Given: zero-valued status info
	info := StatusInfo{}

	// Then: all fields are zero/empty
	assert.Empty(t, info.ProjectName)
	assert.Equal(t, 0, info.TotalFiles)
	assert.Equal(t, 0, info.TotalChunks)
	assert.True(t, info.LastIndexed.IsZero())
}

func TestStatusInfo_JSONSerialization(t *testing.T) {
	// Given: populated status info
	info := StatusInfo{
		ProjectName:    "test-project",
		TotalFiles:     100,
		TotalChunks:    500,
		LastIndexed:    time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		MetadataSize:   1024 * 1024,
		BM25Size:       2 * 1024 * 1024,
		VectorSize:     10 * 1024 * 1024,
		TotalSize:      13 * 1024 * 1024,
		EmbedderType:   "hugot",
		EmbedderStatus: "ready",
		EmbedderModel:  "embeddinggemma-300m",
		WatcherStatus:  "running",
	}

	// When: serializing to JSON
	data, err := json.Marshal(info)
	require.NoError(t, err)

	// Then: JSON is valid and contains expected fields
	var parsed map[string]any
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	assert.Equal(t, "test-project", parsed["project_name"])
	assert.Equal(t, float64(100), parsed["total_files"])
	assert.Equal(t, float64(500), parsed["total_chunks"])
	assert.Equal(t, "hugot", parsed["embedder_type"])
	assert.Equal(t, "running", parsed["watcher_status"])
}

func TestStatusRenderer_Render_Basic(t *testing.T) {
	// Given: status renderer
	buf := &bytes.Buffer{}
	r := NewStatusRenderer(buf, false)

	// When: rendering status info
	info := StatusInfo{
		ProjectName:    "my-project",
		TotalFiles:     50,
		TotalChunks:    250,
		LastIndexed:    time.Now(),
		MetadataSize:   512 * 1024,
		BM25Size:       1024 * 1024,
		VectorSize:     5 * 1024 * 1024,
		TotalSize:      6 * 1024 * 1024 + 512*1024,
		EmbedderType:   "hugot",
		EmbedderStatus: "ready",
		EmbedderModel:  "embeddinggemma-300m",
		WatcherStatus:  "stopped",
	}

	err := r.Render(info)
	require.NoError(t, err)

	// Then: output contains key information
	output := buf.String()
	assert.Contains(t, output, "my-project")
	assert.Contains(t, output, "50")
	assert.Contains(t, output, "250")
	assert.Contains(t, output, "hugot")
	assert.Contains(t, output, "ready")
}

func TestStatusRenderer_RenderJSON(t *testing.T) {
	// Given: status renderer
	buf := &bytes.Buffer{}
	r := NewStatusRenderer(buf, false)

	// When: rendering as JSON
	info := StatusInfo{
		ProjectName: "json-project",
		TotalFiles:  25,
		TotalChunks: 100,
	}

	err := r.RenderJSON(info)
	require.NoError(t, err)

	// Then: output is valid JSON
	var parsed StatusInfo
	err = json.Unmarshal(buf.Bytes(), &parsed)
	require.NoError(t, err)
	assert.Equal(t, "json-project", parsed.ProjectName)
	assert.Equal(t, 25, parsed.TotalFiles)
}

func TestStatusRenderer_NoColor(t *testing.T) {
	// Given: status renderer with noColor
	buf := &bytes.Buffer{}
	r := NewStatusRenderer(buf, true)

	// When: rendering
	info := StatusInfo{
		ProjectName:    "nocolor-project",
		EmbedderStatus: "ready",
	}

	err := r.Render(info)
	require.NoError(t, err)

	// Then: no ANSI codes in output
	output := buf.String()
	assert.NotContains(t, output, "\x1b[")
	assert.NotContains(t, output, "\033[")
}

func TestStatusRenderer_EmbedderOffline(t *testing.T) {
	// Given: status renderer
	buf := &bytes.Buffer{}
	r := NewStatusRenderer(buf, false)

	// When: rendering with offline embedder
	info := StatusInfo{
		ProjectName:    "offline-project",
		EmbedderType:   "static",
		EmbedderStatus: "offline",
	}

	err := r.Render(info)
	require.NoError(t, err)

	// Then: shows offline status
	output := buf.String()
	assert.Contains(t, output, "offline")
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 B"},
		{100, "100 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{5 * 1024 * 1024, "5.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := FormatBytes(tt.bytes)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStatusRenderer_StorageSizes(t *testing.T) {
	// Given: status renderer
	buf := &bytes.Buffer{}
	r := NewStatusRenderer(buf, true) // noColor for easier assertion

	// When: rendering with storage sizes
	info := StatusInfo{
		ProjectName:  "storage-project",
		MetadataSize: 512 * 1024,
		BM25Size:     2 * 1024 * 1024,
		VectorSize:   10 * 1024 * 1024,
		TotalSize:    12*1024*1024 + 512*1024,
	}

	err := r.Render(info)
	require.NoError(t, err)

	// Then: sizes are human-readable
	output := buf.String()
	assert.Contains(t, output, "KB") // Metadata size
	assert.Contains(t, output, "MB") // Vector size
}
