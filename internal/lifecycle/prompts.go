package lifecycle

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// PromptChoice represents user's choice from interactive prompt
type PromptChoice int

const (
	// ChoiceShowInstall shows installation instructions
	ChoiceShowInstall PromptChoice = iota + 1
	// ChoiceOfflineMode uses BM25-only search
	ChoiceOfflineMode
	// ChoiceCancel cancels the operation
	ChoiceCancel
)

// IsTTY returns true if stdin is a terminal
func IsTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	// Check if stdin is a character device (terminal)
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// PromptNoEmbedder shows interactive prompt when Ollama is not installed
// Returns the user's choice or an error
func PromptNoEmbedder(w io.Writer, r io.Reader) (PromptChoice, error) {
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Ollama is required for semantic search but not installed.")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "  [1] Show install instructions (then retry)")
	fmt.Fprintln(w, "  [2] Use offline mode (BM25-only, no semantic search)")
	fmt.Fprintln(w, "  [3] Cancel")
	fmt.Fprintln(w, "")
	fmt.Fprint(w, "Choice [1]: ")

	reader := bufio.NewReader(r)
	input, err := reader.ReadString('\n')
	if err != nil {
		return ChoiceCancel, fmt.Errorf("failed to read input: %w", err)
	}

	input = strings.TrimSpace(input)

	// Default to choice 1 if empty
	if input == "" {
		input = "1"
	}

	switch input {
	case "1":
		return ChoiceShowInstall, nil
	case "2":
		return ChoiceOfflineMode, nil
	case "3":
		return ChoiceCancel, nil
	default:
		return ChoiceCancel, fmt.Errorf("invalid choice: %s", input)
	}
}

// ShowInstallInstructions displays platform-specific install instructions
func ShowInstallInstructions(w io.Writer) {
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, InstallInstructions())
	fmt.Fprintln(w, "")
}

// PromptModelNotFound shows prompt when model is missing
func PromptModelNotFound(w io.Writer, r io.Reader, model string) (bool, error) {
	fmt.Fprintln(w, "")
	fmt.Fprintf(w, "Embedding model '%s' is not installed.\n", model)
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "  [1] Pull model now (recommended)")
	fmt.Fprintln(w, "  [2] Cancel")
	fmt.Fprintln(w, "")
	fmt.Fprint(w, "Choice [1]: ")

	reader := bufio.NewReader(r)
	input, err := reader.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("failed to read input: %w", err)
	}

	input = strings.TrimSpace(input)
	if input == "" {
		input = "1"
	}

	return input == "1", nil
}

// ProgressBar renders a simple progress bar
type ProgressBar struct {
	w       io.Writer
	width   int
	current float64
	message string
}

// NewProgressBar creates a new progress bar
func NewProgressBar(w io.Writer, width int) *ProgressBar {
	if width <= 0 {
		width = 40
	}
	return &ProgressBar{
		w:     w,
		width: width,
	}
}

// Update updates the progress bar
func (p *ProgressBar) Update(percent float64, message string) {
	p.current = percent
	p.message = message

	filled := int(percent / 100 * float64(p.width))
	if filled > p.width {
		filled = p.width
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("░", p.width-filled)
	fmt.Fprintf(p.w, "\r[%s] %.0f%% %s", bar, percent, message)
}

// Finish completes the progress bar with a newline
func (p *ProgressBar) Finish() {
	fmt.Fprintln(p.w)
}

// FormatBytes formats bytes in human-readable format
func FormatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// CreatePullProgressFunc creates a progress function for model pulling
func CreatePullProgressFunc(w io.Writer) func(PullProgress) {
	bar := NewProgressBar(w, 40)
	lastStatus := ""

	return func(p PullProgress) {
		if p.Total > 0 {
			message := fmt.Sprintf("%s/%s", FormatBytes(p.Completed), FormatBytes(p.Total))
			bar.Update(p.Percent, message)
		} else if p.Status != lastStatus {
			lastStatus = p.Status
			fmt.Fprintf(w, "\r%s...", p.Status)
		}
	}
}
