package ui

import "github.com/charmbracelet/lipgloss"

// Color palette - asitop-inspired lime green theme
// Single accent color for professional, distinctive look
const (
	ColorLime     = "154" // Primary accent (#AFFF00) - bright lime green
	ColorLimeDim  = "106" // Dimmed lime for inactive/borders
	ColorWhite    = "255" // Headers, important text
	ColorGray     = "245" // Secondary text, labels
	ColorDarkGray = "238" // Box borders, separators
	ColorRed      = "196" // Errors
	ColorYellow   = "220" // Warnings
)

// Styles holds all UI styles for TUI rendering.
type Styles struct {
	// Text styles
	Header   lipgloss.Style
	Success  lipgloss.Style
	Warning  lipgloss.Style
	Error    lipgloss.Style
	Dim      lipgloss.Style
	Stage    lipgloss.Style
	Active   lipgloss.Style
	Progress lipgloss.Style

	// Panel/layout styles
	Border    lipgloss.Style
	Panel     lipgloss.Style
	Sparkline lipgloss.Style
	Speed     lipgloss.Style
	Label     lipgloss.Style
}

// DefaultStyles returns styled components for TUI mode.
// Uses asitop-inspired lime green palette.
func DefaultStyles() Styles {
	return Styles{
		// Text styles - lime green accent
		Header:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorLime)),
		Success:  lipgloss.NewStyle().Foreground(lipgloss.Color(ColorLime)),
		Warning:  lipgloss.NewStyle().Foreground(lipgloss.Color(ColorYellow)),
		Error:    lipgloss.NewStyle().Foreground(lipgloss.Color(ColorRed)),
		Dim:      lipgloss.NewStyle().Foreground(lipgloss.Color(ColorDarkGray)),
		Stage:    lipgloss.NewStyle().Foreground(lipgloss.Color(ColorLimeDim)),
		Active:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorLime)),
		Progress: lipgloss.NewStyle().Foreground(lipgloss.Color(ColorLime)),

		// Panel/layout styles
		Border: lipgloss.NewStyle().Foreground(lipgloss.Color(ColorDarkGray)),
		Panel: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(ColorDarkGray)).
			Padding(0, 1),
		Sparkline: lipgloss.NewStyle().Foreground(lipgloss.Color(ColorLime)),
		Speed:     lipgloss.NewStyle().Foreground(lipgloss.Color(ColorGray)),
		Label:     lipgloss.NewStyle().Foreground(lipgloss.Color(ColorGray)),
	}
}

// NoColorStyles returns unstyled components for plain mode.
func NoColorStyles() Styles {
	return Styles{
		Header:    lipgloss.NewStyle(),
		Success:   lipgloss.NewStyle(),
		Warning:   lipgloss.NewStyle(),
		Error:     lipgloss.NewStyle(),
		Dim:       lipgloss.NewStyle(),
		Stage:     lipgloss.NewStyle(),
		Active:    lipgloss.NewStyle(),
		Progress:  lipgloss.NewStyle(),
		Border:    lipgloss.NewStyle(),
		Panel:     lipgloss.NewStyle(),
		Sparkline: lipgloss.NewStyle(),
		Speed:     lipgloss.NewStyle(),
		Label:     lipgloss.NewStyle(),
	}
}

// GetStyles returns the appropriate styles based on color preference.
func GetStyles(noColor bool) Styles {
	if noColor {
		return NoColorStyles()
	}
	return DefaultStyles()
}
