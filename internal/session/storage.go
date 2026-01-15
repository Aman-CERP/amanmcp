package session

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

const (
	// sessionFileName is the metadata file name within each session directory.
	sessionFileName = "session.json"

	// maxSessionNameLength is the maximum allowed session name length.
	maxSessionNameLength = 64
)

// validSessionNamePattern matches alphanumeric, hyphen, and underscore.
var validSessionNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// ValidateSessionName validates a session name.
// Valid names contain only letters, numbers, hyphens, and underscores.
func ValidateSessionName(name string) error {
	if name == "" {
		return fmt.Errorf("session name cannot be empty")
	}
	if len(name) > maxSessionNameLength {
		return fmt.Errorf("session name too long (max %d chars)", maxSessionNameLength)
	}
	if !validSessionNamePattern.MatchString(name) {
		return fmt.Errorf("session name can only contain letters, numbers, hyphens, and underscores")
	}
	return nil
}

// SaveSession persists a session to disk.
// Creates the session directory if it doesn't exist.
// Uses atomic write (temp file + rename) for safety.
func SaveSession(sess *Session) error {
	// Create session directory
	if err := os.MkdirAll(sess.SessionDir, 0755); err != nil {
		return fmt.Errorf("failed to create session directory: %w", err)
	}

	// Marshal to JSON with indentation for readability
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	// Atomic write: write to temp file, then rename
	sessionPath := filepath.Join(sess.SessionDir, sessionFileName)
	tmpPath := sessionPath + ".tmp"

	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write session file: %w", err)
	}

	if err := os.Rename(tmpPath, sessionPath); err != nil {
		// Clean up temp file on failure
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to save session file: %w", err)
	}

	return nil
}

// LoadSession loads a session from disk.
// Returns an error if the session doesn't exist or is corrupted.
func LoadSession(sessionDir string) (*Session, error) {
	sessionPath := filepath.Join(sessionDir, sessionFileName)

	data, err := os.ReadFile(sessionPath)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("session.json not found in %s", sessionDir)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read session.json: %w", err)
	}

	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, fmt.Errorf("failed to parse session.json: %w", err)
	}

	// Set computed field
	sess.SessionDir = sessionDir

	return &sess, nil
}

// CalculateDirSize calculates the total size of all files in a directory.
// Returns 0 for nonexistent directories (graceful handling).
func CalculateDirSize(dir string) (int64, error) {
	var size int64

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// Skip inaccessible files
			return nil
		}
		if !d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return nil
			}
			size += info.Size()
		}
		return nil
	})

	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	return size, nil
}

// CopyIndexFiles copies index files from source to destination directory.
// Copies: metadata.db, vectors.hnsw, and BM25 index (bm25.db for SQLite, bm25.bleve for Bleve).
func CopyIndexFiles(srcDir, dstDir string) error {
	// Verify source exists
	if _, err := os.Stat(srcDir); os.IsNotExist(err) {
		return fmt.Errorf("source directory does not exist: %s", srcDir)
	}

	// Create destination directory
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Files to copy
	indexFiles := []string{
		"metadata.db",
		"vectors.hnsw",
		"vectors.hnsw.meta",
	}

	// Copy individual files
	for _, file := range indexFiles {
		src := filepath.Join(srcDir, file)
		dst := filepath.Join(dstDir, file)

		if _, err := os.Stat(src); os.IsNotExist(err) {
			// File doesn't exist, skip
			continue
		}

		if err := copyFile(src, dst); err != nil {
			return fmt.Errorf("failed to copy %s: %w", file, err)
		}
	}

	// Copy BM25 index - check SQLite first, then Bleve
	bm25SQLiteSrc := filepath.Join(srcDir, "bm25.db")
	if _, err := os.Stat(bm25SQLiteSrc); err == nil {
		// SQLite FTS5 backend
		if err := copyFile(bm25SQLiteSrc, filepath.Join(dstDir, "bm25.db")); err != nil {
			return fmt.Errorf("failed to copy bm25.db: %w", err)
		}
	} else {
		// Bleve backend (legacy)
		bm25BleveSrc := filepath.Join(srcDir, "bm25.bleve")
		if _, err := os.Stat(bm25BleveSrc); err == nil {
			if err := copyDir(bm25BleveSrc, filepath.Join(dstDir, "bm25.bleve")); err != nil {
				return fmt.Errorf("failed to copy bm25.bleve: %w", err)
			}
		}
	}

	return nil
}

// copyFile copies a single file from src to dst.
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source file: %w", err)
	}
	defer func() { _ = srcFile.Close() }()

	// Get source file info for permissions
	srcInfo, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("stat source file: %w", err)
	}

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return fmt.Errorf("create destination file: %w", err)
	}
	defer func() { _ = dstFile.Close() }()

	if _, err = io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("copy file contents: %w", err)
	}
	return nil
}

// copyDir recursively copies a directory from src to dst.
func copyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat source directory: %w", err)
	}

	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("read source directory: %w", err)
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err // Already wrapped by recursive call
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err // Already wrapped by copyFile
			}
		}
	}

	return nil
}
