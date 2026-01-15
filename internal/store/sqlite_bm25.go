package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite" // Pure Go SQLite driver (no CGO)
)

// SQLiteBM25Index implements BM25Index using SQLite FTS5.
// It provides concurrent multi-process access via WAL mode, solving BUG-064.
type SQLiteBM25Index struct {
	mu        sync.RWMutex
	db        *sql.DB
	path      string
	config    BM25Config
	closed    bool
	stopWords map[string]struct{}
}

// Verify interface implementation at compile time
var _ BM25Index = (*SQLiteBM25Index)(nil)

// validateSQLiteIntegrity checks if a SQLite FTS5 index is valid before opening.
// Returns nil if valid, error describing corruption if not.
// This mirrors the BUG-049 fix pattern from BleveBM25Index.
func validateSQLiteIntegrity(path string) error {
	// Check if database file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil // Database doesn't exist, will be created
	}

	// Try to open read-only for validation
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return fmt.Errorf("cannot open for validation: %w", err)
	}
	defer db.Close()

	// Quick integrity check
	var result string
	if err := db.QueryRow("PRAGMA integrity_check").Scan(&result); err != nil {
		return fmt.Errorf("integrity check failed: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("database corrupted: %s", result)
	}

	// Verify FTS5 table exists
	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master
                       WHERE type='table' AND name='fts_content'`).Scan(&count)
	if err != nil {
		return fmt.Errorf("cannot query schema: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("FTS5 table 'fts_content' missing")
	}

	return nil
}

// NewSQLiteBM25Index creates a new SQLite FTS5-based BM25 index.
// If path is empty, creates an in-memory index for testing.
// Uses WAL mode for concurrent multi-process access (solves BUG-064).
func NewSQLiteBM25Index(path string, config BM25Config) (*SQLiteBM25Index, error) {
	var dsn string
	if path == "" {
		// In-memory index for testing
		dsn = ":memory:"
	} else {
		// Create directory if needed
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
		}

		// Validate integrity before opening (BUG-049 pattern)
		if validErr := validateSQLiteIntegrity(path); validErr != nil {
			slog.Warn("sqlite_bm25_index_corrupted",
				slog.String("path", path),
				slog.String("error", validErr.Error()))

			// Auto-clear corrupted index
			if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
				return nil, fmt.Errorf("BM25 index corrupted at %s and cannot remove: %w (original error: %v)", path, removeErr, validErr)
			}
			// Also remove WAL and SHM files
			_ = os.Remove(path + "-wal")
			_ = os.Remove(path + "-shm")

			slog.Info("sqlite_bm25_index_cleared",
				slog.String("path", path),
				slog.String("reason", "corruption detected, please reindex"))
		}

		// WAL mode for concurrent access (solves BUG-064)
		// _busy_timeout handles lock contention gracefully
		dsn = path + "?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000"
	}

	// IMPORTANT: Use modernc.org/sqlite driver (pure Go, no CGO)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool for SQLite
	// Single writer to prevent lock contention
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0) // Don't expire connections

	// Set additional pragmas via statements (DSN params may be ignored by modernc.org/sqlite)
	// CRITICAL: WAL mode must be set via PRAGMA for modernc.org/sqlite
	pragmas := []string{
		"PRAGMA journal_mode = WAL",       // WAL mode for concurrent access (BUG-064 fix)
		"PRAGMA busy_timeout = 5000",      // 5 second timeout for lock contention
		"PRAGMA synchronous = NORMAL",     // Balance durability and performance
		"PRAGMA cache_size = -65536",      // 64MB cache (negative = KB)
		"PRAGMA temp_store = MEMORY",      // Keep temp tables in memory
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("failed to set pragma: %w", err)
		}
	}

	idx := &SQLiteBM25Index{
		db:        db,
		path:      path,
		config:    config,
		stopWords: BuildStopWordMap(config.StopWords),
	}

	// Initialize FTS5 schema
	if err := idx.initSchema(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return idx, nil
}

// initSchema creates the FTS5 virtual table and supporting tables.
func (s *SQLiteBM25Index) initSchema() error {
	schema := `
	-- Schema version tracking
	CREATE TABLE IF NOT EXISTS schema_version (
		version INTEGER PRIMARY KEY
	);

	-- FTS5 virtual table for full-text search with BM25 scoring
	-- doc_id is UNINDEXED (stored but not searchable)
	-- content stores pre-tokenized text (camelCase/snake_case split)
	CREATE VIRTUAL TABLE IF NOT EXISTS fts_content USING fts5(
		doc_id UNINDEXED,
		content,
		tokenize='unicode61'
	);

	-- Auxiliary table for tracking document IDs (AllIDs method)
	-- FTS5 doesn't expose rowid reliably for external content tables
	CREATE TABLE IF NOT EXISTS doc_ids (
		doc_id TEXT PRIMARY KEY
	);

	INSERT OR IGNORE INTO schema_version (version) VALUES (1);
	`

	_, err := s.db.Exec(schema)
	return err
}

// Index adds documents to the index.
// Content is pre-tokenized using CodeTokenizer for camelCase/snake_case handling.
// If a document ID already exists, it is updated (delete + insert).
func (s *SQLiteBM25Index) Index(ctx context.Context, docs []*Document) error {
	if len(docs) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("index is closed")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Prepare statements for batch operations
	// NOTE: FTS5 virtual tables don't support REPLACE, so we delete first
	deleteStmt, err := tx.PrepareContext(ctx,
		`DELETE FROM fts_content WHERE doc_id = ?`)
	if err != nil {
		return fmt.Errorf("failed to prepare delete statement: %w", err)
	}
	defer deleteStmt.Close()

	insertStmt, err := tx.PrepareContext(ctx,
		`INSERT INTO fts_content(doc_id, content) VALUES (?, ?)`)
	if err != nil {
		return fmt.Errorf("failed to prepare FTS statement: %w", err)
	}
	defer insertStmt.Close()

	idStmt, err := tx.PrepareContext(ctx,
		`INSERT OR REPLACE INTO doc_ids(doc_id) VALUES (?)`)
	if err != nil {
		return fmt.Errorf("failed to prepare ID statement: %w", err)
	}
	defer idStmt.Close()

	for _, doc := range docs {
		// Pre-process content with code-aware tokenization
		// This handles camelCase, snake_case, and stop word filtering
		tokens := TokenizeCode(doc.Content)
		tokens = FilterStopWords(tokens, s.stopWords)
		processedContent := strings.Join(tokens, " ")

		// Delete existing entry first (FTS5 doesn't support REPLACE)
		if _, err := deleteStmt.ExecContext(ctx, doc.ID); err != nil {
			return fmt.Errorf("failed to delete existing document %s: %w", doc.ID, err)
		}

		// Insert new content
		if _, err := insertStmt.ExecContext(ctx, doc.ID, processedContent); err != nil {
			return fmt.Errorf("failed to index document %s: %w", doc.ID, err)
		}
		if _, err := idStmt.ExecContext(ctx, doc.ID); err != nil {
			return fmt.Errorf("failed to track document ID %s: %w", doc.ID, err)
		}
	}

	return tx.Commit()
}

// Search returns documents matching query, scored by BM25.
// Query is pre-tokenized using the same tokenization as indexing.
func (s *SQLiteBM25Index) Search(ctx context.Context, queryStr string, limit int) ([]*BM25Result, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, fmt.Errorf("index is closed")
	}

	// Handle empty query (matches Bleve behavior)
	if queryStr == "" || strings.TrimSpace(queryStr) == "" {
		return []*BM25Result{}, nil
	}

	// Pre-process query with same tokenization as indexing
	tokens := TokenizeCode(queryStr)
	tokens = FilterStopWords(tokens, s.stopWords)
	if len(tokens) == 0 {
		return []*BM25Result{}, nil
	}

	// Build FTS5 MATCH query
	// FTS5 uses space-separated terms for AND matching by default
	processedQuery := strings.Join(tokens, " ")

	// FTS5 bm25() returns negative values where lower = better match
	// ORDER BY score puts best matches first (most negative)
	query := `
		SELECT doc_id, bm25(fts_content) as score
		FROM fts_content
		WHERE content MATCH ?
		ORDER BY score
		LIMIT ?
	`

	rows, err := s.db.QueryContext(ctx, query, processedQuery, limit)
	if err != nil {
		// FTS5 returns error for invalid match queries, treat as no results
		if strings.Contains(err.Error(), "fts5:") || strings.Contains(err.Error(), "syntax error") {
			return []*BM25Result{}, nil
		}
		return nil, fmt.Errorf("search failed: %w", err)
	}
	defer rows.Close()

	var results []*BM25Result
	for rows.Next() {
		var docID string
		var score float64
		if err := rows.Scan(&docID, &score); err != nil {
			return nil, fmt.Errorf("failed to scan result: %w", err)
		}
		// Negate score: FTS5 bm25() returns negative values
		// Higher positive = better match (consistent with Bleve)
		results = append(results, &BM25Result{
			DocID:        docID,
			Score:        -score,
			MatchedTerms: tokens, // Return preprocessed query tokens
		})
	}

	return results, rows.Err()
}

// Delete removes documents from the index.
func (s *SQLiteBM25Index) Delete(ctx context.Context, docIDs []string) error {
	if len(docIDs) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("index is closed")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Build parameterized query for batch delete
	placeholders := make([]string, len(docIDs))
	args := make([]any, len(docIDs))
	for i, id := range docIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	inClause := strings.Join(placeholders, ",")

	// Delete from FTS5 content table
	ftsQuery := fmt.Sprintf("DELETE FROM fts_content WHERE doc_id IN (%s)", inClause)
	if _, err := tx.ExecContext(ctx, ftsQuery, args...); err != nil {
		return fmt.Errorf("failed to delete from FTS: %w", err)
	}

	// Delete from doc_ids tracking table
	idsQuery := fmt.Sprintf("DELETE FROM doc_ids WHERE doc_id IN (%s)", inClause)
	if _, err := tx.ExecContext(ctx, idsQuery, args...); err != nil {
		return fmt.Errorf("failed to delete from doc_ids: %w", err)
	}

	return tx.Commit()
}

// AllIDs returns all document IDs in the index.
// Used for consistency checking between stores.
func (s *SQLiteBM25Index) AllIDs() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, fmt.Errorf("index is closed")
	}

	query := `SELECT doc_id FROM doc_ids ORDER BY doc_id`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query IDs: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan ID: %w", err)
		}
		ids = append(ids, id)
	}

	return ids, rows.Err()
}

// Stats returns index statistics.
func (s *SQLiteBM25Index) Stats() *IndexStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return &IndexStats{}
	}

	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM doc_ids`).Scan(&count)
	if err != nil {
		return &IndexStats{}
	}

	return &IndexStats{
		DocumentCount: count,
		// Note: TermCount and AvgDocLength not readily available in FTS5
		// Would require querying internal tables
	}
}

// Save persists the index to disk.
// Forces a WAL checkpoint to ensure durability.
func (s *SQLiteBM25Index) Save(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("index is closed")
	}

	// Force WAL checkpoint to ensure all changes are in main database
	_, err := s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	return err
}

// Load opens an existing index from disk.
func (s *SQLiteBM25Index) Load(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Close existing connection
	if s.db != nil && !s.closed {
		_ = s.db.Close()
	}

	// Reopen at new path with WAL mode
	dsn := path + "?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("failed to open index: %w", err)
	}

	s.db = db
	s.path = path
	s.closed = false

	return nil
}

// Close closes the index.
// Forces a WAL checkpoint before closing.
func (s *SQLiteBM25Index) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil // Idempotent (matches Bleve behavior)
	}

	s.closed = true
	if s.db != nil {
		// Checkpoint before close to ensure durability
		_, _ = s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
		return s.db.Close()
	}
	return nil
}
