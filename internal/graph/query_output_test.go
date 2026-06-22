package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewQueryToolOutput_ProjectsMCPEnvelopeFields(t *testing.T) {
	response := QueryResponse{
		Status:   GraphStatusPartial,
		Degraded: true,
		Mode:     QueryModeExplainSymbol,
		Query:    "QueryService",
		Results: []QueryResult{{
			NodeID:          "symbol:QueryService",
			NodeKind:        NodeKindSymbol,
			SourcePath:      "internal/graph/query.go",
			Role:            "symbol_context",
			Relation:        EdgeKindSymbolHasChunk,
			ConfidenceLabel: ConfidenceExact,
			EvidenceMethod:  "chunk_symbol_membership",
			Path: GraphPath{
				From: GraphNodeEvidence{ID: "symbol:QueryService", Kind: NodeKindSymbol},
				To:   GraphNodeEvidence{ID: "chunk:query", Kind: NodeKindChunk},
				Hops: []GraphEvidence{{
					Node:     GraphNodeEvidence{ID: "chunk:query", Kind: NodeKindChunk},
					Relation: string(EdgeKindSymbolHasChunk),
				}},
			},
		}},
		Warnings: []StatusWarning{{
			Code:    WarningExtractorFailed,
			Message: "extractor failed",
		}},
	}

	output := NewQueryToolOutput(response)

	assert.True(t, output.Available)
	assert.Equal(t, response.Status, output.Status)
	assert.Equal(t, response.Degraded, output.Degraded)
	assert.Equal(t, response.Mode, output.Mode)
	assert.Equal(t, response.Query, output.Query)
	assert.Equal(t, response.Results, output.Results)
	assert.Equal(t, response.Warnings, output.Warnings)
}

// TestNewQueryToolOutput_ProjectsResolutionAndCandidates proves the additive
// GRA19 disambiguation fields cross the MCP envelope boundary so a client can act
// on disambiguation_required / subject_not_found instead of a silent empty list.
func TestNewQueryToolOutput_ProjectsResolutionAndCandidates(t *testing.T) {
	response := QueryResponse{
		Status:     GraphStatusFresh,
		Mode:       QueryModeExplainSymbol,
		Query:      "Search",
		Resolution: ResolutionDisambiguationRequired,
		Candidates: []Candidate{
			{SubjectID: "a", QualifiedName: "internal/search/engine.go#Search:42", Kind: NodeKindSymbol, SourcePath: "internal/search/engine.go", Line: 42},
			{SubjectID: "b", QualifiedName: "pkg/searcher/searcher.go#Search:9", Kind: NodeKindSymbol, SourcePath: "pkg/searcher/searcher.go", Line: 9},
		},
	}

	output := NewQueryToolOutput(response)

	assert.Equal(t, ResolutionDisambiguationRequired, output.Resolution)
	assert.Equal(t, response.Candidates, output.Candidates)
	assert.Empty(t, output.Results)
}

func TestNewUnavailableQueryToolOutput_MatchesGraphQueryUnavailableShape(t *testing.T) {
	output := NewUnavailableQueryToolOutput(QueryModeFindReferences, "x.go", "graph.query is unavailable")

	assert.False(t, output.Available)
	assert.Equal(t, GraphStatusUnavailable, output.Status)
	assert.True(t, output.Degraded)
	assert.Equal(t, QueryModeFindReferences, output.Mode)
	assert.Equal(t, "x.go", output.Query)
	assert.Equal(t, []StatusWarning{{
		Code:    WarningGraphUnavailable,
		Message: "graph.query is unavailable",
	}}, output.Warnings)
}
