package store

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3" // SQLite driver
)

// SQLiteStore implements MetadataStore using SQLite.
type SQLiteStore struct {
	db *sql.DB
}

// StoreConfig configures the SQLite metadata store.
type StoreConfig struct {
	// CacheSizeMB is the SQLite cache size in megabytes.
	// Default is 64MB. Set to 0 to use default.
	CacheSizeMB int
}

// DefaultStoreConfig returns sensible defaults for the metadata store.
func DefaultStoreConfig() StoreConfig {
	return StoreConfig{
		CacheSizeMB: 64, // 64MB default cache
	}
}

// NewSQLiteStore creates a new SQLite-based metadata store with default configuration.
// It creates the database file and directory if they don't exist,
// and initializes the schema automatically.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	return NewSQLiteStoreWithConfig(dbPath, DefaultStoreConfig())
}

// NewSQLiteStoreWithConfig creates a new SQLite-based metadata store with custom configuration.
// It creates the database file and directory if they don't exist,
// and initializes the schema automatically.
func NewSQLiteStoreWithConfig(dbPath string, cfg StoreConfig) (*SQLiteStore, error) {
	// Create directory if it doesn't exist
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Open database with WAL mode and other pragmas
	// Note: _busy_timeout in DSN may be ignored by mattn/go-sqlite3, so we set it via PRAGMA below
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_synchronous=NORMAL&_foreign_keys=ON")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool for SQLite WAL mode
	// Single writer prevents lock contention between concurrent processes
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0) // Don't expire connections

	// Determine cache size (use default if not specified)
	cacheSizeMB := cfg.CacheSizeMB
	if cacheSizeMB <= 0 {
		cacheSizeMB = 64 // Default 64MB
	}
	// SQLite cache_size is in KB when negative (page count when positive)
	// -N means N kilobytes
	cacheSizeKB := cacheSizeMB * 1024

	// Set additional pragmas
	// CRITICAL: busy_timeout MUST be set via PRAGMA, not DSN (DSN syntax may be ignored)
	pragmas := []string{
		"PRAGMA busy_timeout = 5000", // 5 second timeout for lock contention
		fmt.Sprintf("PRAGMA cache_size=-%d", cacheSizeKB), // Negative = KB
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("failed to set pragma: %w", err)
		}
	}

	// BUG-057: Check database integrity on startup (warn but don't fail)
	// This helps detect corruption early rather than experiencing unpredictable failures
	var integrityResult string
	if err := db.QueryRow("PRAGMA integrity_check").Scan(&integrityResult); err != nil {
		slog.Warn("sqlite_integrity_check_failed", slog.String("error", err.Error()))
	} else if integrityResult != "ok" {
		slog.Error("sqlite_corruption_detected",
			slog.String("result", integrityResult),
			slog.String("db_path", dbPath),
			slog.String("action", "recommend running 'amanmcp index --force' to rebuild"))
	}

	store := &SQLiteStore{db: db}

	// Initialize schema
	if err := store.initSchema(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return store, nil
}

// initSchema creates all required tables if they don't exist.
func (s *SQLiteStore) initSchema() error {
	schema := `
	-- Schema version for migrations
	CREATE TABLE IF NOT EXISTS schema_version (
		version INTEGER PRIMARY KEY,
		applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- Project information
	CREATE TABLE IF NOT EXISTS projects (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		root_path TEXT NOT NULL,
		project_type TEXT,
		indexed_at TIMESTAMP,
		chunk_count INTEGER DEFAULT 0,
		file_count INTEGER DEFAULT 0,
		schema_version TEXT
	);

	-- File tracking
	CREATE TABLE IF NOT EXISTS files (
		id TEXT PRIMARY KEY,
		project_id TEXT NOT NULL,
		path TEXT NOT NULL,
		size INTEGER,
		mod_time TIMESTAMP,
		content_hash TEXT,
		language TEXT,
		content_type TEXT,
		indexed_at TIMESTAMP,
		FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_files_project ON files(project_id);
	CREATE INDEX IF NOT EXISTS idx_files_path ON files(project_id, path);
	CREATE INDEX IF NOT EXISTS idx_files_mod_time ON files(project_id, mod_time);

	-- Chunk metadata
	CREATE TABLE IF NOT EXISTS chunks (
		id TEXT PRIMARY KEY,
		file_id TEXT NOT NULL,
		file_path TEXT NOT NULL,
		content TEXT NOT NULL,
		raw_content TEXT,
		context TEXT,
		content_type TEXT,
		language TEXT,
		start_line INTEGER NOT NULL,
		end_line INTEGER NOT NULL,
		metadata TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (file_id) REFERENCES files(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_chunks_file ON chunks(file_id);

	-- Symbols in chunks
	CREATE TABLE IF NOT EXISTS symbols (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		chunk_id TEXT NOT NULL,
		name TEXT NOT NULL,
		type TEXT NOT NULL,
		start_line INTEGER,
		end_line INTEGER,
		signature TEXT,
		doc_comment TEXT,
		FOREIGN KEY (chunk_id) REFERENCES chunks(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_symbols_chunk ON symbols(chunk_id);
	CREATE INDEX IF NOT EXISTS idx_symbols_name ON symbols(name);

	-- Key-value store for misc state
	CREATE TABLE IF NOT EXISTS state (
		key TEXT PRIMARY KEY,
		value TEXT,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- Insert schema version if not exists
	INSERT OR IGNORE INTO schema_version (version) VALUES (1);
	`

	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("execute database schema: %w", err)
	}

	// Run migrations
	if err := s.runMigrations(); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	return nil
}

// runMigrations applies schema migrations based on current version.
func (s *SQLiteStore) runMigrations() error {
	// Get current schema version
	var version int
	err := s.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&version)
	if err != nil {
		return fmt.Errorf("get schema version: %w", err)
	}

	// Migration 2: Add embedding columns to chunks table
	if version < 2 {
		slog.Info("applying migration 2: add embedding columns to chunks")
		// SQLite doesn't support multiple ALTER TABLE in one statement
		stmts := []string{
			"ALTER TABLE chunks ADD COLUMN embedding BLOB",
			"ALTER TABLE chunks ADD COLUMN embedding_model TEXT",
			"ALTER TABLE chunks ADD COLUMN embedding_dims INTEGER",
			"INSERT INTO schema_version (version) VALUES (2)",
		}
		for _, stmt := range stmts {
			if _, err := s.db.Exec(stmt); err != nil {
				// Ignore "duplicate column name" errors (column already exists)
				if !strings.Contains(err.Error(), "duplicate column name") {
					return fmt.Errorf("migration 2 failed: %w", err)
				}
			}
		}
		slog.Info("migration 2 complete: embedding columns added")
	}

	// Migration 3: Add telemetry tables for query pattern tracking (AI-6)
	if version < 3 {
		slog.Info("applying migration 3: add telemetry tables")
		stmts := []string{
			// Query type frequency (aggregated daily)
			`CREATE TABLE IF NOT EXISTS query_type_stats (
				date TEXT NOT NULL,
				query_type TEXT NOT NULL,
				count INTEGER NOT NULL DEFAULT 0,
				PRIMARY KEY (date, query_type)
			)`,
			// Top query terms (with frequency count)
			`CREATE TABLE IF NOT EXISTS query_terms (
				term TEXT PRIMARY KEY,
				count INTEGER NOT NULL DEFAULT 1,
				last_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP
			)`,
			`CREATE INDEX IF NOT EXISTS idx_query_terms_count ON query_terms(count DESC)`,
			// Zero-result queries (circular buffer)
			`CREATE TABLE IF NOT EXISTS zero_result_queries (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				query TEXT NOT NULL,
				timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP
			)`,
			// Latency histogram
			`CREATE TABLE IF NOT EXISTS query_latency_stats (
				date TEXT NOT NULL,
				bucket TEXT NOT NULL,
				count INTEGER NOT NULL DEFAULT 0,
				PRIMARY KEY (date, bucket)
			)`,
			"INSERT INTO schema_version (version) VALUES (3)",
		}
		for _, stmt := range stmts {
			if _, err := s.db.Exec(stmt); err != nil {
				// Ignore "table already exists" errors
				if !strings.Contains(err.Error(), "already exists") {
					return fmt.Errorf("migration 3 failed: %w", err)
				}
			}
		}
		slog.Info("migration 3 complete: telemetry tables added")
	}

	return nil
}

// Close closes the database connection.
func (s *SQLiteStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// DB returns the underlying database connection.
// This is used by the telemetry package to share the connection.
func (s *SQLiteStore) DB() *sql.DB {
	return s.db
}

// SaveProject saves or updates a project.
func (s *SQLiteStore) SaveProject(ctx context.Context, project *Project) error {
	query := `
		INSERT INTO projects (id, name, root_path, project_type, indexed_at, chunk_count, file_count, schema_version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			root_path = excluded.root_path,
			project_type = excluded.project_type,
			indexed_at = excluded.indexed_at,
			chunk_count = excluded.chunk_count,
			file_count = excluded.file_count,
			schema_version = excluded.schema_version
	`
	_, err := s.db.ExecContext(ctx, query,
		project.ID, project.Name, project.RootPath, project.ProjectType,
		project.IndexedAt, project.ChunkCount, project.FileCount, project.Version)
	if err != nil {
		return fmt.Errorf("failed to save project: %w", err)
	}
	return nil
}

// GetProject retrieves a project by ID.
func (s *SQLiteStore) GetProject(ctx context.Context, id string) (*Project, error) {
	query := `
		SELECT id, name, root_path, project_type, indexed_at, chunk_count, file_count, schema_version
		FROM projects WHERE id = ?
	`
	row := s.db.QueryRowContext(ctx, query, id)

	var p Project
	var indexedAt sql.NullTime
	var projectType, schemaVersion sql.NullString

	err := row.Scan(&p.ID, &p.Name, &p.RootPath, &projectType, &indexedAt, &p.ChunkCount, &p.FileCount, &schemaVersion)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get project: %w", err)
	}

	if indexedAt.Valid {
		p.IndexedAt = indexedAt.Time
	}
	if projectType.Valid {
		p.ProjectType = projectType.String
	}
	if schemaVersion.Valid {
		p.Version = schemaVersion.String
	}

	return &p, nil
}

// UpdateProjectStats updates the file and chunk counts for a project.
func (s *SQLiteStore) UpdateProjectStats(ctx context.Context, id string, fileCount, chunkCount int) error {
	query := `UPDATE projects SET file_count = ?, chunk_count = ?, indexed_at = ? WHERE id = ?`
	_, err := s.db.ExecContext(ctx, query, fileCount, chunkCount, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to update project stats: %w", err)
	}
	return nil
}

// RefreshProjectStats recalculates file/chunk counts from the database and updates indexed_at.
// This is used by the coordinator after incremental indexing to keep stats accurate.
func (s *SQLiteStore) RefreshProjectStats(ctx context.Context, id string) error {
	// Count files for this project
	var fileCount int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM files WHERE project_id = ?`, id).Scan(&fileCount)
	if err != nil {
		return fmt.Errorf("failed to count files: %w", err)
	}

	// Count chunks for this project
	var chunkCount int
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM chunks WHERE file_id IN (SELECT id FROM files WHERE project_id = ?)`, id).Scan(&chunkCount)
	if err != nil {
		return fmt.Errorf("failed to count chunks: %w", err)
	}

	// Update project stats with fresh counts
	return s.UpdateProjectStats(ctx, id, fileCount, chunkCount)
}

// SaveFiles saves or updates multiple files in a single transaction.
func (s *SQLiteStore) SaveFiles(ctx context.Context, files []*File) error {
	if len(files) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO files (id, project_id, path, size, mod_time, content_hash, language, content_type, indexed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			project_id = excluded.project_id,
			path = excluded.path,
			size = excluded.size,
			mod_time = excluded.mod_time,
			content_hash = excluded.content_hash,
			language = excluded.language,
			content_type = excluded.content_type,
			indexed_at = excluded.indexed_at
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, f := range files {
		_, err := stmt.ExecContext(ctx, f.ID, f.ProjectID, f.Path, f.Size, f.ModTime, f.ContentHash, f.Language, f.ContentType, f.IndexedAt)
		if err != nil {
			return fmt.Errorf("failed to save file %s: %w", f.Path, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// GetFileByPath retrieves a file by its path within a project.
func (s *SQLiteStore) GetFileByPath(ctx context.Context, projectID, path string) (*File, error) {
	query := `
		SELECT id, project_id, path, size, mod_time, content_hash, language, content_type, indexed_at
		FROM files WHERE project_id = ? AND path = ?
	`
	row := s.db.QueryRowContext(ctx, query, projectID, path)

	var f File
	var modTime, indexedAt sql.NullTime
	var contentHash, language, contentType sql.NullString

	err := row.Scan(&f.ID, &f.ProjectID, &f.Path, &f.Size, &modTime, &contentHash, &language, &contentType, &indexedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get file by path: %w", err)
	}

	if modTime.Valid {
		f.ModTime = modTime.Time
	}
	if indexedAt.Valid {
		f.IndexedAt = indexedAt.Time
	}
	if contentHash.Valid {
		f.ContentHash = contentHash.String
	}
	if language.Valid {
		f.Language = language.String
	}
	if contentType.Valid {
		f.ContentType = contentType.String
	}

	return &f, nil
}

// GetChangedFiles returns files modified since the given timestamp.
func (s *SQLiteStore) GetChangedFiles(ctx context.Context, projectID string, since time.Time) ([]*File, error) {
	query := `
		SELECT id, project_id, path, size, mod_time, content_hash, language, content_type, indexed_at
		FROM files WHERE project_id = ? AND mod_time > ?
		ORDER BY mod_time ASC
	`
	rows, err := s.db.QueryContext(ctx, query, projectID, since)
	if err != nil {
		return nil, fmt.Errorf("failed to query changed files: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var files []*File
	for rows.Next() {
		var f File
		var modTime, indexedAt sql.NullTime
		var contentHash, language, contentType sql.NullString

		err := rows.Scan(&f.ID, &f.ProjectID, &f.Path, &f.Size, &modTime, &contentHash, &language, &contentType, &indexedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan file row: %w", err)
		}

		if modTime.Valid {
			f.ModTime = modTime.Time
		}
		if indexedAt.Valid {
			f.IndexedAt = indexedAt.Time
		}
		if contentHash.Valid {
			f.ContentHash = contentHash.String
		}
		if language.Valid {
			f.Language = language.String
		}
		if contentType.Valid {
			f.ContentType = contentType.String
		}

		files = append(files, &f)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating files: %w", err)
	}

	return files, nil
}

// ListFiles returns files for a project with cursor-based pagination.
// The cursor is a base64-encoded offset. Returns files, next cursor, and error.
func (s *SQLiteStore) ListFiles(ctx context.Context, projectID string, cursor string, limit int) ([]*File, string, error) {
	// Parse cursor (base64-encoded offset)
	offset := 0
	if cursor != "" {
		decoded, err := base64.StdEncoding.DecodeString(cursor)
		if err != nil {
			return nil, "", fmt.Errorf("invalid cursor: %w", err)
		}
		_, err = fmt.Sscanf(string(decoded), "offset:%d", &offset)
		if err != nil {
			return nil, "", fmt.Errorf("invalid cursor format: %w", err)
		}
		// Validate offset is non-negative
		if offset < 0 {
			return nil, "", fmt.Errorf("cursor offset must be non-negative: %d", offset)
		}
	}

	// Clamp limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	// Query with LIMIT and OFFSET for pagination
	query := `
		SELECT id, project_id, path, size, mod_time, content_hash, language, content_type, indexed_at
		FROM files WHERE project_id = ?
		ORDER BY path ASC
		LIMIT ? OFFSET ?
	`
	rows, err := s.db.QueryContext(ctx, query, projectID, limit+1, offset) // +1 to check if more exist
	if err != nil {
		return nil, "", fmt.Errorf("failed to query files: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var files []*File
	for rows.Next() {
		var f File
		var modTime, indexedAt sql.NullTime
		var contentHash, language, contentType sql.NullString

		err := rows.Scan(&f.ID, &f.ProjectID, &f.Path, &f.Size, &modTime, &contentHash, &language, &contentType, &indexedAt)
		if err != nil {
			return nil, "", fmt.Errorf("failed to scan file row: %w", err)
		}

		if modTime.Valid {
			f.ModTime = modTime.Time
		}
		if indexedAt.Valid {
			f.IndexedAt = indexedAt.Time
		}
		if contentHash.Valid {
			f.ContentHash = contentHash.String
		}
		if language.Valid {
			f.Language = language.String
		}
		if contentType.Valid {
			f.ContentType = contentType.String
		}

		files = append(files, &f)
	}

	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("error iterating files: %w", err)
	}

	// Determine if there are more results
	var nextCursor string
	if len(files) > limit {
		// There are more results, create next cursor
		files = files[:limit] // Trim to requested limit
		nextOffset := offset + limit
		nextCursor = base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("offset:%d", nextOffset)))
	}

	return files, nextCursor, nil
}

// DeleteFilesByProject deletes all files for a project.
// Due to ON DELETE CASCADE, this also deletes associated chunks and symbols.
func (s *SQLiteStore) DeleteFilesByProject(ctx context.Context, projectID string) error {
	query := `DELETE FROM files WHERE project_id = ?`
	_, err := s.db.ExecContext(ctx, query, projectID)
	if err != nil {
		return fmt.Errorf("failed to delete files: %w", err)
	}
	return nil
}

// GetFilePathsByProject returns all file paths for a project.
// This is used for gitignore synchronization to determine which indexed files
// should be removed when gitignore patterns change.
func (s *SQLiteStore) GetFilePathsByProject(ctx context.Context, projectID string) ([]string, error) {
	query := `SELECT path FROM files WHERE project_id = ? ORDER BY path`
	rows, err := s.db.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to query file paths: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var paths []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, fmt.Errorf("failed to scan path: %w", err)
		}
		paths = append(paths, path)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating paths: %w", err)
	}

	return paths, nil
}

// ListFilePathsUnder returns all file paths under a directory prefix.
// Used for differential gitignore reconciliation (BUG-028).
// Only files with paths starting with dirPrefix/ are returned.
func (s *SQLiteStore) ListFilePathsUnder(ctx context.Context, projectID, dirPrefix string) ([]string, error) {
	// Normalize prefix: ensure no trailing slash, then add for LIKE match
	dirPrefix = strings.TrimSuffix(dirPrefix, "/")
	if dirPrefix == "" {
		// Empty prefix means all files - use GetFilePathsByProject instead
		return s.GetFilePathsByProject(ctx, projectID)
	}

	// Use LIKE with escaped prefix + /% to match files under directory
	// Note: SQLite LIKE is case-insensitive by default; paths should be case-sensitive
	// We use || to concatenate in SQLite since Go's fmt.Sprintf might cause issues
	query := `SELECT path FROM files WHERE project_id = ? AND (path LIKE ? OR path = ?) ORDER BY path`
	likePattern := dirPrefix + "/%"

	rows, err := s.db.QueryContext(ctx, query, projectID, likePattern, dirPrefix)
	if err != nil {
		return nil, fmt.Errorf("failed to query files under %s: %w", dirPrefix, err)
	}
	defer func() { _ = rows.Close() }()

	var paths []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, fmt.Errorf("failed to scan path: %w", err)
		}
		paths = append(paths, path)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating paths under %s: %w", dirPrefix, err)
	}

	return paths, nil
}

// GetFilesForReconciliation returns all files for a project as a map keyed by path.
// This is optimized for startup file reconciliation where we need to compare
// indexed file metadata (mtime, size) against the current filesystem state.
// BUG-036: Used to detect files created/modified/deleted while server was stopped.
func (s *SQLiteStore) GetFilesForReconciliation(ctx context.Context, projectID string) (map[string]*File, error) {
	query := `
		SELECT id, project_id, path, size, mod_time, content_hash, language, content_type, indexed_at
		FROM files WHERE project_id = ?
	`
	rows, err := s.db.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to query files for reconciliation: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string]*File)
	for rows.Next() {
		var f File
		var modTime, indexedAt sql.NullTime
		var contentHash, language, contentType sql.NullString

		err := rows.Scan(&f.ID, &f.ProjectID, &f.Path, &f.Size, &modTime, &contentHash, &language, &contentType, &indexedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan file row: %w", err)
		}

		if modTime.Valid {
			f.ModTime = modTime.Time
		}
		if indexedAt.Valid {
			f.IndexedAt = indexedAt.Time
		}
		if contentHash.Valid {
			f.ContentHash = contentHash.String
		}
		if language.Valid {
			f.Language = language.String
		}
		if contentType.Valid {
			f.ContentType = contentType.String
		}

		result[f.Path] = &f
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating files: %w", err)
	}

	return result, nil
}

// DeleteFile deletes a single file by ID.
// Due to ON DELETE CASCADE, this also deletes associated chunks and symbols.
func (s *SQLiteStore) DeleteFile(ctx context.Context, fileID string) error {
	query := `DELETE FROM files WHERE id = ?`
	_, err := s.db.ExecContext(ctx, query, fileID)
	if err != nil {
		return fmt.Errorf("failed to delete file: %w", err)
	}
	return nil
}

// SaveChunks saves multiple chunks in a single transaction.
func (s *SQLiteStore) SaveChunks(ctx context.Context, chunks []*Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Prepare chunk insert statement
	chunkStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO chunks (id, file_id, file_path, content, raw_content, context, content_type, language, start_line, end_line, metadata, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			file_id = excluded.file_id,
			file_path = excluded.file_path,
			content = excluded.content,
			raw_content = excluded.raw_content,
			context = excluded.context,
			content_type = excluded.content_type,
			language = excluded.language,
			start_line = excluded.start_line,
			end_line = excluded.end_line,
			metadata = excluded.metadata,
			updated_at = excluded.updated_at
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare chunk statement: %w", err)
	}
	defer func() { _ = chunkStmt.Close() }()

	// Prepare symbol insert statement
	symbolStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO symbols (chunk_id, name, type, start_line, end_line, signature, doc_comment)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare symbol statement: %w", err)
	}
	defer func() { _ = symbolStmt.Close() }()

	// Delete existing symbols statement (for updates)
	deleteSymbolsStmt, err := tx.PrepareContext(ctx, `DELETE FROM symbols WHERE chunk_id = ?`)
	if err != nil {
		return fmt.Errorf("failed to prepare delete symbols statement: %w", err)
	}
	defer func() { _ = deleteSymbolsStmt.Close() }()

	for _, chunk := range chunks {
		// Serialize metadata
		var metadataJSON []byte
		if chunk.Metadata != nil {
			metadataJSON, _ = json.Marshal(chunk.Metadata)
		}

		_, err := chunkStmt.ExecContext(ctx,
			chunk.ID, chunk.FileID, chunk.FilePath, chunk.Content, chunk.RawContent, chunk.Context,
			string(chunk.ContentType), chunk.Language, chunk.StartLine, chunk.EndLine,
			string(metadataJSON), chunk.CreatedAt, chunk.UpdatedAt)
		if err != nil {
			return fmt.Errorf("failed to save chunk %s: %w", chunk.ID, err)
		}

		// Delete existing symbols for this chunk (in case of update)
		if _, err := deleteSymbolsStmt.ExecContext(ctx, chunk.ID); err != nil {
			return fmt.Errorf("failed to delete old symbols: %w", err)
		}

		// Insert symbols
		for _, sym := range chunk.Symbols {
			_, err := symbolStmt.ExecContext(ctx, chunk.ID, sym.Name, string(sym.Type), sym.StartLine, sym.EndLine, sym.Signature, sym.DocComment)
			if err != nil {
				return fmt.Errorf("failed to save symbol %s: %w", sym.Name, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// GetChunk retrieves a chunk by ID.
func (s *SQLiteStore) GetChunk(ctx context.Context, id string) (*Chunk, error) {
	query := `
		SELECT id, file_id, file_path, content, raw_content, context, content_type, language, start_line, end_line, metadata, created_at, updated_at
		FROM chunks WHERE id = ?
	`
	row := s.db.QueryRowContext(ctx, query, id)

	var c Chunk
	var rawContent, chunkContext, contentType, language, metadataJSON sql.NullString
	var createdAt, updatedAt sql.NullTime

	err := row.Scan(&c.ID, &c.FileID, &c.FilePath, &c.Content, &rawContent, &chunkContext, &contentType, &language, &c.StartLine, &c.EndLine, &metadataJSON, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get chunk: %w", err)
	}

	if rawContent.Valid {
		c.RawContent = rawContent.String
	}
	if chunkContext.Valid {
		c.Context = chunkContext.String
	}
	if contentType.Valid {
		c.ContentType = ContentType(contentType.String)
	}
	if language.Valid {
		c.Language = language.String
	}
	if createdAt.Valid {
		c.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		c.UpdatedAt = updatedAt.Time
	}
	if metadataJSON.Valid && metadataJSON.String != "" {
		_ = json.Unmarshal([]byte(metadataJSON.String), &c.Metadata)
	}

	// Load symbols
	symbols, err := s.getSymbolsForChunk(ctx, id)
	if err != nil {
		return nil, err
	}
	c.Symbols = symbols

	return &c, nil
}

// getSymbolsForChunk retrieves all symbols for a chunk.
func (s *SQLiteStore) getSymbolsForChunk(ctx context.Context, chunkID string) ([]*Symbol, error) {
	query := `
		SELECT name, type, start_line, end_line, signature, doc_comment
		FROM symbols WHERE chunk_id = ?
	`
	rows, err := s.db.QueryContext(ctx, query, chunkID)
	if err != nil {
		return nil, fmt.Errorf("failed to query symbols: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var symbols []*Symbol
	for rows.Next() {
		var sym Symbol
		var symType string
		var signature, docComment sql.NullString

		err := rows.Scan(&sym.Name, &symType, &sym.StartLine, &sym.EndLine, &signature, &docComment)
		if err != nil {
			return nil, fmt.Errorf("failed to scan symbol: %w", err)
		}

		sym.Type = SymbolType(symType)
		if signature.Valid {
			sym.Signature = signature.String
		}
		if docComment.Valid {
			sym.DocComment = docComment.String
		}

		symbols = append(symbols, &sym)
	}

	return symbols, rows.Err()
}

// GetChunks retrieves multiple chunks by ID in a single query (batch operation).
// This is more efficient than multiple GetChunk calls when fetching many chunks.
// Returns chunks in the same order as the input IDs. Missing chunks are excluded.
func (s *SQLiteStore) GetChunks(ctx context.Context, ids []string) ([]*Chunk, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	// Build parameterized query with placeholders
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := `
		SELECT id, file_id, file_path, content, raw_content, context, content_type, language, start_line, end_line, metadata, created_at, updated_at
		FROM chunks WHERE id IN (` + strings.Join(placeholders, ",") + `)
	`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query chunks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// Pre-allocate with expected capacity
	chunkMap := make(map[string]*Chunk, len(ids))
	chunkIDs := make([]string, 0, len(ids))

	for rows.Next() {
		var c Chunk
		var rawContent, chunkContext, contentType, language, metadataJSON sql.NullString
		var createdAt, updatedAt sql.NullTime

		err := rows.Scan(&c.ID, &c.FileID, &c.FilePath, &c.Content, &rawContent, &chunkContext, &contentType, &language, &c.StartLine, &c.EndLine, &metadataJSON, &createdAt, &updatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan chunk: %w", err)
		}

		if rawContent.Valid {
			c.RawContent = rawContent.String
		}
		if chunkContext.Valid {
			c.Context = chunkContext.String
		}
		if contentType.Valid {
			c.ContentType = ContentType(contentType.String)
		}
		if language.Valid {
			c.Language = language.String
		}
		if createdAt.Valid {
			c.CreatedAt = createdAt.Time
		}
		if updatedAt.Valid {
			c.UpdatedAt = updatedAt.Time
		}
		if metadataJSON.Valid && metadataJSON.String != "" {
			_ = json.Unmarshal([]byte(metadataJSON.String), &c.Metadata)
		}

		chunkMap[c.ID] = &c
		chunkIDs = append(chunkIDs, c.ID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate chunks: %w", err)
	}

	// Batch load symbols for all chunks
	if len(chunkIDs) > 0 {
		symbolsMap, err := s.getSymbolsForChunks(ctx, chunkIDs)
		if err != nil {
			return nil, err
		}
		for id, symbols := range symbolsMap {
			if chunk, ok := chunkMap[id]; ok {
				chunk.Symbols = symbols
			}
		}
	}

	// Return chunks in the order of input IDs
	result := make([]*Chunk, 0, len(ids))
	for _, id := range ids {
		if chunk, ok := chunkMap[id]; ok {
			result = append(result, chunk)
		}
	}

	return result, nil
}

// getSymbolsForChunks retrieves symbols for multiple chunks in a single query.
// Returns a map of chunk_id -> symbols.
func (s *SQLiteStore) getSymbolsForChunks(ctx context.Context, chunkIDs []string) (map[string][]*Symbol, error) {
	if len(chunkIDs) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(chunkIDs))
	args := make([]any, len(chunkIDs))
	for i, id := range chunkIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := `
		SELECT chunk_id, name, type, start_line, end_line, signature, doc_comment
		FROM symbols WHERE chunk_id IN (` + strings.Join(placeholders, ",") + `)
	`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query symbols: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string][]*Symbol, len(chunkIDs))
	for rows.Next() {
		var chunkID string
		var sym Symbol
		var symType string
		var signature, docComment sql.NullString

		err := rows.Scan(&chunkID, &sym.Name, &symType, &sym.StartLine, &sym.EndLine, &signature, &docComment)
		if err != nil {
			return nil, fmt.Errorf("failed to scan symbol: %w", err)
		}

		sym.Type = SymbolType(symType)
		if signature.Valid {
			sym.Signature = signature.String
		}
		if docComment.Valid {
			sym.DocComment = docComment.String
		}

		result[chunkID] = append(result[chunkID], &sym)
	}

	return result, rows.Err()
}

// GetChunksByFile retrieves all chunks for a file.
func (s *SQLiteStore) GetChunksByFile(ctx context.Context, fileID string) ([]*Chunk, error) {
	query := `
		SELECT id, file_id, file_path, content, raw_content, context, content_type, language, start_line, end_line, metadata, created_at, updated_at
		FROM chunks WHERE file_id = ?
		ORDER BY start_line ASC
	`
	rows, err := s.db.QueryContext(ctx, query, fileID)
	if err != nil {
		return nil, fmt.Errorf("failed to query chunks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var chunks []*Chunk
	for rows.Next() {
		var c Chunk
		var rawContent, chunkContext, contentType, language, metadataJSON sql.NullString
		var createdAt, updatedAt sql.NullTime

		err := rows.Scan(&c.ID, &c.FileID, &c.FilePath, &c.Content, &rawContent, &chunkContext, &contentType, &language, &c.StartLine, &c.EndLine, &metadataJSON, &createdAt, &updatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan chunk: %w", err)
		}

		if rawContent.Valid {
			c.RawContent = rawContent.String
		}
		if chunkContext.Valid {
			c.Context = chunkContext.String
		}
		if contentType.Valid {
			c.ContentType = ContentType(contentType.String)
		}
		if language.Valid {
			c.Language = language.String
		}
		if createdAt.Valid {
			c.CreatedAt = createdAt.Time
		}
		if updatedAt.Valid {
			c.UpdatedAt = updatedAt.Time
		}
		if metadataJSON.Valid && metadataJSON.String != "" {
			_ = json.Unmarshal([]byte(metadataJSON.String), &c.Metadata)
		}

		chunks = append(chunks, &c)
	}

	// Load symbols for each chunk
	for _, c := range chunks {
		symbols, err := s.getSymbolsForChunk(ctx, c.ID)
		if err != nil {
			return nil, err
		}
		c.Symbols = symbols
	}

	return chunks, rows.Err()
}

// DeleteChunks deletes chunks by their IDs.
// Due to ON DELETE CASCADE, this also deletes associated symbols.
func (s *SQLiteStore) DeleteChunks(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	// Build placeholders for IN clause
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf("DELETE FROM chunks WHERE id IN (%s)", strings.Join(placeholders, ","))
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to delete chunks: %w", err)
	}

	// BUG-031 fix: Log warning if row count doesn't match expected
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		slog.Warn("unable to get rows affected from chunk delete",
			slog.String("error", err.Error()))
	} else if int(rowsAffected) != len(ids) {
		slog.Debug("chunk delete count mismatch (some may have been already deleted)",
			slog.Int("requested", len(ids)),
			slog.Int64("deleted", rowsAffected))
	}

	return nil
}

// DeleteChunksByFile deletes all chunks for a file.
// Due to ON DELETE CASCADE, this also deletes associated symbols.
func (s *SQLiteStore) DeleteChunksByFile(ctx context.Context, fileID string) error {
	query := `DELETE FROM chunks WHERE file_id = ?`
	_, err := s.db.ExecContext(ctx, query, fileID)
	if err != nil {
		return fmt.Errorf("failed to delete chunks: %w", err)
	}
	return nil
}

// SearchSymbols searches for symbols by name (partial match).
func (s *SQLiteStore) SearchSymbols(ctx context.Context, name string, limit int) ([]*Symbol, error) {
	if limit <= 0 {
		limit = 10
	}

	query := `
		SELECT name, type, start_line, end_line, signature, doc_comment
		FROM symbols WHERE name LIKE ?
		LIMIT ?
	`
	rows, err := s.db.QueryContext(ctx, query, "%"+name+"%", limit)
	if err != nil {
		return nil, fmt.Errorf("failed to search symbols: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var symbols []*Symbol
	for rows.Next() {
		var sym Symbol
		var symType string
		var signature, docComment sql.NullString

		err := rows.Scan(&sym.Name, &symType, &sym.StartLine, &sym.EndLine, &signature, &docComment)
		if err != nil {
			return nil, fmt.Errorf("failed to scan symbol: %w", err)
		}

		sym.Type = SymbolType(symType)
		if signature.Valid {
			sym.Signature = signature.String
		}
		if docComment.Valid {
			sym.DocComment = docComment.String
		}

		symbols = append(symbols, &sym)
	}

	return symbols, rows.Err()
}

// GetState retrieves a value from the state table by key.
// Returns empty string if key doesn't exist (not an error).
func (s *SQLiteStore) GetState(ctx context.Context, key string) (string, error) {
	query := `SELECT value FROM state WHERE key = ?`
	var value sql.NullString
	err := s.db.QueryRowContext(ctx, query, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil // Key not found is not an error
	}
	if err != nil {
		return "", fmt.Errorf("failed to get state %q: %w", key, err)
	}
	if value.Valid {
		return value.String, nil
	}
	return "", nil
}

// SetState saves a key-value pair to the state table.
// Uses upsert to insert or update existing keys.
func (s *SQLiteStore) SetState(ctx context.Context, key, value string) error {
	query := `
		INSERT INTO state (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`
	_, err := s.db.ExecContext(ctx, query, key, value, time.Now())
	if err != nil {
		return fmt.Errorf("failed to set state %q: %w", key, err)
	}
	return nil
}

// --- Checkpoint Methods for Resumable Indexing ---

// SaveIndexCheckpoint saves the current indexing progress for resume capability.
// BUG-055: Uses single transaction for atomicity - prevents partial checkpoint on crash.
// BUG-053: Now includes embedder model to validate on resume and prevent dimension mismatch.
func (s *SQLiteStore) SaveIndexCheckpoint(ctx context.Context, stage string, total, embeddedCount int, embedderModel string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin checkpoint transaction: %w", err)
	}
	defer tx.Rollback()

	now := time.Now()
	query := `INSERT INTO state (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`

	keys := map[string]string{
		StateKeyCheckpointStage:         stage,
		StateKeyCheckpointTotal:         strconv.Itoa(total),
		StateKeyCheckpointEmbedded:      strconv.Itoa(embeddedCount),
		StateKeyCheckpointTimestamp:     now.Format(time.RFC3339),
		StateKeyCheckpointEmbedderModel: embedderModel,
	}

	for key, value := range keys {
		if _, err := tx.ExecContext(ctx, query, key, value, now); err != nil {
			return fmt.Errorf("save checkpoint %s: %w", key, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit checkpoint transaction: %w", err)
	}
	return nil
}

// LoadIndexCheckpoint retrieves the current checkpoint state.
// Returns nil if no checkpoint exists or indexing was completed.
// BUG-053: Now includes embedder model for validation on resume.
func (s *SQLiteStore) LoadIndexCheckpoint(ctx context.Context) (*IndexCheckpoint, error) {
	// Add timeout to prevent indefinite blocking on database lock contention
	// This is critical when another process (e.g., serve) holds the database
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	stage, err := s.GetState(ctx, StateKeyCheckpointStage)
	if err != nil {
		return nil, fmt.Errorf("get checkpoint stage: %w", err)
	}

	// No checkpoint or completed indexing
	if stage == "" || stage == "complete" {
		return nil, nil
	}

	totalStr, _ := s.GetState(ctx, StateKeyCheckpointTotal)
	total, _ := strconv.Atoi(totalStr)

	embeddedStr, _ := s.GetState(ctx, StateKeyCheckpointEmbedded)
	embedded, _ := strconv.Atoi(embeddedStr)

	timestampStr, _ := s.GetState(ctx, StateKeyCheckpointTimestamp)
	timestamp, _ := time.Parse(time.RFC3339, timestampStr)

	// BUG-053: Load embedder model for validation on resume
	embedderModel, _ := s.GetState(ctx, StateKeyCheckpointEmbedderModel)

	return &IndexCheckpoint{
		Stage:         stage,
		Total:         total,
		EmbeddedCount: embedded,
		Timestamp:     timestamp,
		EmbedderModel: embedderModel,
	}, nil
}

// ClearIndexCheckpoint removes all checkpoint data (called on successful completion).
// BUG-055: Uses single transaction for atomicity - prevents partial clear on crash.
// BUG-053: Now also clears the embedder model key.
func (s *SQLiteStore) ClearIndexCheckpoint(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin clear checkpoint transaction: %w", err)
	}
	defer tx.Rollback()

	keys := []string{
		StateKeyCheckpointStage,
		StateKeyCheckpointTotal,
		StateKeyCheckpointEmbedded,
		StateKeyCheckpointTimestamp,
		StateKeyCheckpointEmbedderModel,
	}

	query := `DELETE FROM state WHERE key = ?`
	for _, key := range keys {
		if _, err := tx.ExecContext(ctx, query, key); err != nil {
			return fmt.Errorf("clear checkpoint %s: %w", key, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit clear checkpoint transaction: %w", err)
	}
	return nil
}

// --- Embedding Storage Methods ---

// embeddingToBytes converts a float32 slice to bytes for BLOB storage.
func embeddingToBytes(embedding []float32) []byte {
	buf := make([]byte, len(embedding)*4)
	for i, v := range embedding {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

// bytesToEmbedding converts bytes from BLOB storage to a float32 slice.
func bytesToEmbedding(data []byte) []float32 {
	if len(data) == 0 {
		return nil
	}
	embedding := make([]float32, len(data)/4)
	for i := range embedding {
		bits := binary.LittleEndian.Uint32(data[i*4:])
		embedding[i] = math.Float32frombits(bits)
	}
	return embedding
}

// SaveChunkEmbeddings saves embeddings for multiple chunks in a single transaction.
// This is called during indexing to persist embeddings for later compaction.
func (s *SQLiteStore) SaveChunkEmbeddings(ctx context.Context, chunkIDs []string, embeddings [][]float32, model string) error {
	if len(chunkIDs) != len(embeddings) {
		return fmt.Errorf("chunk IDs and embeddings length mismatch: %d vs %d", len(chunkIDs), len(embeddings))
	}
	if len(chunkIDs) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		UPDATE chunks SET embedding = ?, embedding_model = ?, embedding_dims = ?
		WHERE id = ?
	`)
	if err != nil {
		return fmt.Errorf("prepare statement: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for i, id := range chunkIDs {
		emb := embeddings[i]
		embBytes := embeddingToBytes(emb)
		dims := len(emb)

		if _, err := stmt.ExecContext(ctx, embBytes, model, dims, id); err != nil {
			return fmt.Errorf("save embedding for chunk %s: %w", id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// GetAllEmbeddings retrieves all chunk IDs and their embeddings for compaction.
// Returns a map of chunk ID to embedding vector.
// Chunks without embeddings (NULL) are skipped.
func (s *SQLiteStore) GetAllEmbeddings(ctx context.Context) (map[string][]float32, error) {
	query := `SELECT id, embedding FROM chunks WHERE embedding IS NOT NULL`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query embeddings: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]float32)
	for rows.Next() {
		var id string
		var embBytes []byte

		if err := rows.Scan(&id, &embBytes); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}

		embedding := bytesToEmbedding(embBytes)
		if embedding != nil {
			result[id] = embedding
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}

	return result, nil
}

// GetEmbeddingStats returns the count of chunks with and without embeddings.
func (s *SQLiteStore) GetEmbeddingStats(ctx context.Context) (withEmbedding, withoutEmbedding int, err error) {
	query := `
		SELECT
			COUNT(CASE WHEN embedding IS NOT NULL THEN 1 END) as with_emb,
			COUNT(CASE WHEN embedding IS NULL THEN 1 END) as without_emb
		FROM chunks
	`
	err = s.db.QueryRowContext(ctx, query).Scan(&withEmbedding, &withoutEmbedding)
	if err != nil {
		return 0, 0, fmt.Errorf("query embedding stats: %w", err)
	}
	return withEmbedding, withoutEmbedding, nil
}

// Verify SQLiteStore implements MetadataStore interface.
var _ MetadataStore = (*SQLiteStore)(nil)
