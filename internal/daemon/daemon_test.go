package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockEmbedder is a simple embedder for daemon tests that doesn't require Ollama.
type mockEmbedder struct {
	dims int
}

func (m *mockEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return make([]float32, m.dims), nil
}

func (m *mockEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i := range texts {
		result[i] = make([]float32, m.dims)
	}
	return result, nil
}

func (m *mockEmbedder) Dimensions() int {
	return m.dims
}

func (m *mockEmbedder) ModelName() string {
	return "mock-embedder"
}

func (m *mockEmbedder) Available(_ context.Context) bool {
	return true
}

func (m *mockEmbedder) Close() error {
	return nil
}

func (m *mockEmbedder) SetBatchIndex(_ int) {}

func (m *mockEmbedder) SetFinalBatch(_ bool) {}

func newMockEmbedder() *mockEmbedder {
	return &mockEmbedder{dims: 768}
}

// daemonTestConfig creates a test configuration with unique paths.
func daemonTestConfig(t *testing.T) Config {
	t.Helper()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	socketPath := filepath.Join("/tmp", fmt.Sprintf("amanmcp-daemon-test-%s.sock", suffix))
	pidPath := filepath.Join("/tmp", fmt.Sprintf("amanmcp-daemon-test-%s.pid", suffix))

	t.Cleanup(func() {
		os.Remove(socketPath)
		os.Remove(pidPath)
	})

	return Config{
		SocketPath:          socketPath,
		PIDPath:             pidPath,
		Timeout:             5 * time.Second,
		ShutdownGracePeriod: 2 * time.Second,
		MaxProjects:         5,
	}
}

func TestNewDaemon(t *testing.T) {
	cfg := daemonTestConfig(t)

	d, err := NewDaemon(cfg)
	require.NoError(t, err)
	assert.NotNil(t, d)
}

func TestDaemon_StartStop(t *testing.T) {
	cfg := daemonTestConfig(t)

	// Use mock embedder to avoid Ollama dependency and speed up startup
	d, err := NewDaemon(cfg, WithEmbedder(newMockEmbedder()))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start daemon in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- d.Start(ctx)
	}()

	// Wait for startup (mock embedder is fast)
	time.Sleep(100 * time.Millisecond)

	// PID file should exist
	pf := NewPIDFile(cfg.PIDPath)
	assert.True(t, pf.IsRunning(), "daemon should be running")

	// Socket should exist
	_, err = os.Stat(cfg.SocketPath)
	require.NoError(t, err, "socket should exist")

	// Stop daemon
	cancel()

	select {
	case err := <-errCh:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not stop")
	}
}

func TestDaemon_ClientCanConnect(t *testing.T) {
	cfg := daemonTestConfig(t)

	// Use mock embedder to avoid Ollama dependency and speed up startup
	d, err := NewDaemon(cfg, WithEmbedder(newMockEmbedder()))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = d.Start(ctx)
	}()

	// Wait for startup (mock embedder is fast)
	time.Sleep(100 * time.Millisecond)

	// Client should be able to connect and ping
	client := NewClient(cfg)
	assert.True(t, client.IsRunning())

	err = client.Ping(ctx)
	require.NoError(t, err)
}

func TestDaemon_Status(t *testing.T) {
	cfg := daemonTestConfig(t)

	// Use mock embedder to avoid Ollama dependency and speed up startup
	d, err := NewDaemon(cfg, WithEmbedder(newMockEmbedder()))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = d.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	client := NewClient(cfg)
	status, err := client.Status(ctx)
	require.NoError(t, err)

	assert.True(t, status.Running)
	assert.Equal(t, os.Getpid(), status.PID)
	assert.NotEmpty(t, status.Uptime)
	assert.Equal(t, "mock-embedder", status.EmbedderType) // Now using mock
}

func TestDaemon_StaleSocketCleaned(t *testing.T) {
	cfg := daemonTestConfig(t)

	// Create a stale socket file
	err := os.WriteFile(cfg.SocketPath, []byte("stale"), 0644)
	require.NoError(t, err)

	// Use mock embedder to avoid Ollama dependency and speed up startup
	d, err := NewDaemon(cfg, WithEmbedder(newMockEmbedder()))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = d.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Daemon should start successfully despite stale socket
	client := NewClient(cfg)
	assert.True(t, client.IsRunning())
}

func TestDaemon_StalePIDCleaned(t *testing.T) {
	cfg := daemonTestConfig(t)

	// Create a stale PID file with non-existent process
	err := os.WriteFile(cfg.PIDPath, []byte("4194304"), 0644)
	require.NoError(t, err)

	// Use mock embedder to avoid Ollama dependency and speed up startup
	d, err := NewDaemon(cfg, WithEmbedder(newMockEmbedder()))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = d.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Daemon should start successfully despite stale PID
	pf := NewPIDFile(cfg.PIDPath)
	assert.True(t, pf.IsRunning())

	// PID should be current process
	pid, err := pf.Read()
	require.NoError(t, err)
	assert.Equal(t, os.Getpid(), pid)
}

// ============================================================================
// DEBT-028: Additional Daemon Tests for Coverage
// ============================================================================

func TestNewDaemon_InvalidConfig(t *testing.T) {
	// Given: invalid config (empty socket path)
	cfg := Config{
		SocketPath: "",
		PIDPath:    "/tmp/test.pid",
		Timeout:    5 * time.Second,
	}

	// When: creating daemon
	_, err := NewDaemon(cfg)

	// Then: should fail validation
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid config")
}

func TestNewDaemon_WithEmbedder(t *testing.T) {
	// Given: config with custom embedder
	cfg := daemonTestConfig(t)
	customEmbedder := &mockEmbedder{dims: 384}

	// When: creating daemon with custom embedder
	d, err := NewDaemon(cfg, WithEmbedder(customEmbedder))

	// Then: should use custom embedder
	require.NoError(t, err)
	assert.Equal(t, customEmbedder, d.embedder)
	assert.Equal(t, 384, d.embedder.Dimensions())
}

func TestDaemon_HandleSearch_NoIndex(t *testing.T) {
	cfg := daemonTestConfig(t)

	// Use mock embedder
	d, err := NewDaemon(cfg, WithEmbedder(newMockEmbedder()))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = d.Start(ctx)
	}()
	time.Sleep(100 * time.Millisecond)

	// Search on a path with no index
	tmpDir := t.TempDir()
	params := SearchParams{
		Query:    "test query",
		RootPath: tmpDir,
		Limit:    10,
	}

	_, err = d.HandleSearch(ctx, params)

	// Should fail with "no index found" error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no index found")
}

func TestDaemon_GetStatus_NoEmbedder(t *testing.T) {
	cfg := daemonTestConfig(t)

	// Create daemon without embedder (will be nil)
	d, err := NewDaemon(cfg)
	require.NoError(t, err)

	// Don't start daemon, just check status
	d.started = time.Now()

	// When: getting status without embedder
	status := d.GetStatus()

	// Then: should report unavailable
	assert.True(t, status.Running)
	assert.Equal(t, "unavailable", status.EmbedderType)
	assert.Equal(t, "unavailable", status.EmbedderStatus)
}

func TestDaemon_GetStatus_WithEmbedder(t *testing.T) {
	cfg := daemonTestConfig(t)

	d, err := NewDaemon(cfg, WithEmbedder(newMockEmbedder()))
	require.NoError(t, err)

	d.started = time.Now()

	// When: getting status with embedder
	status := d.GetStatus()

	// Then: should report embedder info
	assert.Equal(t, "mock-embedder", status.EmbedderType)
	assert.Equal(t, "ready", status.EmbedderStatus)
	assert.Equal(t, 0, status.ProjectsLoaded)
}

func TestProjectState_Close(t *testing.T) {
	// Given: a project state with mock stores
	state := &projectState{
		rootPath: "/test/path",
		loadedAt: time.Now(),
		lastUsed: time.Now(),
	}

	// When: closing with nil stores (edge case)
	err := state.Close()

	// Then: should succeed without error
	assert.NoError(t, err)
}

func TestDaemon_EvictLRU_MultipleProjects(t *testing.T) {
	cfg := daemonTestConfig(t)
	cfg.MaxProjects = 2

	d, err := NewDaemon(cfg, WithEmbedder(newMockEmbedder()))
	require.NoError(t, err)

	// Add three mock projects directly to test eviction
	d.projects = map[string]*projectState{
		"/project1": {
			rootPath: "/project1",
			lastUsed: time.Now().Add(-3 * time.Hour), // oldest
		},
		"/project2": {
			rootPath: "/project2",
			lastUsed: time.Now().Add(-1 * time.Hour), // newest
		},
	}

	// When: evicting LRU
	d.evictLRU()

	// Then: should evict oldest project
	assert.Len(t, d.projects, 1)
	assert.Nil(t, d.projects["/project1"], "oldest project should be evicted")
	assert.NotNil(t, d.projects["/project2"], "newest project should remain")
}

func TestDaemon_EvictLRU_EmptyProjects(t *testing.T) {
	cfg := daemonTestConfig(t)

	d, err := NewDaemon(cfg, WithEmbedder(newMockEmbedder()))
	require.NoError(t, err)

	// Empty projects map
	d.projects = map[string]*projectState{}

	// When: evicting with no projects (edge case)
	d.evictLRU()

	// Then: should not panic
	assert.Empty(t, d.projects)
}

func TestDaemon_Cleanup(t *testing.T) {
	cfg := daemonTestConfig(t)

	mockEmb := newMockEmbedder()
	d, err := NewDaemon(cfg, WithEmbedder(mockEmb))
	require.NoError(t, err)

	// Add a project with nil stores (to test nil handling)
	d.projects = map[string]*projectState{
		"/test": {
			rootPath: "/test",
			lastUsed: time.Now(),
		},
	}

	// When: running cleanup
	d.cleanup()

	// Then: projects should be cleared and embedder should be nil
	assert.Empty(t, d.projects)
	assert.Nil(t, d.embedder)
}
