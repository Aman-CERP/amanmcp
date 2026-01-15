package preflight

import (
	"fmt"
	"syscall"
)

// MinDiskSpaceBytes is the minimum required free disk space (100MB).
const MinDiskSpaceBytes = 100 * 1024 * 1024

// CheckDiskSpace checks if there's sufficient disk space at the given path.
func (c *Checker) CheckDiskSpace(path string) CheckResult {
	result := CheckResult{
		Name:     "disk_space",
		Required: true,
	}

	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		result.Status = StatusFail
		result.Message = fmt.Sprintf("failed to check disk space: %v", err)
		return result
	}

	// Calculate available space in bytes
	availableBytes := stat.Bavail * uint64(stat.Bsize)

	if availableBytes < MinDiskSpaceBytes {
		result.Status = StatusFail
		result.Message = fmt.Sprintf("%s free (minimum: 100 MB)", formatBytes(availableBytes))
		return result
	}

	result.Status = StatusPass
	result.Message = fmt.Sprintf("%s free (minimum: 100 MB)", formatBytes(availableBytes))
	return result
}

// formatBytes formats bytes as a human-readable string.
func formatBytes(bytes uint64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
		TB = 1024 * GB
	)

	switch {
	case bytes >= TB:
		return fmt.Sprintf("%.1f TB", float64(bytes)/TB)
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d bytes", bytes)
	}
}
