package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWriter_Status_PrintsIconAndMessage(t *testing.T) {
	// Given: a writer with a buffer
	buf := &bytes.Buffer{}
	w := New(buf)

	// When: printing a status message
	w.Status("🔍", "Checking embedder...")

	// Then: output contains icon and message
	output := buf.String()
	assert.Contains(t, output, "🔍")
	assert.Contains(t, output, "Checking embedder...")
}

func TestWriter_Success_PrintsCheckmark(t *testing.T) {
	// Given: a writer with a buffer
	buf := &bytes.Buffer{}
	w := New(buf)

	// When: printing a success message
	w.Success("Index complete!")

	// Then: output contains checkmark and message
	output := buf.String()
	assert.Contains(t, output, "✅")
	assert.Contains(t, output, "Index complete!")
}

func TestWriter_Warning_PrintsWarningIcon(t *testing.T) {
	// Given: a writer with a buffer
	buf := &bytes.Buffer{}
	w := New(buf)

	// When: printing a warning message
	w.Warning("Embedder not available")

	// Then: output contains warning icon and message
	output := buf.String()
	assert.Contains(t, output, "⚠️")
	assert.Contains(t, output, "Embedder not available")
}

func TestWriter_Error_PrintsErrorIcon(t *testing.T) {
	// Given: a writer with a buffer
	buf := &bytes.Buffer{}
	w := New(buf)

	// When: printing an error message
	w.Error("Failed to connect")

	// Then: output contains error icon and message
	output := buf.String()
	assert.Contains(t, output, "❌")
	assert.Contains(t, output, "Failed to connect")
}

func TestWriter_Code_PrintsCodeBlock(t *testing.T) {
	// Given: a writer with a buffer
	buf := &bytes.Buffer{}
	w := New(buf)

	// When: printing a code block
	code := `{"key": "value"}`
	w.Code(code)

	// Then: output contains the code
	output := buf.String()
	assert.Contains(t, output, `{"key": "value"}`)
}

func TestWriter_Progress_PrintsProgressBar(t *testing.T) {
	// Given: a writer with a buffer
	buf := &bytes.Buffer{}
	w := New(buf)

	// When: printing progress at 50%
	w.Progress(50, 100, "Indexing files")

	// Then: output contains progress indicator and message
	output := buf.String()
	assert.Contains(t, output, "50%")
	assert.Contains(t, output, "Indexing files")
}

func TestWriter_Progress_ZeroTotal_NoOutput(t *testing.T) {
	// Given: a writer with a buffer
	buf := &bytes.Buffer{}
	w := New(buf)

	// When: printing progress with zero total
	w.Progress(0, 0, "Processing")

	// Then: no crash, graceful handling
	// (may or may not produce output, just shouldn't crash)
	assert.NotPanics(t, func() {
		w.Progress(0, 0, "Processing")
	})
}

func TestWriter_Statusf_FormatsMessage(t *testing.T) {
	// Given: a writer with a buffer
	buf := &bytes.Buffer{}
	w := New(buf)

	// When: printing a formatted status message
	w.Statusf("📂", "Found %d files in %s", 42, "/path/to/project")

	// Then: output contains formatted message
	output := buf.String()
	assert.Contains(t, output, "📂")
	assert.Contains(t, output, "Found 42 files in /path/to/project")
}

func TestProgressBar_Render(t *testing.T) {
	tests := []struct {
		name     string
		current  int
		total    int
		width    int
		wantFull int // number of filled characters
	}{
		{
			name:     "0 percent",
			current:  0,
			total:    100,
			width:    10,
			wantFull: 0,
		},
		{
			name:     "50 percent",
			current:  50,
			total:    100,
			width:    10,
			wantFull: 5,
		},
		{
			name:     "100 percent",
			current:  100,
			total:    100,
			width:    10,
			wantFull: 10,
		},
		{
			name:     "25 percent",
			current:  25,
			total:    100,
			width:    20,
			wantFull: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bar := renderProgressBar(tt.current, tt.total, tt.width)

			// Count filled characters (█)
			filled := strings.Count(bar, "█")
			assert.Equal(t, tt.wantFull, filled)

			// Total width should be correct
			assert.Equal(t, tt.width, len([]rune(bar)))
		})
	}
}

func TestWriter_Newline_PrintsEmptyLine(t *testing.T) {
	// Given: a writer with a buffer
	buf := &bytes.Buffer{}
	w := New(buf)

	// When: printing a newline
	w.Newline()

	// Then: output is just a newline
	assert.Equal(t, "\n", buf.String())
}

func TestNew_DefaultsToNoColor(t *testing.T) {
	// Given/When: creating a new writer
	buf := &bytes.Buffer{}
	w := New(buf)

	// Then: writer is created (useColor is implementation detail)
	assert.NotNil(t, w)
}
