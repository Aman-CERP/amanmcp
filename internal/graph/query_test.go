package graph

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateQueryRequest_WrapsTypedSentinel proves every input-validation
// rejection wraps the exported graph.ErrInvalidQueryParams sentinel so eval
// classification can use errors.Is instead of brittle message-substring matching
// (DEBT-037 finding #3). A valid request returns nil, and the human-readable
// message is preserved so operator-facing logs stay informative.
func TestValidateQueryRequest_WrapsTypedSentinel(t *testing.T) {
	cases := []struct {
		name string
		req  QueryRequest
	}{
		{"missing project_id", QueryRequest{Mode: QueryModeFindReferences, Query: "internal/x.go"}},
		{"unsupported mode", QueryRequest{ProjectID: "p", Mode: "bogus_mode", Query: "internal/x.go"}},
		{"unsupported subject_type", QueryRequest{ProjectID: "p", Mode: QueryModeFindReferences, Query: "internal/x.go", SubjectType: "bogus_subject"}},
		{"missing query", QueryRequest{ProjectID: "p", Mode: QueryModeFindReferences, Query: ""}},
		{"nul byte", QueryRequest{ProjectID: "p", Mode: QueryModeFindReferences, Query: "internal/\x00.go"}},
		{"absolute path", QueryRequest{ProjectID: "p", Mode: QueryModeFindReferences, Query: "/etc/passwd"}},
		{"path traversal", QueryRequest{ProjectID: "p", Mode: QueryModeFindReferences, Query: "../secrets.go"}},
		{"limit over cap", QueryRequest{ProjectID: "p", Mode: QueryModeFindReferences, Query: "internal/x.go", Limit: maxGraphQueryLimit + 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateQueryRequest(tc.req, maxGraphQueryLimit)
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrInvalidQueryParams),
				"validation error %q must wrap ErrInvalidQueryParams", err)
			assert.NotEmpty(t, err.Error(), "human-readable message must be preserved")
		})
	}

	valid := QueryRequest{ProjectID: "p", Mode: QueryModeFindReferences, Query: "internal/x.go"}
	require.NoError(t, validateQueryRequest(valid, maxGraphQueryLimit))
}

func TestQueryService_SubjectTypePathRequiresExactSourcePath(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-subject-path"

	file := upsertTestNode(t, ctx, repo, projectID, NodeKindFile, "internal/search/engine.go")
	symbol, err := repo.UpsertNode(ctx, Node{
		ProjectID:  projectID,
		Kind:       NodeKindSymbol,
		Key:        "internal/search/engine.go#Search:42",
		SourcePath: "internal/search/engine.go",
		Name:       "Search",
		SymbolKind: "method",
		StartLine:  42,
	})
	require.NoError(t, err)
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID:  projectID,
		Kind:       EdgeKindFileDefinesSymbol,
		FromNodeID: file.ID,
		ToNodeID:   symbol.ID,
		Extractor:  ExtractorCheap,
		SourcePath: "internal/search/engine.go",
		Confidence: 0.95,
		Evidence:   Evidence{Method: "chunk_symbol", SourcePath: "internal/search/engine.go", LineStart: 42, LineEnd: 42},
	}))
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID: projectID, Status: GraphStatusFresh, CompletedAt: fixedGraphTime(),
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	exact, err := service.Query(ctx, QueryRequest{
		ProjectID: projectID, Mode: QueryModeFindReferences, Query: "internal/search/engine.go", SubjectType: SubjectTypePath,
	})
	require.NoError(t, err)
	assert.Equal(t, ResolutionResolved, exact.Resolution)
	require.Len(t, exact.Results, 1)
	assert.Equal(t, symbol.ID, exact.Results[0].NodeID)

	substring, err := service.Query(ctx, QueryRequest{
		ProjectID: projectID, Mode: QueryModeFindReferences, Query: "engine.go", SubjectType: SubjectTypePath,
	})
	require.NoError(t, err)
	assert.Equal(t, ResolutionSubjectNotFound, substring.Resolution)
	assert.Empty(t, substring.Results, "subject_type=path must not substring-match source paths")
	require.NotEmpty(t, substring.Candidates, "path misses should still provide nearest-path hints")
	assert.Equal(t, file.ID, substring.Candidates[0].SubjectID)
}

func TestQueryService_SubjectTypeAutoAndOmittedPreserveCurrentBehavior(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-subject-auto"

	file := upsertTestNode(t, ctx, repo, projectID, NodeKindFile, "internal/search/engine.go")
	symbol := upsertTestNode(t, ctx, repo, projectID, NodeKindSymbol, "internal/search/engine.go#Search:42")
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID: projectID, Kind: EdgeKindFileDefinesSymbol, FromNodeID: file.ID, ToNodeID: symbol.ID,
		Extractor: ExtractorCheap, SourcePath: "internal/search/engine.go", Confidence: 0.95,
		Evidence: Evidence{Method: "chunk_symbol", SourcePath: "internal/search/engine.go", LineStart: 42, LineEnd: 42},
	}))
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID: projectID, Status: GraphStatusFresh, CompletedAt: fixedGraphTime(),
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	omitted, err := service.Query(ctx, QueryRequest{
		ProjectID: projectID, Mode: QueryModeFindReferences, Query: "engine.go",
	})
	require.NoError(t, err)
	explicitAuto, err := service.Query(ctx, QueryRequest{
		ProjectID: projectID, Mode: QueryModeFindReferences, Query: "engine.go", SubjectType: SubjectTypeAuto,
	})
	require.NoError(t, err)

	assert.Equal(t, omitted.Resolution, explicitAuto.Resolution)
	assert.Equal(t, omitted.Results, explicitAuto.Results)
	assert.Equal(t, omitted.Candidates, explicitAuto.Candidates)
}

func TestQueryService_SubjectTypeAutoExplainSymbolPathPreservesSymbolResolution(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-auto-explain-path"

	file := upsertTestNode(t, ctx, repo, projectID, NodeKindFile, "internal/search/engine.go")
	search, err := repo.UpsertNode(ctx, Node{
		ProjectID: projectID, Kind: NodeKindSymbol,
		Key: "internal/search/engine.go#Search:42", SourcePath: "internal/search/engine.go",
		Name: "Search", SymbolKind: "function", StartLine: 42,
	})
	require.NoError(t, err)
	index, err := repo.UpsertNode(ctx, Node{
		ProjectID: projectID, Kind: NodeKindSymbol,
		Key: "internal/search/engine.go#Index:84", SourcePath: "internal/search/engine.go",
		Name: "Index", SymbolKind: "function", StartLine: 84,
	})
	require.NoError(t, err)
	for _, symbol := range []Node{search, index} {
		require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
			ProjectID: projectID, Kind: EdgeKindFileDefinesSymbol, FromNodeID: file.ID, ToNodeID: symbol.ID,
			Extractor: ExtractorCheap, SourcePath: "internal/search/engine.go", Confidence: 0.95,
			Evidence: Evidence{Method: "chunk_symbol", SourcePath: "internal/search/engine.go", LineStart: symbol.StartLine, LineEnd: symbol.StartLine},
		}))
	}
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID: projectID, Status: GraphStatusFresh, CompletedAt: fixedGraphTime(),
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	got, err := service.Query(ctx, QueryRequest{
		ProjectID: projectID, Mode: QueryModeExplainSymbol, Query: "internal/search/engine.go",
	})
	require.NoError(t, err)
	assert.Equal(t, ResolutionDisambiguationRequired, got.Resolution)
	assert.Empty(t, got.Results, "auto explain_symbol must not resolve a file subject and traverse it")
	require.Len(t, got.Candidates, 2)
	assert.ElementsMatch(t, []string{search.ID, index.ID}, []string{got.Candidates[0].SubjectID, got.Candidates[1].SubjectID})
}

func TestQueryService_SubjectTypeSymbolRestrictsToSymbolResolver(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-subject-symbol"

	_, err := repo.UpsertNode(ctx, Node{
		ProjectID: projectID, Kind: NodeKindPackage, Key: "internal/search#Search", Name: "Search",
	})
	require.NoError(t, err)
	symbol, err := repo.UpsertNode(ctx, Node{
		ProjectID: projectID, Kind: NodeKindSymbol, Key: "internal/search/engine.go#Search:42",
		SourcePath: "internal/search/engine.go", Name: "Search", SymbolKind: "function", StartLine: 42,
	})
	require.NoError(t, err)
	chunk := upsertTestNode(t, ctx, repo, projectID, NodeKindChunk, "internal/search/engine.go#chunk:1")
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID: projectID, Kind: EdgeKindSymbolHasChunk, FromNodeID: symbol.ID, ToNodeID: chunk.ID,
		Extractor: ExtractorCheap, SourcePath: "internal/search/engine.go", Confidence: 1.0,
		Evidence: Evidence{Method: "chunk_symbol_membership", SourcePath: "internal/search/engine.go"},
	}))
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID: projectID, Status: GraphStatusFresh, CompletedAt: fixedGraphTime(),
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	got, err := service.Query(ctx, QueryRequest{
		ProjectID: projectID, Mode: QueryModeFindReferences, Query: "Search", SubjectType: SubjectTypeSymbol,
	})
	require.NoError(t, err)
	assert.Equal(t, ResolutionResolved, got.Resolution)
	require.Len(t, got.Results, 1)
	assert.Equal(t, chunk.ID, got.Results[0].NodeID)
}

func TestQueryService_SubjectTypePackageResolvesNameKeyAndAmbiguity(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-subject-package"

	file := upsertTestNode(t, ctx, repo, projectID, NodeKindFile, "internal/util/a.go")
	internalPkg, err := repo.UpsertNode(ctx, Node{
		ProjectID: projectID, Kind: NodeKindPackage, Key: "internal/util#util", Name: "util",
	})
	require.NoError(t, err)
	_, err = repo.UpsertNode(ctx, Node{
		ProjectID: projectID, Kind: NodeKindPackage, Key: "pkg/util#util", Name: "util",
	})
	require.NoError(t, err)
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID: projectID, Kind: EdgeKindFileDeclaresPackage, FromNodeID: file.ID, ToNodeID: internalPkg.ID,
		Extractor: ExtractorCheap, SourcePath: "internal/util/a.go", Confidence: 1.0,
		Evidence: Evidence{Method: "go_package_declaration", SourcePath: "internal/util/a.go", LineStart: 1, LineEnd: 1},
	}))
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID: projectID, Status: GraphStatusFresh, CompletedAt: fixedGraphTime(),
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	ambiguous, err := service.Query(ctx, QueryRequest{
		ProjectID: projectID, Mode: QueryModeFindReferences, Query: "util", SubjectType: SubjectTypePackage,
	})
	require.NoError(t, err)
	assert.Equal(t, ResolutionDisambiguationRequired, ambiguous.Resolution)
	assert.Empty(t, ambiguous.Results)
	assert.Len(t, ambiguous.Candidates, 2)

	byDir, err := service.Query(ctx, QueryRequest{
		ProjectID: projectID, Mode: QueryModeFindReferences, Query: "internal/util", SubjectType: SubjectTypePackage,
	})
	require.NoError(t, err)
	assert.Equal(t, ResolutionResolved, byDir.Resolution)
	require.Len(t, byDir.Results, 1)
	assert.Equal(t, file.ID, byDir.Results[0].NodeID)

	byKey, err := service.Query(ctx, QueryRequest{
		ProjectID: projectID, Mode: QueryModeFindReferences, Query: "internal/util#util", SubjectType: SubjectTypePackage,
	})
	require.NoError(t, err)
	assert.Equal(t, ResolutionResolved, byKey.Resolution)
	require.Len(t, byKey.Results, 1)
	assert.Equal(t, file.ID, byKey.Results[0].NodeID)
}

func TestQueryService_SubjectTypeResultIDResolvesGraphNodeIDOnly(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-subject-result-id"

	symbol, err := repo.UpsertNode(ctx, Node{
		ProjectID: projectID, Kind: NodeKindSymbol, Key: "internal/search/engine.go#Search:42",
		SourcePath: "internal/search/engine.go", Name: "Search", StartLine: 42,
	})
	require.NoError(t, err)
	chunk := upsertTestNode(t, ctx, repo, projectID, NodeKindChunk, "internal/search/engine.go#chunk:1")
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID: projectID, Kind: EdgeKindSymbolHasChunk, FromNodeID: symbol.ID, ToNodeID: chunk.ID,
		Extractor: ExtractorCheap, SourcePath: "internal/search/engine.go", Confidence: 1.0,
		Evidence: Evidence{Method: "chunk_symbol_membership", SourcePath: "internal/search/engine.go"},
	}))
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID: projectID, Status: GraphStatusFresh, CompletedAt: fixedGraphTime(),
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	resolved, err := service.Query(ctx, QueryRequest{
		ProjectID: projectID, Mode: QueryModeFindReferences, Query: chunk.ID, SubjectType: SubjectTypeResultID,
	})
	require.NoError(t, err)
	assert.Equal(t, ResolutionResolved, resolved.Resolution)
	require.Len(t, resolved.Results, 1)
	assert.Equal(t, symbol.ID, resolved.Results[0].NodeID)

	missing, err := service.Query(ctx, QueryRequest{
		ProjectID: projectID, Mode: QueryModeFindReferences, Query: "result_v1_deadbeef", SubjectType: SubjectTypeResultID,
	})
	require.NoError(t, err)
	assert.Equal(t, ResolutionSubjectNotFound, missing.Resolution)
	assert.Empty(t, missing.Results)
	assertWarningCode(t, missing.Warnings, WarningCode("graph_result_id_not_found"), "not in graph")
}

func TestQueryService_UnsupportedLanguagePathEmitsExplicitWarning(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-unsupported-language"

	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID: projectID, Status: GraphStatusFresh, CompletedAt: fixedGraphTime(),
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	got, err := service.Query(ctx, QueryRequest{
		ProjectID:   projectID,
		Mode:        QueryModeFindReferences,
		Query:       "src/main.rs",
		SubjectType: SubjectTypePath,
	})
	require.NoError(t, err)
	assert.Equal(t, ResolutionSubjectNotFound, got.Resolution)
	assert.Empty(t, got.Results)
	assertWarningCode(t, got.Warnings, WarningUnsupportedLanguage, "extractor not present")
}

func TestQueryService_DocPathDoesNotEmitUnsupportedLanguageWarning(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-doc-language"

	upsertTestNode(t, ctx, repo, projectID, NodeKindDoc, "docs/changelog.md")
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID: projectID, Status: GraphStatusFresh, CompletedAt: fixedGraphTime(),
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	got, err := service.Query(ctx, QueryRequest{
		ProjectID:   projectID,
		Mode:        QueryModeFindReferences,
		Query:       "docs/changelog.md",
		SubjectType: SubjectTypePath,
	})
	require.NoError(t, err)
	for _, warning := range got.Warnings {
		assert.NotEqual(t, WarningUnsupportedLanguage, warning.Code)
	}
}

func TestQueryService_UnsupportedSymbolPrefixEmitsExplicitWarning(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-unsupported-symbol"

	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID: projectID, Status: GraphStatusFresh, CompletedAt: fixedGraphTime(),
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	got, err := service.Query(ctx, QueryRequest{
		ProjectID: projectID,
		Mode:      QueryModeExplainSymbol,
		Query:     "rust_main_entry_only_gra26",
	})
	require.NoError(t, err)
	assert.Equal(t, ResolutionSubjectNotFound, got.Resolution)
	assertWarningCode(t, got.Warnings, WarningUnsupportedLanguage, "extractor not present")
}

func TestQueryService_SupportedLanguagePathDoesNotEmitUnsupportedWarning(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-supported-language"

	upsertTestNode(t, ctx, repo, projectID, NodeKindFile, "internal/graph/query.go")
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID: projectID, Status: GraphStatusFresh, CompletedAt: fixedGraphTime(),
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	got, err := service.Query(ctx, QueryRequest{
		ProjectID:   projectID,
		Mode:        QueryModeFindReferences,
		Query:       "internal/graph/query.go",
		SubjectType: SubjectTypePath,
	})
	require.NoError(t, err)
	assert.Equal(t, ResolutionResolved, got.Resolution)
	for _, warning := range got.Warnings {
		assert.NotEqual(t, WarningUnsupportedLanguage, warning.Code)
	}
}

func TestQueryService_ResultSurfacesHeuristicFlagFlatAndPerHop(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-heuristic-flag"

	testFile := upsertTestNode(t, ctx, repo, projectID, NodeKindTestFile, "internal/search/engine_test.go")
	implFile := upsertTestNode(t, ctx, repo, projectID, NodeKindFile, "internal/search/engine.go")
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID: projectID, Kind: EdgeKindTestCoversImplementation,
		FromNodeID: testFile.ID, ToNodeID: implFile.ID, Extractor: ExtractorCheap,
		SourcePath: "internal/search/engine_test.go", Confidence: 0.72,
		Evidence: Evidence{
			Method: "test_filename_convention", SourcePath: "internal/search/engine_test.go",
			LineStart: 1, LineEnd: 1, Heuristic: true,
		},
	}))
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID: projectID, Status: GraphStatusFresh, CompletedAt: fixedGraphTime(),
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	got, err := service.Query(ctx, QueryRequest{ProjectID: projectID, Query: "internal/search/engine_test.go", SubjectType: SubjectTypePath})
	require.NoError(t, err)
	require.Len(t, got.Results, 1)
	result := got.Results[0]
	assert.True(t, result.Heuristic, "flat result exposes heuristic for clients that do not inspect structured paths")
	require.Len(t, result.Path.Hops, 1)
	assert.True(t, result.Path.Hops[0].EdgeEvidence.Heuristic, "structured hop exposes the same heuristic flag")
	assert.Equal(t, ConfidenceMedium, result.ConfidenceLabel, "label remains magnitude, not method")
}

// TestErrInvalidQueryParams_NotMatchedByInfraErrors guards the boundary: a
// non-validation (transport/infra) error must NOT be classified as invalid
// params, so the eval taxonomy keeps the two failure modes distinct.
func TestErrInvalidQueryParams_NotMatchedByInfraErrors(t *testing.T) {
	infra := fmt.Errorf("list graph edges: %w", errors.New("disk i/o error"))
	assert.False(t, errors.Is(infra, ErrInvalidQueryParams))
}

func TestQueryService_ReturnsBoundedSourceCitedEvidence(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-graph-query"

	file := upsertTestNode(t, ctx, repo, projectID, NodeKindFile, "internal/search/engine.go")
	symbol := upsertTestNode(t, ctx, repo, projectID, NodeKindSymbol, "internal/search/engine.go#Search:42")
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID:       projectID,
		Kind:            EdgeKindFileDefinesSymbol,
		FromNodeID:      file.ID,
		ToNodeID:        symbol.ID,
		Extractor:       ExtractorCheap,
		SourcePath:      "internal/search/engine.go",
		Confidence:      0.95,
		ConfidenceLabel: ConfidenceHigh,
		Evidence:        Evidence{Method: "chunk_symbol", Snippet: "func Search(ctx context.Context)"},
	}))
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID:   projectID,
		Status:      GraphStatusFresh,
		CompletedAt: fixedGraphTime(),
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	// GRA19: query the exact subject. The prior partial substring "engine.go"
	// now correctly disambiguates (it matches both the file and its symbol as
	// substrings); an exact path resolves to the file and traverses its scope.
	got, err := service.Query(ctx, QueryRequest{
		ProjectID: projectID,
		Mode:      QueryModeFindReferences,
		Query:     "internal/search/engine.go",
		Limit:     1,
	})

	require.NoError(t, err)
	assert.Equal(t, GraphStatusFresh, got.Status)
	assert.False(t, got.Degraded)
	assert.Equal(t, ResolutionResolved, got.Resolution)
	require.Len(t, got.Results, 1)
	assert.Equal(t, "internal/search/engine.go", got.Results[0].SourcePath)
	assert.Equal(t, EdgeKindFileDefinesSymbol, got.Results[0].Relation)
	assert.Equal(t, ConfidenceHigh, got.Results[0].ConfidenceLabel)
	assert.Equal(t, "chunk_symbol", got.Results[0].EvidenceMethod)
	assert.Len(t, got.Results[0].Path.Hops, 1, "a resolved result carries a structured single-hop path")
}

// TestQueryService_ReturnsStructuredSingleHopPath is the GRA21 contract: every
// graph.query result carries a structured, source-citable GraphPath with exactly
// one hop (single-hop semantics this sprint), unifying on the same GraphPath shape
// the multi-hop engine already uses. The hop must carry full node + edge +
// evidence + confidence + freshness so every relationship is traceable to a
// file/line/snippet rather than a bare 3-string array.
func TestQueryService_ReturnsStructuredSingleHopPath(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-structured-path"

	file := upsertTestNode(t, ctx, repo, projectID, NodeKindFile, "internal/search/engine.go")
	symbol, err := repo.UpsertNode(ctx, Node{
		ProjectID: projectID, Kind: NodeKindSymbol,
		Key: "internal/search/engine.go#Search:42", SourcePath: "internal/search/engine.go",
		Name: "Search", SymbolKind: "method", StartLine: 42, EndLine: 88,
	})
	require.NoError(t, err)
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID:  projectID,
		Kind:       EdgeKindFileDefinesSymbol,
		FromNodeID: file.ID,
		ToNodeID:   symbol.ID,
		Extractor:  ExtractorCheap,
		SourcePath: "internal/search/engine.go",
		Confidence: 0.95,
		Evidence: Evidence{
			Method: "chunk_symbol", SourcePath: "internal/search/engine.go",
			Snippet: "func Search(ctx context.Context)", LineStart: 42, LineEnd: 42, Heuristic: false,
		},
	}))
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID: projectID, Status: GraphStatusFresh, CompletedAt: fixedGraphTime(),
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	got, err := service.Query(ctx, QueryRequest{
		ProjectID: projectID, Mode: QueryModeFindReferences, Query: "internal/search/engine.go",
	})
	require.NoError(t, err)
	require.Len(t, got.Results, 1)
	res := got.Results[0]

	// Flat top-level fields are retained for backward-compatible eval scoring.
	assert.Equal(t, symbol.ID, res.NodeID)
	assert.Equal(t, EdgeKindFileDefinesSymbol, res.Relation)
	assert.Equal(t, "chunk_symbol", res.EvidenceMethod)

	// Structured single-hop path: From=seed, To=related, exactly one hop.
	path := res.Path
	require.Len(t, path.Hops, 1, "single-hop semantics this sprint (multi-hop is FEAT-SYN9)")
	assert.Equal(t, file.ID, path.From.ID, "From is the seed node")
	assert.Equal(t, "internal/search/engine.go", path.From.SourcePath)
	assert.Equal(t, symbol.ID, path.To.ID, "To is the related node")
	assert.Equal(t, NodeKindSymbol, path.To.Kind)
	assert.Equal(t, "Search", path.To.Name)
	assert.Equal(t, 42, path.To.StartLine)

	hop := path.Hops[0]
	assert.Equal(t, string(EdgeKindFileDefinesSymbol), hop.Relation)
	assert.NotEmpty(t, hop.Role, "hop carries a structured graph role")
	assert.Equal(t, symbol.ID, hop.Node.ID)
	assert.Equal(t, "internal/search/engine.go", hop.EdgeSourcePath)
	// Edge evidence: method + source path + line range + snippet + heuristic flag.
	assert.Equal(t, "chunk_symbol", hop.EdgeEvidence.Method)
	assert.Equal(t, "internal/search/engine.go", hop.EdgeEvidence.SourcePath)
	assert.Equal(t, "func Search(ctx context.Context)", hop.EdgeEvidence.Snippet)
	assert.Equal(t, 42, hop.EdgeEvidence.LineStart)
	assert.False(t, hop.EdgeEvidence.Heuristic)
	// Freshness is DERIVED from edge UpdatedAt + Stale (no new storage column).
	assert.False(t, hop.EdgeUpdatedAt.IsZero(), "freshness edge_updated_at derives from edge UpdatedAt")
	assert.False(t, hop.Stale)

	// Path-level confidence is the bottleneck (== the single edge label for 1 hop).
	assert.Equal(t, ConfidenceHigh, path.ConfidenceLabel)
	assert.Equal(t, hop.ConfidenceLabel, path.ConfidenceLabel)
	assert.NotEmpty(t, path.Explanation, "the path is the explanation")
}

// TestQueryService_StructuredPathFreshnessReflectsStaleEdge proves freshness is a
// live projection of the edge: a stale edge surfaces stale=true on the hop, with
// no dedicated storage column backing it.
func TestQueryService_StructuredPathFreshnessReflectsStaleEdge(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-structured-stale"
	doc := upsertTestNode(t, ctx, repo, projectID, NodeKindDoc, "docs/design.md")
	file := upsertTestNode(t, ctx, repo, projectID, NodeKindFile, "internal/impl.go")
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID: projectID, Kind: EdgeKindDocMentionsFile,
		FromNodeID: doc.ID, ToNodeID: file.ID, Extractor: ExtractorCheap,
		SourcePath: "docs/design.md", Confidence: 0.9, Stale: true,
		Evidence: Evidence{Method: "doc_link", SourcePath: "docs/design.md", LineStart: 3, LineEnd: 3},
	}))
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID: projectID, Status: GraphStatusFresh, CompletedAt: fixedGraphTime(),
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	got, err := service.Query(ctx, QueryRequest{ProjectID: projectID, Query: "docs/design.md", IncludeStale: true})
	require.NoError(t, err)
	require.Len(t, got.Results, 1)
	require.Len(t, got.Results[0].Path.Hops, 1)
	assert.True(t, got.Results[0].Path.Hops[0].Stale, "stale edge surfaces as stale hop freshness")
	assert.True(t, got.Results[0].Stale, "flat stale field is preserved")
}

func TestQueryService_DegradesForStaleGraphButReturnsEvidence(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-stale"
	file := upsertTestNode(t, ctx, repo, projectID, NodeKindFile, "a.go")
	symbol := upsertTestNode(t, ctx, repo, projectID, NodeKindSymbol, "a.go#A:1")
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID:  projectID,
		Kind:       EdgeKindFileDefinesSymbol,
		FromNodeID: file.ID,
		ToNodeID:   symbol.ID,
		Extractor:  ExtractorCheap,
		SourcePath: "a.go",
		Confidence: 0.9,
		Evidence:   Evidence{Method: "test"},
	}))
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID:   projectID,
		Status:      GraphStatusFresh,
		CompletedAt: fixedGraphTime().Add(-48 * time.Hour),
	}))

	service := NewQueryService(repo, QueryServiceOptions{
		Now:        fixedGraphTime,
		StaleAfter: time.Hour,
	})
	got, err := service.Query(ctx, QueryRequest{ProjectID: projectID, Query: "a.go"})

	require.NoError(t, err)
	assert.Equal(t, GraphStatusStale, got.Status)
	assert.True(t, got.Degraded)
	assert.NotEmpty(t, got.Warnings)
	assert.NotEmpty(t, got.Results)
}

func TestQueryService_ResultIDNotFoundWarningSurvivesSnapshotWarnings(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-result-id-warning-order"
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID: projectID, Status: GraphStatusFresh, CompletedAt: fixedGraphTime().Add(-48 * time.Hour),
	}))

	service := NewQueryService(repo, QueryServiceOptions{
		Now:        fixedGraphTime,
		StaleAfter: time.Hour,
	})
	got, err := service.Query(ctx, QueryRequest{
		ProjectID: projectID, Mode: QueryModeFindReferences, Query: "result_v1_deadbeef", SubjectType: SubjectTypeResultID,
	})

	require.NoError(t, err)
	assert.Equal(t, GraphStatusStale, got.Status)
	assert.Equal(t, ResolutionSubjectNotFound, got.Resolution)
	assertWarningCode(t, got.Warnings, WarningGraphStale, "stale")
	assertWarningCode(t, got.Warnings, WarningCode("graph_result_id_not_found"), "not in graph")
}

func TestQueryService_ExcludesStaleEdgesByDefaultAndIncludesOnOptIn(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-stale-edge-query"
	doc := upsertTestNode(t, ctx, repo, projectID, NodeKindDoc, "docs/design.md")
	file := upsertTestNode(t, ctx, repo, projectID, NodeKindFile, "internal/impl.go")
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID:       projectID,
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
		ProjectID:   projectID,
		Status:      GraphStatusFresh,
		CompletedAt: fixedGraphTime(),
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	defaultResult, err := service.Query(ctx, QueryRequest{ProjectID: projectID, Query: "impl.go"})
	require.NoError(t, err)
	assert.Empty(t, defaultResult.Results)

	withStale, err := service.Query(ctx, QueryRequest{ProjectID: projectID, Query: "impl.go", IncludeStale: true})
	require.NoError(t, err)
	require.Len(t, withStale.Results, 1)
	assert.True(t, withStale.Results[0].Stale)
}

func TestQueryService_UnusableGraphReturnsVisibleWarning(t *testing.T) {
	service := NewQueryService(newTestSQLiteRepository(t), QueryServiceOptions{Now: fixedGraphTime})

	got, err := service.Query(context.Background(), QueryRequest{
		ProjectID: "empty-project",
		Query:     "anything",
	})

	require.NoError(t, err)
	assert.Equal(t, GraphStatusEmpty, got.Status)
	assert.True(t, got.Degraded)
	assert.Empty(t, got.Results)
	assert.NotEmpty(t, got.Warnings)
}

func TestQueryServable_OnlyFreshStalePartialServeRealAnswers(t *testing.T) {
	// QueryServable is the servability SSOT shared by the query service
	// short-circuit and direct graph eval measurement accounting. `failed` keeps
	// available=true (QueryAvailable) but serves no real answer, so it must not
	// be servable.
	servable := []GraphStatus{GraphStatusFresh, GraphStatusStale, GraphStatusPartial}
	notServable := []GraphStatus{GraphStatusUnavailable, GraphStatusIncompatible, GraphStatusEmpty, GraphStatusFailed}

	for _, status := range servable {
		assert.True(t, QueryServable(status), "status %s should be servable", status)
	}
	for _, status := range notServable {
		assert.False(t, QueryServable(status), "status %s should not be servable", status)
	}
}

func TestQueryService_ValidatesInputs(t *testing.T) {
	service := NewQueryService(newTestSQLiteRepository(t), QueryServiceOptions{})

	tests := []QueryRequest{
		{ProjectID: "", Query: "x"},
		{ProjectID: "p", Query: ""},
		{ProjectID: "p", Query: "../x"},
		{ProjectID: "p", Query: "x", Mode: "unknown"},
		{ProjectID: "p", Query: "x", Limit: -1},
		{ProjectID: "p", Query: "x", Limit: 51},
	}
	for _, tt := range tests {
		_, err := service.Query(context.Background(), tt)
		require.Error(t, err)
	}
}

// TestQueryService_DisambiguatesAmbiguousSymbol proves the cornerstone GRA19
// behavior: a bare symbol name that matches two distinct definitions no longer
// merges their edges into one misleading list. Resolution is
// disambiguation_required, Results is empty, and the caller receives candidates
// it can re-query by SubjectID.
func TestQueryService_DisambiguatesAmbiguousSymbol(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-ambiguous"

	searchA, err := repo.UpsertNode(ctx, Node{
		ProjectID: projectID, Kind: NodeKindSymbol,
		Key: "internal/search/engine.go#Search:42", SourcePath: "internal/search/engine.go",
		Name: "Search", SymbolKind: "method", StartLine: 42,
	})
	require.NoError(t, err)
	searchB, err := repo.UpsertNode(ctx, Node{
		ProjectID: projectID, Kind: NodeKindSymbol,
		Key: "pkg/searcher/searcher.go#Search:9", SourcePath: "pkg/searcher/searcher.go",
		Name: "Search", SymbolKind: "function", StartLine: 9,
	})
	require.NoError(t, err)
	chunkA := upsertTestNode(t, ctx, repo, projectID, NodeKindChunk, "internal/search/engine.go#chunk:1")
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID: projectID, Kind: EdgeKindSymbolHasChunk,
		FromNodeID: searchA.ID, ToNodeID: chunkA.ID, Extractor: ExtractorCheap,
		SourcePath: "internal/search/engine.go", Confidence: 0.9,
		Evidence: Evidence{Method: "chunk_symbol_membership"},
	}))
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID: projectID, Status: GraphStatusFresh, CompletedAt: fixedGraphTime(),
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	got, err := service.Query(ctx, QueryRequest{
		ProjectID: projectID, Mode: QueryModeExplainSymbol, Query: "Search",
	})

	require.NoError(t, err)
	assert.Equal(t, ResolutionDisambiguationRequired, got.Resolution)
	assert.Empty(t, got.Results, "an ambiguous subject must not traverse or merge")
	require.Len(t, got.Candidates, 2)
	gotIDs := []string{got.Candidates[0].SubjectID, got.Candidates[1].SubjectID}
	assert.ElementsMatch(t, []string{searchA.ID, searchB.ID}, gotIDs)
	for _, c := range got.Candidates {
		assert.NotEmpty(t, c.QualifiedName)
		assert.NotEmpty(t, c.SourcePath)
		assert.Positive(t, c.Line)
	}
}

// TestQueryService_SubjectNotFoundEmitsResolutionAndHints turns the previously
// silent empty result into an actionable subject_not_found signal with a
// near-miss hint.
func TestQueryService_SubjectNotFoundEmitsResolutionAndHints(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-notfound"
	_, err := repo.UpsertNode(ctx, Node{
		ProjectID: projectID, Kind: NodeKindSymbol,
		Key: "internal/search/engine.go#Search:42", SourcePath: "internal/search/engine.go",
		Name: "Search", SymbolKind: "method", StartLine: 42,
	})
	require.NoError(t, err)
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID: projectID, Status: GraphStatusFresh, CompletedAt: fixedGraphTime(),
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	got, err := service.Query(ctx, QueryRequest{
		ProjectID: projectID, Mode: QueryModeExplainSymbol, Query: "Searcz",
	})

	require.NoError(t, err)
	assert.Equal(t, ResolutionSubjectNotFound, got.Resolution)
	assert.Empty(t, got.Results)
	require.NotEmpty(t, got.Candidates, "a near miss must surface at least one hint")
	assert.NotEmpty(t, got.Candidates[0].Hint)
}

// TestQueryService_ResolvedSetsResolutionAndTraverses confirms the happy path is
// preserved: a single unambiguous subject resolves and traversal runs as before,
// with Resolution explicitly set to resolved (additive, backward-compatible).
func TestQueryService_ResolvedSetsResolutionAndTraverses(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-resolved"
	file := upsertTestNode(t, ctx, repo, projectID, NodeKindFile, "internal/search/engine.go")
	symbol, err := repo.UpsertNode(ctx, Node{
		ProjectID: projectID, Kind: NodeKindSymbol,
		Key: "internal/search/engine.go#Search:42", SourcePath: "internal/search/engine.go",
		Name: "Search", SymbolKind: "method", StartLine: 42,
	})
	require.NoError(t, err)
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID: projectID, Kind: EdgeKindFileDefinesSymbol,
		FromNodeID: file.ID, ToNodeID: symbol.ID, Extractor: ExtractorCheap,
		SourcePath: "internal/search/engine.go", Confidence: 0.95, ConfidenceLabel: ConfidenceHigh,
		Evidence: Evidence{Method: "chunk_symbol"},
	}))
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID: projectID, Status: GraphStatusFresh, CompletedAt: fixedGraphTime(),
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	got, err := service.Query(ctx, QueryRequest{
		ProjectID: projectID, Mode: QueryModeFindReferences, Query: "internal/search/engine.go",
	})

	require.NoError(t, err)
	assert.Equal(t, ResolutionResolved, got.Resolution)
	assert.Empty(t, got.Candidates)
	require.NotEmpty(t, got.Results, "a resolved file subject must still traverse its scope")
}

// TestQueryService_ResolvedFileSubjectSeedsMembers proves the seedScope contract
// end-to-end: a resolved file subject seeds the file's symbols, so a reference
// flowing through a symbol's edge (symbol_has_chunk) is surfaced. If seedScope
// returned only the lone file node, the symbol→chunk edge would never be walked
// and Results would be empty.
func TestQueryService_ResolvedFileSubjectSeedsMembers(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-file-scope"
	// The file node is the resolution target; its members are seeded by seedScope.
	_ = upsertTestNode(t, ctx, repo, projectID, NodeKindFile, "internal/search/engine.go")
	symbol, err := repo.UpsertNode(ctx, Node{
		ProjectID: projectID, Kind: NodeKindSymbol,
		Key: "internal/search/engine.go#Search:42", SourcePath: "internal/search/engine.go",
		Name: "Search", SymbolKind: "method", StartLine: 42,
	})
	require.NoError(t, err)
	chunk, err := repo.UpsertNode(ctx, Node{
		ProjectID: projectID, Kind: NodeKindChunk,
		Key: "internal/search/engine.go#chunk:1", SourcePath: "internal/search/engine.go",
	})
	require.NoError(t, err)
	// The ONLY edge is anchored on the in-file symbol, not the file node.
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID: projectID, Kind: EdgeKindSymbolHasChunk,
		FromNodeID: symbol.ID, ToNodeID: chunk.ID, Extractor: ExtractorCheap,
		SourcePath: "internal/search/engine.go", Confidence: 0.95, ConfidenceLabel: ConfidenceHigh,
		Evidence: Evidence{Method: "chunk_symbol_membership"},
	}))
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID: projectID, Status: GraphStatusFresh, CompletedAt: fixedGraphTime(),
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	got, err := service.Query(ctx, QueryRequest{
		ProjectID: projectID, Mode: QueryModeFindReferences, Query: "internal/search/engine.go",
	})

	require.NoError(t, err)
	assert.Equal(t, ResolutionResolved, got.Resolution)
	require.Len(t, got.Results, 1, "the symbol-anchored edge proves the file's members were seeded")
	assert.Equal(t, chunk.ID, got.Results[0].NodeID)
	assert.Equal(t, EdgeKindSymbolHasChunk, got.Results[0].Relation)
}

// TestQueryService_DisambiguationTruncationEmitsWarning proves a subject matching
// more candidates than the cap returns a bounded list plus a
// graph_candidates_truncated warning, mirroring the results-truncation contract.
func TestQueryService_DisambiguationTruncationEmitsWarning(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-cand-truncate"
	for i := range 12 {
		_, err := repo.UpsertNode(ctx, Node{
			ProjectID: projectID, Kind: NodeKindSymbol,
			Key:        fmt.Sprintf("internal/widget/w%02d.go#Widget:%d", i, i+1),
			SourcePath: fmt.Sprintf("internal/widget/w%02d.go", i),
			Name:       "Widget", SymbolKind: "type", StartLine: i + 1,
		})
		require.NoError(t, err)
	}
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID: projectID, Status: GraphStatusFresh, CompletedAt: fixedGraphTime(),
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	got, err := service.Query(ctx, QueryRequest{
		ProjectID: projectID, Mode: QueryModeExplainSymbol, Query: "Widget", Limit: 1,
	})

	require.NoError(t, err)
	assert.Equal(t, ResolutionDisambiguationRequired, got.Resolution)
	assert.Empty(t, got.Results)
	// Candidates are capped independently of the result Limit (=1 here).
	assert.Len(t, got.Candidates, defaultGraphQueryLimit)
	var found bool
	for _, w := range got.Warnings {
		if w.Code == WarningCode("graph_candidates_truncated") {
			found = true
		}
	}
	assert.True(t, found, "a truncated candidate list must emit graph_candidates_truncated")
}

func TestQueryService_TruncatesResultsWithWarning(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-truncate"
	file := upsertTestNode(t, ctx, repo, projectID, NodeKindFile, "a.go")
	for _, key := range []string{"a.go#A:1", "a.go#B:2"} {
		symbol := upsertTestNode(t, ctx, repo, projectID, NodeKindSymbol, key)
		require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
			ProjectID:  projectID,
			Kind:       EdgeKindFileDefinesSymbol,
			FromNodeID: file.ID,
			ToNodeID:   symbol.ID,
			Extractor:  ExtractorCheap,
			SourcePath: "a.go",
			Confidence: 0.9,
			Evidence:   Evidence{Method: "test"},
		}))
	}
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID:   projectID,
		Status:      GraphStatusFresh,
		CompletedAt: fixedGraphTime(),
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	got, err := service.Query(ctx, QueryRequest{ProjectID: projectID, Query: "a.go", Limit: 1})

	require.NoError(t, err)
	assert.Len(t, got.Results, 1)
	assert.NotEmpty(t, got.Warnings)
	assert.Equal(t, WarningTraversalBudgetExhausted, got.Warnings[0].Code)
	assert.Contains(t, got.Warnings[0].Message, string(TraversalBudgetResults))
}
