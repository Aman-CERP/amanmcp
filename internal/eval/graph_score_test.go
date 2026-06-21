package eval

import (
	"testing"

	"github.com/Aman-CERP/amanmcp/internal/graph"
	"github.com/stretchr/testify/assert"
)

// result builds a minimal graph.QueryResult keyed only on SourcePath, which is
// enough for matchesGraphEvidence when the expected row matches on source_path.
func sourcePathResult(path string) graph.QueryResult {
	return graph.QueryResult{SourcePath: path}
}

func sourcePathExpected(path string) GraphExpectedEvidence {
	return GraphExpectedEvidence{SourcePath: path, Rationale: "test"}
}

func TestScoreGraphCase_RankAwareMetrics(t *testing.T) {
	tests := []struct {
		name     string
		expected []GraphExpectedEvidence
		results  []graph.QueryResult
		want     graphCaseMetrics
	}{
		{
			name:     "single expected matched at top of three distinct results",
			expected: []GraphExpectedEvidence{sourcePathExpected("a.go")},
			results:  []graph.QueryResult{sourcePathResult("a.go"), sourcePathResult("b.go"), sourcePathResult("c.go")},
			want: graphCaseMetrics{
				ExpectedRecallAt10: 1.0,
				PrecisionAt3:       1.0 / 3.0,
				PrecisionAt5:       1.0 / 3.0,
				PrecisionAt10:      1.0 / 3.0, // 1 relevant unique result / 3 unique in window
				HitAt3:             true,
				HitAt10:            true,
				MatchedExpected:    1,
				MatchedPositions:   1,
				WindowSize:         3,
				UniqueResultCount:  3,
			},
		},
		{
			// Three identical-identity rows collapse to one unique result for
			// precision: a broad row matched ten ways is one relevant result, not
			// ten. Recall/hits stay on the raw window (owned by GRA12/13/14).
			name:     "duplicate identities collapse for precision denominator",
			expected: []GraphExpectedEvidence{sourcePathExpected("a.go")},
			results:  []graph.QueryResult{sourcePathResult("a.go"), sourcePathResult("a.go"), sourcePathResult("a.go")},
			want: graphCaseMetrics{
				ExpectedRecallAt10: 1.0,
				PrecisionAt3:       1.0,
				PrecisionAt5:       1.0,
				PrecisionAt10:      1.0, // 1 relevant unique / 1 unique
				HitAt3:             true,
				HitAt10:            true,
				MatchedExpected:    1,
				MatchedPositions:   3, // raw-window diagnostic unchanged
				WindowSize:         3,
				UniqueResultCount:  1,
			},
		},
		{
			name:     "hit at raw rank 3 (index 3) is a top-10 hit but not a top-3 hit",
			expected: []GraphExpectedEvidence{sourcePathExpected("target.go")},
			results: []graph.QueryResult{
				sourcePathResult("x.go"), sourcePathResult("x.go"), sourcePathResult("x.go"),
				sourcePathResult("target.go"),
			},
			want: graphCaseMetrics{
				ExpectedRecallAt10: 1.0,
				// Unique results: [x.go, target.go]. precision window = 2, 1 relevant.
				PrecisionAt3:      1.0 / 2.0,
				PrecisionAt5:      1.0 / 2.0,
				PrecisionAt10:     1.0 / 2.0,
				HitAt3:            false, // hit ranks stay on the raw window
				HitAt10:           true,
				MatchedExpected:   1,
				MatchedPositions:  1,
				WindowSize:        4,
				UniqueResultCount: 2,
			},
		},
		{
			name:     "no expected matched yields zero precision",
			expected: []GraphExpectedEvidence{sourcePathExpected("missing.go")},
			results:  []graph.QueryResult{sourcePathResult("a.go"), sourcePathResult("b.go")},
			want: graphCaseMetrics{
				ExpectedRecallAt10: 0.0,
				PrecisionAt3:       0.0,
				PrecisionAt5:       0.0,
				PrecisionAt10:      0.0,
				HitAt3:             false,
				HitAt10:            false,
				MatchedExpected:    0,
				MatchedPositions:   0,
				WindowSize:         2,
				UniqueResultCount:  2,
			},
		},
		{
			name: "duplicate noise collapses; short denominator over unique results",
			expected: []GraphExpectedEvidence{
				sourcePathExpected("found.go"),
				sourcePathExpected("absent.go"),
			},
			results: []graph.QueryResult{
				sourcePathResult("found.go"), sourcePathResult("noise.go"), sourcePathResult("noise.go"),
				sourcePathResult("noise.go"), sourcePathResult("noise.go"), sourcePathResult("noise.go"),
				sourcePathResult("noise.go"), sourcePathResult("noise.go"), sourcePathResult("noise.go"),
				sourcePathResult("noise.go"), sourcePathResult("found.go"), // 11th result outside raw window
			},
			want: graphCaseMetrics{
				ExpectedRecallAt10: 0.5, // found.go matched, absent.go not (raw window)
				// Unique results: [found.go, noise.go]. window = 2, 1 relevant.
				PrecisionAt3:      1.0 / 2.0,
				PrecisionAt5:      1.0 / 2.0,
				PrecisionAt10:     1.0 / 2.0,
				HitAt3:            true,
				HitAt10:           true,
				MatchedExpected:   1,
				MatchedPositions:  1, // only index 0 within the 10-wide raw window
				WindowSize:        10,
				UniqueResultCount: 2,
			},
		},
		{
			name:     "empty results yields zero precision and zero unique count",
			expected: []GraphExpectedEvidence{sourcePathExpected("a.go")},
			results:  nil,
			want: graphCaseMetrics{
				ExpectedRecallAt10: 0.0,
				PrecisionAt3:       0.0,
				PrecisionAt5:       0.0,
				PrecisionAt10:      0.0,
				HitAt3:             false,
				HitAt10:            false,
				MatchedExpected:    0,
				MatchedPositions:   0,
				WindowSize:         0,
				UniqueResultCount:  0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scoreGraphCase(tt.expected, nil, tt.results, graph.QueryModeFindReferences)

			assert.InDelta(t, tt.want.ExpectedRecallAt10, got.ExpectedRecallAt10, 0.0001, "recall")
			assert.InDelta(t, tt.want.PrecisionAt3, got.PrecisionAt3, 0.0001, "precision@3")
			assert.InDelta(t, tt.want.PrecisionAt5, got.PrecisionAt5, 0.0001, "precision@5")
			assert.InDelta(t, tt.want.PrecisionAt10, got.PrecisionAt10, 0.0001, "precision@10")
			assert.Equal(t, tt.want.HitAt3, got.HitAt3, "hit@3")
			assert.Equal(t, tt.want.HitAt10, got.HitAt10, "hit@10")
			assert.Equal(t, tt.want.MatchedExpected, got.MatchedExpected, "matched expected")
			assert.Equal(t, tt.want.MatchedPositions, got.MatchedPositions, "matched positions")
			assert.Equal(t, tt.want.WindowSize, got.WindowSize, "window size")
			assert.Equal(t, tt.want.UniqueResultCount, got.UniqueResultCount, "unique result count")
		})
	}
}

// TestSummarizeDirectGraphByMode_QualityAggregatesExcludeNonQuality proves the
// per-mode relevance aggregates (TASK-GRA12/13/14) are macro-averaged over only
// measured, quality-class, non-blocking cases — degraded/gap and unmeasured
// cases are reported (MeasuredCount) but excluded from the quality denominator.
func TestSummarizeDirectGraphByMode_QualityAggregatesExcludeNonQuality(t *testing.T) {
	results := []DirectGraphQueryResult{
		{ // quality, scored, perfect top hit
			Mode: graph.QueryModeFindReferences, ExpectationClass: GraphExpectationClassQuality,
			Status: graph.GraphStatusPartial, ResultCount: 10, Scored: true,
			ExpectedRecallAt10: 1.0, PrecisionAt10: 1.0, HitAt3: true, HitAt10: true, Passed: true,
		},
		{ // quality, scored, partial relevance, hit only beyond rank 3
			Mode: graph.QueryModeFindReferences, ExpectationClass: GraphExpectationClassQuality,
			Status: graph.GraphStatusPartial, ResultCount: 10, Scored: true,
			ExpectedRecallAt10: 0.5, PrecisionAt10: 0.25, HitAt3: false, HitAt10: true,
		},
		{ // degraded class, scored — excluded from quality aggregates by class
			Mode: graph.QueryModeFindReferences, ExpectationClass: GraphExpectationClassDegraded,
			Status: graph.GraphStatusPartial, ResultCount: 0, Scored: true,
			ExpectedRecallAt10: 0.0, PrecisionAt10: 0.0, HitAt3: false, HitAt10: false,
		},
		{ // quality but NOT scored (empty graph not servable) — excluded
			Mode: graph.QueryModeFindReferences, ExpectationClass: GraphExpectationClassQuality,
			Status: graph.GraphStatusEmpty, ResultCount: 0,
		},
	}

	byMode := summarizeDirectGraphByMode(results)
	fr := byMode[graph.QueryModeFindReferences]

	assert.Equal(t, 4, fr.QueryCount, "all cases counted in QueryCount")
	assert.Equal(t, 3, fr.MeasuredCount, "three servable-status cases are measured")
	assert.Equal(t, 2, fr.QualityCount, "only the two measured quality cases score relevance")
	assert.InDelta(t, 0.75, fr.ExpectedRecallAt10, 0.0001, "(1.0+0.5)/2")
	assert.InDelta(t, 0.625, fr.PrecisionAt10, 0.0001, "(1.0+0.25)/2")
	assert.InDelta(t, 0.5, fr.HitRateAt3, 0.0001, "1 of 2 quality cases hit within top 3")
	assert.InDelta(t, 1.0, fr.HitRateAt10, 0.0001, "2 of 2 quality cases hit within top 10")
}

// TestSummarizeDirectGraphByMode_EmptyQualitySetDoesNotInventScores guards
// against vacuous passes: a mode with cases but no measured quality case must
// report zero aggregates and QualityCount 0 rather than a misleading 1.0.
func TestSummarizeDirectGraphByMode_EmptyQualitySetDoesNotInventScores(t *testing.T) {
	results := []DirectGraphQueryResult{
		{
			Mode: graph.QueryModeImpactAnalysis, ExpectationClass: GraphExpectationClassGap,
			Status: graph.GraphStatusPartial, ResultCount: 0,
		},
	}

	ia := summarizeDirectGraphByMode(results)[graph.QueryModeImpactAnalysis]

	assert.Equal(t, 0, ia.QualityCount)
	assert.Zero(t, ia.ExpectedRecallAt10)
	assert.Zero(t, ia.PrecisionAt10)
	assert.Zero(t, ia.HitRateAt3)
	assert.Zero(t, ia.HitRateAt10)
}

// TestScoreGraphCase_MatchedExpectedAgreesWithLegacyCounter proves the new
// scorer's MatchedExpected stays consistent with the pre-existing
// matchedGraphEvidenceCount within the rank window, so the matching contract
// cannot silently drift between the two code paths.
func TestScoreGraphCase_MatchedExpectedAgreesWithLegacyCounter(t *testing.T) {
	expected := []GraphExpectedEvidence{
		sourcePathExpected("a.go"),
		sourcePathExpected("b.go"),
		sourcePathExpected("missing.go"),
	}
	results := []graph.QueryResult{
		sourcePathResult("a.go"), sourcePathResult("b.go"), sourcePathResult("c.go"),
	}

	metrics := scoreGraphCase(expected, nil, results, graph.QueryModeFindReferences)
	legacy := matchedGraphEvidenceCount(expected, results)

	assert.Equal(t, legacy, metrics.MatchedExpected,
		"scoreGraphCase MatchedExpected must agree with matchedGraphEvidenceCount when all matches fall inside the window")
}

// TestGraphResultIdentity_StableTuple proves the find_references (base) dedup key
// is exactly source_path|node_kind|relation|role and that two results agree iff
// every tuple field agrees. node_id, confidence, and graph_path are intentionally
// excluded from the base key: node_id is index-volatile, confidence is a ranking
// input, and graph_path embeds per-result node IDs (so two distinct chunks of the
// same file — the dominant duplicate shape — collapse to one identity, which is
// the dedup the ticket requires).
func TestGraphResultIdentity_StableTuple(t *testing.T) {
	const mode = graph.QueryModeFindReferences
	base := graph.QueryResult{
		NodeID:     "node-1",
		SourcePath: "internal/graph/query.go",
		NodeKind:   graph.NodeKindChunk,
		Relation:   graph.EdgeKindSymbolHasChunk,
		Role:       "related",
		GraphPath:  []string{"seed", "symbol_has_chunk", "node:chunk:p:a"},
	}

	// Same matcher fields, different node_id + confidence + graph_path (distinct
	// chunk nodes / edge paths of the same file) => same identity, so they
	// collapse for precision.
	sameTuple := base
	sameTuple.NodeID = "node-2"
	sameTuple.Confidence = 0.9
	sameTuple.GraphPath = []string{"seed", "symbol_has_chunk", "node:chunk:p:b"}
	assert.Equal(t, graphResultIdentity(base, mode), graphResultIdentity(sameTuple, mode),
		"node_id, confidence, and graph_path must not affect the find_references identity")

	// Differing relation/role/kind/source_path each break identity.
	for _, mut := range []func(*graph.QueryResult){
		func(r *graph.QueryResult) { r.SourcePath = "other.go" },
		func(r *graph.QueryResult) { r.NodeKind = graph.NodeKindSymbol },
		func(r *graph.QueryResult) { r.Relation = graph.EdgeKindFileDefinesSymbol },
		func(r *graph.QueryResult) { r.Role = "downstream" },
	} {
		mutated := base
		mut(&mutated)
		assert.NotEqual(t, graphResultIdentity(base, mode), graphResultIdentity(mutated, mode),
			"each tuple field must contribute to identity")
	}
}

// TestGraphResultIdentity_PerModeImpactAnalysis proves the per-mode identity
// (DEBT-037 finding #1): impact_analysis appends node_id so distinct targets that
// share the seed's source_path/role/relation stay distinct, while find_references
// keeps the base key so those same rows collapse.
func TestGraphResultIdentity_PerModeImpactAnalysis(t *testing.T) {
	// Two impact targets sharing every base-key field, differing only by node_id —
	// exactly the impact_analysis shape (downstream of one seed).
	targetA := graph.QueryResult{
		NodeID:     "node:symbol:p:caller_a",
		SourcePath: "internal/search/engine.go",
		NodeKind:   graph.NodeKindSymbol,
		Relation:   graph.EdgeKindFileDefinesSymbol,
		Role:       "downstream",
	}
	targetB := targetA
	targetB.NodeID = "node:symbol:p:caller_b"

	// impact_analysis: node_id participates, so the two targets are distinct.
	assert.NotEqual(t,
		graphResultIdentity(targetA, graph.QueryModeImpactAnalysis),
		graphResultIdentity(targetB, graph.QueryModeImpactAnalysis),
		"impact_analysis must keep distinct targets distinct via node_id")

	// find_references: node_id is excluded, so the same two rows collapse.
	assert.Equal(t,
		graphResultIdentity(targetA, graph.QueryModeFindReferences),
		graphResultIdentity(targetB, graph.QueryModeFindReferences),
		"find_references must collapse rows that share the base key")

	// Same node_id under impact_analysis still collapses (idempotent identity).
	assert.Equal(t,
		graphResultIdentity(targetA, graph.QueryModeImpactAnalysis),
		graphResultIdentity(targetA, graph.QueryModeImpactAnalysis),
		"identical results share one identity in every mode")
}

// TestScoreGraphCase_ImpactAnalysisKeepsDistinctTargets proves the per-mode
// identity flows into precision@K: distinct impact targets do NOT collapse under
// impact_analysis (so precision measures distinct-target fraction) but the same
// rows DO collapse under find_references (DEBT-037 finding #1 + the find_references
// invariance non-goal).
func TestScoreGraphCase_ImpactAnalysisKeepsDistinctTargets(t *testing.T) {
	// Three downstream targets of one seed: same base key, distinct node_id, two of
	// them relevant (match expected node_ids), one noise.
	base := graph.QueryResult{
		SourcePath: "internal/search/engine.go",
		NodeKind:   graph.NodeKindSymbol,
		Relation:   graph.EdgeKindFileDefinesSymbol,
		Role:       "downstream",
	}
	mk := func(id string) graph.QueryResult { r := base; r.NodeID = id; return r }
	results := []graph.QueryResult{mk("caller_a"), mk("caller_b"), mk("noise")}
	expected := []GraphExpectedEvidence{
		{NodeID: "caller_a", Rationale: "target a"},
		{NodeID: "caller_b", Rationale: "target b"},
	}

	impact := scoreGraphCase(expected, nil, results, graph.QueryModeImpactAnalysis)
	assert.Equal(t, 3, impact.UniqueResultCount, "impact_analysis keeps the three distinct targets")
	assert.InDelta(t, 2.0/3.0, impact.PrecisionAt10, 0.0001, "2 of 3 distinct targets relevant")

	// Under find_references the three rows collapse to one identity; node_id-only
	// matchers then score the single first-seen survivor.
	findRefs := scoreGraphCase(expected, nil, results, graph.QueryModeFindReferences)
	assert.Equal(t, 1, findRefs.UniqueResultCount, "find_references collapses the shared base key")
}

// TestScoreGraphCase_PrecisionRecallWindowConsistency proves precision and
// recall/hits never disagree about whether a relevant row exists: precision is
// deduped over the SAME raw rank window recall uses, so a relevant result beyond
// the window cannot inflate precision while recall reports zero. Guards the
// (latent) asymmetry where deduping the full list could pull a rank-10+ row into
// the deduped top-K.
func TestScoreGraphCase_PrecisionRecallWindowConsistency(t *testing.T) {
	// Ten leading results, then the only relevant row at raw rank 10 — beyond the
	// top-10 window. It must be invisible to BOTH precision and recall.
	results := make([]graph.QueryResult, 0, 11)
	for i := 0; i < 10; i++ {
		results = append(results, sourcePathResult("noise.go"))
	}
	results = append(results, sourcePathResult("target.go"))

	got := scoreGraphCase([]GraphExpectedEvidence{sourcePathExpected("target.go")}, nil, results, graph.QueryModeFindReferences)

	assert.Equal(t, 0.0, got.PrecisionAt10, "relevant row beyond the raw window must not count toward precision")
	assert.InDelta(t, 0.0, got.ExpectedRecallAt10, 0.0001, "relevant row beyond the raw window is not recalled")
	assert.False(t, got.HitAt10, "no hit within the raw window")
	// The core invariant: precision>0 implies a hit within the window.
	if got.PrecisionAt10 > 0 {
		assert.True(t, got.HitAt10, "precision>0 must imply hit@10 — windows must agree")
	}

	// A relevant row WITHIN the window is seen by both families.
	within := []graph.QueryResult{sourcePathResult("target.go"), sourcePathResult("noise.go")}
	gotWithin := scoreGraphCase([]GraphExpectedEvidence{sourcePathExpected("target.go")}, nil, within, graph.QueryModeFindReferences)
	assert.Greater(t, gotWithin.PrecisionAt10, 0.0)
	assert.True(t, gotWithin.HitAt10, "in-window relevant row is a hit")
}

// TestScoreGraphCase_AcceptedAlternativesCountAsRelevant proves a result that
// matches an accepted alternative (not a required expected row) counts toward
// precision but NOT toward recall, and a result matching both expected and an
// alternative is not double-counted.
func TestScoreGraphCase_AcceptedAlternativesCountAsRelevant(t *testing.T) {
	expected := []GraphExpectedEvidence{sourcePathExpected("primary.go")}
	alternatives := []GraphExpectedEvidence{sourcePathExpected("alt.go")}
	results := []graph.QueryResult{
		sourcePathResult("primary.go"), // matches expected
		sourcePathResult("alt.go"),     // matches accepted alternative only
		sourcePathResult("noise.go"),   // irrelevant
	}

	got := scoreGraphCase(expected, alternatives, results, graph.QueryModeFindReferences)

	// Recall is over expected only: 1/1.
	assert.InDelta(t, 1.0, got.ExpectedRecallAt10, 0.0001, "recall counts expected only")
	// Precision relevance includes the alternative: 2 of 3 unique results relevant.
	assert.InDelta(t, 2.0/3.0, got.PrecisionAt10, 0.0001, "alternative counts as relevant for precision")
	assert.Equal(t, 3, got.UniqueResultCount)

	// A result matching BOTH expected and an alternative is counted once.
	overlap := scoreGraphCase(
		[]GraphExpectedEvidence{sourcePathExpected("shared.go")},
		[]GraphExpectedEvidence{sourcePathExpected("shared.go")},
		[]graph.QueryResult{sourcePathResult("shared.go"), sourcePathResult("noise.go")},
		graph.QueryModeFindReferences,
	)
	assert.InDelta(t, 1.0/2.0, overlap.PrecisionAt10, 0.0001, "expected+alternative overlap is not double-counted")
}

// TestScoreGraphCase_FindReferencesIdentityIsNodeIDInvariant is the AC2
// regression guard for DEBT-037's find_references non-goal: the per-mode identity
// must NOT change find_references precision. node_id is the only field the new
// per-mode rule adds (for impact_analysis), so we prove find_references precision
// is invariant to node_id — three chunk rows of one file (distinct node_ids)
// collapse to one unique result exactly as before, and precision is unchanged
// whether the node_ids are equal or distinct.
func TestScoreGraphCase_FindReferencesIdentityIsNodeIDInvariant(t *testing.T) {
	base := graph.QueryResult{
		SourcePath: "internal/search/engine.go",
		NodeKind:   graph.NodeKindChunk,
		Relation:   graph.EdgeKindSymbolHasChunk,
		Role:       "related",
	}
	withID := func(id string) graph.QueryResult { r := base; r.NodeID = id; return r }
	expected := []GraphExpectedEvidence{sourcePathExpected("internal/search/engine.go")}

	distinctIDs := scoreGraphCase(expected, nil,
		[]graph.QueryResult{withID("c1"), withID("c2"), withID("c3")}, graph.QueryModeFindReferences)
	sameIDs := scoreGraphCase(expected, nil,
		[]graph.QueryResult{withID("c1"), withID("c1"), withID("c1")}, graph.QueryModeFindReferences)

	assert.Equal(t, 1, distinctIDs.UniqueResultCount, "find_references collapses chunk rows regardless of node_id")
	assert.Equal(t, sameIDs.UniqueResultCount, distinctIDs.UniqueResultCount, "node_id must not affect find_references dedup")
	assert.Equal(t, sameIDs.PrecisionAt10, distinctIDs.PrecisionAt10, "find_references precision@10 is node_id-invariant")
	assert.Equal(t, sameIDs.PrecisionAt3, distinctIDs.PrecisionAt3, "find_references precision@3 is node_id-invariant")
	assert.InDelta(t, 1.0, distinctIDs.PrecisionAt10, 0.0001, "the single collapsed chunk is relevant")
}

// TestGraphPrecisionIdentityAmbiguity_FlagsNonUniformBucket proves the runtime
// guard (DEBT-037 finding #2): when two results share a per-mode identity but
// disagree on relevance, the precision dedup survivor is order-dependent, so the
// bucket is flagged. A relevance-uniform bucket is not flagged, and distinct
// identities are independent. The corpus legitimately matches on out-of-identity
// fields (confidence_label/evidence_method/graph_path), so this is validated
// against real returned results rather than statically rejecting those matchers.
func TestGraphPrecisionIdentityAmbiguity_FlagsNonUniformBucket(t *testing.T) {
	expected := []GraphExpectedEvidence{{
		SourcePath:      "a.go",
		ConfidenceLabel: graph.ConfidenceExact,
		Rationale:       "exact-confidence target",
	}}
	// Same find_references base identity (source_path/kind/relation/role all agree),
	// but confidence_label — outside the identity — flips relevance.
	notRelevant := graph.QueryResult{SourcePath: "a.go", ConfidenceLabel: graph.ConfidenceLow}
	relevant := graph.QueryResult{SourcePath: "a.go", ConfidenceLabel: graph.ConfidenceExact}

	ambiguous := graphPrecisionIdentityAmbiguity(
		[]graph.QueryResult{notRelevant, relevant}, expected, nil, graph.QueryModeFindReferences)
	assert.NotEmpty(t, ambiguous, "non-uniform relevance within one identity must be flagged")

	// Uniform bucket: both relevant (same confidence), differing only by node_id
	// (excluded from the find_references identity) — must NOT be flagged.
	relevant2 := relevant
	relevant2.NodeID = "n2"
	clean := graphPrecisionIdentityAmbiguity(
		[]graph.QueryResult{relevant, relevant2}, expected, nil, graph.QueryModeFindReferences)
	assert.Empty(t, clean, "relevance-uniform identity bucket must not be flagged")

	// Distinct identities (different source_path) are never ambiguous even with
	// differing relevance across them.
	other := graph.QueryResult{SourcePath: "b.go", ConfidenceLabel: graph.ConfidenceLow}
	distinct := graphPrecisionIdentityAmbiguity(
		[]graph.QueryResult{relevant, other}, expected, nil, graph.QueryModeFindReferences)
	assert.Empty(t, distinct, "different identities are independent")
}

// TestScoreGraphCase_PrecisionAtKWindows proves precision@3/@5/@10 use distinct
// rank windows over the deduped result list, and a short (<K) list uses the
// returned unique count as the denominator rather than K.
func TestScoreGraphCase_PrecisionAtKWindows(t *testing.T) {
	// One relevant result at rank 0, then six distinct noise results.
	results := []graph.QueryResult{
		sourcePathResult("hit.go"),
		sourcePathResult("n1.go"), sourcePathResult("n2.go"), sourcePathResult("n3.go"),
		sourcePathResult("n4.go"), sourcePathResult("n5.go"), sourcePathResult("n6.go"),
	}
	got := scoreGraphCase([]GraphExpectedEvidence{sourcePathExpected("hit.go")}, nil, results, graph.QueryModeFindReferences)

	assert.InDelta(t, 1.0/3.0, got.PrecisionAt3, 0.0001, "precision@3 over first 3 unique")
	assert.InDelta(t, 1.0/5.0, got.PrecisionAt5, 0.0001, "precision@5 over first 5 unique")
	assert.InDelta(t, 1.0/7.0, got.PrecisionAt10, 0.0001, "precision@10 over all 7 unique (< K)")
	assert.Equal(t, 7, got.UniqueResultCount)
}
