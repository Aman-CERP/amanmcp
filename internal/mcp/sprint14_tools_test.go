package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Aman-CERP/amanmcp/internal/graph"
	"github.com/Aman-CERP/amanmcp/internal/pmmutation"
)

func TestGraphQueryTool_ReturnsSourceCitedEvidence(t *testing.T) {
	ctx := context.Background()
	repo, err := graph.OpenSQLiteRepository(filepath.Join(t.TempDir(), "graph.db"))
	require.NoError(t, err)
	defer func() { require.NoError(t, repo.Close()) }()

	file, err := repo.UpsertNode(ctx, graph.Node{
		ProjectID:  "project-1",
		Kind:       graph.NodeKindFile,
		Key:        "internal/search/engine.go",
		SourcePath: "internal/search/engine.go",
		Name:       "engine.go",
	})
	require.NoError(t, err)
	symbol, err := repo.UpsertNode(ctx, graph.Node{
		ProjectID:  "project-1",
		Kind:       graph.NodeKindSymbol,
		Key:        "internal/search/engine.go#Search:42",
		SourcePath: "internal/search/engine.go",
		Name:       "Search",
	})
	require.NoError(t, err)
	_, err = repo.UpsertEdge(ctx, graph.Edge{
		ProjectID:  "project-1",
		Kind:       graph.EdgeKindFileDefinesSymbol,
		FromNodeID: file.ID,
		ToNodeID:   symbol.ID,
		Extractor:  graph.ExtractorCheap,
		SourcePath: "internal/search/engine.go",
		Evidence: graph.Evidence{
			Method:  "go_symbol",
			Snippet: "func Search(ctx context.Context)",
			Line:    42,
		},
		Confidence: 0.95,
	})
	require.NoError(t, err)
	require.NoError(t, repo.RecordBuild(ctx, graph.BuildMetadata{
		ProjectID:   "project-1",
		Status:      graph.GraphStatusFresh,
		CompletedAt: time.Now().UTC(),
	}))

	srv := newTestServer(t)
	srv.SetGraphRepository(repo)

	result, err := srv.CallTool(ctx, "graph.query", map[string]any{
		"project_id": "project-1",
		"mode":       graph.QueryModeFindReferences,
		"query":      "engine.go",
		"limit":      float64(5),
	})

	require.NoError(t, err)
	output, ok := result.(GraphQueryOutput)
	require.True(t, ok, "expected GraphQueryOutput, got %T", result)
	assert.True(t, output.Available)
	assert.Equal(t, graph.GraphStatusFresh, output.Status)
	assert.False(t, output.Degraded)
	require.NotEmpty(t, output.Results)
	assert.Equal(t, "internal/search/engine.go", output.Results[0].SourcePath)
	assert.Equal(t, graph.ConfidenceHigh, output.Results[0].ConfidenceLabel)
	assert.Equal(t, "go_symbol", output.Results[0].EvidenceMethod)
	assert.NotEmpty(t, output.Results[0].GraphPath)
}

func TestGraphQueryTool_DegradesWhenRepositoryMissing(t *testing.T) {
	srv := newTestServer(t)

	result, err := srv.CallTool(context.Background(), "graph.query", map[string]any{
		"query": "anything",
	})

	require.NoError(t, err)
	output, ok := result.(GraphQueryOutput)
	require.True(t, ok, "expected GraphQueryOutput, got %T", result)
	assert.False(t, output.Available)
	assert.Equal(t, graph.GraphStatusUnavailable, output.Status)
	assert.True(t, output.Degraded)
	require.NotEmpty(t, output.Warnings)
	assert.Equal(t, graph.WarningGraphUnavailable, output.Warnings[0].Code)
}

func TestPMMutateTool_AcquiresTokensAndCapturesLearning(t *testing.T) {
	root := t.TempDir()
	writeMCPFixtureFile(t, root, pmmutation.LearningPath, "# Learnings\n\n")
	writeMCPFixtureFile(t, root, pmmutation.ChangelogPath, "# Unreleased Changes\n\n## Added\n\n")
	srv := newTestServer(t)
	srv.SetPMMutator(pmmutation.New(root))

	result, err := srv.CallTool(context.Background(), "pm.mutate", map[string]any{
		"operation": "acquire_tokens",
		"paths":     []any{pmmutation.LearningPath},
	})
	require.NoError(t, err)
	tokenOutput, ok := result.(PMMutateOutput)
	require.True(t, ok, "expected PMMutateOutput, got %T", result)
	require.Len(t, tokenOutput.LockTokens, 1)

	output, err := srv.handlePMMutateTool(context.Background(), PMMutateInput{
		Operation:  "capture_learning",
		What:       "MCP pm.mutate uses file-scoped lock tokens",
		Context:    "Sprint 14 requires mutation receipts",
		Action:     "Capture through the grouped mutation gateway",
		LockTokens: tokenOutput.LockTokens,
	})

	require.NoError(t, err)
	require.NotNil(t, output.Receipt)
	assert.Equal(t, pmmutation.ValidationOK, output.Receipt.Validation.Status)
	require.Len(t, output.Receipt.ChangedFiles, 1)
	assert.Equal(t, pmmutation.LearningPath, output.Receipt.ChangedFiles[0].Path)
	assert.Contains(t, output.Receipt.SuggestedCommitMessage, "Authored-By: Niraj Kumar <nirajkvinit@gmail.com>")
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(pmmutation.LearningPath)))
	require.NoError(t, err)
	assert.Contains(t, string(content), "MCP pm.mutate uses file-scoped lock tokens")
}

func TestPMMutationErrorMapping_DistinguishesActionableErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode int
		wantMsg  string
	}{
		{
			name:     "conflict",
			err:      pmmutation.ErrConflict,
			wantCode: ErrCodePMMutationConflict,
			wantMsg:  "refresh",
		},
		{
			name:     "confirmation required",
			err:      pmmutation.ErrConfirmationRequired,
			wantCode: ErrCodePMMutationConfirmationRequired,
			wantMsg:  "human confirmation",
		},
		{
			name:     "target missing",
			err:      pmmutation.ErrNotFound,
			wantCode: ErrCodeFileNotFound,
			wantMsg:  "missing",
		},
		{
			name:     "invalid input",
			err:      pmmutation.ErrInvalidInput,
			wantCode: ErrCodeInvalidParams,
			wantMsg:  "invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapPMMutationError(fmt.Errorf("wrapped: %w", tt.err))

			var mcpErr *MCPError
			require.True(t, errors.As(got, &mcpErr), "expected MCPError, got %T", got)
			assert.Equal(t, tt.wantCode, mcpErr.Code)
			assert.Contains(t, mcpErr.Message, tt.wantMsg)
		})
	}
}

func TestCallTool_PMMutateDoesNotDeadlockDuringSetPMMutatorChurn(t *testing.T) {
	root := t.TempDir()
	writeMCPFixtureFile(t, root, pmmutation.LearningPath, "# Learnings\n\n")
	writeMCPFixtureFile(t, root, pmmutation.ChangelogPath, "# Unreleased Changes\n\n## Added\n\n")
	srv := newTestServer(t)
	srv.SetPMMutator(pmmutation.New(root))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ctx.Err() == nil {
			srv.SetPMMutator(pmmutation.New(root))
		}
	}()

	for i := 0; i < 100; i++ {
		_, err := srv.CallTool(ctx, "pm.mutate", map[string]any{
			"operation": "acquire_tokens",
			"paths":     []any{pmmutation.LearningPath},
		})
		require.NoError(t, err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SetPMMutator churn did not stop; possible server lock deadlock")
	}
}
