// Package profiling provides CPU, memory, and trace profiling utilities.
package profiling

import (
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
)

// Profiler manages performance profiling for the application.
type Profiler struct {
	cpuFile   *os.File
	traceFile *os.File
}

// NewProfiler creates a new Profiler instance.
func NewProfiler() *Profiler {
	return &Profiler{}
}

// StartCPU starts CPU profiling to the specified file.
// Returns a cleanup function that must be called to stop profiling and flush data.
func (p *Profiler) StartCPU(path string) (cleanup func(), err error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("failed to create CPU profile file: %w", err)
	}

	if err := pprof.StartCPUProfile(f); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("failed to start CPU profile: %w", err)
	}

	p.cpuFile = f

	return func() {
		pprof.StopCPUProfile()
		_ = p.cpuFile.Close()
		p.cpuFile = nil
	}, nil
}

// WriteHeap writes a heap profile to the specified file.
// This is a point-in-time snapshot of memory allocations.
func (p *Profiler) WriteHeap(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create heap profile file: %w", err)
	}
	defer func() { _ = f.Close() }()

	// Force garbage collection before profiling for accurate results
	runtime.GC()

	if err := pprof.WriteHeapProfile(f); err != nil {
		return fmt.Errorf("failed to write heap profile: %w", err)
	}

	return nil
}

// StartTrace starts execution tracing to the specified file.
// Returns a cleanup function that must be called to stop tracing.
func (p *Profiler) StartTrace(path string) (cleanup func(), err error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("failed to create trace file: %w", err)
	}

	if err := trace.Start(f); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("failed to start trace: %w", err)
	}

	p.traceFile = f

	return func() {
		trace.Stop()
		_ = p.traceFile.Close()
		p.traceFile = nil
	}, nil
}

// WriteAllocs writes an allocations profile to the specified file.
// Shows all past memory allocations, not just live objects.
func (p *Profiler) WriteAllocs(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create allocs profile file: %w", err)
	}
	defer func() { _ = f.Close() }()

	runtime.GC()

	if err := pprof.Lookup("allocs").WriteTo(f, 0); err != nil {
		return fmt.Errorf("failed to write allocs profile: %w", err)
	}

	return nil
}

// WriteGoroutine writes a goroutine profile to the specified file.
// Shows stack traces of all current goroutines.
func (p *Profiler) WriteGoroutine(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create goroutine profile file: %w", err)
	}
	defer func() { _ = f.Close() }()

	if err := pprof.Lookup("goroutine").WriteTo(f, 1); err != nil {
		return fmt.Errorf("failed to write goroutine profile: %w", err)
	}

	return nil
}

// WriteBlock writes a block profile to the specified file.
// Shows where goroutines block waiting on synchronization primitives.
func (p *Profiler) WriteBlock(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create block profile file: %w", err)
	}
	defer func() { _ = f.Close() }()

	if err := pprof.Lookup("block").WriteTo(f, 0); err != nil {
		return fmt.Errorf("failed to write block profile: %w", err)
	}

	return nil
}

// MemStats returns current memory statistics.
func MemStats() runtime.MemStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m
}

// FormatBytes formats bytes into human-readable form.
func FormatBytes(bytes uint64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
