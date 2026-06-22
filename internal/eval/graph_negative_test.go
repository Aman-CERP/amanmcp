package eval

import (
	"testing"

	"github.com/Aman-CERP/amanmcp/internal/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScoreGraphNegativeCase_DisambiguationRequiresEmptyResultsAndCandidates(t *testing.T) {
	maxZero := 0
	neg := GraphNegativeExpectation{
		ExpectedResolution: graph.ResolutionDisambiguationRequired,
		MinCandidates:      2,
		MaxResults:         &maxZero,
		ProhibitedEvidence: []GraphExpectedEvidence{{
			NodeKind: graph.NodeKindChunk,
			Relation: graph.EdgeKindSymbolHasChunk,
			Rationale: "must not merge ambiguous definitions into fabricated results",
		}},
	}

	passed, reason := scoreGraphNegativeCase(neg, graphNegativeInput{
		Resolution:     graph.ResolutionDisambiguationRequired,
		CandidateCount: 3,
		ResultCount:    0,
	})
	assert.True(t, passed, reason)

	passed, reason = scoreGraphNegativeCase(neg, graphNegativeInput{
		Resolution:     graph.ResolutionDisambiguationRequired,
		CandidateCount: 1,
		ResultCount:    0,
	})
	assert.False(t, passed)
	assert.Contains(t, reason, "candidate count")

	passed, reason = scoreGraphNegativeCase(neg, graphNegativeInput{
		Resolution:     graph.ResolutionDisambiguationRequired,
		CandidateCount: 2,
		ResultCount:    1,
		Results: []graph.QueryResult{{
			NodeKind: graph.NodeKindChunk,
			Relation: graph.EdgeKindSymbolHasChunk,
		}},
	})
	assert.False(t, passed)
	assert.Contains(t, reason, "result count")
}

func TestScoreGraphNegativeCase_SubjectNotFoundProhibitsFabricatedEvidence(t *testing.T) {
	maxZero := 0
	neg := GraphNegativeExpectation{
		ExpectedResolution: graph.ResolutionSubjectNotFound,
		MaxResults:         &maxZero,
		ProhibitedEvidence: []GraphExpectedEvidence{{
			NodeKind: graph.NodeKindChunk,
			Rationale: "must not hallucinate chunk evidence for missing subjects",
		}},
	}

	passed, _ := scoreGraphNegativeCase(neg, graphNegativeInput{
		Resolution:  graph.ResolutionSubjectNotFound,
		ResultCount: 0,
	})
	assert.True(t, passed)

	passed, reason := scoreGraphNegativeCase(neg, graphNegativeInput{
		Resolution:  graph.ResolutionResolved,
		ResultCount: 1,
		Results: []graph.QueryResult{{
			NodeKind: graph.NodeKindChunk,
		}},
	})
	assert.False(t, passed)
	assert.Contains(t, reason, "resolution")
}

func TestScoreGraphNegativeCase_BudgetExhaustionRequiresWarningNotSilentEmpty(t *testing.T) {
	neg := GraphNegativeExpectation{
		MinResults:           1,
		ExpectedWarningCodes: []graph.WarningCode{graph.WarningTraversalBudgetExhausted},
		ExpectedLabels:       []GraphDegradationLabel{DegradationTraversalBudgetExhausted},
	}

	passed, _ := scoreGraphNegativeCase(neg, graphNegativeInput{
		ResultCount: 2,
		WarningCodes: []graph.WarningCode{graph.WarningTraversalBudgetExhausted},
		DegradationLabels: []GraphDegradationLabel{DegradationTraversalBudgetExhausted},
	})
	assert.True(t, passed)

	passed, reason := scoreGraphNegativeCase(neg, graphNegativeInput{
		ResultCount: 0,
		WarningCodes: []graph.WarningCode{graph.WarningTraversalBudgetExhausted},
		DegradationLabels: []GraphDegradationLabel{DegradationTraversalBudgetExhausted},
	})
	assert.False(t, passed)
	assert.Contains(t, reason, "silent empty")

	passed, reason = scoreGraphNegativeCase(neg, graphNegativeInput{
		ResultCount: 2,
	})
	assert.False(t, passed)
	assert.Contains(t, reason, "missing expected warning")
}

func TestLoadGraphCorpus_ParsesNegativeAdversarialClass(t *testing.T) {
	path := writeTempGraphCorpus(t, `
schema_version: 1
queries:
  - id: GRA-NEG-Q01
    name: ambiguous symbol must not merge
    mode: explain_symbol
    query: QueryResult
    subsets: [quick]
    holdout: false
    source: task-gra26
    expectation_class: negative_adversarial
    negative:
      expected_resolution: disambiguation_required
      min_candidates: 2
      max_results: 0
      prohibited_evidence:
        - node_kind: chunk
          relation: symbol_has_chunk
          rationale: must not merge ambiguous definitions
    metadata:
      owner_task: TASK-GRA26
      family: ambiguous
`)

	corpus, err := LoadGraphCorpus(path)
	require.NoError(t, err)
	require.Len(t, corpus.Queries, 1)
	q := corpus.Queries[0]
	assert.Equal(t, GraphExpectationClassNegativeAdversarial, q.ExpectationClass)
	assert.Equal(t, graph.ResolutionDisambiguationRequired, q.Negative.ExpectedResolution)
	assert.Equal(t, 2, q.Negative.MinCandidates)
	require.NotNil(t, q.Negative.MaxResults)
	assert.Equal(t, 0, *q.Negative.MaxResults)
}

func TestLoadGraphCorpus_RejectsNegativeWithoutAssertions(t *testing.T) {
	_, err := LoadGraphCorpus(writeTempGraphCorpus(t, `
schema_version: 1
queries:
  - id: GRA-NEG-BAD
    name: empty negative block
    mode: explain_symbol
    query: Foo
    subsets: [quick]
    holdout: false
    source: task-gra26
    expectation_class: negative_adversarial
    negative: {}
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "negative expectation requires at least one assertion")
}

func TestGraphNegativeGateFailures_RequiresPerfectPassRate(t *testing.T) {
	assert.Nil(t, graphNegativeGateFailures(DirectGraphSummary{
		NegativeAdversarialCount:     10,
		NegativeAdversarialPassCount: 10,
		NegativeAdversarialPassRate:  1.0,
	}, GraphSubsetQuick))

	failures := graphNegativeGateFailures(DirectGraphSummary{
		NegativeAdversarialCount:     8,
		NegativeAdversarialPassCount: 6,
		NegativeAdversarialPassRate:  0.75,
	}, GraphSubsetQuick)
	require.Len(t, failures, 1)
	assert.Contains(t, failures[0], "negative_adversarial_pass_rate")
}

func TestGraphNegativeGateFailures_FailsClosedWhenGatedSubsetHasNoNegativeCases(t *testing.T) {
	failures := graphNegativeGateFailures(DirectGraphSummary{}, GraphSubsetQuick)
	require.Len(t, failures, 1)
	assert.Contains(t, failures[0], "no negative_adversarial cases")

	assert.Nil(t, graphNegativeGateFailures(DirectGraphSummary{}, "mode:explain_symbol"))
}

func TestGraphNegativeGateFailures_AllowsPartialNegativeCoverageForGatedSubset(t *testing.T) {
	assert.Nil(t, graphNegativeGateFailures(DirectGraphSummary{
		NegativeAdversarialCount:     3,
		NegativeAdversarialPassCount: 3,
		NegativeAdversarialPassRate:  1.0,
	}, GraphSubsetFull))
}