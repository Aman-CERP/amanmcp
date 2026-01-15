package profiling

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProfiler_StartCPU(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "cpu.prof")

	p := NewProfiler()
	cleanup, err := p.StartCPU(path)
	require.NoError(t, err)

	// Do some work to generate CPU data
	sum := 0
	for i := 0; i < 1000000; i++ {
		sum += i
	}
	_ = sum

	cleanup()

	// Verify file was created and has content
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Greater(t, info.Size(), int64(0))
}

func TestProfiler_WriteHeap(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "heap.prof")

	p := NewProfiler()
	err := p.WriteHeap(path)
	require.NoError(t, err)

	// Verify file was created and has content
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Greater(t, info.Size(), int64(0))
}

func TestProfiler_StartTrace(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "trace.out")

	p := NewProfiler()
	cleanup, err := p.StartTrace(path)
	require.NoError(t, err)

	// Do some work to generate trace data
	sum := 0
	for i := 0; i < 1000; i++ {
		sum += i
	}
	_ = sum

	cleanup()

	// Verify file was created and has content
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Greater(t, info.Size(), int64(0))
}

func TestProfiler_WriteAllocs(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "allocs.prof")

	// Allocate some memory
	_ = make([]byte, 1024*1024)

	p := NewProfiler()
	err := p.WriteAllocs(path)
	require.NoError(t, err)

	// Verify file was created and has content
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Greater(t, info.Size(), int64(0))
}

func TestProfiler_WriteGoroutine(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "goroutine.prof")

	p := NewProfiler()
	err := p.WriteGoroutine(path)
	require.NoError(t, err)

	// Verify file was created and has content
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Greater(t, info.Size(), int64(0))
}

func TestMemStats(t *testing.T) {
	stats := MemStats()
	assert.Greater(t, stats.Alloc, uint64(0))
	assert.Greater(t, stats.TotalAlloc, uint64(0))
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes    uint64
		expected string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.00 KB"},
		{1536, "1.50 KB"},
		{1024 * 1024, "1.00 MB"},
		{1024 * 1024 * 1024, "1.00 GB"},
		{2 * 1024 * 1024 * 1024, "2.00 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := FormatBytes(tt.bytes)
			assert.Equal(t, tt.expected, result)
		})
	}
}
