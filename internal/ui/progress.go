package ui

import (
	"sync"
	"time"
)

// ProgressTracker manages progress state across stages.
// It is safe for concurrent use.
type ProgressTracker struct {
	mu          sync.RWMutex
	stage       Stage
	current     int
	total       int
	currentFile string
	startTime   time.Time
	stageStart  time.Time
	errors      []ErrorEvent
	warnings    []ErrorEvent

	// ETA smoothing to prevent wild fluctuations
	lastETA time.Duration // Previous ETA for exponential smoothing

	// Speed tracking for asitop-style metrics
	lastCurrent   int           // Previous current value for speed calculation
	lastSpeedCalc time.Time     // Last time we calculated speed
	currentSpeed  float64       // Current items/sec
	avgSpeed      float64       // Rolling average speed
	peakSpeed     float64       // Maximum observed speed
	speedSamples  int           // Number of speed samples taken
	sparkline     *Sparkline    // Sparkline for throughput visualization
}

// SpeedStats contains speed metrics for display.
type SpeedStats struct {
	Current float64 // Current items/sec
	Avg     float64 // Rolling average
	Peak    float64 // Maximum observed
}

// ProgressStats contains a snapshot of current progress.
type ProgressStats struct {
	Stage       Stage
	Current     int
	Total       int
	Progress    float64
	ETA         time.Duration
	CurrentFile string
	ErrorCount  int
	WarnCount   int
	Speed       SpeedStats // Speed metrics for display
}

// NewProgressTracker creates a new progress tracker.
func NewProgressTracker() *ProgressTracker {
	now := time.Now()
	return &ProgressTracker{
		stage:         StageScanning,
		startTime:     now,
		stageStart:    now,
		lastSpeedCalc: now,
		sparkline:     NewSparkline(60), // 60 samples for sparkline
	}
}

// SetStage transitions to a new stage.
func (p *ProgressTracker) SetStage(stage Stage, total int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.stage = stage
	p.total = total
	p.current = 0
	p.currentFile = ""
	p.stageStart = time.Now()
	p.lastETA = 0 // Reset ETA smoothing on stage change

	// Reset speed tracking on stage change
	p.lastCurrent = 0
	p.lastSpeedCalc = time.Now()
	p.currentSpeed = 0
	p.avgSpeed = 0
	p.peakSpeed = 0
	p.speedSamples = 0
	p.sparkline.Clear()
}

// Update updates progress within current stage.
func (p *ProgressTracker) Update(current int, file string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.current = current
	if file != "" {
		p.currentFile = file
	}

	// Calculate speed every 500ms to avoid noise
	now := time.Now()
	elapsed := now.Sub(p.lastSpeedCalc)
	if elapsed >= 500*time.Millisecond {
		delta := current - p.lastCurrent
		if delta > 0 && elapsed > 0 {
			// Items per second
			speed := float64(delta) / elapsed.Seconds()
			p.currentSpeed = speed

			// Update rolling average (exponential smoothing)
			p.speedSamples++
			if p.speedSamples == 1 {
				p.avgSpeed = speed
			} else {
				// Smoothing factor 0.2 gives responsive but stable average
				p.avgSpeed = 0.2*speed + 0.8*p.avgSpeed
			}

			// Track peak
			if speed > p.peakSpeed {
				p.peakSpeed = speed
			}

			// Add to sparkline
			p.sparkline.Add(speed)
		}

		p.lastCurrent = current
		p.lastSpeedCalc = now
	}
}

// AddError records an error or warning.
func (p *ProgressTracker) AddError(event ErrorEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if event.IsWarn {
		p.warnings = append(p.warnings, event)
	} else {
		p.errors = append(p.errors, event)
	}
}

// Progress returns current progress percentage (0.0-1.0).
func (p *ProgressTracker) Progress() float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.total == 0 {
		return 0.0
	}

	progress := float64(p.current) / float64(p.total)
	if progress > 1.0 {
		return 1.0
	}
	return progress
}

// ETA estimates remaining time based on current progress.
// Uses write lock because calculateETA modifies lastETA for smoothing.
func (p *ProgressTracker) ETA() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.calculateETA()
}

// Elapsed returns time since tracker creation.
func (p *ProgressTracker) Elapsed() time.Duration {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return time.Since(p.startTime)
}

// Stats returns current statistics snapshot.
// Uses write lock because calculateETA modifies lastETA for smoothing.
func (p *ProgressTracker) Stats() ProgressStats {
	p.mu.Lock()
	defer p.mu.Unlock()

	progress := 0.0
	if p.total > 0 {
		progress = float64(p.current) / float64(p.total)
		if progress > 1.0 {
			progress = 1.0
		}
	}

	return ProgressStats{
		Stage:       p.stage,
		Current:     p.current,
		Total:       p.total,
		Progress:    progress,
		ETA:         p.calculateETA(),
		CurrentFile: p.currentFile,
		ErrorCount:  len(p.errors),
		WarnCount:   len(p.warnings),
		Speed: SpeedStats{
			Current: p.currentSpeed,
			Avg:     p.avgSpeed,
			Peak:    p.peakSpeed,
		},
	}
}

// etaSmoothingFactor controls how much weight is given to new ETA values.
// 0.3 means 30% new value + 70% previous value, providing smooth updates.
const etaSmoothingFactor = 0.3

// calculateETA calculates ETA with exponential smoothing (must be called with lock held).
// The smoothing prevents wild fluctuations when batch embedding times vary.
func (p *ProgressTracker) calculateETA() time.Duration {
	if p.current == 0 || p.total == 0 {
		return 0
	}

	elapsed := time.Since(p.stageStart)
	progress := float64(p.current) / float64(p.total)

	if progress <= 0 || progress >= 1.0 {
		return 0
	}

	// Calculate raw ETA
	totalEstimate := time.Duration(float64(elapsed) / progress)
	rawRemaining := totalEstimate - elapsed

	if rawRemaining < 0 {
		return 0
	}

	// Apply exponential smoothing: smoothed = α * new + (1-α) * old
	// This prevents wild fluctuations from batch-to-batch variance
	if p.lastETA == 0 {
		p.lastETA = rawRemaining
		return rawRemaining
	}

	smoothed := time.Duration(
		etaSmoothingFactor*float64(rawRemaining) +
			(1-etaSmoothingFactor)*float64(p.lastETA),
	)
	p.lastETA = smoothed

	return smoothed
}

// Errors returns the list of recorded errors.
func (p *ProgressTracker) Errors() []ErrorEvent {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make([]ErrorEvent, len(p.errors))
	copy(result, p.errors)
	return result
}

// Warnings returns the list of recorded warnings.
func (p *ProgressTracker) Warnings() []ErrorEvent {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make([]ErrorEvent, len(p.warnings))
	copy(result, p.warnings)
	return result
}

// RenderSparkline returns the sparkline visualization string.
func (p *ProgressTracker) RenderSparkline(width int) string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.sparkline == nil {
		return ""
	}

	if width <= 0 {
		return p.sparkline.Render()
	}
	return p.sparkline.RenderWithWidth(width)
}

// SpeedStats returns current speed statistics.
func (p *ProgressTracker) SpeedStats() SpeedStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return SpeedStats{
		Current: p.currentSpeed,
		Avg:     p.avgSpeed,
		Peak:    p.peakSpeed,
	}
}
