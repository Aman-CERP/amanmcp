package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	// Should have sensible defaults
	assert.NotEmpty(t, cfg.SocketPath, "SocketPath should not be empty")
	assert.NotEmpty(t, cfg.PIDPath, "PIDPath should not be empty")
	assert.Greater(t, cfg.Timeout, time.Duration(0), "Timeout should be positive")
	assert.Greater(t, cfg.ShutdownGracePeriod, time.Duration(0), "ShutdownGracePeriod should be positive")
	assert.Greater(t, cfg.MaxProjects, 0, "MaxProjects should be positive")
}

func TestDefaultConfig_PathsInAmanMCPDir(t *testing.T) {
	cfg := DefaultConfig()

	// Both paths should be in ~/.amanmcp/
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	expectedDir := filepath.Join(home, ".amanmcp")
	assert.True(t, strings.HasPrefix(cfg.SocketPath, expectedDir),
		"SocketPath should be in ~/.amanmcp/")
	assert.True(t, strings.HasPrefix(cfg.PIDPath, expectedDir),
		"PIDPath should be in ~/.amanmcp/")
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid default config",
			config:  DefaultConfig(),
			wantErr: false,
		},
		{
			name: "empty socket path",
			config: Config{
				SocketPath:          "",
				PIDPath:             "/tmp/test.pid",
				Timeout:             30 * time.Second,
				ShutdownGracePeriod: 10 * time.Second,
				MaxProjects:         5,
			},
			wantErr: true,
			errMsg:  "socket path",
		},
		{
			name: "empty PID path",
			config: Config{
				SocketPath:          "/tmp/test.sock",
				PIDPath:             "",
				Timeout:             30 * time.Second,
				ShutdownGracePeriod: 10 * time.Second,
				MaxProjects:         5,
			},
			wantErr: true,
			errMsg:  "PID path",
		},
		{
			name: "zero timeout",
			config: Config{
				SocketPath:          "/tmp/test.sock",
				PIDPath:             "/tmp/test.pid",
				Timeout:             0,
				ShutdownGracePeriod: 10 * time.Second,
				MaxProjects:         5,
			},
			wantErr: true,
			errMsg:  "timeout",
		},
		{
			name: "zero max projects",
			config: Config{
				SocketPath:          "/tmp/test.sock",
				PIDPath:             "/tmp/test.pid",
				Timeout:             30 * time.Second,
				ShutdownGracePeriod: 10 * time.Second,
				MaxProjects:         0,
			},
			wantErr: true,
			errMsg:  "max projects",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestConfig_WithCustomPaths(t *testing.T) {
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "custom.sock")
	pidPath := filepath.Join(tmpDir, "custom.pid")

	cfg := Config{
		SocketPath:          socketPath,
		PIDPath:             pidPath,
		Timeout:             60 * time.Second,
		ShutdownGracePeriod: 5 * time.Second,
		MaxProjects:         10,
	}

	err := cfg.Validate()
	require.NoError(t, err)

	assert.Equal(t, socketPath, cfg.SocketPath)
	assert.Equal(t, pidPath, cfg.PIDPath)
	assert.Equal(t, 60*time.Second, cfg.Timeout)
	assert.Equal(t, 5*time.Second, cfg.ShutdownGracePeriod)
	assert.Equal(t, 10, cfg.MaxProjects)
}

func TestConfig_EnsureDir(t *testing.T) {
	tmpDir := t.TempDir()
	nestedDir := filepath.Join(tmpDir, "nested", "deeply")
	socketPath := filepath.Join(nestedDir, "daemon.sock")
	pidPath := filepath.Join(nestedDir, "daemon.pid")

	cfg := Config{
		SocketPath:          socketPath,
		PIDPath:             pidPath,
		Timeout:             30 * time.Second,
		ShutdownGracePeriod: 10 * time.Second,
		MaxProjects:         5,
	}

	// Directory should not exist yet
	_, err := os.Stat(nestedDir)
	require.True(t, os.IsNotExist(err))

	// EnsureDir should create the directory
	err = cfg.EnsureDir()
	require.NoError(t, err)

	// Directory should now exist
	info, err := os.Stat(nestedDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}
