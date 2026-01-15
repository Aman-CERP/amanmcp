package daemon

import (
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPIDFile_Write(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "test.pid")

	pf := NewPIDFile(pidPath)
	err := pf.Write()
	require.NoError(t, err)

	// File should exist
	_, err = os.Stat(pidPath)
	require.NoError(t, err)

	// Should contain current PID
	data, err := os.ReadFile(pidPath)
	require.NoError(t, err)

	pid, err := strconv.Atoi(string(data))
	require.NoError(t, err)
	assert.Equal(t, os.Getpid(), pid)
}

func TestPIDFile_Read(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "test.pid")

	// Write a PID
	expectedPID := 12345
	err := os.WriteFile(pidPath, []byte(strconv.Itoa(expectedPID)), 0644)
	require.NoError(t, err)

	pf := NewPIDFile(pidPath)
	pid, err := pf.Read()
	require.NoError(t, err)
	assert.Equal(t, expectedPID, pid)
}

func TestPIDFile_Read_NotExists(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "nonexistent.pid")

	pf := NewPIDFile(pidPath)
	_, err := pf.Read()
	require.Error(t, err)
	assert.True(t, os.IsNotExist(err) || err == ErrPIDFileNotFound)
}

func TestPIDFile_Read_InvalidContent(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "test.pid")

	// Write invalid content
	err := os.WriteFile(pidPath, []byte("not-a-number"), 0644)
	require.NoError(t, err)

	pf := NewPIDFile(pidPath)
	_, err = pf.Read()
	require.Error(t, err)
}

func TestPIDFile_Remove(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "test.pid")

	// Create PID file
	err := os.WriteFile(pidPath, []byte("12345"), 0644)
	require.NoError(t, err)

	pf := NewPIDFile(pidPath)
	err = pf.Remove()
	require.NoError(t, err)

	// File should not exist
	_, err = os.Stat(pidPath)
	assert.True(t, os.IsNotExist(err))
}

func TestPIDFile_Remove_NotExists(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "nonexistent.pid")

	pf := NewPIDFile(pidPath)
	err := pf.Remove()
	// Should not error if file doesn't exist
	require.NoError(t, err)
}

func TestPIDFile_IsRunning_CurrentProcess(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "test.pid")

	// Write current process PID
	err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0644)
	require.NoError(t, err)

	pf := NewPIDFile(pidPath)
	running := pf.IsRunning()
	assert.True(t, running, "Current process should be detected as running")
}

func TestPIDFile_IsRunning_NoFile(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "nonexistent.pid")

	pf := NewPIDFile(pidPath)
	running := pf.IsRunning()
	assert.False(t, running, "Should return false when PID file doesn't exist")
}

func TestPIDFile_IsRunning_StalePID(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "test.pid")

	// Write a PID that definitely doesn't exist (very high number)
	// PID 4194304 is higher than typical max PID on most systems
	stalePID := 4194304
	err := os.WriteFile(pidPath, []byte(strconv.Itoa(stalePID)), 0644)
	require.NoError(t, err)

	pf := NewPIDFile(pidPath)
	running := pf.IsRunning()
	assert.False(t, running, "Stale PID should be detected as not running")
}

func TestPIDFile_Signal(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "test.pid")

	// Write current process PID
	err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0644)
	require.NoError(t, err)

	pf := NewPIDFile(pidPath)

	// Signal 0 is a null signal - used to check if process exists
	err = pf.Signal(syscall.Signal(0))
	require.NoError(t, err, "Signal to current process should succeed")
}

func TestPIDFile_Signal_NoProcess(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "test.pid")

	// Write a stale PID
	err := os.WriteFile(pidPath, []byte("4194304"), 0644)
	require.NoError(t, err)

	pf := NewPIDFile(pidPath)
	err = pf.Signal(syscall.Signal(0))
	require.Error(t, err, "Signal to non-existent process should fail")
}

func TestPIDFile_WriteCreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	nestedPath := filepath.Join(tmpDir, "nested", "deep", "test.pid")

	pf := NewPIDFile(nestedPath)
	err := pf.Write()
	require.NoError(t, err)

	// File should exist
	_, err = os.Stat(nestedPath)
	require.NoError(t, err)
}
