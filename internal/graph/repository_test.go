package graph

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"

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
		assert.Equal(t, 1, snapshot.Confidence[string(ConfidenceHigh)])
		require.Len(t, snapshot.Extractors, 1)
		assert.Equal(t, ExtractorStatusSuccess, snapshot.Extractors[0].Status)
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
