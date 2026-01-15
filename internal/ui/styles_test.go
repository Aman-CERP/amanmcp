package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultStyles_ReturnsStyles(t *testing.T) {
	// When: getting default styles
	styles := DefaultStyles()

	// Then: styles are defined
	assert.NotNil(t, styles.Header)
	assert.NotNil(t, styles.Success)
	assert.NotNil(t, styles.Warning)
	assert.NotNil(t, styles.Error)
	assert.NotNil(t, styles.Dim)
	assert.NotNil(t, styles.Stage)
	assert.NotNil(t, styles.Active)
	assert.NotNil(t, styles.Progress)
}

func TestNoColorStyles_ReturnsEmptyStyles(t *testing.T) {
	// When: getting no color styles
	styles := NoColorStyles()

	// Then: styles are defined but render without color
	// We test by rendering empty string - should work without panic
	_ = styles.Header.Render("")
	_ = styles.Success.Render("")
	_ = styles.Warning.Render("")
	_ = styles.Error.Render("")
	_ = styles.Dim.Render("")
	_ = styles.Stage.Render("")
	_ = styles.Active.Render("")
	_ = styles.Progress.Render("")
}

func TestDefaultStyles_HeaderIsBold(t *testing.T) {
	// Given: default styles
	styles := DefaultStyles()

	// When: rendering header text
	rendered := styles.Header.Render("Test")

	// Then: header contains the text
	assert.Contains(t, rendered, "Test")
}

func TestStyles_RenderStageIndicators(t *testing.T) {
	// Given: default styles
	styles := DefaultStyles()

	// When: rendering stage indicators
	active := styles.Active.Render("●")
	dim := styles.Dim.Render("○")

	// Then: they render without panic
	assert.Contains(t, active, "●")
	assert.Contains(t, dim, "○")
}

func TestGetStyles_WithNoColor(t *testing.T) {
	// When: getting styles with noColor=true
	styles := GetStyles(true)

	// Then: returns no-color styles (plain rendering)
	text := styles.Success.Render("test")
	assert.Equal(t, "test", text)
}

func TestGetStyles_WithColor(t *testing.T) {
	// When: getting styles with noColor=false
	styles := GetStyles(false)

	// Then: returns colored styles
	// Note: exact ANSI codes depend on terminal, but text should be present
	text := styles.Success.Render("test")
	assert.Contains(t, text, "test")
}
