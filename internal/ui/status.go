package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// StatusInfo contains index health information.
type StatusInfo struct {
	// Index stats
	ProjectName string    `json:"project_name"`
	TotalFiles  int       `json:"total_files"`
	TotalChunks int       `json:"total_chunks"`
	LastIndexed time.Time `json:"last_indexed"`

	// Storage sizes (in bytes)
	MetadataSize int64 `json:"metadata_size"`
	BM25Size     int64 `json:"bm25_size"`
	VectorSize   int64 `json:"vector_size"`
	TotalSize    int64 `json:"total_size"`

	// Component status
	EmbedderType   string `json:"embedder_type"`
	EmbedderStatus string `json:"embedder_status"` // "ready", "offline", "error"
	EmbedderModel  string `json:"embedder_model,omitempty"`
	WatcherStatus  string `json:"watcher_status"` // "running", "stopped", "n/a"
}

// StatusRenderer displays index status.
type StatusRenderer struct {
	out     io.Writer
	styles  Styles
	noColor bool
}

// NewStatusRenderer creates a status renderer.
func NewStatusRenderer(out io.Writer, noColor bool) *StatusRenderer {
	return &StatusRenderer{
		out:     out,
		styles:  GetStyles(noColor),
		noColor: noColor,
	}
}

// Render displays status info to terminal.
func (r *StatusRenderer) Render(info StatusInfo) error {
	// Header
	_, _ = fmt.Fprintf(r.out, "%s\n\n", r.styles.Header.Render("Index Status: "+info.ProjectName))

	// Index stats
	_, _ = fmt.Fprintf(r.out, "  Files:        %d\n", info.TotalFiles)
	_, _ = fmt.Fprintf(r.out, "  Chunks:       %d\n", info.TotalChunks)
	if !info.LastIndexed.IsZero() {
		_, _ = fmt.Fprintf(r.out, "  Last indexed: %s\n", formatTime(info.LastIndexed))
	}
	_, _ = fmt.Fprintln(r.out)

	// Storage sizes
	_, _ = fmt.Fprintln(r.out, "  Storage:")
	_, _ = fmt.Fprintf(r.out, "    Metadata:   %s\n", FormatBytes(info.MetadataSize))
	_, _ = fmt.Fprintf(r.out, "    BM25 Index: %s\n", FormatBytes(info.BM25Size))
	_, _ = fmt.Fprintf(r.out, "    Vectors:    %s\n", FormatBytes(info.VectorSize))
	_, _ = fmt.Fprintf(r.out, "    Total:      %s\n", FormatBytes(info.TotalSize))
	_, _ = fmt.Fprintln(r.out)

	// Embedder status
	_, _ = fmt.Fprintln(r.out, "  Embedder:")
	_, _ = fmt.Fprintf(r.out, "    Type:   %s\n", info.EmbedderType)
	_, _ = fmt.Fprintf(r.out, "    Status: %s\n", r.renderStatus(info.EmbedderStatus))
	if info.EmbedderModel != "" {
		_, _ = fmt.Fprintf(r.out, "    Model:  %s\n", info.EmbedderModel)
	}
	_, _ = fmt.Fprintln(r.out)

	// Watcher status
	if info.WatcherStatus != "" && info.WatcherStatus != "n/a" {
		_, _ = fmt.Fprintf(r.out, "  Watcher: %s\n", r.renderStatus(info.WatcherStatus))
	}

	return nil
}

// RenderJSON outputs status as JSON.
func (r *StatusRenderer) RenderJSON(info StatusInfo) error {
	encoder := json.NewEncoder(r.out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(info)
}

// renderStatus formats a status string with color.
func (r *StatusRenderer) renderStatus(status string) string {
	switch status {
	case "ready", "running":
		return r.styles.Success.Render(status)
	case "offline", "stopped":
		return r.styles.Warning.Render(status)
	case "error":
		return r.styles.Error.Render(status)
	default:
		return status
	}
}

// formatTime formats a time for display.
func formatTime(t time.Time) string {
	now := time.Now()
	diff := now.Sub(t)

	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		mins := int(diff.Minutes())
		if mins == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	case diff < 24*time.Hour:
		hours := int(diff.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	case diff < 7*24*time.Hour:
		days := int(diff.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	default:
		return t.Format("2006-01-02 15:04")
	}
}

// FormatBytes formats bytes to human-readable format.
func FormatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
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
