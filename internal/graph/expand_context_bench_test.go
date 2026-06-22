package graph

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countingExpandRepository struct {
	Repository
	listNodesCalls int
	listEdgesCalls int
}

func (c *countingExpandRepository) ListNodes(ctx context.Context, query NodeQuery) ([]Node, error) {
	c.listNodesCalls++
	return c.Repository.ListNodes(ctx, query)
}

func (c *countingExpandRepository) ListEdges(ctx context.Context, query EdgeQuery) ([]Edge, error) {
	c.listEdgesCalls++
	return c.Repository.ListEdges(ctx, query)
}

func TestExpandContext_SingleGraphLoadPerCall(t *testing.T) {
	ctx := context.Background()
	base := newTestSQLiteRepository(t)
	fixture := seedGraphServiceFixture(t, ctx, base, GraphStatusFresh)
	otherFile := upsertFixtureNode(t, ctx, base, Node{
		ProjectID: "project-1", Kind: NodeKindFile,
		Key: "internal/graph/query.go", SourcePath: "internal/graph/query.go",
	})
	require.NoError(t, base.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID: "project-1", Kind: EdgeKindFileImports,
		FromNodeID: fixture.file.ID, ToNodeID: otherFile.ID,
		Extractor: ExtractorCheap, SourcePath: "internal/graph/service.go", Confidence: 0.8,
	}))

	repo := &countingExpandRepository{Repository: base}
	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	_, err := service.ExpandContext(ctx, ExpandContextRequest{
		ProjectID: "project-1",
		Seed:      fixture.symbol.ID,
		SeedType:  SubjectTypeResultID,
		Limit:     10,
		Depth:     2,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, repo.listNodesCalls, "expand_context must load nodes once per call")
	assert.Equal(t, 1, repo.listEdgesCalls, "expand_context must load edges once per call")
}

func BenchmarkExpandContext_Depth2(b *testing.B) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(&testing.T{})
	seedGraphServiceFixture(&testing.T{}, ctx, repo, GraphStatusFresh)
	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	req := ExpandContextRequest{
		ProjectID: "project-1",
		Seed:      "NewQueryService",
		SeedType:  SubjectTypeSymbol,
		Limit:     10,
		Depth:     2,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := service.ExpandContext(ctx, req); err != nil {
			b.Fatal(err)
		}
	}
}