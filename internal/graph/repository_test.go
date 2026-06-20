package graph

import (
	"context"
	"database/sql"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestSQLiteRepository(t *testing.T) *SQLiteRepository {
	t.Helper()

	repo, err := OpenSQLiteRepository(filepath.Join(t.TempDir(), "graph.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, repo.Close())
	})
	return repo
}

func TestSQLiteRepository_NodeAndEdgeUpsertAreIdempotent(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)

	file, err := repo.UpsertNode(ctx, Node{
		ProjectID:  "project-1",
		Kind:       NodeKindFile,
		Key:        "internal/graph/store.go",
		SourcePath: "internal/graph/store.go",
		Name:       "store.go",
	})
	require.NoError(t, err)

	fileAgain, err := repo.UpsertNode(ctx, Node{
		ProjectID:  "project-1",
		Kind:       NodeKindFile,
		Key:        "internal/graph/store.go",
		SourcePath: "internal/graph/store.go",
		Name:       "store.go",
	})
	require.NoError(t, err)
	assert.Equal(t, file.ID, fileAgain.ID)

	symbol, err := repo.UpsertNode(ctx, Node{
		ProjectID:  "project-1",
		Kind:       NodeKindSymbol,
		Key:        "internal/graph/store.go#OpenSQLiteRepository:10",
		SourcePath: "internal/graph/store.go",
		Name:       "OpenSQLiteRepository",
		SymbolKind: "function",
		StartLine:  10,
		EndLine:    20,
	})
	require.NoError(t, err)

	edge := Edge{
		ProjectID:  "project-1",
		Kind:       EdgeKindFileDefinesSymbol,
		FromNodeID: file.ID,
		ToNodeID:   symbol.ID,
		Extractor:  ExtractorCheap,
		SourcePath: "internal/graph/store.go",
		Evidence: Evidence{
			Method:  "chunk_symbol",
			Snippet: "func OpenSQLiteRepository",
		},
		Confidence: 0.95,
	}

	first, err := repo.UpsertEdge(ctx, edge)
	require.NoError(t, err)
	second, err := repo.UpsertEdge(ctx, edge)
	require.NoError(t, err)
	assert.Equal(t, first.ID, second.ID)

	nodes, err := repo.ListNodes(ctx, NodeQuery{ProjectID: "project-1"})
	require.NoError(t, err)
	assert.Len(t, nodes, 2)

	edges, err := repo.ListEdges(ctx, EdgeQuery{ProjectID: "project-1"})
	require.NoError(t, err)
	assert.Len(t, edges, 1)
	assert.Equal(t, ConfidenceHigh, edges[0].ConfidenceLabel)
}

func TestSQLiteRepository_PersistsEdgeSourceVersion(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)

	file := upsertTestNode(t, ctx, repo, "project-1", NodeKindFile, "internal/graph/store.go")
	symbol := upsertTestNode(t, ctx, repo, "project-1", NodeKindSymbol, "internal/graph/store.go#OpenSQLiteRepository:10")

	_, err := repo.UpsertEdge(ctx, Edge{
		ProjectID:     "project-1",
		Kind:          EdgeKindFileDefinesSymbol,
		FromNodeID:    file.ID,
		ToNodeID:      symbol.ID,
		Extractor:     ExtractorCheap,
		SourcePath:    "internal/graph/store.go",
		SourceVersion: "sha256:abc123",
		Evidence:      Evidence{Method: "chunk_symbol"},
		Confidence:    0.95,
	})
	require.NoError(t, err)

	edges, err := repo.ListEdges(ctx, EdgeQuery{ProjectID: "project-1"})
	require.NoError(t, err)
	require.Len(t, edges, 1)
	assert.Equal(t, "sha256:abc123", edges[0].SourceVersion)
}

func TestSQLiteRepository_ReplaceEdgesByExtractorAndSourcePreservesUnrelatedEdges(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)

	fileA := upsertTestNode(t, ctx, repo, "project-1", NodeKindFile, "a.go")
	fileB := upsertTestNode(t, ctx, repo, "project-1", NodeKindFile, "b.go")
	symbolA := upsertTestNode(t, ctx, repo, "project-1", NodeKindSymbol, "a.go#A:3")
	symbolB := upsertTestNode(t, ctx, repo, "project-1", NodeKindSymbol, "b.go#B:3")
	importA := upsertTestNode(t, ctx, repo, "project-1", NodeKindImport, "fmt")

	require.NoError(t, repo.ReplaceEdges(ctx, EdgeReplacement{
		ProjectID:  "project-1",
		Extractor:  ExtractorCheap,
		SourcePath: "a.go",
		Edges: []Edge{{
			ProjectID:  "project-1",
			Kind:       EdgeKindFileDefinesSymbol,
			FromNodeID: fileA.ID,
			ToNodeID:   symbolA.ID,
			Extractor:  ExtractorCheap,
			SourcePath: "a.go",
			Confidence: 0.95,
			Evidence:   Evidence{Method: "old"},
		}},
		Run: ExtractorRun{
			Status:      ExtractorStatusSuccess,
			CompletedAt: fixedGraphTime(),
		},
	}))
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID:  "project-1",
		Kind:       EdgeKindPackageImports,
		FromNodeID: fileA.ID,
		ToNodeID:   importA.ID,
		Extractor:  "scip-go",
		SourcePath: "a.go",
		Confidence: 0.99,
		Evidence:   Evidence{Method: "scip"},
	}))
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID:  "project-1",
		Kind:       EdgeKindFileDefinesSymbol,
		FromNodeID: fileB.ID,
		ToNodeID:   symbolB.ID,
		Extractor:  ExtractorCheap,
		SourcePath: "b.go",
		Confidence: 0.95,
		Evidence:   Evidence{Method: "other_source"},
	}))

	require.NoError(t, repo.ReplaceEdges(ctx, EdgeReplacement{
		ProjectID:  "project-1",
		Extractor:  ExtractorCheap,
		SourcePath: "a.go",
		Edges: []Edge{{
			ProjectID:  "project-1",
			Kind:       EdgeKindFileDefinesSymbol,
			FromNodeID: fileA.ID,
			ToNodeID:   symbolB.ID,
			Extractor:  ExtractorCheap,
			SourcePath: "a.go",
			Confidence: 0.9,
			Evidence:   Evidence{Method: "new"},
		}},
		Run: ExtractorRun{
			Status:      ExtractorStatusSuccess,
			CompletedAt: fixedGraphTime(),
		},
	}))

	edges, err := repo.ListEdges(ctx, EdgeQuery{ProjectID: "project-1"})
	require.NoError(t, err)
	require.Len(t, edges, 3)

	keys := edgeNaturalKeys(edges)
	assert.Contains(t, keys, "project-1|cheap|a.go|file_defines_symbol|node:"+string(NodeKindFile)+":project-1:a.go|node:"+string(NodeKindSymbol)+":project-1:b.go#B:3")
	assert.Contains(t, keys, "project-1|cheap|b.go|file_defines_symbol|node:"+string(NodeKindFile)+":project-1:b.go|node:"+string(NodeKindSymbol)+":project-1:b.go#B:3")
	assert.Contains(t, keys, "project-1|scip-go|a.go|package_imports|node:"+string(NodeKindFile)+":project-1:a.go|node:"+string(NodeKindImport)+":project-1:fmt")
}

func TestSQLiteRepository_MarkEdgesToSourceStaleMarksInboundEdgesOnly(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)

	doc := upsertTestNode(t, ctx, repo, "project-1", NodeKindFile, "docs/design.md")
	impl := upsertTestNode(t, ctx, repo, "project-1", NodeKindFile, "internal/impl.go")
	other := upsertTestNode(t, ctx, repo, "project-1", NodeKindFile, "internal/other.go")

	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID:  "project-1",
		Kind:       EdgeKindDocMentionsPath,
		FromNodeID: doc.ID,
		ToNodeID:   impl.ID,
		Extractor:  ExtractorCheap,
		SourcePath: "docs/design.md",
		Evidence:   Evidence{Method: "test"},
		Confidence: 0.8,
	}))
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID:  "project-1",
		Kind:       EdgeKindDocMentionsPath,
		FromNodeID: doc.ID,
		ToNodeID:   other.ID,
		Extractor:  ExtractorCheap,
		SourcePath: "docs/design.md",
		Evidence:   Evidence{Method: "test"},
		Confidence: 0.8,
	}))

	require.NoError(t, repo.ReplaceEdges(ctx, EdgeReplacement{
		ProjectID:  "project-1",
		Extractor:  ExtractorCheap,
		SourcePath: "internal/impl.go",
		Run: ExtractorRun{
			Status:      ExtractorStatusSuccess,
			CompletedAt: fixedGraphTime(),
		},
	}))
	require.NoError(t, repo.MarkEdgesToSourceStale(ctx, "project-1", "internal/impl.go"))

	edges, err := repo.ListEdges(ctx, EdgeQuery{ProjectID: "project-1", SourcePath: "docs/design.md"})
	require.NoError(t, err)
	require.Len(t, edges, 2)

	staleByTarget := map[string]bool{}
	for _, edge := range edges {
		staleByTarget[edge.ToNodeID] = edge.Stale
	}
	assert.True(t, staleByTarget[impl.ID], "edge pointing to deleted source should be stale")
	assert.False(t, staleByTarget[other.ID], "unrelated inbound edge should remain active")
}

func TestSQLiteRepository_PurgeStaleEdgesUsesStrictOlderThanThreshold(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-1"
	doc := upsertTestNode(t, ctx, repo, projectID, NodeKindDoc, "docs/design.md")
	oldTarget := upsertTestNode(t, ctx, repo, projectID, NodeKindFile, "old.go")
	equalTarget := upsertTestNode(t, ctx, repo, projectID, NodeKindFile, "equal.go")
	freshTarget := upsertTestNode(t, ctx, repo, projectID, NodeKindFile, "fresh.go")
	activeTarget := upsertTestNode(t, ctx, repo, projectID, NodeKindFile, "active.go")

	for _, tc := range []struct {
		name   string
		target Node
		stale  bool
	}{
		{name: "old", target: oldTarget, stale: true},
		{name: "equal", target: equalTarget, stale: true},
		{name: "fresh", target: freshTarget, stale: true},
		{name: "active", target: activeTarget, stale: false},
	} {
		require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
			ProjectID:       projectID,
			Kind:            EdgeKindDocMentionsFile,
			FromNodeID:      doc.ID,
			ToNodeID:        tc.target.ID,
			Extractor:       ExtractorCheap,
			SourcePath:      "docs/design.md",
			Confidence:      0.9,
			ConfidenceLabel: ConfidenceHigh,
			Stale:           tc.stale,
			Evidence:        Evidence{Method: "test", SourcePath: "docs/design.md", LineStart: 1, LineEnd: 1},
		}), tc.name)
	}

	threshold := fixedGraphTime()
	setEdgeUpdatedAtForTest(t, repo, projectID, oldTarget.ID, threshold.Add(-time.Second))
	setEdgeUpdatedAtForTest(t, repo, projectID, equalTarget.ID, threshold)
	setEdgeUpdatedAtForTest(t, repo, projectID, freshTarget.ID, threshold.Add(time.Second))
	setEdgeUpdatedAtForTest(t, repo, projectID, activeTarget.ID, threshold.Add(-time.Hour))

	deleted, err := repo.PurgeStaleEdges(ctx, projectID, threshold)
	require.NoError(t, err)
	assert.Equal(t, 1, deleted)

	edges, err := repo.ListEdges(ctx, EdgeQuery{ProjectID: projectID})
	require.NoError(t, err)
	targets := map[string]bool{}
	for _, edge := range edges {
		targets[edge.ToNodeID] = true
	}
	assert.False(t, targets[oldTarget.ID])
	assert.True(t, targets[equalTarget.ID])
	assert.True(t, targets[freshTarget.ID])
	assert.True(t, targets[activeTarget.ID])
}

func TestSQLiteRepository_SnapshotReportsLastFullBuildAndIncrementalUpdate(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-build-times"
	fullStarted := fixedGraphTime().Add(-2 * time.Hour)
	fullCompleted := fixedGraphTime().Add(-90 * time.Minute)
	incrementalStarted := fixedGraphTime().Add(-10 * time.Minute)
	incrementalCompleted := fixedGraphTime().Add(-9 * time.Minute)

	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID:     projectID,
		Kind:          BuildKindFull,
		Status:        GraphStatusFresh,
		StartedAt:     fullStarted,
		CompletedAt:   fullCompleted,
		SourceVersion: "full-version",
	}))
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID:     projectID,
		Kind:          BuildKindIncremental,
		Status:        GraphStatusFresh,
		StartedAt:     incrementalStarted,
		CompletedAt:   incrementalCompleted,
		SourceVersion: "incremental-version",
	}))

	snapshot, err := repo.Snapshot(ctx, StatusOptions{ProjectID: projectID, Now: fixedGraphTime()})
	require.NoError(t, err)
	require.NotNil(t, snapshot.LastFullBuild)
	require.NotNil(t, snapshot.LastIncrementalUpdate)
	assert.Equal(t, formatTime(fullStarted), snapshot.LastFullBuild.StartedAt)
	assert.Equal(t, formatTime(fullCompleted), snapshot.LastFullBuild.CompletedAt)
	assert.Equal(t, "full-version", snapshot.LastFullBuild.SourceVersion)
	assert.Equal(t, formatTime(incrementalStarted), snapshot.LastIncrementalUpdate.StartedAt)
	assert.Equal(t, formatTime(incrementalCompleted), snapshot.LastIncrementalUpdate.CompletedAt)
	assert.Equal(t, "incremental-version", snapshot.LastIncrementalUpdate.SourceVersion)
	assert.Equal(t, formatTime(incrementalCompleted), snapshot.Freshness.CompletedAt)
}

func TestSQLiteRepository_RejectsInvalidConfidenceAndOrphanEdges(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)

	file := upsertTestNode(t, ctx, repo, "project-1", NodeKindFile, "a.go")
	symbol := upsertTestNode(t, ctx, repo, "project-1", NodeKindSymbol, "a.go#A:3")

	for _, confidence := range []float64{-0.01, 1.01, math.NaN()} {
		_, err := repo.UpsertEdge(ctx, Edge{
			ProjectID:  "project-1",
			Kind:       EdgeKindFileDefinesSymbol,
			FromNodeID: file.ID,
			ToNodeID:   symbol.ID,
			Extractor:  ExtractorCheap,
			SourcePath: "a.go",
			Confidence: confidence,
			Evidence:   Evidence{Method: "test"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "confidence")
	}

	_, err := repo.UpsertEdge(ctx, Edge{
		ProjectID:  "project-1",
		Kind:       EdgeKindFileDefinesSymbol,
		FromNodeID: "missing-from",
		ToNodeID:   symbol.ID,
		Extractor:  ExtractorCheap,
		SourcePath: "a.go",
		Confidence: 0.9,
		Evidence:   Evidence{Method: "test"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "orphan")
}

func TestSQLiteRepository_AcceptsSelfLoopEdges(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)

	node := upsertTestNode(t, ctx, repo, "project-1", NodeKindSymbol, "a.go#Recurse:3")

	edge := Edge{
		ProjectID:  "project-1",
		Kind:       EdgeKindSymbolHasChunk,
		FromNodeID: node.ID,
		ToNodeID:   node.ID,
		Extractor:  ExtractorCheap,
		SourcePath: "a.go",
		Confidence: 0.9,
		Evidence:   Evidence{Method: "recursive_symbol"},
	}
	_, err := repo.UpsertEdge(ctx, edge)
	require.NoError(t, err)

	require.NoError(t, repo.ReplaceEdges(ctx, EdgeReplacement{
		ProjectID:  "project-1",
		Extractor:  ExtractorCheap,
		SourcePath: "a.go",
		Edges:      []Edge{edge},
		Run: ExtractorRun{
			Status:      ExtractorStatusSuccess,
			CompletedAt: fixedGraphTime(),
		},
	}))

	edges, err := repo.ListEdges(ctx, EdgeQuery{ProjectID: "project-1"})
	require.NoError(t, err)
	require.Len(t, edges, 1)
	assert.Equal(t, node.ID, edges[0].FromNodeID)
	assert.Equal(t, node.ID, edges[0].ToNodeID)
}

func TestSQLiteRepository_FreshDatabaseCreationAndReset(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "fresh", "graph.db")

	repo, err := OpenSQLiteRepository(dbPath)
	require.NoError(t, err)
	file := upsertTestNode(t, ctx, repo, "project-1", NodeKindFile, "a.go")
	assert.NotEmpty(t, file.ID)
	require.NoError(t, repo.Close())

	reopened, err := OpenSQLiteRepository(dbPath)
	require.NoError(t, err)
	defer func() { require.NoError(t, reopened.Close()) }()

	nodes, err := reopened.ListNodes(ctx, NodeQuery{ProjectID: "project-1"})
	require.NoError(t, err)
	assert.Len(t, nodes, 1)

	require.NoError(t, reopened.Reset(ctx))
	nodes, err = reopened.ListNodes(ctx, NodeQuery{ProjectID: "project-1"})
	require.NoError(t, err)
	assert.Empty(t, nodes)
}

func TestSQLiteRepository_FreshSchemaMatchesCurrentContract(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)

	version, err := repo.schemaVersion(ctx)
	require.NoError(t, err)
	assert.Equal(t, SchemaVersion, version)
	require.Greater(t, SchemaVersion, 1, "TASK-GRA01 must replace the legacy v1 bootstrap with an explicit migration contract")

	assertTableColumns(t, repo.db, "graph_schema_version", "version", "applied_at", "description")
	assertTableColumns(t, repo.db, "graph_edges", "id", "project_id", "kind", "from_node_id", "to_node_id", "extractor", "source_path", "evidence_json", "confidence", "confidence_label", "stale", "source_version", "created_at", "updated_at")
	assertIndexes(t, repo.db, "graph_nodes",
		"idx_graph_nodes_project_kind",
		"idx_graph_nodes_project_source_path",
		"idx_graph_nodes_project_language",
	)
	assertIndexes(t, repo.db, "graph_edges",
		"idx_graph_edges_project_kind",
		"idx_graph_edges_project_from_node",
		"idx_graph_edges_project_to_node",
		"idx_graph_edges_project_source_path",
		"idx_graph_edges_project_extractor",
		"idx_graph_edges_project_stale",
		"idx_graph_edges_scope",
	)
}

func TestSQLiteRepository_MigratesLegacyVersion1DatabaseInPlace(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "legacy", "graph.db")
	createLegacyGraphV1Database(t, dbPath)

	repo, err := OpenSQLiteRepository(dbPath)
	require.NoError(t, err)
	defer func() { require.NoError(t, repo.Close()) }()

	version, err := repo.schemaVersion(ctx)
	require.NoError(t, err)
	assert.Equal(t, SchemaVersion, version)

	nodes, err := repo.ListNodes(ctx, NodeQuery{ProjectID: "project-1"})
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	assert.Equal(t, "a.go", nodes[0].SourcePath)

	assertTableColumns(t, repo.db, "graph_schema_version", "version", "applied_at", "description")
	assertTableColumns(t, repo.db, "graph_edges", "source_version")
}

func TestOpenSQLiteRepository_RejectsForwardIncompatibleDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "future", "graph.db")
	createLegacyGraphV1Database(t, dbPath)
	setRawGraphSchemaVersion(t, dbPath, SchemaVersion+1)

	repo, err := OpenSQLiteRepository(dbPath)
	require.Error(t, err)
	assert.Nil(t, repo)
	assert.Contains(t, err.Error(), "created by a newer AmanMCP")
	assert.Contains(t, err.Error(), "amanmcp index --force")
	assert.NotContains(t, err.Error(), "amanmcp graph rebuild")
}

func TestOpenSQLiteRepository_RejectsCorruptedDatabaseWithRebuildGuidance(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "corrupt", "graph.db")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0755))
	require.NoError(t, os.WriteFile(dbPath, []byte("not a sqlite database"), 0644))

	repo, err := OpenSQLiteRepository(dbPath)
	require.Error(t, err)
	assert.Nil(t, repo)
	assert.Contains(t, err.Error(), "corrupt")
	assert.Contains(t, err.Error(), "amanmcp index --force")
	assert.NotContains(t, err.Error(), "amanmcp graph rebuild")
}

func TestSQLiteRepository_StatusSnapshots(t *testing.T) {
	ctx := context.Background()

	t.Run("empty graph", func(t *testing.T) {
		repo := newTestSQLiteRepository(t)
		snapshot, err := repo.Snapshot(ctx, StatusOptions{
			ProjectID:  "project-1",
			Now:        fixedGraphTime(),
			StaleAfter: 24 * time.Hour,
		})
		require.NoError(t, err)
		assert.True(t, snapshot.Available)
		assert.Equal(t, GraphStatusEmpty, snapshot.Status)
		assert.Equal(t, 0, snapshot.Nodes.Total)
		assert.Equal(t, 0, snapshot.Edges.Total)
	})

	t.Run("fresh graph", func(t *testing.T) {
		repo := newTestSQLiteRepository(t)
		file := upsertTestNode(t, ctx, repo, "project-1", NodeKindFile, "a.go")
		symbol := upsertTestNode(t, ctx, repo, "project-1", NodeKindSymbol, "a.go#A:3")
		_, err := repo.UpsertEdge(ctx, Edge{
			ProjectID:  "project-1",
			Kind:       EdgeKindFileDefinesSymbol,
			FromNodeID: file.ID,
			ToNodeID:   symbol.ID,
			Extractor:  ExtractorCheap,
			SourcePath: "a.go",
			Confidence: 0.95,
			Evidence:   Evidence{Method: "test"},
		})
		require.NoError(t, err)
		require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
			ProjectID:     "project-1",
			Status:        GraphStatusFresh,
			StartedAt:     fixedGraphTime().Add(-time.Second),
			CompletedAt:   fixedGraphTime(),
			SourceVersion: "hash-1",
		}))
		require.NoError(t, repo.RecordExtractorRun(ctx, ExtractorRun{
			ProjectID:   "project-1",
			Extractor:   ExtractorCheap,
			SourcePath:  "a.go",
			Status:      ExtractorStatusSuccess,
			CompletedAt: fixedGraphTime(),
			NodeCount:   2,
			EdgeCount:   1,
		}))

		snapshot, err := repo.Snapshot(ctx, StatusOptions{
			ProjectID:  "project-1",
			Now:        fixedGraphTime().Add(time.Minute),
			StaleAfter: 24 * time.Hour,
		})
		require.NoError(t, err)
		assert.Equal(t, GraphStatusFresh, snapshot.Status)
		assert.Equal(t, 2, snapshot.Nodes.Total)
		assert.Equal(t, 1, snapshot.Edges.Total)
		assert.Equal(t, 1, snapshot.Edges.ByKind[string(EdgeKindFileDefinesSymbol)])
		assert.Equal(t, 1, snapshot.ActiveEdges.ByKind[string(EdgeKindFileDefinesSymbol)])
		assert.Equal(t, 0, snapshot.StaleEdges.Total)
		assert.Equal(t, 1, snapshot.Confidence[string(ConfidenceHigh)])
		require.Len(t, snapshot.Extractors, 1)
		assert.Equal(t, ExtractorStatusSuccess, snapshot.Extractors[0].Status)
	})

	t.Run("stale edge counts are separate", func(t *testing.T) {
		repo := newTestSQLiteRepository(t)
		doc := upsertTestNode(t, ctx, repo, "project-1", NodeKindDoc, "docs/design.md")
		file := upsertTestNode(t, ctx, repo, "project-1", NodeKindFile, "a.go")
		require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
			ProjectID:       "project-1",
			Kind:            EdgeKindDocMentionsFile,
			FromNodeID:      doc.ID,
			ToNodeID:        file.ID,
			Extractor:       ExtractorCheap,
			SourcePath:      "docs/design.md",
			Confidence:      0.9,
			ConfidenceLabel: ConfidenceHigh,
			Stale:           true,
			Evidence:        Evidence{Method: "test", SourcePath: "docs/design.md", LineStart: 1, LineEnd: 1},
		}))
		require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
			ProjectID:   "project-1",
			Status:      GraphStatusFresh,
			CompletedAt: fixedGraphTime(),
		}))

		snapshot, err := repo.Snapshot(ctx, StatusOptions{
			ProjectID:  "project-1",
			Now:        fixedGraphTime(),
			StaleAfter: 24 * time.Hour,
		})
		require.NoError(t, err)
		assert.Equal(t, 0, snapshot.ActiveEdges.Total)
		assert.Equal(t, 1, snapshot.StaleEdges.Total)
		assert.Equal(t, 1, snapshot.StaleEdges.ByKind[string(EdgeKindDocMentionsFile)])
		require.NotEmpty(t, snapshot.Warnings)
		assert.Equal(t, WarningGraphStaleEdges, snapshot.Warnings[0].Code)
	})

	t.Run("stale graph", func(t *testing.T) {
		repo := newTestSQLiteRepository(t)
		require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
			ProjectID:   "project-1",
			Status:      GraphStatusFresh,
			StartedAt:   fixedGraphTime().Add(-49 * time.Hour),
			CompletedAt: fixedGraphTime().Add(-48 * time.Hour),
		}))

		snapshot, err := repo.Snapshot(ctx, StatusOptions{
			ProjectID:  "project-1",
			Now:        fixedGraphTime(),
			StaleAfter: 24 * time.Hour,
		})
		require.NoError(t, err)
		assert.Equal(t, GraphStatusStale, snapshot.Status)
		assert.Equal(t, FreshnessStale, snapshot.Freshness.State)
		require.NotEmpty(t, snapshot.Warnings)
		assert.Equal(t, WarningGraphStale, snapshot.Warnings[0].Code)
	})

	t.Run("partial extractor failure", func(t *testing.T) {
		repo := newTestSQLiteRepository(t)
		require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
			ProjectID:   "project-1",
			Status:      GraphStatusPartial,
			StartedAt:   fixedGraphTime().Add(-time.Second),
			CompletedAt: fixedGraphTime(),
			Message:     "one extractor failed",
		}))
		require.NoError(t, repo.RecordExtractorRun(ctx, ExtractorRun{
			ProjectID:   "project-1",
			Extractor:   ExtractorCheap,
			SourcePath:  ".amanmcp.yaml",
			Status:      ExtractorStatusFailed,
			CompletedAt: fixedGraphTime(),
			Errors:      []string{"parse config: unexpected EOF"},
		}))

		snapshot, err := repo.Snapshot(ctx, StatusOptions{
			ProjectID:  "project-1",
			Now:        fixedGraphTime(),
			StaleAfter: 24 * time.Hour,
		})
		require.NoError(t, err)
		assert.Equal(t, GraphStatusPartial, snapshot.Status)
		require.NotEmpty(t, snapshot.Warnings)
		assert.Equal(t, WarningExtractorFailed, snapshot.Warnings[0].Code)
	})

	t.Run("incompatible metadata", func(t *testing.T) {
		repo := newTestSQLiteRepository(t)
		require.NoError(t, repo.setSchemaVersionForTest(ctx, 999))

		snapshot, err := repo.Snapshot(ctx, StatusOptions{
			ProjectID:  "project-1",
			Now:        fixedGraphTime(),
			StaleAfter: 24 * time.Hour,
		})
		require.NoError(t, err)
		assert.False(t, snapshot.Available)
		assert.Equal(t, GraphStatusIncompatible, snapshot.Status)
		require.NotEmpty(t, snapshot.Warnings)
		assert.Equal(t, WarningSchemaIncompatible, snapshot.Warnings[0].Code)
	})
}

func TestSQLiteRepository_RejectsWritesAgainstIncompatibleSchema(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)

	file := upsertTestNode(t, ctx, repo, "project-1", NodeKindFile, "a.go")
	symbol := upsertTestNode(t, ctx, repo, "project-1", NodeKindSymbol, "a.go#A:3")
	require.NoError(t, repo.setSchemaVersionForTest(ctx, SchemaVersion+1))

	_, err := repo.UpsertNode(ctx, Node{
		ProjectID:  "project-1",
		Kind:       NodeKindFile,
		Key:        "b.go",
		SourcePath: "b.go",
		Name:       "b.go",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "graph schema version")

	_, err = repo.UpsertEdge(ctx, Edge{
		ProjectID:  "project-1",
		Kind:       EdgeKindFileDefinesSymbol,
		FromNodeID: file.ID,
		ToNodeID:   symbol.ID,
		Extractor:  ExtractorCheap,
		SourcePath: "a.go",
		Confidence: 0.9,
		Evidence:   Evidence{Method: "test"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "graph schema version")

	err = repo.ReplaceEdges(ctx, EdgeReplacement{
		ProjectID:  "project-1",
		Extractor:  ExtractorCheap,
		SourcePath: "a.go",
		Edges: []Edge{{
			ProjectID:  "project-1",
			Kind:       EdgeKindFileDefinesSymbol,
			FromNodeID: file.ID,
			ToNodeID:   symbol.ID,
			Extractor:  ExtractorCheap,
			SourcePath: "a.go",
			Confidence: 0.9,
			Evidence:   Evidence{Method: "test"},
		}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "graph schema version")

	err = repo.RecordBuild(ctx, BuildMetadata{ProjectID: "project-1", Status: GraphStatusFresh})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "graph schema version")

	err = repo.RecordExtractorRun(ctx, ExtractorRun{
		ProjectID:  "project-1",
		Extractor:  ExtractorCheap,
		SourcePath: "a.go",
		Status:     ExtractorStatusSuccess,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "graph schema version")
}

func upsertTestNode(t *testing.T, ctx context.Context, repo *SQLiteRepository, projectID string, kind NodeKind, key string) Node {
	t.Helper()
	node, err := repo.UpsertNode(ctx, Node{
		ProjectID:  projectID,
		Kind:       kind,
		Key:        key,
		SourcePath: key,
		Name:       key,
	})
	require.NoError(t, err)
	return node
}

func setEdgeUpdatedAtForTest(t *testing.T, repo *SQLiteRepository, projectID, toNodeID string, updatedAt time.Time) {
	t.Helper()
	_, err := repo.db.Exec(
		`UPDATE graph_edges SET updated_at = ? WHERE project_id = ? AND to_node_id = ?`,
		formatTime(updatedAt),
		projectID,
		toNodeID,
	)
	require.NoError(t, err)
}

func edgeNaturalKeys(edges []Edge) []string {
	keys := make([]string, 0, len(edges))
	for _, edge := range edges {
		keys = append(keys, edge.NaturalKey())
	}
	return keys
}

func fixedGraphTime() time.Time {
	return time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
}

func createLegacyGraphV1Database(t *testing.T, dbPath string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0755))

	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	_, err = db.Exec(`
PRAGMA foreign_keys = ON;

CREATE TABLE graph_schema_version (
	version INTEGER PRIMARY KEY,
	applied_at TEXT NOT NULL
);

CREATE TABLE graph_nodes (
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

CREATE INDEX idx_graph_nodes_project_kind ON graph_nodes(project_id, kind);

CREATE TABLE graph_edges (
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

CREATE INDEX idx_graph_edges_project_kind ON graph_edges(project_id, kind);
CREATE INDEX idx_graph_edges_scope ON graph_edges(project_id, extractor, source_path);

CREATE TABLE graph_builds (
	project_id TEXT PRIMARY KEY,
	status TEXT NOT NULL,
	started_at TEXT NOT NULL DEFAULT '',
	completed_at TEXT NOT NULL DEFAULT '',
	source_version TEXT NOT NULL DEFAULT '',
	message TEXT NOT NULL DEFAULT '',
	updated_at TEXT NOT NULL
);

CREATE TABLE graph_extractor_runs (
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

INSERT INTO graph_schema_version (version, applied_at) VALUES (1, CURRENT_TIMESTAMP);
INSERT INTO graph_nodes (
	id, project_id, kind, key, source_path, name, language, symbol_kind,
	start_line, end_line, metadata_json, created_at, updated_at
) VALUES (
	'node:file:project-1:a.go', 'project-1', 'file', 'a.go', 'a.go', 'a.go', 'go', '',
	0, 0, '{}', '2026-05-01T12:00:00Z', '2026-05-01T12:00:00Z'
);
`)
	require.NoError(t, err)
}

func setRawGraphSchemaVersion(t *testing.T, dbPath string, version int) {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	_, err = db.Exec(`DELETE FROM graph_schema_version`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO graph_schema_version(version, applied_at) VALUES (?, CURRENT_TIMESTAMP)`, version)
	require.NoError(t, err)
}

func assertTableColumns(t *testing.T, db *sql.DB, table string, columns ...string) {
	t.Helper()

	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()

	seen := map[string]struct{}{}
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, pk int
		var defaultValue sql.NullString
		require.NoError(t, rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk))
		seen[name] = struct{}{}
	}
	require.NoError(t, rows.Err())

	for _, column := range columns {
		assert.Contains(t, seen, column, "missing column %s.%s", table, column)
	}
}

func assertIndexes(t *testing.T, db *sql.DB, table string, indexes ...string) {
	t.Helper()

	rows, err := db.Query(`PRAGMA index_list(` + table + `)`)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()

	seen := map[string]struct{}{}
	for rows.Next() {
		var seq int
		var name, unique, origin, partial any
		require.NoError(t, rows.Scan(&seq, &name, &unique, &origin, &partial))
		if indexName, ok := name.(string); ok {
			seen[indexName] = struct{}{}
		}
	}
	require.NoError(t, rows.Err())

	for _, index := range indexes {
		assert.Contains(t, seen, index, "missing index %s on %s", index, table)
	}
}
