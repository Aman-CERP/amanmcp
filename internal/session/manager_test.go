package session

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewManager_WithDefaults(t *testing.T) {
	// Given: a temp directory for sessions
	tmpDir := t.TempDir()
	cfg := ManagerConfig{
		StoragePath: tmpDir,
	}

	// When: creating a manager
	mgr, err := NewManager(cfg)

	// Then: manager is created with defaults
	require.NoError(t, err)
	assert.Equal(t, tmpDir, mgr.storagePath)
	assert.Equal(t, DefaultMaxSessions, mgr.maxSessions)
}

func TestNewManager_WithMaxSessions(t *testing.T) {
	// Given: config with custom max sessions
	tmpDir := t.TempDir()
	cfg := ManagerConfig{
		StoragePath: tmpDir,
		MaxSessions: 10,
	}

	// When: creating a manager
	mgr, err := NewManager(cfg)

	// Then: uses custom value
	require.NoError(t, err)
	assert.Equal(t, 10, mgr.maxSessions)
}

func TestNewManager_CreatesStorageDir(t *testing.T) {
	// Given: a non-existent storage path
	tmpDir := t.TempDir()
	storagePath := filepath.Join(tmpDir, "new", "sessions")
	cfg := ManagerConfig{
		StoragePath: storagePath,
	}

	// When: creating a manager
	_, err := NewManager(cfg)

	// Then: directory is created
	require.NoError(t, err)
	assert.DirExists(t, storagePath)
}

func TestManager_Open_NewSession(t *testing.T) {
	// Given: an empty session store
	tmpDir := t.TempDir()
	mgr, err := NewManager(ManagerConfig{StoragePath: tmpDir})
	require.NoError(t, err)

	// When: opening a new session
	projectPath := "/home/user/my-project"
	sess, err := mgr.Open("my-project", projectPath)

	// Then: new session is created
	require.NoError(t, err)
	assert.Equal(t, "my-project", sess.Name)
	assert.Equal(t, projectPath, sess.ProjectPath)
	assert.DirExists(t, sess.SessionDir)
	assert.FileExists(t, filepath.Join(sess.SessionDir, "session.json"))
}

func TestManager_Open_ExistingSession_SamePath(t *testing.T) {
	// Given: an existing session
	tmpDir := t.TempDir()
	mgr, err := NewManager(ManagerConfig{StoragePath: tmpDir})
	require.NoError(t, err)

	projectPath := "/home/user/existing-project"
	_, err = mgr.Open("existing", projectPath)
	require.NoError(t, err)

	// When: opening the same session again
	sess, err := mgr.Open("existing", projectPath)

	// Then: loads existing session
	require.NoError(t, err)
	assert.Equal(t, "existing", sess.Name)
	assert.Equal(t, projectPath, sess.ProjectPath)
}

func TestManager_Open_ExistingSession_DifferentPath(t *testing.T) {
	// Given: an existing session for path A
	tmpDir := t.TempDir()
	mgr, err := NewManager(ManagerConfig{StoragePath: tmpDir})
	require.NoError(t, err)

	_, err = mgr.Open("conflict", "/path/a")
	require.NoError(t, err)

	// When: opening with different path
	_, err = mgr.Open("conflict", "/path/b")

	// Then: returns error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists for")
}

func TestManager_Open_InvalidName(t *testing.T) {
	// Given: a manager
	tmpDir := t.TempDir()
	mgr, err := NewManager(ManagerConfig{StoragePath: tmpDir})
	require.NoError(t, err)

	// When: opening with invalid name
	_, err = mgr.Open("invalid/name", "/path")

	// Then: returns error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid session name")
}

func TestManager_Save_UpdatesTimestamp(t *testing.T) {
	// Given: a session
	tmpDir := t.TempDir()
	mgr, err := NewManager(ManagerConfig{StoragePath: tmpDir})
	require.NoError(t, err)

	sess, err := mgr.Open("save-test", "/path")
	require.NoError(t, err)
	oldLastUsed := sess.LastUsed

	time.Sleep(time.Millisecond)

	// When: saving the session
	err = mgr.Save(sess)
	require.NoError(t, err)

	// Then: LastUsed is updated
	loaded, err := mgr.Get("save-test")
	require.NoError(t, err)
	assert.True(t, loaded.LastUsed.After(oldLastUsed))
}

func TestManager_List_ReturnsAllSessions(t *testing.T) {
	// Given: multiple sessions
	tmpDir := t.TempDir()
	mgr, err := NewManager(ManagerConfig{StoragePath: tmpDir})
	require.NoError(t, err)

	_, err = mgr.Open("project-a", "/path/a")
	require.NoError(t, err)
	_, err = mgr.Open("project-b", "/path/b")
	require.NoError(t, err)
	_, err = mgr.Open("project-c", "/path/c")
	require.NoError(t, err)

	// When: listing sessions
	sessions, err := mgr.List()

	// Then: returns all 3
	require.NoError(t, err)
	assert.Len(t, sessions, 3)

	names := make(map[string]bool)
	for _, s := range sessions {
		names[s.Name] = true
	}
	assert.True(t, names["project-a"])
	assert.True(t, names["project-b"])
	assert.True(t, names["project-c"])
}

func TestManager_List_Empty(t *testing.T) {
	// Given: no sessions
	tmpDir := t.TempDir()
	mgr, err := NewManager(ManagerConfig{StoragePath: tmpDir})
	require.NoError(t, err)

	// When: listing sessions
	sessions, err := mgr.List()

	// Then: returns empty slice
	require.NoError(t, err)
	assert.Empty(t, sessions)
}

func TestManager_List_MarksInvalidSessions(t *testing.T) {
	// Given: a session with nonexistent project path
	tmpDir := t.TempDir()
	mgr, err := NewManager(ManagerConfig{StoragePath: tmpDir})
	require.NoError(t, err)

	// Create session pointing to nonexistent path
	_, err = mgr.Open("orphan", "/nonexistent/project/path")
	require.NoError(t, err)

	// When: listing sessions
	sessions, err := mgr.List()

	// Then: session is marked as invalid
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.False(t, sessions[0].Valid)
}

func TestManager_Get_Existing(t *testing.T) {
	// Given: an existing session
	tmpDir := t.TempDir()
	mgr, err := NewManager(ManagerConfig{StoragePath: tmpDir})
	require.NoError(t, err)

	_, err = mgr.Open("get-test", "/path")
	require.NoError(t, err)

	// When: getting the session
	sess, err := mgr.Get("get-test")

	// Then: returns the session
	require.NoError(t, err)
	assert.Equal(t, "get-test", sess.Name)
}

func TestManager_Get_NotFound(t *testing.T) {
	// Given: no sessions
	tmpDir := t.TempDir()
	mgr, err := NewManager(ManagerConfig{StoragePath: tmpDir})
	require.NoError(t, err)

	// When: getting nonexistent session
	_, err = mgr.Get("nonexistent")

	// Then: returns error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestManager_Delete_RemovesAllData(t *testing.T) {
	// Given: an existing session
	tmpDir := t.TempDir()
	mgr, err := NewManager(ManagerConfig{StoragePath: tmpDir})
	require.NoError(t, err)

	sess, err := mgr.Open("delete-me", "/path")
	require.NoError(t, err)
	sessionDir := sess.SessionDir

	// When: deleting the session
	err = mgr.Delete("delete-me")

	// Then: directory is removed
	require.NoError(t, err)
	assert.NoDirExists(t, sessionDir)
}

func TestManager_Delete_NotFound(t *testing.T) {
	// Given: no sessions
	tmpDir := t.TempDir()
	mgr, err := NewManager(ManagerConfig{StoragePath: tmpDir})
	require.NoError(t, err)

	// When: deleting nonexistent session
	err = mgr.Delete("nonexistent")

	// Then: returns error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestManager_Prune_RemovesOldSessions(t *testing.T) {
	// Given: sessions with varying ages
	tmpDir := t.TempDir()
	mgr, err := NewManager(ManagerConfig{StoragePath: tmpDir})
	require.NoError(t, err)

	// Create old session
	oldSess, err := mgr.Open("old-session", "/path/old")
	require.NoError(t, err)
	oldSess.LastUsed = time.Now().Add(-48 * time.Hour)
	require.NoError(t, SaveSession(oldSess))

	// Create new session
	_, err = mgr.Open("new-session", "/path/new")
	require.NoError(t, err)

	// When: pruning sessions older than 24h
	count, err := mgr.Prune(24 * time.Hour)

	// Then: only old session is removed
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	assert.False(t, mgr.Exists("old-session"))
	assert.True(t, mgr.Exists("new-session"))
}

func TestManager_Prune_NoOldSessions(t *testing.T) {
	// Given: only recent sessions
	tmpDir := t.TempDir()
	mgr, err := NewManager(ManagerConfig{StoragePath: tmpDir})
	require.NoError(t, err)

	_, err = mgr.Open("recent", "/path")
	require.NoError(t, err)

	// When: pruning
	count, err := mgr.Prune(24 * time.Hour)

	// Then: nothing removed
	require.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.True(t, mgr.Exists("recent"))
}

func TestManager_Exists_True(t *testing.T) {
	// Given: an existing session
	tmpDir := t.TempDir()
	mgr, err := NewManager(ManagerConfig{StoragePath: tmpDir})
	require.NoError(t, err)

	_, err = mgr.Open("exists-test", "/path")
	require.NoError(t, err)

	// When: checking existence
	exists := mgr.Exists("exists-test")

	// Then: returns true
	assert.True(t, exists)
}

func TestManager_Exists_False(t *testing.T) {
	// Given: no sessions
	tmpDir := t.TempDir()
	mgr, err := NewManager(ManagerConfig{StoragePath: tmpDir})
	require.NoError(t, err)

	// When: checking existence
	exists := mgr.Exists("nonexistent")

	// Then: returns false
	assert.False(t, exists)
}

func TestManager_Open_MaxSessionsExceeded(t *testing.T) {
	// Given: manager with max 2 sessions, already has 2
	tmpDir := t.TempDir()
	mgr, err := NewManager(ManagerConfig{
		StoragePath: tmpDir,
		MaxSessions: 2,
	})
	require.NoError(t, err)

	_, err = mgr.Open("session-1", "/path/1")
	require.NoError(t, err)
	_, err = mgr.Open("session-2", "/path/2")
	require.NoError(t, err)

	// When: trying to create a 3rd session
	_, err = mgr.Open("session-3", "/path/3")

	// Then: returns error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maximum")
}
