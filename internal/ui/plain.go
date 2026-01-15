package ui

import (
	"context"
	"fmt"
	"io"
	"sync"
)

// PlainRenderer outputs plain text progress (for CI/pipes).
type PlainRenderer struct {
	mu      sync.Mutex
	out     io.Writer
	noColor bool
	stage   Stage
	errors  []ErrorEvent
}

// NewPlainRenderer creates a plain text renderer.
func NewPlainRenderer(cfg Config) *PlainRenderer {
	return &PlainRenderer{
		out:     cfg.Output,
		noColor: cfg.NoColor,
	}
}

// Start implements Renderer.
func (r *PlainRenderer) Start(ctx context.Context) error {
	return nil
}

// UpdateProgress implements Renderer.
func (r *PlainRenderer) UpdateProgress(event ProgressEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.stage = event.Stage

	// Format: [STAGE] current/total - message or file
	var msg string
	if event.Message != "" {
		msg = event.Message
	} else if event.CurrentFile != "" {
		msg = event.CurrentFile
	}

	if event.Total > 0 {
		_, _ = fmt.Fprintf(r.out, "[%s] %d/%d - %s\n", event.Stage.Icon(), event.Current, event.Total, msg)
	} else if msg != "" {
		_, _ = fmt.Fprintf(r.out, "[%s] %s\n", event.Stage.Icon(), msg)
	}
}

// AddError implements Renderer.
func (r *PlainRenderer) AddError(event ErrorEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.errors = append(r.errors, event)

	prefix := "ERROR"
	if event.IsWarn {
		prefix = "WARN"
	}

	if event.File != "" {
		_, _ = fmt.Fprintf(r.out, "%s: %s: %v\n", prefix, event.File, event.Err)
	} else {
		_, _ = fmt.Fprintf(r.out, "%s: %v\n", prefix, event.Err)
	}
}

// Complete implements Renderer.
func (r *PlainRenderer) Complete(stats CompletionStats) {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, _ = fmt.Fprintf(r.out, "Complete: %d files, %d chunks indexed in %s",
		stats.Files, stats.Chunks, stats.Duration.Round(100*millisecond))

	if stats.Errors > 0 || stats.Warnings > 0 {
		_, _ = fmt.Fprintf(r.out, " (%d errors, %d warnings)", stats.Errors, stats.Warnings)
	}

	_, _ = fmt.Fprintln(r.out)

	// Show detailed stage breakdown if available
	if stats.Stages.Scan > 0 || stats.Stages.Embed > 0 {
		_, _ = fmt.Fprintln(r.out)
		_, _ = fmt.Fprintln(r.out, "Stage Breakdown:")
		_, _ = fmt.Fprintf(r.out, "  Scan:    %s (files discovered)\n", stats.Stages.Scan.Round(100*millisecond))
		_, _ = fmt.Fprintf(r.out, "  Chunk:   %s (code parsed)\n", stats.Stages.Chunk.Round(100*millisecond))
		if stats.Stages.Context > 0 {
			_, _ = fmt.Fprintf(r.out, "  Context: %s (CR-1 enrichment)\n", stats.Stages.Context.Round(100*millisecond))
		}
		if stats.Stages.Embed > 0 && stats.Chunks > 0 {
			chunksPerSec := float64(stats.Chunks) / stats.Stages.Embed.Seconds()
			_, _ = fmt.Fprintf(r.out, "  Embed:   %s (%d chunks @ %.1f/sec)\n",
				stats.Stages.Embed.Round(100*millisecond), stats.Chunks, chunksPerSec)
		}
		_, _ = fmt.Fprintf(r.out, "  Index:   %s (BM25 + vector)\n", stats.Stages.Index.Round(100*millisecond))
	}

	// Show embedder backend info if available
	if stats.Embedder.Backend != "" {
		_, _ = fmt.Fprintln(r.out)
		_, _ = fmt.Fprintf(r.out, "Backend: %s (%s, %d dims)\n",
			stats.Embedder.Backend, stats.Embedder.Model, stats.Embedder.Dimensions)
	}
}

// Stop implements Renderer.
func (r *PlainRenderer) Stop() error {
	return nil
}

const millisecond = 1000000 // nanoseconds
