package session

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// DefaultMaxSessions is the default maximum number of sessions.
const DefaultMaxSessions = 20

// ManagerConfig configures the session manager.
type ManagerConfig struct {
	// StoragePath is the directory where sessions are stored.
	// Defaults to ~/.amanmcp/sessions
	StoragePath string

	// MaxSessions is the maximum number of sessions allowed.
	// Defaults to DefaultMaxSessions (20).
	MaxSessions int
}

// Manager handles session lifecycle operations.
type Manager struct {
	storagePath string
	maxSessions int
}

// NewManager creates a new session manager.
// Creates the storage directory if it doesn't exist.
func NewManager(cfg ManagerConfig) (*Manager, error) {
	if cfg.StoragePath == "" {
		return nil, fmt.Errorf("storage path is required")
	}

	// Create storage directory
	if err := os.MkdirAll(cfg.StoragePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create session storage: %w", err)
	}

	maxSessions := cfg.MaxSessions
	if maxSessions <= 0 {
		maxSessions = DefaultMaxSessions
	}

	return &Manager{
		storagePath: cfg.StoragePath,
		maxSessions: maxSessions,
	}, nil
}

// Open creates a new session or loads an existing one.
// If session exists with a different project path, returns an error.
func (m *Manager) Open(name, projectPath string) (*Session, error) {
	// Validate session name
	if err := ValidateSessionName(name); err != nil {
		return nil, fmt.Errorf("invalid session name: %w", err)
	}

	sessionDir := filepath.Join(m.storagePath, name)

	// Check if session already exists
	if m.Exists(name) {
		sess, err := LoadSession(sessionDir)
		if err != nil {
			return nil, fmt.Errorf("failed to load existing session: %w", err)
		}

		// Verify project path matches
		if sess.ProjectPath != projectPath {
			return nil, fmt.Errorf("session '%s' already exists for %s (requested: %s)",
				name, sess.ProjectPath, projectPath)
		}

		// Update session directory (computed field)
		sess.SessionDir = sessionDir
		return sess, nil
	}

	// Check max sessions limit
	count, err := m.sessionCount()
	if err != nil {
		return nil, fmt.Errorf("failed to count sessions: %w", err)
	}
	if count >= m.maxSessions {
		return nil, fmt.Errorf("maximum %d sessions reached; delete old sessions first", m.maxSessions)
	}

	// Create new session
	sess := NewSession(name, projectPath, sessionDir)
	if err := SaveSession(sess); err != nil {
		return nil, fmt.Errorf("failed to save new session: %w", err)
	}

	return sess, nil
}

// Save persists a session to disk.
// Updates LastUsed timestamp automatically.
func (m *Manager) Save(sess *Session) error {
	sess.UpdateLastUsed()
	return SaveSession(sess)
}

// List returns all saved sessions with their status.
func (m *Manager) List() ([]*SessionInfo, error) {
	entries, err := os.ReadDir(m.storagePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []*SessionInfo{}, nil
		}
		return nil, fmt.Errorf("failed to read sessions directory: %w", err)
	}

	var sessions []*SessionInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		sessionDir := filepath.Join(m.storagePath, entry.Name())
		sess, err := LoadSession(sessionDir)
		if err != nil {
			// Skip invalid sessions
			continue
		}

		// Check if project path exists
		valid := true
		if _, err := os.Stat(sess.ProjectPath); os.IsNotExist(err) {
			valid = false
		}

		// Calculate size
		size, _ := CalculateDirSize(sessionDir)

		sessions = append(sessions, sess.ToInfo(size, valid))
	}

	return sessions, nil
}

// Get retrieves a session by name without modifying it.
func (m *Manager) Get(name string) (*Session, error) {
	if !m.Exists(name) {
		return nil, fmt.Errorf("session '%s' not found", name)
	}

	sessionDir := filepath.Join(m.storagePath, name)
	return LoadSession(sessionDir)
}

// Delete removes a session and all its data.
func (m *Manager) Delete(name string) error {
	if !m.Exists(name) {
		return fmt.Errorf("session '%s' not found", name)
	}

	sessionDir := filepath.Join(m.storagePath, name)
	if err := os.RemoveAll(sessionDir); err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	return nil
}

// Prune removes sessions older than the specified duration.
// Returns the count of deleted sessions.
func (m *Manager) Prune(olderThan time.Duration) (int, error) {
	sessions, err := m.List()
	if err != nil {
		return 0, err
	}

	deleted := 0
	for _, info := range sessions {
		if time.Since(info.LastUsed) > olderThan {
			if err := m.Delete(info.Name); err != nil {
				// Log but continue
				continue
			}
			deleted++
		}
	}

	return deleted, nil
}

// Exists checks if a session exists by name.
func (m *Manager) Exists(name string) bool {
	sessionDir := filepath.Join(m.storagePath, name)
	sessionFile := filepath.Join(sessionDir, sessionFileName)
	_, err := os.Stat(sessionFile)
	return err == nil
}

// sessionCount returns the number of existing sessions.
func (m *Manager) sessionCount() (int, error) {
	entries, err := os.ReadDir(m.storagePath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			sessionFile := filepath.Join(m.storagePath, entry.Name(), sessionFileName)
			if _, err := os.Stat(sessionFile); err == nil {
				count++
			}
		}
	}

	return count, nil
}

// SessionDir returns the directory path for a session.
func (m *Manager) SessionDir(name string) string {
	return filepath.Join(m.storagePath, name)
}
