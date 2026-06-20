package graph

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteRepository stores the disposable AmanGraph overlay in SQLite.
// Schema migrations are forward-only; rollback across incompatible binaries
// requires deleting the disposable graph database and rebuilding it.
type SQLiteRepository struct {
	db *sql.DB

	schemaMu      sync.RWMutex
	schemaCurrent bool
}

type graphMigration struct {
	version     int
	description string
	apply       func(context.Context, *sql.Tx) error
}

func graphRecoveryHint() string {
	return "run `amanmcp index --force <project-path>` or remove `.amanmcp/graph.db` and rerun `amanmcp index <project-path>`"
}

// OpenSQLiteRepository opens or creates a graph repository at dbPath.
func OpenSQLiteRepository(dbPath string) (*SQLiteRepository, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create graph db directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open graph db: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	repo := &SQLiteRepository{db: db}
	if err := repo.initSchema(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to initialize graph schema: %w", err)
	}
	return repo, nil
}

func (r *SQLiteRepository) initSchema() error {
	ctx := context.Background()
	if err := r.checkDatabaseIntegrity(ctx); err != nil {
		return err
	}

	pragmas := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA busy_timeout = 5000",
	}
	for _, pragma := range pragmas {
		if _, err := r.db.Exec(pragma); err != nil {
			return fmt.Errorf("set %s: %w", pragma, err)
		}
	}

	version, hasVersionTable, err := r.detectSchemaVersion(ctx)
	if err != nil {
		return err
	}
	if hasVersionTable && version > SchemaVersion {
		return fmt.Errorf("graph database was created by a newer AmanMCP schema version %d; this binary supports version %d. To rebuild the disposable graph overlay, %s", version, SchemaVersion, graphRecoveryHint())
	}
	if err := r.applySchemaMigrations(ctx, version); err != nil {
		return err
	}
	if err := r.validateSchemaContract(ctx); err != nil {
		return err
	}
	r.schemaMu.Lock()
	r.schemaCurrent = true
	r.schemaMu.Unlock()
	return nil
}

func (r *SQLiteRepository) checkDatabaseIntegrity(ctx context.Context) error {
	var result string
	if err := r.db.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&result); err != nil {
		return fmt.Errorf("corrupt graph database: %s: %w", graphRecoveryHint(), err)
	}
	if result != "ok" {
		return fmt.Errorf("corrupt graph database: PRAGMA quick_check returned %q; %s", result, graphRecoveryHint())
	}
	return nil
}

func (r *SQLiteRepository) detectSchemaVersion(ctx context.Context) (int, bool, error) {
	var tableName string
	err := r.db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'graph_schema_version'`).Scan(&tableName)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("detect graph schema version table: %w", err)
	}

	version, err := r.schemaVersion(ctx)
	if err != nil {
		return 0, true, err
	}
	if version == 0 {
		return 0, true, fmt.Errorf("graph schema version is missing; %s", graphRecoveryHint())
	}
	return version, true, nil
}

func (r *SQLiteRepository) applySchemaMigrations(ctx context.Context, fromVersion int) error {
	if fromVersion == SchemaVersion {
		return nil
	}
	if fromVersion > SchemaVersion {
		return fmt.Errorf("graph database was created by a newer AmanMCP schema version %d; this binary supports version %d. To rebuild the disposable graph overlay, %s", fromVersion, SchemaVersion, graphRecoveryHint())
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin graph schema migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, migration := range graphMigrations() {
		if migration.version <= fromVersion {
			continue
		}
		if migration.version > SchemaVersion {
			break
		}
		if err := migration.apply(ctx, tx); err != nil {
			return fmt.Errorf("apply graph schema migration %d (%s): %w", migration.version, migration.description, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit graph schema migration: %w", err)
	}
	return nil
}

func graphMigrations() []graphMigration {
	return []graphMigration{
		{
			version:     1,
			description: "initial graph overlay schema",
			apply: func(ctx context.Context, tx *sql.Tx) error {
				return execGraphSchemaMigration(ctx, tx, `
CREATE TABLE IF NOT EXISTS graph_schema_version (
	version INTEGER PRIMARY KEY,
	applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS graph_nodes (
	id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL,
	kind TEXT NOT NULL,
	key TEXT NOT NULL,
	source_path TEXT NOT NULL DEFAULT '',
	name TEXT NOT NULL DEFAULT '',
	language TEXT NOT NULL DEFAULT '',
	symbol_kind TEXT NOT NULL DEFAULT '',
	start_line INTEGER NOT NULL DEFAULT 0,
	end_line INTEGER NOT NULL DEFAULT 0,
	metadata_json TEXT NOT NULL DEFAULT '{}',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	UNIQUE(project_id, kind, key)
);

CREATE INDEX IF NOT EXISTS idx_graph_nodes_project_kind ON graph_nodes(project_id, kind);

CREATE TABLE IF NOT EXISTS graph_edges (
	id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL,
	kind TEXT NOT NULL,
	from_node_id TEXT NOT NULL,
	to_node_id TEXT NOT NULL,
	extractor TEXT NOT NULL,
	source_path TEXT NOT NULL,
	evidence_json TEXT NOT NULL,
	confidence REAL NOT NULL CHECK(confidence >= 0 AND confidence <= 1),
	confidence_label TEXT NOT NULL,
	stale INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	UNIQUE(project_id, extractor, source_path, kind, from_node_id, to_node_id),
	FOREIGN KEY(from_node_id) REFERENCES graph_nodes(id) ON DELETE CASCADE,
	FOREIGN KEY(to_node_id) REFERENCES graph_nodes(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_graph_edges_project_kind ON graph_edges(project_id, kind);
CREATE INDEX IF NOT EXISTS idx_graph_edges_scope ON graph_edges(project_id, extractor, source_path);

CREATE TABLE IF NOT EXISTS graph_builds (
	project_id TEXT PRIMARY KEY,
	status TEXT NOT NULL,
	started_at TEXT NOT NULL DEFAULT '',
	completed_at TEXT NOT NULL DEFAULT '',
	source_version TEXT NOT NULL DEFAULT '',
	message TEXT NOT NULL DEFAULT '',
	updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS graph_extractor_runs (
	project_id TEXT NOT NULL,
	extractor TEXT NOT NULL,
	source_path TEXT NOT NULL,
	status TEXT NOT NULL,
	started_at TEXT NOT NULL DEFAULT '',
	completed_at TEXT NOT NULL DEFAULT '',
	node_count INTEGER NOT NULL DEFAULT 0,
	edge_count INTEGER NOT NULL DEFAULT 0,
	warning_count INTEGER NOT NULL DEFAULT 0,
	error_count INTEGER NOT NULL DEFAULT 0,
	warnings_json TEXT NOT NULL DEFAULT '[]',
	errors_json TEXT NOT NULL DEFAULT '[]',
	updated_at TEXT NOT NULL,
	PRIMARY KEY(project_id, extractor, source_path)
);

INSERT OR IGNORE INTO graph_schema_version (version, applied_at) VALUES (1, CURRENT_TIMESTAMP);
`)
			},
		},
		{
			version:     2,
			description: "ADR-041 schema metadata, source versions, and lookup indexes",
			apply: func(ctx context.Context, tx *sql.Tx) error {
				if err := addColumnIfMissing(ctx, tx, "graph_schema_version", "description", "TEXT NOT NULL DEFAULT ''"); err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx,
					`UPDATE graph_schema_version SET description = ? WHERE version = 1 AND description = ''`,
					"initial graph overlay schema"); err != nil {
					return fmt.Errorf("backfill graph schema v1 description: %w", err)
				}
				if err := addColumnIfMissing(ctx, tx, "graph_edges", "source_version", "TEXT NOT NULL DEFAULT ''"); err != nil {
					return err
				}
				if err := execGraphSchemaMigration(ctx, tx, `
CREATE INDEX IF NOT EXISTS idx_graph_nodes_project_source_path ON graph_nodes(project_id, source_path);
CREATE INDEX IF NOT EXISTS idx_graph_nodes_project_language ON graph_nodes(project_id, language);
CREATE INDEX IF NOT EXISTS idx_graph_edges_project_from_node ON graph_edges(project_id, from_node_id);
CREATE INDEX IF NOT EXISTS idx_graph_edges_project_to_node ON graph_edges(project_id, to_node_id);
CREATE INDEX IF NOT EXISTS idx_graph_edges_project_source_path ON graph_edges(project_id, source_path);
CREATE INDEX IF NOT EXISTS idx_graph_edges_project_extractor ON graph_edges(project_id, extractor);
CREATE INDEX IF NOT EXISTS idx_graph_edges_project_stale ON graph_edges(project_id, stale);
INSERT OR IGNORE INTO graph_schema_version (version, applied_at, description) VALUES (2, CURRENT_TIMESTAMP, 'ADR-041 schema metadata, source versions, and lookup indexes');
`); err != nil {
					return err
				}
				return nil
			},
		},
		{
			version:     3,
			description: "separate full and incremental graph build metadata",
			apply: func(ctx context.Context, tx *sql.Tx) error {
				if err := execGraphSchemaMigration(ctx, tx, `
CREATE TABLE IF NOT EXISTS graph_builds_v3 (
	project_id TEXT NOT NULL,
	build_kind TEXT NOT NULL DEFAULT 'full',
	status TEXT NOT NULL,
	started_at TEXT NOT NULL DEFAULT '',
	completed_at TEXT NOT NULL DEFAULT '',
	source_version TEXT NOT NULL DEFAULT '',
	message TEXT NOT NULL DEFAULT '',
	updated_at TEXT NOT NULL,
	PRIMARY KEY(project_id, build_kind)
);
INSERT OR REPLACE INTO graph_builds_v3 (
	project_id, build_kind, status, started_at, completed_at, source_version, message, updated_at
)
SELECT project_id, 'full', status, started_at, completed_at, source_version, message, updated_at
FROM graph_builds;
DROP TABLE graph_builds;
ALTER TABLE graph_builds_v3 RENAME TO graph_builds;
INSERT OR IGNORE INTO graph_schema_version (version, applied_at, description) VALUES (3, CURRENT_TIMESTAMP, 'separate full and incremental graph build metadata');
`); err != nil {
					return err
				}
				return nil
			},
		},
	}
}

func execGraphSchemaMigration(ctx context.Context, tx *sql.Tx, statement string) error {
	if _, err := tx.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("execute graph schema SQL: %w", err)
	}
	return nil
}

func addColumnIfMissing(ctx context.Context, tx *sql.Tx, table, column, definition string) error {
	hasColumn, err := tableHasColumn(ctx, tx, table, column)
	if err != nil {
		return err
	}
	if hasColumn {
		return nil
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition)); err != nil {
		return fmt.Errorf("add graph schema column %s.%s: %w", table, column, err)
	}
	return nil
}

func tableHasColumn(ctx context.Context, query interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, table, column string) (bool, error) {
	rows, err := query.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return false, fmt.Errorf("inspect graph schema table %s: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, pk int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return false, fmt.Errorf("scan graph schema table %s: %w", table, err)
		}
		if name == column {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate graph schema table %s: %w", table, err)
	}
	return false, nil
}

func (r *SQLiteRepository) validateSchemaContract(ctx context.Context) error {
	version, err := r.schemaVersion(ctx)
	if err != nil {
		return err
	}
	if version != SchemaVersion {
		return fmt.Errorf("graph schema version %d incompatible with binary version %d", version, SchemaVersion)
	}

	requiredColumns := map[string][]string{
		"graph_schema_version": {"version", "applied_at", "description"},
		"graph_nodes": {
			"id", "project_id", "kind", "key", "source_path", "name", "language", "symbol_kind",
			"start_line", "end_line", "metadata_json", "created_at", "updated_at",
		},
		"graph_edges": {
			"id", "project_id", "kind", "from_node_id", "to_node_id", "extractor", "source_path",
			"evidence_json", "confidence", "confidence_label", "stale", "source_version", "created_at", "updated_at",
		},
		"graph_builds": {
			"project_id", "build_kind", "status", "started_at", "completed_at", "source_version", "message", "updated_at",
		},
		"graph_extractor_runs": {
			"project_id", "extractor", "source_path", "status", "started_at", "completed_at",
			"node_count", "edge_count", "warning_count", "error_count", "warnings_json", "errors_json", "updated_at",
		},
	}
	for table, columns := range requiredColumns {
		for _, column := range columns {
			hasColumn, err := tableHasColumn(ctx, r.db, table, column)
			if err != nil {
				return err
			}
			if !hasColumn {
				return fmt.Errorf("graph schema missing required column %s.%s; %s", table, column, graphRecoveryHint())
			}
		}
	}

	requiredIndexes := map[string][]string{
		"graph_nodes": {
			"idx_graph_nodes_project_kind",
			"idx_graph_nodes_project_source_path",
			"idx_graph_nodes_project_language",
		},
		"graph_edges": {
			"idx_graph_edges_project_kind",
			"idx_graph_edges_scope",
			"idx_graph_edges_project_from_node",
			"idx_graph_edges_project_to_node",
			"idx_graph_edges_project_source_path",
			"idx_graph_edges_project_extractor",
			"idx_graph_edges_project_stale",
		},
	}
	for table, indexes := range requiredIndexes {
		for _, index := range indexes {
			exists, err := indexExists(ctx, r.db, table, index)
			if err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("graph schema missing required index %s on %s; %s", index, table, graphRecoveryHint())
			}
		}
	}
	return nil
}

func indexExists(ctx context.Context, query interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, table, index string) (bool, error) {
	rows, err := query.QueryContext(ctx, `PRAGMA index_list(`+table+`)`)
	if err != nil {
		return false, fmt.Errorf("inspect graph schema indexes for %s: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var seq, unique, partial int
		var name, origin string
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			return false, fmt.Errorf("scan graph schema indexes for %s: %w", table, err)
		}
		if name == index {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate graph schema indexes for %s: %w", table, err)
	}
	return false, nil
}

// Close closes the underlying SQLite connection.
func (r *SQLiteRepository) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Close()
}

// UpsertNode inserts or updates a graph node by natural key.
func (r *SQLiteRepository) UpsertNode(ctx context.Context, node Node) (Node, error) {
	if err := r.ensureSchemaCurrent(ctx); err != nil {
		return Node{}, err
	}
	node, err := normalizeNode(node)
	if err != nil {
		return Node{}, err
	}
	now := time.Now().UTC()
	if node.CreatedAt.IsZero() {
		node.CreatedAt = now
	}
	node.UpdatedAt = now
	metadataJSON, err := json.Marshal(node.Metadata)
	if err != nil {
		return Node{}, fmt.Errorf("marshal node metadata: %w", err)
	}

	_, err = r.db.ExecContext(ctx, `
INSERT INTO graph_nodes (
	id, project_id, kind, key, source_path, name, language, symbol_kind,
	start_line, end_line, metadata_json, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(project_id, kind, key) DO UPDATE SET
	source_path = excluded.source_path,
	name = excluded.name,
	language = excluded.language,
	symbol_kind = excluded.symbol_kind,
	start_line = excluded.start_line,
	end_line = excluded.end_line,
	metadata_json = excluded.metadata_json,
	updated_at = excluded.updated_at
`, node.ID, node.ProjectID, node.Kind, node.Key, node.SourcePath, node.Name, node.Language,
		node.SymbolKind, node.StartLine, node.EndLine, string(metadataJSON), formatTime(node.CreatedAt), formatTime(node.UpdatedAt))
	if err != nil {
		return Node{}, fmt.Errorf("upsert graph node %s: %w", node.Key, err)
	}
	return node, nil
}

// UpsertEdge inserts or updates a graph edge by natural key.
func (r *SQLiteRepository) UpsertEdge(ctx context.Context, edge Edge) (Edge, error) {
	if err := r.ensureSchemaCurrent(ctx); err != nil {
		return Edge{}, err
	}
	edge, err := normalizeEdge(edge)
	if err != nil {
		return Edge{}, err
	}
	if err := r.ensureEdgeEndpoints(ctx, edge); err != nil {
		return Edge{}, err
	}
	if err := r.execUpsertEdge(ctx, r.db, edge); err != nil {
		return Edge{}, err
	}
	return edge, nil
}

// UpsertEdgeOnlyForTest is an alias used by repository contract tests.
func (r *SQLiteRepository) UpsertEdgeOnlyForTest(ctx context.Context, edge Edge) error {
	_, err := r.UpsertEdge(ctx, edge)
	return err
}

// ReplaceEdges atomically replaces edges for one project/extractor/source scope.
func (r *SQLiteRepository) ReplaceEdges(ctx context.Context, replacement EdgeReplacement) error {
	if replacement.ProjectID == "" {
		return fmt.Errorf("project_id is required")
	}
	if replacement.Extractor == "" {
		return fmt.Errorf("extractor is required")
	}
	if replacement.SourcePath == "" {
		return fmt.Errorf("source_path is required")
	}
	if err := r.ensureSchemaCurrent(ctx); err != nil {
		return err
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin graph edge replacement: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, node := range replacement.Nodes {
		node.ProjectID = replacement.ProjectID
		if _, err := r.upsertNodeTx(ctx, tx, node); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM graph_edges WHERE project_id = ? AND extractor = ? AND source_path = ?`,
		replacement.ProjectID, replacement.Extractor, replacement.SourcePath); err != nil {
		return fmt.Errorf("delete graph replacement scope: %w", err)
	}

	for _, edge := range replacement.Edges {
		edge.ProjectID = replacement.ProjectID
		edge.Extractor = replacement.Extractor
		edge.SourcePath = replacement.SourcePath
		normalized, err := normalizeEdge(edge)
		if err != nil {
			return err
		}
		if err := r.ensureEdgeEndpointsTx(ctx, tx, normalized); err != nil {
			return err
		}
		if err := r.execUpsertEdge(ctx, tx, normalized); err != nil {
			return err
		}
	}

	run := replacement.Run
	run.ProjectID = replacement.ProjectID
	run.Extractor = replacement.Extractor
	run.SourcePath = replacement.SourcePath
	if run.Status == "" {
		run.Status = ExtractorStatusSuccess
	}
	if run.EdgeCount == 0 {
		run.EdgeCount = len(replacement.Edges)
	}
	if run.NodeCount == 0 {
		run.NodeCount = len(replacement.Nodes)
	}
	if err := r.recordExtractorRunTx(ctx, tx, run); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit graph edge replacement: %w", err)
	}
	return nil
}

// MarkEdgesToSourceStale marks active edges that point at nodes owned by the
// supplied source path. It preserves the edge for degraded graph explanations
// while making stale evidence visible to status/query callers.
func (r *SQLiteRepository) MarkEdgesToSourceStale(ctx context.Context, projectID, sourcePath string) error {
	if projectID == "" {
		return fmt.Errorf("project_id is required")
	}
	if sourcePath == "" {
		return fmt.Errorf("source_path is required")
	}
	if err := r.ensureSchemaCurrent(ctx); err != nil {
		return err
	}
	normalized := filepath.ToSlash(sourcePath)
	if _, err := r.db.ExecContext(ctx, `
UPDATE graph_edges
SET stale = 1, updated_at = ?
WHERE project_id = ?
  AND stale = 0
  AND to_node_id IN (
    SELECT id FROM graph_nodes WHERE project_id = ? AND source_path = ?
  )
`, formatTime(time.Now().UTC()), projectID, projectID, normalized); err != nil {
		return fmt.Errorf("mark graph inbound edges stale for %s: %w", normalized, err)
	}
	return nil
}

// PurgeStaleEdges deletes stale edges older than the supplied threshold.
func (r *SQLiteRepository) PurgeStaleEdges(ctx context.Context, projectID string, olderThan time.Time) (int, error) {
	if projectID == "" {
		return 0, fmt.Errorf("project_id is required")
	}
	if olderThan.IsZero() {
		return 0, fmt.Errorf("older_than is required")
	}
	if err := r.ensureSchemaCurrent(ctx); err != nil {
		return 0, err
	}
	result, err := r.db.ExecContext(ctx,
		`DELETE FROM graph_edges WHERE project_id = ? AND stale = 1 AND updated_at < ?`,
		projectID,
		formatTime(olderThan),
	)
	if err != nil {
		return 0, fmt.Errorf("purge stale graph edges: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count purged stale graph edges: %w", err)
	}
	return int(deleted), nil
}

func (r *SQLiteRepository) upsertNodeTx(ctx context.Context, tx *sql.Tx, node Node) (Node, error) {
	node, err := normalizeNode(node)
	if err != nil {
		return Node{}, err
	}
	now := time.Now().UTC()
	if node.CreatedAt.IsZero() {
		node.CreatedAt = now
	}
	node.UpdatedAt = now
	metadataJSON, err := json.Marshal(node.Metadata)
	if err != nil {
		return Node{}, fmt.Errorf("marshal node metadata: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO graph_nodes (
	id, project_id, kind, key, source_path, name, language, symbol_kind,
	start_line, end_line, metadata_json, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(project_id, kind, key) DO UPDATE SET
	source_path = excluded.source_path,
	name = excluded.name,
	language = excluded.language,
	symbol_kind = excluded.symbol_kind,
	start_line = excluded.start_line,
	end_line = excluded.end_line,
	metadata_json = excluded.metadata_json,
	updated_at = excluded.updated_at
`, node.ID, node.ProjectID, node.Kind, node.Key, node.SourcePath, node.Name, node.Language,
		node.SymbolKind, node.StartLine, node.EndLine, string(metadataJSON), formatTime(node.CreatedAt), formatTime(node.UpdatedAt))
	if err != nil {
		return Node{}, fmt.Errorf("upsert graph node %s: %w", node.Key, err)
	}
	return node, nil
}

func (r *SQLiteRepository) execUpsertEdge(ctx context.Context, exec interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, edge Edge) error {
	now := time.Now().UTC()
	if edge.CreatedAt.IsZero() {
		edge.CreatedAt = now
	}
	edge.UpdatedAt = now
	evidenceJSON, err := json.Marshal(edge.Evidence)
	if err != nil {
		return fmt.Errorf("marshal edge evidence: %w", err)
	}
	stale := 0
	if edge.Stale {
		stale = 1
	}
	_, err = exec.ExecContext(ctx, `
INSERT INTO graph_edges (
	id, project_id, kind, from_node_id, to_node_id, extractor, source_path,
	evidence_json, confidence, confidence_label, stale, source_version, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(project_id, extractor, source_path, kind, from_node_id, to_node_id) DO UPDATE SET
	evidence_json = excluded.evidence_json,
	confidence = excluded.confidence,
	confidence_label = excluded.confidence_label,
	stale = excluded.stale,
	source_version = excluded.source_version,
	updated_at = excluded.updated_at
`, edge.ID, edge.ProjectID, edge.Kind, edge.FromNodeID, edge.ToNodeID, edge.Extractor,
		edge.SourcePath, string(evidenceJSON), edge.Confidence, edge.ConfidenceLabel, stale, edge.SourceVersion,
		formatTime(edge.CreatedAt), formatTime(edge.UpdatedAt))
	if err != nil {
		return fmt.Errorf("upsert graph edge %s: %w", edge.NaturalKey(), err)
	}
	return nil
}

func (r *SQLiteRepository) ensureEdgeEndpoints(ctx context.Context, edge Edge) error {
	return ensureEdgeEndpointsWithQuery(ctx, r.db, edge)
}

func (r *SQLiteRepository) ensureEdgeEndpointsTx(ctx context.Context, tx *sql.Tx, edge Edge) error {
	return ensureEdgeEndpointsWithQuery(ctx, tx, edge)
}

func ensureEdgeEndpointsWithQuery(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, edge Edge) error {
	var count int
	if edge.FromNodeID == edge.ToNodeID {
		if err := q.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM graph_nodes WHERE project_id = ? AND id = ?`,
			edge.ProjectID, edge.FromNodeID).Scan(&count); err != nil {
			return fmt.Errorf("check graph edge endpoints: %w", err)
		}
		if count != 1 {
			return fmt.Errorf("orphan graph edge endpoint in %s", edge.NaturalKey())
		}
		return nil
	}

	if err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM graph_nodes WHERE project_id = ? AND id IN (?, ?)`,
		edge.ProjectID, edge.FromNodeID, edge.ToNodeID).Scan(&count); err != nil {
		return fmt.Errorf("check graph edge endpoints: %w", err)
	}
	if count != 2 {
		return fmt.Errorf("orphan graph edge endpoint in %s", edge.NaturalKey())
	}
	return nil
}

// ListNodes returns graph nodes sorted by stable ID.
func (r *SQLiteRepository) ListNodes(ctx context.Context, query NodeQuery) ([]Node, error) {
	sqlQuery := `SELECT id, project_id, kind, key, source_path, name, language, symbol_kind, start_line, end_line, metadata_json, created_at, updated_at FROM graph_nodes WHERE 1=1`
	args := []any{}
	if query.ProjectID != "" {
		sqlQuery += ` AND project_id = ?`
		args = append(args, query.ProjectID)
	}
	if query.Kind != "" {
		sqlQuery += ` AND kind = ?`
		args = append(args, query.Kind)
	}
	sqlQuery += ` ORDER BY id ASC`
	rows, err := r.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("list graph nodes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var nodes []Node
	for rows.Next() {
		node, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate graph nodes: %w", err)
	}
	return nodes, nil
}

// ListEdges returns graph edges sorted by deterministic natural key.
func (r *SQLiteRepository) ListEdges(ctx context.Context, query EdgeQuery) ([]Edge, error) {
	sqlQuery := `SELECT id, project_id, kind, from_node_id, to_node_id, extractor, source_path, source_version, evidence_json, confidence, confidence_label, stale, created_at, updated_at FROM graph_edges WHERE 1=1`
	args := []any{}
	if query.ProjectID != "" {
		sqlQuery += ` AND project_id = ?`
		args = append(args, query.ProjectID)
	}
	if query.Kind != "" {
		sqlQuery += ` AND kind = ?`
		args = append(args, query.Kind)
	}
	if query.Extractor != "" {
		sqlQuery += ` AND extractor = ?`
		args = append(args, query.Extractor)
	}
	if query.SourcePath != "" {
		sqlQuery += ` AND source_path = ?`
		args = append(args, query.SourcePath)
	}
	if query.ExcludeStale {
		sqlQuery += ` AND stale = 0`
	}
	if query.OnlyStale {
		sqlQuery += ` AND stale = 1`
	}
	sqlQuery += ` ORDER BY project_id, extractor, source_path, kind, from_node_id, to_node_id ASC`
	rows, err := r.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("list graph edges: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var edges []Edge
	for rows.Next() {
		edge, err := scanEdge(rows)
		if err != nil {
			return nil, err
		}
		edges = append(edges, edge)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate graph edges: %w", err)
	}
	return edges, nil
}

// RecordBuild stores the latest build metadata for a project/build kind.
func (r *SQLiteRepository) RecordBuild(ctx context.Context, metadata BuildMetadata) error {
	if metadata.ProjectID == "" {
		return fmt.Errorf("project_id is required")
	}
	if err := r.ensureSchemaCurrent(ctx); err != nil {
		return err
	}
	if metadata.Kind == "" {
		metadata.Kind = BuildKindFull
	}
	if metadata.Status == "" {
		metadata.Status = GraphStatusFresh
	}
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx, `
INSERT INTO graph_builds (project_id, build_kind, status, started_at, completed_at, source_version, message, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(project_id, build_kind) DO UPDATE SET
	status = excluded.status,
	started_at = excluded.started_at,
	completed_at = excluded.completed_at,
	source_version = excluded.source_version,
	message = excluded.message,
	updated_at = excluded.updated_at
`, metadata.ProjectID, metadata.Kind, metadata.Status, formatTime(metadata.StartedAt), formatTime(metadata.CompletedAt),
		metadata.SourceVersion, metadata.Message, formatTime(now))
	if err != nil {
		return fmt.Errorf("record graph build: %w", err)
	}
	return nil
}

// RecordExtractorRun stores the latest run metadata for one extractor/source.
func (r *SQLiteRepository) RecordExtractorRun(ctx context.Context, run ExtractorRun) error {
	if err := r.ensureSchemaCurrent(ctx); err != nil {
		return err
	}
	return r.recordExtractorRun(ctx, r.db, run)
}

func (r *SQLiteRepository) recordExtractorRun(ctx context.Context, exec interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, run ExtractorRun) error {
	if run.ProjectID == "" {
		return fmt.Errorf("project_id is required")
	}
	if run.Extractor == "" {
		return fmt.Errorf("extractor is required")
	}
	if run.SourcePath == "" {
		return fmt.Errorf("source_path is required")
	}
	if run.Status == "" {
		run.Status = ExtractorStatusSuccess
	}
	return r.recordExtractorRunExec(ctx, exec, run)
}

func (r *SQLiteRepository) recordExtractorRunTx(ctx context.Context, tx *sql.Tx, run ExtractorRun) error {
	return r.recordExtractorRun(ctx, tx, run)
}

func (r *SQLiteRepository) recordExtractorRunExec(ctx context.Context, exec interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, run ExtractorRun) error {
	warningsJSON, err := json.Marshal(run.Warnings)
	if err != nil {
		return fmt.Errorf("marshal extractor warnings: %w", err)
	}
	errorsJSON, err := json.Marshal(run.Errors)
	if err != nil {
		return fmt.Errorf("marshal extractor errors: %w", err)
	}
	if run.WarningCount == 0 {
		run.WarningCount = len(run.Warnings)
	}
	if run.ErrorCount == 0 {
		run.ErrorCount = len(run.Errors)
	}
	now := time.Now().UTC()
	_, err = exec.ExecContext(ctx, `
INSERT INTO graph_extractor_runs (
	project_id, extractor, source_path, status, started_at, completed_at,
	node_count, edge_count, warning_count, error_count, warnings_json, errors_json, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(project_id, extractor, source_path) DO UPDATE SET
	status = excluded.status,
	started_at = excluded.started_at,
	completed_at = excluded.completed_at,
	node_count = excluded.node_count,
	edge_count = excluded.edge_count,
	warning_count = excluded.warning_count,
	error_count = excluded.error_count,
	warnings_json = excluded.warnings_json,
	errors_json = excluded.errors_json,
	updated_at = excluded.updated_at
`, run.ProjectID, run.Extractor, run.SourcePath, run.Status, formatTime(run.StartedAt), formatTime(run.CompletedAt),
		run.NodeCount, run.EdgeCount, run.WarningCount, run.ErrorCount, string(warningsJSON), string(errorsJSON), formatTime(now))
	if err != nil {
		return fmt.Errorf("record graph extractor run: %w", err)
	}
	return nil
}

// Reset clears graph data while preserving the graph schema.
func (r *SQLiteRepository) Reset(ctx context.Context) error {
	if err := r.ensureSchemaCurrent(ctx); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin graph reset: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range []string{
		`DELETE FROM graph_edges`,
		`DELETE FROM graph_nodes`,
		`DELETE FROM graph_extractor_runs`,
		`DELETE FROM graph_builds`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("reset graph data: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit graph reset: %w", err)
	}
	return nil
}

// Snapshot returns stored graph health and counts without rescanning source files.
func (r *SQLiteRepository) Snapshot(ctx context.Context, opts StatusOptions) (*StatusSnapshot, error) {
	now := effectiveNow(opts.Now)
	staleAfter := opts.StaleAfter
	if staleAfter <= 0 {
		staleAfter = DefaultStaleAfter
	}

	snapshot := &StatusSnapshot{
		Available:     true,
		SchemaVersion: SchemaVersion,
		Status:        GraphStatusEmpty,
		GeneratedAt:   now,
		Freshness: Freshness{
			State:             FreshnessUnknown,
			StaleAfterSeconds: int64(staleAfter.Seconds()),
		},
		Nodes:       CountSummary{ByKind: map[string]int{}},
		Edges:       CountSummary{ByKind: map[string]int{}},
		ActiveEdges: CountSummary{ByKind: map[string]int{}},
		StaleEdges:  CountSummary{ByKind: map[string]int{}},
		Confidence:  map[string]int{},
	}

	version, err := r.schemaVersion(ctx)
	if err != nil {
		return nil, err
	}
	snapshot.SchemaVersion = version
	if version != SchemaVersion {
		snapshot.Available = false
		snapshot.Status = GraphStatusIncompatible
		snapshot.Warnings = append(snapshot.Warnings, StatusWarning{
			Code:    WarningSchemaIncompatible,
			Message: fmt.Sprintf("graph schema version %d is incompatible with expected %d", version, SchemaVersion),
		})
		return snapshot, nil
	}

	if err := r.populateCounts(ctx, opts.ProjectID, snapshot); err != nil {
		return nil, err
	}
	fullBuild, hasFullBuild, err := r.latestBuild(ctx, opts.ProjectID, BuildKindFull)
	if err != nil {
		return nil, err
	}
	incrementalBuild, hasIncrementalBuild, err := r.latestBuild(ctx, opts.ProjectID, BuildKindIncremental)
	if err != nil {
		return nil, err
	}
	if hasFullBuild {
		snapshot.LastFullBuild = buildTiming(fullBuild)
	}
	if hasIncrementalBuild {
		snapshot.LastIncrementalUpdate = buildTiming(incrementalBuild)
	}
	build, ok, err := r.latestBuildAny(ctx, opts.ProjectID)
	if err != nil {
		return nil, err
	}
	if ok {
		snapshot.Status = build.Status
		snapshot.Freshness.StartedAt = formatTime(build.StartedAt)
		snapshot.Freshness.CompletedAt = formatTime(build.CompletedAt)
		snapshot.Freshness.SourceVersion = build.SourceVersion
		if !build.CompletedAt.IsZero() {
			age := now.Sub(build.CompletedAt)
			if age < 0 {
				age = 0
			}
			snapshot.Freshness.AgeSeconds = int64(age.Seconds())
			if age > staleAfter && build.Status == GraphStatusFresh {
				snapshot.Status = GraphStatusStale
				snapshot.Freshness.State = FreshnessStale
				snapshot.Warnings = append(snapshot.Warnings, StatusWarning{
					Code:    WarningGraphStale,
					Message: fmt.Sprintf("graph build is stale: age %s exceeds %s", age.Round(time.Second), staleAfter),
				})
			} else {
				snapshot.Freshness.State = FreshnessFresh
			}
		}
		if build.Status == GraphStatusFailed {
			snapshot.Warnings = append(snapshot.Warnings, StatusWarning{Code: WarningBuildFailed, Message: build.Message})
		}
	}

	extractors, warnings, err := r.extractorSummaries(ctx, opts.ProjectID)
	if err != nil {
		return nil, err
	}
	snapshot.Extractors = extractors
	snapshot.Warnings = append(snapshot.Warnings, warnings...)
	if !ok && (snapshot.Nodes.Total > 0 || snapshot.Edges.Total > 0) {
		snapshot.Status = GraphStatusFresh
		snapshot.Freshness.State = FreshnessUnknown
	}

	if snapshot.Nodes.Total == 0 && snapshot.Edges.Total == 0 && !ok && len(snapshot.Extractors) == 0 {
		snapshot.Status = GraphStatusEmpty
	}
	return snapshot, nil
}

func (r *SQLiteRepository) schemaVersion(ctx context.Context) (int, error) {
	var version int
	if err := r.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM graph_schema_version`).Scan(&version); err != nil {
		return 0, fmt.Errorf("read graph schema version: %w", err)
	}
	return version, nil
}

func (r *SQLiteRepository) ensureSchemaCurrent(ctx context.Context) error {
	r.schemaMu.RLock()
	if r.schemaCurrent {
		r.schemaMu.RUnlock()
		return nil
	}
	r.schemaMu.RUnlock()

	version, err := r.schemaVersion(ctx)
	if err != nil {
		return err
	}
	if version != SchemaVersion {
		return fmt.Errorf("graph schema version %d incompatible with binary version %d", version, SchemaVersion)
	}

	r.schemaMu.Lock()
	r.schemaCurrent = true
	r.schemaMu.Unlock()
	return nil
}

func (r *SQLiteRepository) invalidateSchemaCheck() {
	r.schemaMu.Lock()
	r.schemaCurrent = false
	r.schemaMu.Unlock()
}

func (r *SQLiteRepository) populateCounts(ctx context.Context, projectID string, snapshot *StatusSnapshot) error {
	nodeRows, err := r.db.QueryContext(ctx, `SELECT kind, COUNT(*) FROM graph_nodes WHERE (? = '' OR project_id = ?) GROUP BY kind`, projectID, projectID)
	if err != nil {
		return fmt.Errorf("count graph nodes: %w", err)
	}
	defer func() { _ = nodeRows.Close() }()
	for nodeRows.Next() {
		var kind string
		var count int
		if err := nodeRows.Scan(&kind, &count); err != nil {
			return fmt.Errorf("scan graph node count: %w", err)
		}
		snapshot.Nodes.ByKind[kind] = count
		snapshot.Nodes.Total += count
	}
	if err := nodeRows.Err(); err != nil {
		return fmt.Errorf("iterate graph node counts: %w", err)
	}

	edgeRows, err := r.db.QueryContext(ctx, `SELECT kind, COUNT(*) FROM graph_edges WHERE (? = '' OR project_id = ?) GROUP BY kind`, projectID, projectID)
	if err != nil {
		return fmt.Errorf("count graph edges: %w", err)
	}
	defer func() { _ = edgeRows.Close() }()
	for edgeRows.Next() {
		var kind string
		var count int
		if err := edgeRows.Scan(&kind, &count); err != nil {
			return fmt.Errorf("scan graph edge count: %w", err)
		}
		snapshot.Edges.ByKind[kind] = count
		snapshot.Edges.Total += count
	}
	if err := edgeRows.Err(); err != nil {
		return fmt.Errorf("iterate graph edge counts: %w", err)
	}

	activeRows, err := r.db.QueryContext(ctx, `SELECT kind, COUNT(*) FROM graph_edges WHERE (? = '' OR project_id = ?) AND stale = 0 GROUP BY kind`, projectID, projectID)
	if err != nil {
		return fmt.Errorf("count active graph edges: %w", err)
	}
	defer func() { _ = activeRows.Close() }()
	for activeRows.Next() {
		var kind string
		var count int
		if err := activeRows.Scan(&kind, &count); err != nil {
			return fmt.Errorf("scan active graph edge count: %w", err)
		}
		snapshot.ActiveEdges.ByKind[kind] = count
		snapshot.ActiveEdges.Total += count
	}
	if err := activeRows.Err(); err != nil {
		return fmt.Errorf("iterate active graph edge counts: %w", err)
	}

	staleRows, err := r.db.QueryContext(ctx, `SELECT kind, COUNT(*) FROM graph_edges WHERE (? = '' OR project_id = ?) AND stale = 1 GROUP BY kind`, projectID, projectID)
	if err != nil {
		return fmt.Errorf("count stale graph edges: %w", err)
	}
	defer func() { _ = staleRows.Close() }()
	for staleRows.Next() {
		var kind string
		var count int
		if err := staleRows.Scan(&kind, &count); err != nil {
			return fmt.Errorf("scan stale graph edge count: %w", err)
		}
		snapshot.StaleEdges.ByKind[kind] = count
		snapshot.StaleEdges.Total += count
	}
	if err := staleRows.Err(); err != nil {
		return fmt.Errorf("iterate stale graph edge counts: %w", err)
	}
	if snapshot.StaleEdges.Total > 0 {
		snapshot.Warnings = append(snapshot.Warnings, StatusWarning{
			Code:    WarningGraphStaleEdges,
			Message: fmt.Sprintf("graph contains %d stale edge(s)", snapshot.StaleEdges.Total),
		})
	}

	confidenceRows, err := r.db.QueryContext(ctx, `SELECT confidence_label, COUNT(*) FROM graph_edges WHERE (? = '' OR project_id = ?) GROUP BY confidence_label`, projectID, projectID)
	if err != nil {
		return fmt.Errorf("count graph confidence labels: %w", err)
	}
	defer func() { _ = confidenceRows.Close() }()
	for confidenceRows.Next() {
		var label string
		var count int
		if err := confidenceRows.Scan(&label, &count); err != nil {
			return fmt.Errorf("scan graph confidence count: %w", err)
		}
		snapshot.Confidence[label] = count
	}
	return confidenceRows.Err()
}

func buildTiming(build BuildMetadata) *BuildTiming {
	return &BuildTiming{
		StartedAt:     formatTime(build.StartedAt),
		CompletedAt:   formatTime(build.CompletedAt),
		SourceVersion: build.SourceVersion,
	}
}

func (r *SQLiteRepository) latestBuild(ctx context.Context, projectID string, kind BuildKind) (BuildMetadata, bool, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT project_id, build_kind, status, started_at, completed_at, source_version, message
FROM graph_builds
WHERE (? = '' OR project_id = ?) AND build_kind = ?
ORDER BY updated_at DESC
LIMIT 1`, projectID, projectID, kind)
	return scanBuildMetadata(row)
}

func (r *SQLiteRepository) latestBuildAny(ctx context.Context, projectID string) (BuildMetadata, bool, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT project_id, build_kind, status, started_at, completed_at, source_version, message
FROM graph_builds
WHERE (? = '' OR project_id = ?)
ORDER BY updated_at DESC
LIMIT 1`, projectID, projectID)
	return scanBuildMetadata(row)
}

func scanBuildMetadata(row *sql.Row) (BuildMetadata, bool, error) {
	var build BuildMetadata
	var startedAt, completedAt string
	err := row.Scan(&build.ProjectID, &build.Kind, &build.Status, &startedAt, &completedAt, &build.SourceVersion, &build.Message)
	if errors.Is(err, sql.ErrNoRows) {
		return BuildMetadata{}, false, nil
	}
	if err != nil {
		return BuildMetadata{}, false, fmt.Errorf("read graph build metadata: %w", err)
	}
	build.StartedAt, err = parseTime(startedAt)
	if err != nil {
		return BuildMetadata{}, false, fmt.Errorf("parse graph build started_at: %w", err)
	}
	build.CompletedAt, err = parseTime(completedAt)
	if err != nil {
		return BuildMetadata{}, false, fmt.Errorf("parse graph build completed_at: %w", err)
	}
	return build, true, nil
}

func (r *SQLiteRepository) extractorSummaries(ctx context.Context, projectID string) ([]ExtractorSummary, []StatusWarning, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT extractor, source_path, status, completed_at, node_count, edge_count, warning_count, error_count, warnings_json, errors_json
FROM graph_extractor_runs
WHERE (? = '' OR project_id = ?)
ORDER BY extractor ASC, source_path ASC`, projectID, projectID)
	if err != nil {
		return nil, nil, fmt.Errorf("read graph extractor runs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var summaries []ExtractorSummary
	var warnings []StatusWarning
	for rows.Next() {
		var summary ExtractorSummary
		var completedAt string
		var warningsJSON, errorsJSON string
		if err := rows.Scan(&summary.Name, &summary.SourcePath, &summary.Status, &completedAt, &summary.NodeCount,
			&summary.EdgeCount, &summary.WarningCount, &summary.ErrorCount, &warningsJSON, &errorsJSON); err != nil {
			return nil, nil, fmt.Errorf("scan graph extractor run: %w", err)
		}
		summary.CompletedAt = completedAt
		var runWarnings []string
		var runErrors []string
		if err := json.Unmarshal([]byte(warningsJSON), &runWarnings); err != nil {
			return nil, nil, fmt.Errorf("parse graph extractor warnings for %s/%s: %w", summary.Name, summary.SourcePath, err)
		}
		if err := json.Unmarshal([]byte(errorsJSON), &runErrors); err != nil {
			return nil, nil, fmt.Errorf("parse graph extractor errors for %s/%s: %w", summary.Name, summary.SourcePath, err)
		}
		if len(runErrors) > 0 {
			summary.Message = runErrors[0]
		} else if len(runWarnings) > 0 {
			summary.Message = runWarnings[0]
		}
		switch summary.Status {
		case ExtractorStatusFailed:
			warnings = append(warnings, StatusWarning{
				Code:       WarningExtractorFailed,
				Message:    summary.Message,
				Extractor:  summary.Name,
				SourcePath: summary.SourcePath,
			})
		case ExtractorStatusPartial:
			warnings = append(warnings, StatusWarning{
				Code:       WarningExtractorPartial,
				Message:    summary.Message,
				Extractor:  summary.Name,
				SourcePath: summary.SourcePath,
			})
		}
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate graph extractor runs: %w", err)
	}
	return summaries, warnings, nil
}

func scanNode(scanner interface {
	Scan(dest ...any) error
}) (Node, error) {
	var node Node
	var kind string
	var metadataJSON, createdAt, updatedAt string
	if err := scanner.Scan(&node.ID, &node.ProjectID, &kind, &node.Key, &node.SourcePath, &node.Name,
		&node.Language, &node.SymbolKind, &node.StartLine, &node.EndLine, &metadataJSON, &createdAt, &updatedAt); err != nil {
		return Node{}, fmt.Errorf("scan graph node: %w", err)
	}
	node.Kind = NodeKind(kind)
	if err := json.Unmarshal([]byte(metadataJSON), &node.Metadata); err != nil {
		return Node{}, fmt.Errorf("parse graph node metadata for %s: %w", node.ID, err)
	}
	var err error
	node.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return Node{}, fmt.Errorf("parse graph node created_at for %s: %w", node.ID, err)
	}
	node.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return Node{}, fmt.Errorf("parse graph node updated_at for %s: %w", node.ID, err)
	}
	return node, nil
}

func scanEdge(scanner interface {
	Scan(dest ...any) error
}) (Edge, error) {
	var edge Edge
	var kind, confidenceLabel, evidenceJSON, createdAt, updatedAt string
	var stale int
	if err := scanner.Scan(&edge.ID, &edge.ProjectID, &kind, &edge.FromNodeID, &edge.ToNodeID, &edge.Extractor,
		&edge.SourcePath, &edge.SourceVersion, &evidenceJSON, &edge.Confidence, &confidenceLabel, &stale, &createdAt, &updatedAt); err != nil {
		return Edge{}, fmt.Errorf("scan graph edge: %w", err)
	}
	edge.Kind = EdgeKind(kind)
	edge.ConfidenceLabel = ConfidenceLabel(confidenceLabel)
	edge.Stale = stale != 0
	if err := json.Unmarshal([]byte(evidenceJSON), &edge.Evidence); err != nil {
		return Edge{}, fmt.Errorf("parse graph edge evidence for %s: %w", edge.ID, err)
	}
	var err error
	edge.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return Edge{}, fmt.Errorf("parse graph edge created_at for %s: %w", edge.ID, err)
	}
	edge.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return Edge{}, fmt.Errorf("parse graph edge updated_at for %s: %w", edge.ID, err)
	}
	return edge, nil
}

func parseTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err == nil {
		return parsed, nil
	}
	parsed, fallbackErr := time.Parse("2006-01-02 15:04:05", value)
	if fallbackErr == nil {
		return parsed, nil
	}
	return time.Time{}, fmt.Errorf("invalid graph timestamp %q: %w", value, err)
}

func (r *SQLiteRepository) setSchemaVersionForTest(ctx context.Context, version int) error {
	r.invalidateSchemaCheck()
	_, err := r.db.ExecContext(ctx, `DELETE FROM graph_schema_version`)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO graph_schema_version(version, applied_at) VALUES (?, CURRENT_TIMESTAMP)`, version)
	r.invalidateSchemaCheck()
	return err
}

func sortEdgesByNaturalKey(edges []Edge) {
	sort.Slice(edges, func(i, j int) bool {
		return edges[i].NaturalKey() < edges[j].NaturalKey()
	})
}
