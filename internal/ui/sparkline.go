package ui

import (
	"strings"
)

// Sparkline renders a text-based sparkline chart using Unicode block characters.
// Inspired by asitop's real-time throughput visualization.
type Sparkline struct {
	samples []float64 // Ring buffer of samples
	width   int       // Display width (number of bars)
	head    int       // Current position in ring buffer
	count   int       // Number of samples added
	max     float64   // Maximum value seen (for scaling)
}

// SparklineChars are the Unicode block characters for rendering sparklines.
// 8 levels of height from empty to full.
var SparklineChars = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// NewSparkline creates a new sparkline with the given display width.
func NewSparkline(width int) *Sparkline {
	if width <= 0 {
		width = 60 // Default to 60 samples (1 minute at 1 sample/sec)
	}
	return &Sparkline{
		samples: make([]float64, width),
		width:   width,
		head:    0,
		count:   0,
		max:     0,
	}
}

// Add adds a new sample to the sparkline.
func (s *Sparkline) Add(value float64) {
	s.samples[s.head] = value
	s.head = (s.head + 1) % s.width
	s.count++

	// Update max
	if value > s.max {
		s.max = value
	}

	// Recalculate max periodically to handle decreasing values
	if s.count%s.width == 0 {
		s.recalculateMax()
	}
}

// recalculateMax finds the current maximum in the buffer.
func (s *Sparkline) recalculateMax() {
	s.max = 0
	for _, v := range s.samples {
		if v > s.max {
			s.max = v
		}
	}
	// Ensure max is at least 1 to avoid division by zero
	if s.max < 1 {
		s.max = 1
	}
}

// Render returns the sparkline as a string of block characters.
func (s *Sparkline) Render() string {
	if s.count == 0 {
		return strings.Repeat(string(SparklineChars[0]), s.width)
	}

	// Ensure we have a valid max
	if s.max <= 0 {
		s.recalculateMax()
	}

	var sb strings.Builder
	sb.Grow(s.width * 3) // UTF-8 chars can be up to 3 bytes

	// Render from oldest to newest
	numSamples := min(s.count, s.width)
	start := 0
	if s.count >= s.width {
		start = s.head
	}

	for i := 0; i < s.width; i++ {
		idx := (start + i) % s.width
		value := s.samples[idx]

		// Scale value to 0-7 range for character selection
		var charIdx int
		if s.max > 0 {
			scaled := value / s.max
			charIdx = int(scaled * float64(len(SparklineChars)-1))
			if charIdx < 0 {
				charIdx = 0
			}
			if charIdx >= len(SparklineChars) {
				charIdx = len(SparklineChars) - 1
			}
		}

		// Show empty for samples we haven't reached yet
		if i >= numSamples && s.count < s.width {
			sb.WriteRune(' ')
		} else {
			sb.WriteRune(SparklineChars[charIdx])
		}
	}

	return sb.String()
}

// RenderWithWidth returns the sparkline at a specific width.
// Useful when terminal width changes.
func (s *Sparkline) RenderWithWidth(width int) string {
	if width <= 0 || width >= s.width {
		return s.Render()
	}

	// Render only the most recent 'width' samples
	if s.count == 0 {
		return strings.Repeat(string(SparklineChars[0]), width)
	}

	if s.max <= 0 {
		s.recalculateMax()
	}

	var sb strings.Builder
	sb.Grow(width * 3)

	numSamples := min(s.count, s.width)
	skipCount := 0
	if numSamples > width {
		skipCount = numSamples - width
	}

	start := 0
	if s.count >= s.width {
		start = s.head
	}

	rendered := 0
	for i := 0; i < s.width && rendered < width; i++ {
		if i < skipCount {
			continue
		}

		idx := (start + i) % s.width
		value := s.samples[idx]

		var charIdx int
		if s.max > 0 {
			scaled := value / s.max
			charIdx = int(scaled * float64(len(SparklineChars)-1))
			if charIdx < 0 {
				charIdx = 0
			}
			if charIdx >= len(SparklineChars) {
				charIdx = len(SparklineChars) - 1
			}
		}

		if i >= numSamples && s.count < s.width {
			sb.WriteRune(' ')
		} else {
			sb.WriteRune(SparklineChars[charIdx])
		}
		rendered++
	}

	// Pad with spaces if we don't have enough samples
	for rendered < width {
		sb.WriteRune(' ')
		rendered++
	}

	return sb.String()
}

// Clear resets the sparkline.
func (s *Sparkline) Clear() {
	for i := range s.samples {
		s.samples[i] = 0
	}
	s.head = 0
	s.count = 0
	s.max = 0
}

// Count returns the number of samples added.
func (s *Sparkline) Count() int {
	return s.count
}

// Max returns the current maximum value.
func (s *Sparkline) Max() float64 {
	return s.max
}
