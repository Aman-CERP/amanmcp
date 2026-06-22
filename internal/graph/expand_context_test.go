package graph

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryService_ExpandContext_ResolvesSymbolAndExpandsMultiHop(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	fixture := seedGraphServiceFixture(t, ctx, repo, GraphStatusFresh)
	otherFile := upsertFixtureNode(t, ctx, repo, Node{
		ProjectID: "project-1", Kind: NodeKindFile,
		Key: "internal/graph/query.go", SourcePath: "internal/graph/query.go", Name: "query.go",
	})
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID: "project-1", Kind: EdgeKindFileImports,
		FromNodeID: fixture.file.ID, ToNodeID: otherFile.ID,
		Extractor: ExtractorCheap, SourcePath: "internal/graph/service.go", Confidence: 0.8,
		Evidence: Evidence{Method: "go_import", Heuristic: true},
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	got, err := service.ExpandContext(ctx, ExpandContextRequest{
		ProjectID: "project-1",
		Seed:      "NewQueryService",
		SeedType:  SubjectTypeSymbol,
		Limit:     10,
		Depth:     2,
	})
	require.NoError(t, err)
	assert.Equal(t, ResolutionResolved, got.Resolution)
	assert.True(t, got.Available)
	require.NotEmpty(t, got.Pack)

	var otherItem *PackItem
	for i := range got.Pack {
		if got.Pack[i].NodeID == otherFile.ID {
			otherItem = &got.Pack[i]
			break
		}
	}
	require.NotNil(t, otherItem, "expected multi-hop pack item for imported file")
	require.NotEmpty(t, otherItem.Path.Hops, "pack item must carry graph path hops")
	assert.GreaterOrEqual(t, len(otherItem.Path.Hops), 2, "depth=2 expansion must surface 2+ hop path")
	assert.Equal(t, otherFile.ID, otherItem.Path.To.ID, "pack path must terminate at the claimed node")
	assert.NotContains(t, roleNamesFromPack(otherItem.Roles), ContextRoleCaller,
		"outbound import dependency must not be labeled caller")
}

func TestQueryService_ExpandContext_MultiHopSymbolCollisionAnchorsToResolvedSeed(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	fileA := upsertFixtureNode(t, ctx, repo, Node{
		ProjectID: "project-1", Kind: NodeKindFile,
		Key: "internal/a/search.go", SourcePath: "internal/a/search.go",
	})
	fileB := upsertFixtureNode(t, ctx, repo, Node{
		ProjectID: "project-1", Kind: NodeKindFile,
		Key: "internal/b/search.go", SourcePath: "internal/b/search.go",
	})
	chunkA := upsertFixtureNode(t, ctx, repo, Node{
		ProjectID: "project-1", Kind: NodeKindChunk,
		Key: "chunk:only-a", SourcePath: "internal/a/search.go",
	})
	symbolA := upsertFixtureNode(t, ctx, repo, Node{
		ProjectID: "project-1", Kind: NodeKindSymbol,
		Key: "internal/a/search.go#Search:10", SourcePath: "internal/a/search.go", Name: "Search", StartLine: 10,
	})
	symbolB := upsertFixtureNode(t, ctx, repo, Node{
		ProjectID: "project-1", Kind: NodeKindSymbol,
		Key: "internal/b/search.go#Search:20", SourcePath: "internal/b/search.go", Name: "Search", StartLine: 20,
	})
	onlyBNeighbor := upsertFixtureNode(t, ctx, repo, Node{
		ProjectID: "project-1", Kind: NodeKindChunk,
		Key: "chunk:only-b", SourcePath: "internal/b/search.go",
	})
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID: "project-1", Kind: EdgeKindFileDefinesSymbol,
		FromNodeID: fileA.ID, ToNodeID: symbolA.ID, Extractor: ExtractorCheap,
		SourcePath: "internal/a/search.go", Confidence: 1,
	}))
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID: "project-1", Kind: EdgeKindSymbolHasChunk,
		FromNodeID: symbolA.ID, ToNodeID: chunkA.ID, Extractor: ExtractorCheap,
		SourcePath: "internal/a/search.go", Confidence: 0.9,
	}))
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID: "project-1", Kind: EdgeKindFileDefinesSymbol,
		FromNodeID: fileB.ID, ToNodeID: symbolB.ID, Extractor: ExtractorCheap,
		SourcePath: "internal/b/search.go", Confidence: 1,
	}))
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID: "project-1", Kind: EdgeKindSymbolHasChunk,
		FromNodeID: symbolB.ID, ToNodeID: onlyBNeighbor.ID, Extractor: ExtractorCheap,
		SourcePath: "internal/b/search.go", Confidence: 0.9,
	}))
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID: "project-1", Status: GraphStatusFresh, CompletedAt: fixedGraphTime(),
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	got, err := service.ExpandContext(ctx, ExpandContextRequest{
		ProjectID: "project-1",
		Seed:      symbolA.ID,
		SeedType:  SubjectTypeResultID,
		Limit:     10,
		Depth:     2,
	})
	require.NoError(t, err)
	require.Equal(t, ResolutionResolved, got.Resolution)
	var foundChunkA bool
	for _, item := range got.Pack {
		assert.Equal(t, item.NodeID, item.Path.To.ID)
		if item.NodeID == chunkA.ID {
			foundChunkA = true
		}
		assert.NotEqual(t, onlyBNeighbor.ID, item.NodeID,
			"expansion anchored to symbolA must not include symbolB-only neighbors")
	}
	assert.True(t, foundChunkA, "expected neighbors of the resolved symbolA seed")
}

func TestQueryService_ExpandContext_MultiHopPathTerminatesAtTargetNode(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	fixture := seedGraphServiceFixture(t, ctx, repo, GraphStatusFresh)

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	got, err := service.ExpandContext(ctx, ExpandContextRequest{
		ProjectID: "project-1",
		Seed:      fixture.symbol.ID,
		SeedType:  SubjectTypeResultID,
		Limit:     10,
		Depth:     2,
	})
	require.NoError(t, err)
	require.NotEmpty(t, got.Pack)
	for _, item := range got.Pack {
		assert.Equal(t, item.NodeID, item.Path.To.ID,
			"every pack item path must terminate at the item node id")
	}
}

func roleNamesFromPack(roles []RoleAssignment) []ContextRole {
	out := make([]ContextRole, 0, len(roles))
	for _, role := range roles {
		out = append(out, role.Role)
	}
	return out
}

func TestQueryService_ExpandContext_DisambiguationReturnsCandidates(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	seedAmbiguousSymbolFixture(t, ctx, repo)

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	got, err := service.ExpandContext(ctx, ExpandContextRequest{
		ProjectID: "project-1",
		Seed:      "Search",
		SeedType:  SubjectTypeSymbol,
		Limit:     5,
		Depth:     1,
	})
	require.NoError(t, err)
	assert.Equal(t, ResolutionDisambiguationRequired, got.Resolution)
	assert.Empty(t, got.Pack)
	require.NotEmpty(t, got.Candidates)
}

func TestQueryService_ExpandContext_SubjectNotFoundReturnsHints(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	seedGraphServiceFixture(t, ctx, repo, GraphStatusFresh)

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	got, err := service.ExpandContext(ctx, ExpandContextRequest{
		ProjectID: "project-1",
		Seed:      "DefinitelyMissingSymbol",
		SeedType:  SubjectTypeSymbol,
		Limit:     5,
		Depth:     1,
	})
	require.NoError(t, err)
	assert.Equal(t, ResolutionSubjectNotFound, got.Resolution)
	assert.Empty(t, got.Pack)
}

func TestQueryService_ExpandContext_DedupCollapsesMultiplePaths(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	target := seedDuplicatePathFixture(t, ctx, repo)

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	got, err := service.ExpandContext(ctx, ExpandContextRequest{
		ProjectID: "project-1",
		Seed:      "docs/design.md",
		SeedType:  SubjectTypePath,
		Limit:     10,
		Depth:     1,
	})
	require.NoError(t, err)
	require.Equal(t, ResolutionResolved, got.Resolution)

	var targetItem *PackItem
	for i := range got.Pack {
		if got.Pack[i].NodeID == target.ID {
			targetItem = &got.Pack[i]
			break
		}
	}
	require.NotNil(t, targetItem, "expected deduped pack item for shared target")
	assert.Equal(t, 1, targetItem.AdditionalPathsCount)
	assert.NotEmpty(t, targetItem.AdditionalRelations)
	assert.Equal(t, "internal/pkg/shared.go", targetItem.SourcePath)
	assert.Equal(t, targetItem.SourcePath, targetItem.Path.To.SourcePath,
		"pack source_path must come from the target node, not edge evidence path")
}

func TestQueryService_ExpandContext_DuplicateFirstHopLeavesMultiHopBudget(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	doc := upsertFixtureNode(t, ctx, repo, Node{
		ProjectID: "project-1", Kind: NodeKindDoc,
		Key: "docs/design.md", SourcePath: "docs/design.md",
	})
	target := upsertFixtureNode(t, ctx, repo, Node{
		ProjectID: "project-1", Kind: NodeKindFile,
		Key: "internal/pkg/shared.go", SourcePath: "internal/pkg/shared.go",
	})
	deep := upsertFixtureNode(t, ctx, repo, Node{
		ProjectID: "project-1", Kind: NodeKindFile,
		Key: "internal/pkg/deep.go", SourcePath: "internal/pkg/deep.go",
	})
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID: "project-1", Kind: EdgeKindDocMentionsFile,
		FromNodeID: doc.ID, ToNodeID: target.ID, Extractor: ExtractorCheap,
		SourcePath: "docs/design.md", Confidence: 0.9,
	}))
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID: "project-1", Kind: EdgeKindDocMentionsPath,
		FromNodeID: doc.ID, ToNodeID: target.ID, Extractor: ExtractorCheap,
		SourcePath: "docs/design.md", Confidence: 0.85,
	}))
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID: "project-1", Kind: EdgeKindFileImports,
		FromNodeID: target.ID, ToNodeID: deep.ID, Extractor: ExtractorCheap,
		SourcePath: "internal/pkg/shared.go", Confidence: 0.8,
	}))
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID: "project-1", Status: GraphStatusFresh, CompletedAt: fixedGraphTime(),
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	got, err := service.ExpandContext(ctx, ExpandContextRequest{
		ProjectID: "project-1",
		Seed:      "docs/design.md",
		SeedType:  SubjectTypePath,
		Limit:     2,
		Depth:     2,
	})
	require.NoError(t, err)
	var foundDeep bool
	for _, item := range got.Pack {
		if item.NodeID == deep.ID {
			foundDeep = true
		}
	}
	assert.True(t, foundDeep, "duplicate first-hop paths must not consume multi-hop budget")
}

func TestQueryService_ExpandContext_OutgoingEdgeUsesTargetSourcePath(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	fixture := seedGraphServiceFixture(t, ctx, repo, GraphStatusFresh)
	otherFile := upsertFixtureNode(t, ctx, repo, Node{
		ProjectID: "project-1", Kind: NodeKindFile,
		Key: "internal/graph/query.go", SourcePath: "internal/graph/query.go", Name: "query.go",
	})
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID: "project-1", Kind: EdgeKindFileImports,
		FromNodeID: fixture.file.ID, ToNodeID: otherFile.ID,
		Extractor: ExtractorCheap, SourcePath: "internal/graph/service.go", Confidence: 0.8,
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	got, err := service.ExpandContext(ctx, ExpandContextRequest{
		ProjectID: "project-1",
		Seed:      fixture.file.SourcePath,
		SeedType:  SubjectTypePath,
		Limit:     10,
		Depth:     1,
	})
	require.NoError(t, err)
	var importedItem *PackItem
	for i := range got.Pack {
		if got.Pack[i].NodeID == otherFile.ID {
			importedItem = &got.Pack[i]
			break
		}
	}
	require.NotNil(t, importedItem)
	assert.Equal(t, "internal/graph/query.go", importedItem.SourcePath)
	assert.NotEqual(t, "internal/graph/service.go", importedItem.SourcePath,
		"outgoing import edge evidence path must not override target source_path")
	assert.Equal(t, importedItem.SourcePath, importedItem.Path.To.SourcePath)
}

func TestQueryService_ExpandContext_UnavailableGraphReturnsDegradedEnvelope(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID: "project-1", Status: GraphStatusEmpty, CompletedAt: fixedGraphTime(),
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	got, err := service.ExpandContext(ctx, ExpandContextRequest{
		ProjectID: "project-1",
		Seed:      "anything",
		Limit:     5,
		Depth:     1,
	})
	require.NoError(t, err)
	assert.False(t, got.Available)
	assert.Empty(t, got.Pack)
	require.NotEmpty(t, got.Warnings)
}

func seedAmbiguousSymbolFixture(t *testing.T, ctx context.Context, repo *SQLiteRepository) {
	t.Helper()
	fileA := upsertFixtureNode(t, ctx, repo, Node{
		ProjectID: "project-1", Kind: NodeKindFile,
		Key: "internal/a/search.go", SourcePath: "internal/a/search.go",
	})
	fileB := upsertFixtureNode(t, ctx, repo, Node{
		ProjectID: "project-1", Kind: NodeKindFile,
		Key: "internal/b/search.go", SourcePath: "internal/b/search.go",
	})
	symbolA := upsertFixtureNode(t, ctx, repo, Node{
		ProjectID: "project-1", Kind: NodeKindSymbol,
		Key: "internal/a/search.go#Search:1", SourcePath: "internal/a/search.go", Name: "Search",
	})
	symbolB := upsertFixtureNode(t, ctx, repo, Node{
		ProjectID: "project-1", Kind: NodeKindSymbol,
		Key: "internal/b/search.go#Search:1", SourcePath: "internal/b/search.go", Name: "Search",
	})
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID: "project-1", Kind: EdgeKindFileDefinesSymbol,
		FromNodeID: fileA.ID, ToNodeID: symbolA.ID, Extractor: ExtractorCheap,
		SourcePath: "internal/a/search.go", Confidence: 1,
	}))
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID: "project-1", Kind: EdgeKindFileDefinesSymbol,
		FromNodeID: fileB.ID, ToNodeID: symbolB.ID, Extractor: ExtractorCheap,
		SourcePath: "internal/b/search.go", Confidence: 1,
	}))
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID: "project-1", Status: GraphStatusFresh, CompletedAt: fixedGraphTime(),
	}))
}

func TestHopsToPathInput_PreservesInboundEdgeDirection(t *testing.T) {
	hops := hopsToPathInput("seed-a", GraphPath{
		From: GraphNodeEvidence{ID: "seed-a"},
		Hops: []GraphEvidence{{
			Node:           GraphNodeEvidence{ID: "file-b"},
			Relation:       string(EdgeKindFileImports),
			EdgeFromNodeID: "file-b",
			EdgeToNodeID:   "seed-a",
		}},
	})
	require.Len(t, hops, 1)
	assert.Equal(t, "file-b", hops[0].Edge.FromNodeID)
	assert.Equal(t, "seed-a", hops[0].Edge.ToNodeID)
}

func TestQueryService_ExpandContext_MultiHopPreservesInboundImportDirection(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	seedFile := upsertFixtureNode(t, ctx, repo, Node{
		ProjectID: "project-1", Kind: NodeKindFile,
		Key: "internal/pkg/seed.go", SourcePath: "internal/pkg/seed.go",
	})
	callerFile := upsertFixtureNode(t, ctx, repo, Node{
		ProjectID: "project-1", Kind: NodeKindFile,
		Key: "internal/mcp/server.go", SourcePath: "internal/mcp/server.go",
	})
	bridgeFile := upsertFixtureNode(t, ctx, repo, Node{
		ProjectID: "project-1", Kind: NodeKindFile,
		Key: "internal/graph/bridge.go", SourcePath: "internal/graph/bridge.go",
	})
	seedSymbol := upsertFixtureNode(t, ctx, repo, Node{
		ProjectID: "project-1", Kind: NodeKindSymbol,
		Key: "internal/pkg/seed.go#SeedFunc:10", SourcePath: "internal/pkg/seed.go", Name: "SeedFunc", StartLine: 10,
	})
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID: "project-1", Kind: EdgeKindFileDefinesSymbol,
		FromNodeID: seedFile.ID, ToNodeID: seedSymbol.ID, Extractor: ExtractorCheap,
		SourcePath: "internal/pkg/seed.go", Confidence: 1,
	}))
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID: "project-1", Kind: EdgeKindFileImports,
		FromNodeID: callerFile.ID, ToNodeID: seedFile.ID, Extractor: ExtractorCheap,
		SourcePath: "internal/mcp/server.go", Confidence: 0.8,
		Evidence: Evidence{Method: "go_import", Heuristic: true},
	}))
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID: "project-1", Kind: EdgeKindFileImports,
		FromNodeID: seedFile.ID, ToNodeID: bridgeFile.ID, Extractor: ExtractorCheap,
		SourcePath: "internal/pkg/seed.go", Confidence: 0.7,
		Evidence: Evidence{Method: "go_import", Heuristic: true},
	}))
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID: "project-1", Status: GraphStatusFresh, CompletedAt: fixedGraphTime(),
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	got, err := service.ExpandContext(ctx, ExpandContextRequest{
		ProjectID: "project-1",
		Seed:      seedSymbol.ID,
		SeedType:  SubjectTypeResultID,
		Limit:     10,
		Depth:     2,
	})
	require.NoError(t, err)
	require.Equal(t, ResolutionResolved, got.Resolution)

	var callerItem *PackItem
	for i := range got.Pack {
		if got.Pack[i].NodeID == callerFile.ID {
			callerItem = &got.Pack[i]
			break
		}
	}
	require.NotNil(t, callerItem, "expected multi-hop pack item for inbound import caller")
	require.NotEmpty(t, callerItem.Path.Hops)
	for _, hop := range callerItem.Path.Hops {
		if hop.Relation != string(EdgeKindFileImports) {
			continue
		}
		assert.Equal(t, callerFile.ID, hop.EdgeFromNodeID)
		assert.Equal(t, seedFile.ID, hop.EdgeToNodeID)
	}
	directedHops := hopsToPathInput(seedSymbol.ID, callerItem.Path)
	var importHop *PathHop
	for i := range directedHops {
		if directedHops[i].Edge.Kind == EdgeKindFileImports {
			importHop = &directedHops[i]
		}
	}
	require.NotNil(t, importHop)
	assert.Equal(t, callerFile.ID, importHop.Edge.FromNodeID)
	assert.Equal(t, seedFile.ID, importHop.Edge.ToNodeID)
	assert.Contains(t, roleNamesFromPack(callerItem.Roles), ContextRoleCaller,
		"symbol-seed expansion must label inbound import callers via file anchor")
}

func TestQueryService_ExpandContext_MultiHopDoesNotReincludeSeed(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	fileA := upsertFixtureNode(t, ctx, repo, Node{
		ProjectID: "project-1", Kind: NodeKindFile,
		Key: "internal/a/search.go", SourcePath: "internal/a/search.go",
	})
	symbolA := upsertFixtureNode(t, ctx, repo, Node{
		ProjectID: "project-1", Kind: NodeKindSymbol,
		Key: "internal/a/search.go#Search:10", SourcePath: "internal/a/search.go", Name: "Search", StartLine: 10,
	})
	chunkA := upsertFixtureNode(t, ctx, repo, Node{
		ProjectID: "project-1", Kind: NodeKindChunk,
		Key: "chunk:a", SourcePath: "internal/a/search.go",
	})
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID: "project-1", Kind: EdgeKindFileDefinesSymbol,
		FromNodeID: fileA.ID, ToNodeID: symbolA.ID, Extractor: ExtractorCheap,
		SourcePath: "internal/a/search.go", Confidence: 1,
	}))
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID: "project-1", Kind: EdgeKindSymbolHasChunk,
		FromNodeID: symbolA.ID, ToNodeID: chunkA.ID, Extractor: ExtractorCheap,
		SourcePath: "internal/a/search.go", Confidence: 0.9,
	}))
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID: "project-1", Status: GraphStatusFresh, CompletedAt: fixedGraphTime(),
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	got, err := service.ExpandContext(ctx, ExpandContextRequest{
		ProjectID: "project-1",
		Seed:      symbolA.ID,
		SeedType:  SubjectTypeResultID,
		Limit:     10,
		Depth:     2,
	})
	require.NoError(t, err)
	for _, item := range got.Pack {
		assert.NotEqual(t, symbolA.ID, item.NodeID, "seed must not reappear in multi-hop pack")
	}
}

func TestCollectMultiHopResults_RespectsBudget(t *testing.T) {
	nodes := []Node{
		{ID: "seed", Kind: NodeKindSymbol, Name: "Seed"},
		{ID: "n1", Kind: NodeKindFile, SourcePath: "a.go"},
		{ID: "n2", Kind: NodeKindFile, SourcePath: "b.go"},
		{ID: "n3", Kind: NodeKindFile, SourcePath: "c.go"},
		{ID: "n4", Kind: NodeKindFile, SourcePath: "d.go"},
	}
	edges := []Edge{
		{ID: "e1", FromNodeID: "seed", ToNodeID: "n1", Kind: EdgeKindFileImports, Confidence: 0.8},
		{ID: "e2", FromNodeID: "n1", ToNodeID: "n2", Kind: EdgeKindFileImports, Confidence: 0.8},
		{ID: "e3", FromNodeID: "n2", ToNodeID: "n3", Kind: EdgeKindFileImports, Confidence: 0.8},
		{ID: "e4", FromNodeID: "n3", ToNodeID: "n4", Kind: EdgeKindFileImports, Confidence: 0.8},
	}
	index, err := newGraphIndex(nodes, edges)
	require.NoError(t, err)

	packSeen := map[string]struct{}{}
	got, exhausted := collectMultiHopResults(index, []Node{nodes[0]}, 4, 2, packSeen)
	assert.Len(t, got, 2, "multi-hop BFS must stop once budget is exhausted")
	assert.True(t, exhausted)
}

func TestCollectMultiHopResults_SkipsPackSeenBeforeBudget(t *testing.T) {
	nodes := []Node{
		{ID: "seed", Kind: NodeKindSymbol, Name: "Seed"},
		{ID: "n1", Kind: NodeKindFile, SourcePath: "a.go"},
		{ID: "n2", Kind: NodeKindFile, SourcePath: "b.go"},
		{ID: "n3", Kind: NodeKindFile, SourcePath: "c.go"},
	}
	edges := []Edge{
		{ID: "e1", FromNodeID: "seed", ToNodeID: "n1", Kind: EdgeKindFileImports, Confidence: 0.8},
		{ID: "e2", FromNodeID: "n1", ToNodeID: "n2", Kind: EdgeKindFileImports, Confidence: 0.8},
		{ID: "e3", FromNodeID: "n2", ToNodeID: "n3", Kind: EdgeKindFileImports, Confidence: 0.8},
	}
	index, err := newGraphIndex(nodes, edges)
	require.NoError(t, err)

	packSeen := map[string]struct{}{"n1": {}}
	got, _ := collectMultiHopResults(index, []Node{nodes[0]}, 4, 1, packSeen)
	require.Len(t, got, 1)
	assert.Equal(t, "n2", got[0].NodeID, "budget must not be spent re-emitting depth-1 neighbors")
}

func seedDuplicatePathFixture(t *testing.T, ctx context.Context, repo *SQLiteRepository) Node {
	t.Helper()
	doc := upsertFixtureNode(t, ctx, repo, Node{
		ProjectID: "project-1", Kind: NodeKindDoc,
		Key: "docs/design.md", SourcePath: "docs/design.md",
	})
	target := upsertFixtureNode(t, ctx, repo, Node{
		ProjectID: "project-1", Kind: NodeKindFile,
		Key: "internal/pkg/shared.go", SourcePath: "internal/pkg/shared.go",
	})
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID: "project-1", Kind: EdgeKindDocMentionsFile,
		FromNodeID: doc.ID, ToNodeID: target.ID, Extractor: ExtractorCheap,
		SourcePath: "docs/design.md", Confidence: 0.9,
		Evidence:   Evidence{Method: "doc_link"},
	}))
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID: "project-1", Kind: EdgeKindDocMentionsPath,
		FromNodeID: doc.ID, ToNodeID: target.ID, Extractor: ExtractorCheap,
		SourcePath: "docs/design.md", Confidence: 0.85,
		Evidence:   Evidence{Method: "doc_path"},
	}))
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID: "project-1", Status: GraphStatusFresh, CompletedAt: fixedGraphTime(),
	}))
	return target
}