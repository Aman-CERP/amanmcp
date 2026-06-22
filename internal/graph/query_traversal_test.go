package graph

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Aman-CERP/amanmcp/internal/config"
)

func TestNewQueryService_AppliesTraversalPolicyDepthAndLimit(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	traversal := config.DefaultGraphTraversalConfig()
	traversal.Policy.MaxDepth = 8
	traversal.Policy.MaxResults = 40

	service := NewQueryService(repo, QueryServiceOptions{Traversal: traversal})
	require.NotNil(t, service)
	assert.Equal(t, 8, service.maxDepth)
	assert.Equal(t, 40, service.maxLimit)
}

func TestQueryService_NeighborsHonorsTraversalPolicyMaxDepth(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	seedGraphServiceFixture(t, ctx, repo, GraphStatusFresh)

	traversal := config.DefaultGraphTraversalConfig()
	traversal.Policy.MaxDepth = 8
	service := NewQueryService(repo, QueryServiceOptions{
		Now:       fixedGraphTime,
		Traversal: traversal,
	})

	result, err := service.Neighbors(ctx, NeighborRequest{
		ProjectID: "project-1",
		Query:     "NewQueryService",
		Limit:     3,
		Depth:     8,
	})
	require.NoError(t, err)
	assert.Equal(t, 8, result.Depth)
}