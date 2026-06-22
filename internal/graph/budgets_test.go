package graph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Aman-CERP/amanmcp/internal/config"
)

func TestTraversalBudgets_MaxNodesExhaustion(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-budget-nodes"

	seed := upsertTestNode(t, ctx, repo, projectID, NodeKindFile, "hub.go")
	for i := range 5 {
		target := upsertTestNode(t, ctx, repo, projectID, NodeKindSymbol, fmt.Sprintf("hub.go#Sym%d:1", i))
		require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
			ProjectID: projectID, Kind: EdgeKindFileDefinesSymbol,
			FromNodeID: seed.ID, ToNodeID: target.ID, Extractor: ExtractorCheap,
			SourcePath: "hub.go", Confidence: 0.9 - float64(i)*0.01,
			Evidence: Evidence{Method: "test"},
		}))
	}
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID: projectID, Status: GraphStatusFresh, CompletedAt: fixedGraphTime(),
	}))

	traversal := config.DefaultGraphTraversalConfig()
	traversal.Modes.FindReferences.MaxNodes = 2

	service := NewQueryService(repo, QueryServiceOptions{
		Now:       fixedGraphTime,
		Traversal: traversal,
	})
	got, err := service.Query(ctx, QueryRequest{
		ProjectID: projectID, Mode: QueryModeFindReferences, Query: "hub.go",
	})
	require.NoError(t, err)
	assert.LessOrEqual(t, len(got.Results), 2)
	assertWarningCode(t, got.Warnings, WarningTraversalBudgetExhausted, "nodes")
	assert.Contains(t, warningMessageForCode(got.Warnings, WarningTraversalBudgetExhausted), "nodes")
}

func TestTraversalBudgets_MaxPerEdgeKind(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-budget-edge-kind"

	seed := upsertTestNode(t, ctx, repo, projectID, NodeKindDoc, "docs/readme.md")
	for i := range 4 {
		target := upsertTestNode(t, ctx, repo, projectID, NodeKindFile, fmt.Sprintf("internal/f%d.go", i))
		require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
			ProjectID: projectID, Kind: EdgeKindDocMentionsFile,
			FromNodeID: seed.ID, ToNodeID: target.ID, Extractor: ExtractorCheap,
			SourcePath: "docs/readme.md", Confidence: 0.9,
			Evidence: Evidence{Method: "doc_link"},
		}))
	}
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID: projectID, Status: GraphStatusFresh, CompletedAt: fixedGraphTime(),
	}))

	traversal := config.DefaultGraphTraversalConfig()
	traversal.Modes.FindReferences.MaxPerEdgeKind = 2

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime, Traversal: traversal})
	got, err := service.Query(ctx, QueryRequest{ProjectID: projectID, Query: "docs/readme.md"})
	require.NoError(t, err)
	assert.Len(t, got.Results, 2)
	assertWarningCode(t, got.Warnings, WarningTraversalBudgetExhausted, "per_edge_kind")
}

func TestTraversalBudgets_MaxTokensPartial(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-budget-tokens"

	seed := upsertTestNode(t, ctx, repo, projectID, NodeKindFile, "big.go")
	for i := range 3 {
		target := upsertTestNode(t, ctx, repo, projectID, NodeKindSymbol, fmt.Sprintf("big.go#Fn%d:1", i))
		snippet := fmt.Sprintf("func Fn%d() { %s }", i, repeatString("x", 200))
		require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
			ProjectID: projectID, Kind: EdgeKindFileDefinesSymbol,
			FromNodeID: seed.ID, ToNodeID: target.ID, Extractor: ExtractorCheap,
			SourcePath: "big.go", Confidence: 0.95 - float64(i)*0.01,
			Evidence: Evidence{Method: "chunk_symbol", Snippet: snippet},
		}))
	}
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID: projectID, Status: GraphStatusFresh, CompletedAt: fixedGraphTime(),
	}))

	traversal := config.DefaultGraphTraversalConfig()
	traversal.Modes.FindReferences.MaxTokens = 400

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime, Traversal: traversal})
	got, err := service.Query(ctx, QueryRequest{ProjectID: projectID, Query: "big.go"})
	require.NoError(t, err)
	assert.Less(t, len(got.Results), 3)
	assertWarningCode(t, got.Warnings, WarningTraversalBudgetExhausted, "tokens")
}

func TestTraversalBudgets_MaxNodesDoesNotBreakResolution(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-budget-resolution"

	for i := range 10 {
		upsertTestNode(t, ctx, repo, projectID, NodeKindFile, fmt.Sprintf("internal/pkg/f%d.go", i))
	}
	target := upsertTestNode(t, ctx, repo, projectID, NodeKindFile, "internal/pkg/exact.go")
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID: projectID, Status: GraphStatusFresh, CompletedAt: fixedGraphTime(),
	}))

	traversal := config.DefaultGraphTraversalConfig()
	traversal.Modes.FindReferences.MaxNodes = 1

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime, Traversal: traversal})
	got, err := service.Query(ctx, QueryRequest{
		ProjectID: projectID, Mode: QueryModeFindReferences,
		Query: "internal/pkg/exact.go", SubjectType: SubjectTypePath,
	})
	require.NoError(t, err)
	assert.Equal(t, ResolutionResolved, got.Resolution, "max_nodes must not pre-empt subject resolution")
	_ = target
}

func TestTraversalBudgets_CallerOverrideWithinPolicy(t *testing.T) {
	traversal := config.DefaultGraphTraversalConfig()
	budgets, err := resolveTraversalBudgets(QueryModeFindReferences, traversal, TraversalBudgetOverrides{MaxResults: intPtr(5)})
	require.NoError(t, err)
	assert.Equal(t, 5, budgets.MaxResults)
}

func TestTraversalBudgets_CallerOverrideAbovePolicyRejected(t *testing.T) {
	traversal := config.DefaultGraphTraversalConfig()
	_, err := resolveTraversalBudgets(QueryModeFindReferences, traversal, TraversalBudgetOverrides{
		MaxResults: intPtr(traversal.Policy.MaxResults + 1),
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidQueryParams))
}

func TestEstimateResultsJSONBytes(t *testing.T) {
	results := []QueryResult{{
		NodeID: "n1", Relation: EdgeKindFileDefinesSymbol, Confidence: 0.9,
		Path: GraphPath{Explanation: "seed -> target via file_defines_symbol"},
	}}
	size := estimateResultsJSONBytes(results)
	raw, err := json.Marshal(results)
	require.NoError(t, err)
	assert.Equal(t, len(raw), size)
}

func intPtr(v int) *int { return &v }

func repeatString(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for range n {
		out = append(out, s...)
	}
	return string(out)
}

func warningMessageForCode(warnings []StatusWarning, code WarningCode) string {
	for _, w := range warnings {
		if w.Code == code {
			return w.Message
		}
	}
	return ""
}