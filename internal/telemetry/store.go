package telemetry

import (
	"database/sql"
	"fmt"
	"time"
)

// SQLiteMetricsStore implements QueryMetricsStore using SQLite.
type SQLiteMetricsStore struct {
	db *sql.DB
}

// NewSQLiteMetricsStore creates a new SQLite-backed metrics store.
// It expects the telemetry tables to already exist (created by metadata store migrations).
func NewSQLiteMetricsStore(db *sql.DB) (*SQLiteMetricsStore, error) {
	if db == nil {
		return nil, fmt.Errorf("database connection is required")
	}
	return &SQLiteMetricsStore{db: db}, nil
}

// InitSchema creates the telemetry tables if they don't exist.
// This is called by the metadata store migration system.
func InitTelemetrySchema(db *sql.DB) error {
	schema := `
	-- Query type frequency (aggregated daily)
	CREATE TABLE IF NOT EXISTS query_type_stats (
		date TEXT NOT NULL,
		query_type TEXT NOT NULL,
		count INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (date, query_type)
	);

	-- Top query terms (with frequency count)
	CREATE TABLE IF NOT EXISTS query_terms (
		term TEXT PRIMARY KEY,
		count INTEGER NOT NULL DEFAULT 1,
		last_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_query_terms_count ON query_terms(count DESC);

	-- Zero-result queries (circular buffer - max 100)
	CREATE TABLE IF NOT EXISTS zero_result_queries (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		query TEXT NOT NULL,
		timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- Latency histogram (buckets: <10ms, 10-50ms, 50-100ms, 100-500ms, >500ms)
	CREATE TABLE IF NOT EXISTS query_latency_stats (
		date TEXT NOT NULL,
		bucket TEXT NOT NULL,
		count INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (date, bucket)
	);
	`

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("create telemetry schema: %w", err)
	}
	return nil
}

// SaveQueryTypeCounts upserts daily query type counts.
func (s *SQLiteMetricsStore) SaveQueryTypeCounts(date string, counts map[QueryType]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`
		INSERT INTO query_type_stats (date, query_type, count)
		VALUES (?, ?, ?)
		ON CONFLICT(date, query_type) DO UPDATE SET count = count + excluded.count
	`)
	if err != nil {
		return fmt.Errorf("prepare statement: %w", err)
	}
	defer stmt.Close()

	for qt, count := range counts {
		if _, err := stmt.Exec(date, string(qt), count); err != nil {
			return fmt.Errorf("insert query type count: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// GetQueryTypeCounts retrieves counts for a date range.
func (s *SQLiteMetricsStore) GetQueryTypeCounts(from, to string) (map[QueryType]int64, error) {
	rows, err := s.db.Query(`
		SELECT query_type, SUM(count) as total
		FROM query_type_stats
		WHERE date >= ? AND date <= ?
		GROUP BY query_type
	`, from, to)
	if err != nil {
		return nil, fmt.Errorf("query type counts: %w", err)
	}
	defer rows.Close()

	counts := make(map[QueryType]int64)
	for rows.Next() {
		var qt string
		var count int64
		if err := rows.Scan(&qt, &count); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		counts[QueryType(qt)] = count
	}
	return counts, rows.Err()
}

// UpsertTermCounts updates term frequency counts.
func (s *SQLiteMetricsStore) UpsertTermCounts(terms map[string]int64) error {
	if len(terms) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`
		INSERT INTO query_terms (term, count, last_seen)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(term) DO UPDATE SET
			count = count + excluded.count,
			last_seen = CURRENT_TIMESTAMP
	`)
	if err != nil {
		return fmt.Errorf("prepare statement: %w", err)
	}
	defer stmt.Close()

	for term, count := range terms {
		if _, err := stmt.Exec(term, count); err != nil {
			return fmt.Errorf("upsert term count: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// GetTopTerms retrieves the top N terms by frequency.
func (s *SQLiteMetricsStore) GetTopTerms(limit int) ([]TermCount, error) {
	rows, err := s.db.Query(`
		SELECT term, count
		FROM query_terms
		ORDER BY count DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("query top terms: %w", err)
	}
	defer rows.Close()

	var terms []TermCount
	for rows.Next() {
		var tc TermCount
		if err := rows.Scan(&tc.Term, &tc.Count); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		terms = append(terms, tc)
	}
	return terms, rows.Err()
}

// AddZeroResultQuery adds a query to the zero-result buffer.
// Automatically maintains a maximum of 100 entries (FIFO).
func (s *SQLiteMetricsStore) AddZeroResultQuery(query string, timestamp time.Time) error {
	// Insert new query
	_, err := s.db.Exec(`
		INSERT INTO zero_result_queries (query, timestamp)
		VALUES (?, ?)
	`, query, timestamp)
	if err != nil {
		return fmt.Errorf("insert zero-result query: %w", err)
	}

	// Trim to 100 entries (delete oldest)
	_, err = s.db.Exec(`
		DELETE FROM zero_result_queries
		WHERE id NOT IN (
			SELECT id FROM zero_result_queries
			ORDER BY id DESC
			LIMIT 100
		)
	`)
	if err != nil {
		return fmt.Errorf("trim zero-result queries: %w", err)
	}

	return nil
}

// GetZeroResultQueries retrieves recent zero-result queries.
func (s *SQLiteMetricsStore) GetZeroResultQueries(limit int) ([]string, error) {
	rows, err := s.db.Query(`
		SELECT query
		FROM zero_result_queries
		ORDER BY id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("query zero-result queries: %w", err)
	}
	defer rows.Close()

	var queries []string
	for rows.Next() {
		var q string
		if err := rows.Scan(&q); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		queries = append(queries, q)
	}
	return queries, rows.Err()
}

// SaveLatencyCounts upserts daily latency histogram counts.
func (s *SQLiteMetricsStore) SaveLatencyCounts(date string, counts map[LatencyBucket]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`
		INSERT INTO query_latency_stats (date, bucket, count)
		VALUES (?, ?, ?)
		ON CONFLICT(date, bucket) DO UPDATE SET count = count + excluded.count
	`)
	if err != nil {
		return fmt.Errorf("prepare statement: %w", err)
	}
	defer stmt.Close()

	for bucket, count := range counts {
		if _, err := stmt.Exec(date, string(bucket), count); err != nil {
			return fmt.Errorf("insert latency count: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// GetLatencyCounts retrieves latency distribution for a date range.
func (s *SQLiteMetricsStore) GetLatencyCounts(from, to string) (map[LatencyBucket]int64, error) {
	rows, err := s.db.Query(`
		SELECT bucket, SUM(count) as total
		FROM query_latency_stats
		WHERE date >= ? AND date <= ?
		GROUP BY bucket
	`, from, to)
	if err != nil {
		return nil, fmt.Errorf("query latency counts: %w", err)
	}
	defer rows.Close()

	counts := make(map[LatencyBucket]int64)
	for rows.Next() {
		var bucket string
		var count int64
		if err := rows.Scan(&bucket, &count); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		counts[LatencyBucket(bucket)] = count
	}
	return counts, rows.Err()
}

// Close releases resources. The underlying db is not closed as it's shared.
func (s *SQLiteMetricsStore) Close() error {
	// Don't close db - it's shared with metadata store
	return nil
}
