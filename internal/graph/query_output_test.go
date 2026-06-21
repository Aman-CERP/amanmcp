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
			GraphPath:       []string{"symbol:QueryService", string(EdgeKindSymbolHasChunk), "chunk:query"},
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
