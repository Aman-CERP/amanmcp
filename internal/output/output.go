// Package output provides consistent CLI output formatting with colors and progress indicators.
package output

import (
	"fmt"
	"io"
	"strings"
)

// Writer provides formatted output for CLI.
type Writer struct {
	out      io.Writer
	useColor bool
}

// New creates a new output Writer.
func New(out io.Writer) *Writer {
	return &Writer{
		out:      out,
		useColor: false, // Default to no color for simplicity
	}
}

// Status prints a status message with an icon.
// Errors from writing are intentionally ignored for console output.
func (w *Writer) Status(icon, msg string) {
	if icon != "" {
		_, _ = fmt.Fprintf(w.out, "%s %s\n", icon, msg)
	} else {
		_, _ = fmt.Fprintf(w.out, "   %s\n", msg)
	}
}

// Statusf prints a formatted status message with an icon.
func (w *Writer) Statusf(icon, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	w.Status(icon, msg)
}

// Success prints a success message with checkmark.
func (w *Writer) Success(msg string) {
	w.Status("✅", msg)
}

// Successf prints a formatted success message.
func (w *Writer) Successf(format string, args ...any) {
	w.Success(fmt.Sprintf(format, args...))
}

// Warning prints a warning message.
func (w *Writer) Warning(msg string) {
	w.Status("⚠️ ", msg)
}

// Warningf prints a formatted warning message.
func (w *Writer) Warningf(format string, args ...any) {
	w.Warning(fmt.Sprintf(format, args...))
}

// Error prints an error message.
func (w *Writer) Error(msg string) {
	w.Status("❌", msg)
}

// Errorf prints a formatted error message.
func (w *Writer) Errorf(format string, args ...any) {
	w.Error(fmt.Sprintf(format, args...))
}

// Code prints a code block with indentation.
func (w *Writer) Code(content string) {
	_, _ = fmt.Fprintln(w.out)
	// Indent each line
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		_, _ = fmt.Fprintf(w.out, "  %s\n", line)
	}
	_, _ = fmt.Fprintln(w.out)
}

// Newline prints an empty line.
func (w *Writer) Newline() {
	_, _ = fmt.Fprintln(w.out)
}

// Progress prints a progress bar with message.
func (w *Writer) Progress(current, total int, msg string) {
	if total <= 0 {
		return
	}

	pct := float64(current) / float64(total) * 100
	bar := renderProgressBar(current, total, 30)

	// Use carriage return for in-place updates
	_, _ = fmt.Fprintf(w.out, "\r[%s] %.0f%% %s", bar, pct, msg)

	// Add newline when complete
	if current >= total {
		_, _ = fmt.Fprintln(w.out)
	}
}

// ProgressDone completes a progress line with newline.
func (w *Writer) ProgressDone() {
	_, _ = fmt.Fprintln(w.out)
}

// renderProgressBar creates a text progress bar.
func renderProgressBar(current, total, width int) string {
	if total <= 0 {
		return strings.Repeat("░", width)
	}

	pct := float64(current) / float64(total)
	filled := int(pct * float64(width))

	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}

	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}
