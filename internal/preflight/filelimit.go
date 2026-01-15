package preflight

import (
	"fmt"
	"syscall"
)

// MinFileDescriptors is the minimum required file descriptor limit.
const MinFileDescriptors = 1024

// CheckFileDescriptors checks if the file descriptor limit is sufficient.
func (c *Checker) CheckFileDescriptors() CheckResult {
	result := CheckResult{
		Name:     "file_descriptors",
		Required: true,
	}

	var rLimit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit); err != nil {
		result.Status = StatusFail
		result.Message = fmt.Sprintf("failed to check file descriptor limit: %v", err)
		return result
	}

	currentLimit := rLimit.Cur

	if currentLimit < MinFileDescriptors {
		result.Status = StatusFail
		result.Message = fmt.Sprintf("%d (minimum: %d)", currentLimit, MinFileDescriptors)
		result.Details = "Run 'ulimit -n 10240' to increase the limit"
		return result
	}

	result.Status = StatusPass
	result.Message = fmt.Sprintf("%d (minimum: %d)", currentLimit, MinFileDescriptors)
	return result
}
