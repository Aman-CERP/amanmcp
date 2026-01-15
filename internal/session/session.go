// Package session provides session management for AmanMCP.
// Sessions allow users to persist and switch between different project indices
// without restarting the server.
package session

import (
	"time"

	"github.com/Aman-CERP/amanmcp/pkg/version"
)

// Session represents a named session with associated project index data.
type Session struct {
	// Name is the user-provided session identifier.
	Name string `json:"name"`

	// ProjectPath is the absolute path to the project root.
	ProjectPath string `json:"project_path"`

	// CreatedAt is when the session was first created.
	CreatedAt time.Time `json:"created_at"`

	// LastUsed is when the session was last accessed.
	LastUsed time.Time `json:"last_used"`

	// Version is the AmanMCP version that created this session.
	Version string `json:"version"`

	// IndexStats contains statistics about the indexed content.
	IndexStats IndexStats `json:"index_stats"`

	// SessionDir is the directory where session data is stored.
	// This is computed, not persisted.
	SessionDir string `json:"-"`
}

// IndexStats contains statistics about the indexed content in a session.
type IndexStats struct {
	// FileCount is the number of files indexed.
	FileCount int `json:"file_count"`

	// ChunkCount is the number of chunks created from files.
	ChunkCount int `json:"chunk_count"`

	// LastIndexed is when the index was last updated.
	LastIndexed time.Time `json:"last_indexed"`
}

// SessionInfo provides summary information about a session for listing.
type SessionInfo struct {
	// Name is the session identifier.
	Name string

	// ProjectPath is the absolute path to the project root.
	ProjectPath string

	// LastUsed is when the session was last accessed.
	LastUsed time.Time

	// Size is the total storage size in bytes.
	Size int64

	// Valid indicates if the project path still exists.
	Valid bool
}

// NewSession creates a new session with the given name and project path.
func NewSession(name, projectPath, sessionDir string) *Session {
	now := time.Now()
	return &Session{
		Name:        name,
		ProjectPath: projectPath,
		CreatedAt:   now,
		LastUsed:    now,
		Version:     version.Version,
		IndexStats:  IndexStats{},
		SessionDir:  sessionDir,
	}
}

// UpdateLastUsed updates the LastUsed timestamp to now.
func (s *Session) UpdateLastUsed() {
	s.LastUsed = time.Now()
}

// UpdateIndexStats updates the index statistics.
func (s *Session) UpdateIndexStats(fileCount, chunkCount int) {
	s.IndexStats.FileCount = fileCount
	s.IndexStats.ChunkCount = chunkCount
	s.IndexStats.LastIndexed = time.Now()
}

// IsStale returns true if the session hasn't been used within the given duration.
func (s *Session) IsStale(maxAge time.Duration) bool {
	return time.Since(s.LastUsed) > maxAge
}

// ToInfo converts a Session to SessionInfo for listing.
func (s *Session) ToInfo(size int64, valid bool) *SessionInfo {
	return &SessionInfo{
		Name:        s.Name,
		ProjectPath: s.ProjectPath,
		LastUsed:    s.LastUsed,
		Size:        size,
		Valid:       valid,
	}
}
