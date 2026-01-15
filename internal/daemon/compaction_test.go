package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCompactionManager(t *testing.T) {
	cfg := config.CompactionConfig{
		Enabled:         true,
		OrphanThreshold: 0.2,
		MinOrphanCount:  100,
		IdleTimeout:     "30s",
		Cooldown:        "1h",
	}

	m := NewCompactionManager(nil, cfg)
	require.NotNil(t, m)
	assert.Equal(t, cfg.Enabled, m.config.Enabled)
	assert.Equal(t, cfg.OrphanThreshold, m.config.OrphanThreshold)
	assert.Equal(t, cfg.MinOrphanCount, m.config.MinOrphanCount)
}

func TestCompactionManager_StartStop(t *testing.T) {
	cfg := config.CompactionConfig{
		Enabled:         true,
		OrphanThreshold: 0.2,
		MinOrphanCount:  100,
		IdleTimeout:     "30s",
		Cooldown:        "1h",
	}

	m := NewCompactionManager(nil, cfg)
	ctx := context.Background()

	// Start should not panic
	m.Start(ctx)

	// Stop should not panic and should be idempotent
	m.Stop()
	m.Stop() // Second stop should be safe
}

func TestCompactionManager_DisabledSkipsOperations(t *testing.T) {
	cfg := config.CompactionConfig{
		Enabled:         false,
		OrphanThreshold: 0.2,
		MinOrphanCount:  100,
		IdleTimeout:     "30s",
		Cooldown:        "1h",
	}

	m := NewCompactionManager(nil, cfg)
	ctx := context.Background()
	m.Start(ctx)
	defer m.Stop()

	// These should not panic when disabled
	m.OnSearchComplete("/test/path")
	m.InterruptCompaction("/test/path")
}

func TestCompactionManager_OnSearchComplete_CreatesProjectState(t *testing.T) {
	cfg := config.CompactionConfig{
		Enabled:         true,
		OrphanThreshold: 0.2,
		MinOrphanCount:  100,
		IdleTimeout:     "1h", // Long timeout to prevent immediate trigger
		Cooldown:        "1h",
	}

	m := NewCompactionManager(nil, cfg)
	ctx := context.Background()
	m.Start(ctx)
	defer m.Stop()

	rootPath := "/test/project"
	m.OnSearchComplete(rootPath)

	m.mu.Lock()
	defer m.mu.Unlock()

	state, ok := m.projects[rootPath]
	require.True(t, ok, "project state should be created")
	assert.Equal(t, rootPath, state.rootPath)
	assert.False(t, state.lastSearch.IsZero(), "lastSearch should be set")
}

func TestCompactionManager_InterruptCompaction_NoOpWhenNotCompacting(t *testing.T) {
	cfg := config.CompactionConfig{
		Enabled:         true,
		OrphanThreshold: 0.2,
		MinOrphanCount:  100,
		IdleTimeout:     "30s",
		Cooldown:        "1h",
	}

	m := NewCompactionManager(nil, cfg)
	ctx := context.Background()
	m.Start(ctx)
	defer m.Stop()

	// Should not panic when project doesn't exist
	m.InterruptCompaction("/nonexistent/project")

	// Create project state
	rootPath := "/test/project"
	m.OnSearchComplete(rootPath)

	// Should not panic when not compacting
	m.InterruptCompaction(rootPath)
}

func TestCompactionManager_ShouldCompact_ReturnsFalseWhenDisabled(t *testing.T) {
	cfg := config.CompactionConfig{
		Enabled:         false,
		OrphanThreshold: 0.2,
		MinOrphanCount:  100,
		IdleTimeout:     "30s",
		Cooldown:        "1h",
	}

	m := NewCompactionManager(nil, cfg)
	ctx := context.Background()
	m.Start(ctx)
	defer m.Stop()

	assert.False(t, m.shouldCompact("/test/project"))
}

func TestCompactionManager_ShouldCompact_ReturnsFalseWhenNoProjectState(t *testing.T) {
	cfg := config.CompactionConfig{
		Enabled:         true,
		OrphanThreshold: 0.2,
		MinOrphanCount:  100,
		IdleTimeout:     "30s",
		Cooldown:        "1h",
	}

	m := NewCompactionManager(nil, cfg)
	ctx := context.Background()
	m.Start(ctx)
	defer m.Stop()

	// No project state exists
	assert.False(t, m.shouldCompact("/nonexistent/project"))
}

func TestCompactionManager_ShouldCompact_ReturnsFalseWhenCooldownActive(t *testing.T) {
	cfg := config.CompactionConfig{
		Enabled:         true,
		OrphanThreshold: 0.2,
		MinOrphanCount:  100,
		IdleTimeout:     "30s",
		Cooldown:        "1h",
	}

	m := NewCompactionManager(nil, cfg)
	ctx := context.Background()
	m.Start(ctx)
	defer m.Stop()

	rootPath := "/test/project"
	m.OnSearchComplete(rootPath)

	// Simulate recent compaction
	m.mu.Lock()
	m.projects[rootPath].lastCompact = time.Now()
	m.mu.Unlock()

	// Should return false due to cooldown
	assert.False(t, m.shouldCompact(rootPath))
}

func TestCompactionManager_ShouldCompact_ReturnsFalseWhenAlreadyCompacting(t *testing.T) {
	cfg := config.CompactionConfig{
		Enabled:         true,
		OrphanThreshold: 0.2,
		MinOrphanCount:  100,
		IdleTimeout:     "30s",
		Cooldown:        "1h",
	}

	m := NewCompactionManager(nil, cfg)
	ctx := context.Background()
	m.Start(ctx)
	defer m.Stop()

	rootPath := "/test/project"
	m.OnSearchComplete(rootPath)

	// Simulate compaction in progress
	m.mu.Lock()
	m.projects[rootPath].compacting = true
	m.mu.Unlock()

	// Should return false because already compacting
	assert.False(t, m.shouldCompact(rootPath))
}

func TestCompactionConfig_Defaults(t *testing.T) {
	cfg := config.NewConfig()

	assert.True(t, cfg.Compaction.Enabled)
	assert.Equal(t, 0.2, cfg.Compaction.OrphanThreshold)
	assert.Equal(t, 100, cfg.Compaction.MinOrphanCount)
	assert.Equal(t, "30s", cfg.Compaction.IdleTimeout)
	assert.Equal(t, "1h", cfg.Compaction.Cooldown)
}
