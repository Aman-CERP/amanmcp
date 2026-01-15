package lifecycle

import (
	"bytes"
	"strings"
	"testing"
)

// ============================================================================
// PromptNoEmbedder Tests
// ============================================================================

func TestPromptNoEmbedder_Choice1(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("1\n")

	choice, err := PromptNoEmbedder(&out, in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if choice != ChoiceShowInstall {
		t.Errorf("expected ChoiceShowInstall, got %d", choice)
	}
}

func TestPromptNoEmbedder_Choice2(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("2\n")

	choice, err := PromptNoEmbedder(&out, in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if choice != ChoiceOfflineMode {
		t.Errorf("expected ChoiceOfflineMode, got %d", choice)
	}
}

func TestPromptNoEmbedder_Choice3(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("3\n")

	choice, err := PromptNoEmbedder(&out, in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if choice != ChoiceCancel {
		t.Errorf("expected ChoiceCancel, got %d", choice)
	}
}

func TestPromptNoEmbedder_DefaultChoice(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("\n") // Empty input = default

	choice, err := PromptNoEmbedder(&out, in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if choice != ChoiceShowInstall {
		t.Errorf("expected ChoiceShowInstall (default), got %d", choice)
	}
}

func TestPromptNoEmbedder_InvalidChoice(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("invalid\n")

	choice, err := PromptNoEmbedder(&out, in)
	if err == nil {
		t.Fatal("expected error for invalid choice")
	}
	if choice != ChoiceCancel {
		t.Errorf("expected ChoiceCancel on error, got %d", choice)
	}
}

func TestPromptNoEmbedder_OutputFormat(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("1\n")

	_, err := PromptNoEmbedder(&out, in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "Ollama is required") {
		t.Error("expected prompt to contain 'Ollama is required'")
	}
	if !strings.Contains(output, "[1]") {
		t.Error("expected prompt to contain choice [1]")
	}
	if !strings.Contains(output, "[2]") {
		t.Error("expected prompt to contain choice [2]")
	}
	if !strings.Contains(output, "[3]") {
		t.Error("expected prompt to contain choice [3]")
	}
}

// ============================================================================
// PromptModelNotFound Tests
// ============================================================================

func TestPromptModelNotFound_Pull(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("1\n")

	pull, err := PromptModelNotFound(&out, in, "test-model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !pull {
		t.Error("expected pull to be true")
	}
}

func TestPromptModelNotFound_Cancel(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("2\n")

	pull, err := PromptModelNotFound(&out, in, "test-model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pull {
		t.Error("expected pull to be false")
	}
}

func TestPromptModelNotFound_Default(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("\n")

	pull, err := PromptModelNotFound(&out, in, "test-model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !pull {
		t.Error("expected pull to be true (default)")
	}
}

// ============================================================================
// ShowInstallInstructions Tests
// ============================================================================

func TestShowInstallInstructions(t *testing.T) {
	var out bytes.Buffer
	ShowInstallInstructions(&out)

	output := out.String()
	if output == "" {
		t.Error("expected non-empty output")
	}
	if !strings.Contains(output, "ollama.com") {
		t.Error("expected output to contain ollama.com")
	}
}

// ============================================================================
// ProgressBar Tests
// ============================================================================

func TestProgressBar_Update(t *testing.T) {
	var out bytes.Buffer
	bar := NewProgressBar(&out, 20)

	bar.Update(50, "testing")
	output := out.String()

	if !strings.Contains(output, "50%") {
		t.Errorf("expected output to contain 50%%, got: %s", output)
	}
	if !strings.Contains(output, "█") {
		t.Errorf("expected output to contain filled bar, got: %s", output)
	}
}

func TestProgressBar_DefaultWidth(t *testing.T) {
	var out bytes.Buffer
	bar := NewProgressBar(&out, 0) // Should default to 40

	bar.Update(100, "done")
	if bar.width != 40 {
		t.Errorf("expected default width 40, got %d", bar.width)
	}
}

func TestProgressBar_Finish(t *testing.T) {
	var out bytes.Buffer
	bar := NewProgressBar(&out, 20)

	bar.Update(100, "done")
	bar.Finish()

	if !strings.HasSuffix(out.String(), "\n") {
		t.Error("expected output to end with newline after Finish()")
	}
}

// ============================================================================
// FormatBytes Tests
// ============================================================================

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1572864, "1.5 MB"},
		{1073741824, "1.0 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := FormatBytes(tt.bytes)
			if result != tt.expected {
				t.Errorf("FormatBytes(%d) = %s, want %s", tt.bytes, result, tt.expected)
			}
		})
	}
}

// ============================================================================
// CreatePullProgressFunc Tests
// ============================================================================

func TestCreatePullProgressFunc(t *testing.T) {
	var out bytes.Buffer
	progressFunc := CreatePullProgressFunc(&out)

	// Test with progress
	progressFunc(PullProgress{
		Status:    "downloading",
		Total:     1024 * 1024,
		Completed: 512 * 1024,
		Percent:   50,
	})

	output := out.String()
	if !strings.Contains(output, "50%") {
		t.Errorf("expected progress output to contain 50%%, got: %s", output)
	}
}

func TestCreatePullProgressFunc_StatusOnly(t *testing.T) {
	var out bytes.Buffer
	progressFunc := CreatePullProgressFunc(&out)

	// Test with status only (no total)
	progressFunc(PullProgress{
		Status: "pulling manifest",
		Total:  0,
	})

	output := out.String()
	if !strings.Contains(output, "pulling manifest") {
		t.Errorf("expected output to contain status, got: %s", output)
	}
}

// ============================================================================
// PromptChoice Constants Tests
// ============================================================================

func TestPromptChoiceValues(t *testing.T) {
	// Ensure choices are distinct
	choices := []PromptChoice{ChoiceShowInstall, ChoiceOfflineMode, ChoiceCancel}
	seen := make(map[PromptChoice]bool)

	for _, c := range choices {
		if seen[c] {
			t.Errorf("duplicate choice value: %d", c)
		}
		seen[c] = true
	}

	// Ensure they start at 1 (not 0) for better UX
	if ChoiceShowInstall != 1 {
		t.Errorf("expected ChoiceShowInstall to be 1, got %d", ChoiceShowInstall)
	}
}
