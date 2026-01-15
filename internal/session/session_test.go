package session

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Aman-CERP/amanmcp/pkg/version"
)

func TestNewSession_CreatesWithDefaults(t *testing.T) {
	// Given: session parameters
	name := "test-session"
	projectPath := "/home/user/project"
	sessionDir := "/home/user/.amanmcp/sessions/test-session"

	// When: creating a new session
	before := time.Now()
	sess := NewSession(name, projectPath, sessionDir)
	after := time.Now()

	// Then: session is created with correct values
	require.NotNil(t, sess)
	assert.Equal(t, name, sess.Name)
	assert.Equal(t, projectPath, sess.ProjectPath)
	assert.Equal(t, sessionDir, sess.SessionDir)
	assert.Equal(t, version.Version, sess.Version)
	assert.True(t, sess.CreatedAt.After(before) || sess.CreatedAt.Equal(before))
	assert.True(t, sess.CreatedAt.Before(after) || sess.CreatedAt.Equal(after))
	assert.Equal(t, sess.CreatedAt, sess.LastUsed)
	assert.Equal(t, 0, sess.IndexStats.FileCount)
	assert.Equal(t, 0, sess.IndexStats.ChunkCount)
}

func TestSession_UpdateLastUsed(t *testing.T) {
	// Given: a session with old LastUsed
	sess := NewSession("test", "/path", "/sessions/test")
	oldLastUsed := sess.LastUsed

	// Wait a tiny bit to ensure time difference
	time.Sleep(time.Millisecond)

	// When: updating last used
	sess.UpdateLastUsed()

	// Then: LastUsed is updated
	assert.True(t, sess.LastUsed.After(oldLastUsed))
}

func TestSession_UpdateIndexStats(t *testing.T) {
	// Given: a new session
	sess := NewSession("test", "/path", "/sessions/test")

	// When: updating index stats
	before := time.Now()
	sess.UpdateIndexStats(100, 500)
	after := time.Now()

	// Then: stats are updated
	assert.Equal(t, 100, sess.IndexStats.FileCount)
	assert.Equal(t, 500, sess.IndexStats.ChunkCount)
	assert.True(t, sess.IndexStats.LastIndexed.After(before) || sess.IndexStats.LastIndexed.Equal(before))
	assert.True(t, sess.IndexStats.LastIndexed.Before(after) || sess.IndexStats.LastIndexed.Equal(after))
}

func TestSession_IsStale(t *testing.T) {
	tests := []struct {
		name     string
		lastUsed time.Time
		maxAge   time.Duration
		want     bool
	}{
		{
			name:     "recent session is not stale",
			lastUsed: time.Now().Add(-1 * time.Hour),
			maxAge:   24 * time.Hour,
			want:     false,
		},
		{
			name:     "old session is stale",
			lastUsed: time.Now().Add(-48 * time.Hour),
			maxAge:   24 * time.Hour,
			want:     true,
		},
		{
			name:     "session at boundary is stale",
			lastUsed: time.Now().Add(-25 * time.Hour),
			maxAge:   24 * time.Hour,
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sess := NewSession("test", "/path", "/sessions/test")
			sess.LastUsed = tt.lastUsed

			got := sess.IsStale(tt.maxAge)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSession_ToInfo(t *testing.T) {
	// Given: a session
	sess := NewSession("work-api", "/home/user/work/api", "/sessions/work-api")
	sess.LastUsed = time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	// When: converting to info
	info := sess.ToInfo(1024*1024, true)

	// Then: info has correct values
	assert.Equal(t, "work-api", info.Name)
	assert.Equal(t, "/home/user/work/api", info.ProjectPath)
	assert.Equal(t, sess.LastUsed, info.LastUsed)
	assert.Equal(t, int64(1024*1024), info.Size)
	assert.True(t, info.Valid)
}

func TestSession_ToInfo_InvalidProject(t *testing.T) {
	// Given: a session with deleted project
	sess := NewSession("old-project", "/nonexistent/path", "/sessions/old-project")

	// When: converting to info with valid=false
	info := sess.ToInfo(512, false)

	// Then: info marks as invalid
	assert.Equal(t, "old-project", info.Name)
	assert.False(t, info.Valid)
}
