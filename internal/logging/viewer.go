package logging

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// LogEntry represents a parsed JSON log line.
type LogEntry struct {
	Time    time.Time              `json:"time"`
	Level   string                 `json:"level"`
	Msg     string                 `json:"msg"`
	Source  string                 `json:"source"` // Log source: "go", "mlx", etc.
	Attrs   map[string]interface{} `json:"-"`      // Additional attributes
	Raw     string                 `json:"-"`      // Original line
	IsValid bool                   `json:"-"`      // Whether JSON parsing succeeded
}

// ViewerConfig configures the log viewer.
type ViewerConfig struct {
	Level      string         // Filter by level (debug, info, warn, error)
	Pattern    *regexp.Regexp // Filter by pattern
	NoColor    bool           // Disable colors
	ShowSource bool           // Show source label in output
}

// Viewer provides log viewing and filtering capabilities.
type Viewer struct {
	config ViewerConfig
	out    io.Writer
}

// NewViewer creates a new log viewer.
func NewViewer(cfg ViewerConfig, out io.Writer) *Viewer {
	return &Viewer{
		config: cfg,
		out:    out,
	}
}

// Tail reads the last n lines from a log file and returns matching entries.
func (v *Viewer) Tail(path string, n int) ([]LogEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}
	defer func() { _ = file.Close() }()

	// Read all lines (for simplicity; could optimize with seek for large files)
	var lines []string
	scanner := bufio.NewScanner(file)
	// Increase buffer size for long log lines
	const maxCapacity = 1024 * 1024 // 1MB
	buf := make([]byte, maxCapacity)
	scanner.Buffer(buf, maxCapacity)

	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read log file: %w", err)
	}

	// Take last n lines
	start := 0
	if len(lines) > n {
		start = len(lines) - n
	}
	lines = lines[start:]

	// Parse and filter entries
	var entries []LogEntry
	for _, line := range lines {
		entry := v.parseLine(line)
		if v.matchesFilter(entry) {
			entries = append(entries, entry)
		}
	}

	return entries, nil
}

// TailMultiple reads the last n lines from multiple log files and returns merged entries.
// Entries are sorted by timestamp for a unified timeline view.
func (v *Viewer) TailMultiple(paths []string, n int) ([]LogEntry, error) {
	var allEntries []LogEntry

	for _, path := range paths {
		// Determine source from filename
		source := sourceFromPath(path)

		file, err := os.Open(path)
		if err != nil {
			// Skip files that can't be opened, continue with others
			continue
		}

		var lines []string
		scanner := bufio.NewScanner(file)
		const maxCapacity = 1024 * 1024
		buf := make([]byte, maxCapacity)
		scanner.Buffer(buf, maxCapacity)

		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		_ = file.Close()

		if scanner.Err() != nil {
			continue
		}

		// Take last n lines from each file
		start := 0
		if len(lines) > n {
			start = len(lines) - n
		}
		lines = lines[start:]

		// Parse and filter entries
		for _, line := range lines {
			entry := v.parseLineWithSource(line, source)
			if v.matchesFilter(entry) {
				allEntries = append(allEntries, entry)
			}
		}
	}

	// Sort all entries by timestamp
	sort.Slice(allEntries, func(i, j int) bool {
		return allEntries[i].Time.Before(allEntries[j].Time)
	})

	// Take last n entries from merged result
	if len(allEntries) > n {
		allEntries = allEntries[len(allEntries)-n:]
	}

	return allEntries, nil
}

// FollowMultiple watches multiple log files for new entries and sends them to the channel.
// Entries from all files are merged and sorted by timestamp.
func (v *Viewer) FollowMultiple(ctx context.Context, paths []string, entries chan<- LogEntry) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(paths))

	// Start a goroutine for each file
	for _, path := range paths {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			source := sourceFromPath(p)

			file, err := os.Open(p)
			if err != nil {
				errCh <- fmt.Errorf("failed to open %s: %w", p, err)
				return
			}
			defer func() { _ = file.Close() }()

			// Seek to end
			if _, err := file.Seek(0, io.SeekEnd); err != nil {
				errCh <- fmt.Errorf("failed to seek in %s: %w", p, err)
				return
			}

			reader := bufio.NewReader(file)
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					for {
						line, err := reader.ReadString('\n')
						if err != nil {
							break
						}

						line = strings.TrimSuffix(line, "\n")
						if line == "" {
							continue
						}

						entry := v.parseLineWithSource(line, source)
						if v.matchesFilter(entry) {
							select {
							case entries <- entry:
							case <-ctx.Done():
								return
							}
						}
					}
				}
			}
		}(path)
	}

	// Wait for all followers to finish
	go func() {
		wg.Wait()
		close(errCh)
	}()

	// Wait for context cancellation
	<-ctx.Done()
	return nil
}

// sourceFromPath extracts the log source from a file path.
func sourceFromPath(path string) string {
	base := filepath.Base(path)
	switch {
	case strings.HasPrefix(base, "mlx-server"):
		return "mlx"
	case strings.HasPrefix(base, "server"):
		return "go"
	default:
		return "unknown"
	}
}

// parseLineWithSource parses a JSON log line and sets the source if not present.
func (v *Viewer) parseLineWithSource(line, defaultSource string) LogEntry {
	entry := v.parseLine(line)
	if entry.Source == "" {
		entry.Source = defaultSource
	}
	return entry
}

// Follow watches a log file for new entries and sends them to the channel.
// Blocks until context is cancelled.
func (v *Viewer) Follow(ctx context.Context, path string, entries chan<- LogEntry) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer func() { _ = file.Close() }()

	// Seek to end of file
	_, err = file.Seek(0, io.SeekEnd)
	if err != nil {
		return fmt.Errorf("failed to seek to end: %w", err)
	}

	reader := bufio.NewReader(file)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			// Try to read new lines
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					break // No more data available
				}

				line = strings.TrimSuffix(line, "\n")
				if line == "" {
					continue
				}

				entry := v.parseLine(line)
				if v.matchesFilter(entry) {
					select {
					case entries <- entry:
					case <-ctx.Done():
						return nil
					}
				}
			}
		}
	}
}

// FormatEntry formats a log entry for display.
func (v *Viewer) FormatEntry(entry LogEntry) string {
	if !entry.IsValid {
		// Return raw line for unparseable entries
		return entry.Raw
	}

	// Format timestamp
	timestamp := entry.Time.Format("15:04:05.000")

	// Format level with color
	level := v.formatLevel(entry.Level)

	// Format source label if enabled
	sourceLabel := ""
	if v.config.ShowSource && entry.Source != "" {
		sourceLabel = v.formatSource(entry.Source) + " "
	}

	// Format message
	msg := entry.Msg

	// Format additional attributes (exclude source since we show it separately)
	var attrs []string
	for k, val := range entry.Attrs {
		if k != "source" { // Don't duplicate source in attrs
			attrs = append(attrs, fmt.Sprintf("%s=%v", k, val))
		}
	}
	attrStr := ""
	if len(attrs) > 0 {
		attrStr = " " + strings.Join(attrs, " ")
	}

	return fmt.Sprintf("%s %s %s%s%s", timestamp, level, sourceLabel, msg, attrStr)
}

// formatSource formats the source label with optional color.
func (v *Viewer) formatSource(source string) string {
	label := fmt.Sprintf("[%s]", source)

	if v.config.NoColor {
		return label
	}

	// Different colors for different sources
	switch source {
	case "go":
		return "\033[36m" + label + "\033[0m" // Cyan
	case "mlx":
		return "\033[35m" + label + "\033[0m" // Magenta
	default:
		return "\033[90m" + label + "\033[0m" // Gray
	}
}

// Print prints entries to the output.
func (v *Viewer) Print(entries []LogEntry) {
	for _, entry := range entries {
		_, _ = fmt.Fprintln(v.out, v.FormatEntry(entry))
	}
}

// parseLine parses a JSON log line into LogEntry.
func (v *Viewer) parseLine(line string) LogEntry {
	entry := LogEntry{
		Raw:     line,
		IsValid: false,
	}

	// Try to parse as JSON
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(line), &data); err != nil {
		return entry
	}

	entry.IsValid = true

	// Extract standard fields
	if t, ok := data["time"].(string); ok {
		if parsed, err := time.Parse(time.RFC3339Nano, t); err == nil {
			entry.Time = parsed
		}
	}

	if l, ok := data["level"].(string); ok {
		entry.Level = l
	}

	if m, ok := data["msg"].(string); ok {
		entry.Msg = m
	}

	// Extract source field (for multi-source log viewing)
	if s, ok := data["source"].(string); ok {
		entry.Source = s
	}

	// Collect remaining attributes
	entry.Attrs = make(map[string]interface{})
	for k, val := range data {
		if k != "time" && k != "level" && k != "msg" && k != "source" {
			entry.Attrs[k] = val
		}
	}

	return entry
}

// matchesFilter checks if an entry matches the configured filters.
func (v *Viewer) matchesFilter(entry LogEntry) bool {
	// Level filter
	if v.config.Level != "" {
		entryLevel := LevelFromString(entry.Level)
		filterLevel := LevelFromString(v.config.Level)
		if entryLevel < filterLevel {
			return false
		}
	}

	// Pattern filter
	if v.config.Pattern != nil {
		if !v.config.Pattern.MatchString(entry.Raw) {
			return false
		}
	}

	return true
}

// formatLevel formats the log level with optional color.
func (v *Viewer) formatLevel(level string) string {
	levelStr := strings.ToUpper(level)
	if len(levelStr) > 5 {
		levelStr = levelStr[:5]
	}
	levelStr = fmt.Sprintf("%-5s", levelStr)

	if v.config.NoColor {
		return levelStr
	}

	// ANSI colors
	switch strings.ToLower(level) {
	case "debug":
		return "\033[90m" + levelStr + "\033[0m" // Gray
	case "info":
		return "\033[32m" + levelStr + "\033[0m" // Green
	case "warn", "warning":
		return "\033[33m" + levelStr + "\033[0m" // Yellow
	case "error":
		return "\033[31m" + levelStr + "\033[0m" // Red
	default:
		return levelStr
	}
}
