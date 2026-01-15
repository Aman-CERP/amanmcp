package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// ErrPIDFileNotFound is returned when the PID file doesn't exist.
var ErrPIDFileNotFound = errors.New("PID file not found")

// PIDFile manages a daemon process ID file.
type PIDFile struct {
	path string
}

// NewPIDFile creates a new PIDFile manager for the given path.
func NewPIDFile(path string) *PIDFile {
	return &PIDFile{path: path}
}

// Path returns the PID file path.
func (p *PIDFile) Path() string {
	return p.path
}

// Write writes the current process's PID to the file.
// Creates the directory if it doesn't exist.
func (p *PIDFile) Write() error {
	// Ensure directory exists
	dir := filepath.Dir(p.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create PID directory: %w", err)
	}

	pid := os.Getpid()
	data := []byte(strconv.Itoa(pid))

	if err := os.WriteFile(p.path, data, 0644); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	return nil
}

// Read reads the PID from the file.
func (p *PIDFile) Read() (int, error) {
	data, err := os.ReadFile(p.path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, ErrPIDFileNotFound
		}
		return 0, fmt.Errorf("failed to read PID file: %w", err)
	}

	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return 0, fmt.Errorf("invalid PID in file: %w", err)
	}

	return pid, nil
}

// Remove deletes the PID file.
// Returns nil if the file doesn't exist.
func (p *PIDFile) Remove() error {
	err := os.Remove(p.path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove PID file: %w", err)
	}
	return nil
}

// IsRunning checks if a process with the stored PID is running.
// Returns false if the PID file doesn't exist or the process isn't running.
func (p *PIDFile) IsRunning() bool {
	pid, err := p.Read()
	if err != nil {
		return false
	}

	return processExists(pid)
}

// Signal sends a signal to the process with the stored PID.
func (p *PIDFile) Signal(sig syscall.Signal) error {
	pid, err := p.Read()
	if err != nil {
		return fmt.Errorf("failed to read PID: %w", err)
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process %d: %w", pid, err)
	}

	if err := process.Signal(sig); err != nil {
		return fmt.Errorf("failed to signal process %d: %w", pid, err)
	}

	return nil
}

// processExists checks if a process with the given PID exists.
func processExists(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// On Unix, FindProcess always succeeds, so we need to send signal 0
	// to check if the process actually exists
	err = process.Signal(syscall.Signal(0))
	return err == nil
}
