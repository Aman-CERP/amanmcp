package ui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// TUIRenderer provides rich terminal UI using bubbletea.
type TUIRenderer struct {
	mu       sync.Mutex
	cfg      Config
	program  *tea.Program
	model    *indexingModel
	tracker  *ProgressTracker
	ctx      context.Context
	cancel   context.CancelFunc
	started  bool
	done     chan struct{}
}

// NewTUIRenderer creates a TUI renderer.
// Returns an error if TUI initialization fails (e.g., non-TTY output).
func NewTUIRenderer(cfg Config) (*TUIRenderer, error) {
	// Verify output is a TTY
	if !IsTTY(cfg.Output) {
		return nil, fmt.Errorf("output is not a TTY")
	}

	tracker := NewProgressTracker()
	model := newIndexingModel(tracker, cfg.ProjectDir)

	// Apply color settings
	if cfg.NoColor || DetectNoColor() {
		model.styles = NoColorStyles()
	}

	return &TUIRenderer{
		cfg:     cfg,
		tracker: tracker,
		model:   model,
		done:    make(chan struct{}),
	}, nil
}

// Start implements Renderer.
func (r *TUIRenderer) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.started {
		return nil
	}

	r.ctx, r.cancel = context.WithCancel(ctx)

	// Create program with output
	var opts []tea.ProgramOption
	if f, ok := r.cfg.Output.(*os.File); ok {
		opts = append(opts, tea.WithOutput(f))
	}
	// Use alternate screen buffer for proper clearing between renders
	opts = append(opts, tea.WithAltScreen())

	r.program = tea.NewProgram(r.model, opts...)
	r.started = true

	// Run in background
	go func() {
		defer close(r.done)
		_, _ = r.program.Run()
	}()

	return nil
}

// UpdateProgress implements Renderer.
func (r *TUIRenderer) UpdateProgress(event ProgressEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Update tracker
	if event.Stage != r.tracker.Stats().Stage {
		r.tracker.SetStage(event.Stage, event.Total)
	}
	r.tracker.Update(event.Current, event.CurrentFile)

	// Send message to program if running
	if r.program != nil {
		r.program.Send(progressUpdateMsg(event))
	}
}

// AddError implements Renderer.
func (r *TUIRenderer) AddError(event ErrorEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.tracker.AddError(event)

	if r.program != nil {
		r.program.Send(errorMsg(event))
	}
}

// Complete implements Renderer.
func (r *TUIRenderer) Complete(stats CompletionStats) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.tracker.SetStage(StageComplete, 0)

	if r.program != nil {
		r.program.Send(completeMsg(stats))
	}
}

// Stop implements Renderer.
func (r *TUIRenderer) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.cancel != nil {
		r.cancel()
	}

	if r.program != nil {
		r.program.Quit()

		// Wait with timeout to avoid hanging on unresponsive TUI
		select {
		case <-r.done:
			// Clean exit
		case <-time.After(2 * time.Second):
			// TUI didn't respond to quit, proceed anyway
			// This prevents the process from hanging on Ctrl+C
		}
	}

	return nil
}

// Message types for bubbletea
type progressUpdateMsg ProgressEvent
type errorMsg ErrorEvent
type completeMsg CompletionStats
type tickMsg time.Time

// indexingModel is the bubbletea model for indexing progress.
type indexingModel struct {
	tracker     *ProgressTracker
	width       int
	height      int
	quitting    bool
	complete    bool
	stats       CompletionStats
	spinner     spinner.Model
	progressBar progress.Model
	styles      Styles
	projectDir  string // Project directory path for header display
}

// newIndexingModel creates a new indexing model.
func newIndexingModel(tracker *ProgressTracker, projectDir string) *indexingModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorLime))

	// Solid lime green progress bar (asitop-inspired, not gradient)
	p := progress.New(
		progress.WithSolidFill(ColorLime),
		progress.WithWidth(50),
		progress.WithoutPercentage(),
	)

	return &indexingModel{
		tracker:     tracker,
		spinner:     s,
		progressBar: p,
		styles:      DefaultStyles(),
		width:       80,
		height:      24,
		projectDir:  projectDir,
	}
}

// Init implements tea.Model.
func (m *indexingModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		tickCmd(),
	)
}

// tickCmd returns a command that ticks every 100ms.
func tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Update implements tea.Model.
func (m *indexingModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.quitting = true
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Responsive progress bar width - scales with terminal
		m.progressBar.Width = msg.Width - 20
		if m.progressBar.Width < 20 {
			m.progressBar.Width = 20
		}

	case progressUpdateMsg:
		// Already handled by tracker in renderer
		return m, nil

	case errorMsg:
		// Already handled by tracker in renderer
		return m, nil

	case completeMsg:
		m.complete = true
		m.stats = CompletionStats(msg)
		return m, tea.Quit

	case tickMsg:
		return m, tickCmd()

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

// View implements tea.Model.
func (m *indexingModel) View() string {
	if m.quitting {
		return "Cancelled.\n"
	}

	if m.complete {
		return m.renderComplete()
	}

	// Calculate content width for panels - full terminal width minus borders
	contentWidth := m.width - 4
	if contentWidth < 40 {
		contentWidth = 40 // Minimum readable width
	}

	var sections []string

	// Header with stage indicators
	sections = append(sections, m.renderStages())

	// Divider
	sections = append(sections, m.renderDivider(contentWidth))

	// Progress section
	sections = append(sections, m.renderProgress())

	// Speed metrics
	sections = append(sections, m.renderSpeedMetrics())

	// Divider before sparkline
	sections = append(sections, m.renderDivider(contentWidth))

	// Sparkline throughput visualization
	sections = append(sections, m.renderSparkline(contentWidth))

	// Current file (if any)
	if file := m.tracker.Stats().CurrentFile; file != "" {
		sections = append(sections, m.renderDivider(contentWidth))
		sections = append(sections, m.renderCurrentFile(contentWidth))
	}

	// Join sections
	content := strings.Join(sections, "\n")

	// Wrap in panel with box border - include project path in header
	title := "AmanMCP Indexer"
	if m.projectDir != "" {
		title = fmt.Sprintf("AmanMCP Indexer • %s", m.projectDir)
	}
	panel := m.wrapInPanel(title, content, contentWidth)

	// Add status bar below panel
	statusBar := m.renderStatusBar(contentWidth)

	return panel + "\n" + statusBar
}

// renderStages renders the pipeline stage indicators.
func (m *indexingModel) renderStages() string {
	currentStage := m.tracker.Stats().Stage

	stages := []struct {
		stage Stage
		name  string
	}{
		{StageScanning, "Scan"},
		{StageChunking, "Chunk"},
		{StageEmbedding, "Embed"},
		{StageIndexing, "Index"},
	}

	var parts []string
	for _, s := range stages {
		var icon string
		var style lipgloss.Style

		switch {
		case s.stage < currentStage:
			// Completed
			icon = "●"
			style = m.styles.Success
		case s.stage == currentStage:
			// Active - show spinner
			icon = m.spinner.View()
			style = m.styles.Active
		default:
			// Pending
			icon = "○"
			style = m.styles.Dim
		}

		parts = append(parts, style.Render(icon+" "+s.name))
	}

	arrow := m.styles.Dim.Render(" → ")
	return strings.Join(parts, arrow)
}

// renderProgress renders the progress bar with percentage.
func (m *indexingModel) renderProgress() string {
	stats := m.tracker.Stats()

	if stats.Total == 0 {
		// Unknown total, show spinner with preparing state
		return fmt.Sprintf("%s %s...\n%s",
			m.spinner.View(),
			stats.Stage.String(),
			m.styles.Dim.Render("Preparing..."))
	}

	// Show progress bar with percentage aligned right
	percent := stats.Progress
	bar := m.progressBar.ViewAs(percent)
	pctStr := m.styles.Active.Render(fmt.Sprintf("%3.0f%%", percent*100))

	// Count line below progress bar
	countLine := m.styles.Label.Render(fmt.Sprintf("%d / %d chunks", stats.Current, stats.Total))

	return fmt.Sprintf("%s  %s\n%s", bar, pctStr, countLine)
}

// renderSpeedMetrics renders speed stats (current/avg/peak) and ETA.
func (m *indexingModel) renderSpeedMetrics() string {
	stats := m.tracker.Stats()

	// Speed: 42/s (avg: 38, peak: 67)  •  ETA: 2m 15s
	var parts []string

	// Always show speed for consistency (even 0/s)
	speedStr := fmt.Sprintf("Speed: %.0f/s", stats.Speed.Current)
	if stats.Speed.Avg > 0 {
		speedStr += fmt.Sprintf(" (avg: %.0f, peak: %.0f)", stats.Speed.Avg, stats.Speed.Peak)
	}
	parts = append(parts, m.styles.Speed.Render(speedStr))

	if e := stats.ETA; e > 0 {
		etaStr := fmt.Sprintf("ETA: %s", formatDuration(e))
		parts = append(parts, m.styles.Label.Render(etaStr))
	}

	separator := m.styles.Dim.Render("  •  ")
	return strings.Join(parts, separator)
}

// renderSparkline renders the throughput sparkline.
func (m *indexingModel) renderSparkline(width int) string {
	// Responsive sparkline width - scales with terminal
	sparkWidth := width - 10
	if sparkWidth < 10 {
		sparkWidth = 10
	}

	spark := m.tracker.RenderSparkline(sparkWidth)
	label := m.styles.Dim.Render("throughput ─")

	return m.styles.Sparkline.Render(spark) + " " + label
}

// renderCurrentFile renders the current file being processed.
func (m *indexingModel) renderCurrentFile(width int) string {
	file := m.tracker.Stats().CurrentFile
	if file == "" {
		return ""
	}

	truncated := truncateFilePath(file, width-2)
	return m.styles.Dim.Render(truncated)
}

// renderDivider renders a horizontal divider line.
func (m *indexingModel) renderDivider(width int) string {
	line := strings.Repeat("─", width)
	return m.styles.Border.Render(line)
}

// wrapInPanel wraps content in a box border with title.
func (m *indexingModel) wrapInPanel(title, content string, width int) string {
	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorDarkGray)).
		Padding(0, 1).
		Width(width)

	// Render title in header style
	titleStyled := m.styles.Header.Render(title)

	// Build the panel with title
	return lipgloss.JoinVertical(lipgloss.Left,
		titleStyled,
		panel.Render(content),
	)
}

// renderStatusBar renders the bottom status bar with warnings/errors.
func (m *indexingModel) renderStatusBar(width int) string {
	stats := m.tracker.Stats()
	var parts []string

	if stats.WarnCount > 0 {
		parts = append(parts, m.styles.Warning.Render(fmt.Sprintf("⚠ %d warnings", stats.WarnCount)))
	}
	if stats.ErrorCount > 0 {
		parts = append(parts, m.styles.Error.Render(fmt.Sprintf("✗ %d errors", stats.ErrorCount)))
	}

	if len(parts) == 0 {
		// Show hint when no errors
		return m.styles.Dim.Render("q to quit")
	}

	separator := m.styles.Dim.Render("  │  ")
	status := strings.Join(parts, separator)
	hint := m.styles.Dim.Render("  │  q to quit")

	return status + hint
}

// formatDuration formats a duration in a human-friendly way.
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		if s == 0 {
			return fmt.Sprintf("%dm", m)
		}
		return fmt.Sprintf("%dm %ds", m, s)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh %dm", h, m)
}


// renderComplete renders the completion summary with polished box layout.
func (m *indexingModel) renderComplete() string {
	// Responsive completion view width - scales with terminal
	contentWidth := m.width - 4
	if contentWidth < 40 {
		contentWidth = 40
	}

	var lines []string

	// Success header with checkmark
	lines = append(lines, m.styles.Success.Render("✓ Indexing Complete"))
	lines = append(lines, "")

	// Stats in a clean format
	filesLabel := m.styles.Label.Render("Files:")
	chunksLabel := m.styles.Label.Render("Chunks:")
	durationLabel := m.styles.Label.Render("Duration:")

	lines = append(lines, fmt.Sprintf("%s    %s", filesLabel, m.styles.Active.Render(fmt.Sprintf("%d", m.stats.Files))))
	lines = append(lines, fmt.Sprintf("%s   %s", chunksLabel, m.styles.Active.Render(fmt.Sprintf("%d", m.stats.Chunks))))
	lines = append(lines, fmt.Sprintf("%s %s", durationLabel, m.styles.Active.Render(formatDuration(m.stats.Duration))))

	// Speed stats if available
	speedStats := m.tracker.SpeedStats()
	if speedStats.Avg > 0 {
		speedLabel := m.styles.Label.Render("Avg Speed:")
		lines = append(lines, fmt.Sprintf("%s %s", speedLabel, m.styles.Speed.Render(fmt.Sprintf("%.0f chunks/sec", speedStats.Avg))))
	}

	// Errors/warnings section
	if m.stats.Errors > 0 || m.stats.Warnings > 0 {
		lines = append(lines, "")
		if m.stats.Errors > 0 {
			lines = append(lines, m.styles.Error.Render(fmt.Sprintf("✗ %d errors", m.stats.Errors)))
		}
		if m.stats.Warnings > 0 {
			lines = append(lines, m.styles.Warning.Render(fmt.Sprintf("⚠ %d warnings", m.stats.Warnings)))
		}
	}

	content := strings.Join(lines, "\n")

	// Wrap in panel
	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorLime)). // Lime border for success
		Padding(1, 2).
		Width(contentWidth)

	return panel.Render(content) + "\n"
}

// truncateFilePath truncates a file path to fit within maxLen.
func truncateFilePath(path string, maxLen int) string {
	if path == "" || len(path) <= maxLen {
		return path
	}

	// Keep the filename and as much of the path as fits
	parts := strings.Split(path, "/")
	if len(parts) == 1 {
		// No separators, just truncate
		if maxLen < 4 {
			return "..."
		}
		return "..." + path[len(path)-maxLen+3:]
	}

	filename := parts[len(parts)-1]
	if len(filename)+4 > maxLen {
		// Filename alone is too long
		return "..." + filename[len(filename)-maxLen+3:]
	}

	// Try to fit as much path as possible
	remaining := maxLen - len(filename) - 4 // 4 for ".../"
	if remaining <= 0 {
		return ".../" + filename
	}

	prefix := strings.Join(parts[:len(parts)-1], "/")
	if len(prefix) <= remaining {
		return path
	}

	return "..." + prefix[len(prefix)-remaining:] + "/" + filename
}

// Ensure TUIRenderer implements Renderer
var _ Renderer = (*TUIRenderer)(nil)
