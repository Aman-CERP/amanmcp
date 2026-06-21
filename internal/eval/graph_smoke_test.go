package eval

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Aman-CERP/amanmcp/internal/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// DEBT-038: real-data graph-eval smoke test (precision-identity ambiguity guard).
//
// The direct graph-scoring pipeline — scoreGraphCase, dedupeGraphResults,
// graphResultIdentity, matchesGraphEvidence, and the DEBT-037 runtime guard
// graphPrecisionIdentityAmbiguity — is exercised elsewhere only through the
// synthetic fakeDirectGraphClient, which hand-builds graph.QueryResult rows. That
// leaves the guard unexercised against output from the REAL extractor + REAL
// SQLiteDirectGraphClient, so a corpus or extractor regression that broke the
// "relevance is uniform within each per-mode identity bucket" invariant would not
// surface until TASK-GRA17's real-data gate, and then only as a per-case contract
// failure rather than a clear "your corpus tripped the guard" signal.
//
// This test closes that gap. It indexes a minimal Go fixture into a real graph.db
// via the production cheap extractor (graph.IndexCheapEdges), then runs the full
// direct graph.query eval pipeline against the real client (NOT the fake) over a
// dedicated fixture corpus, asserting the guard does not trip and the run is
// GraphToolMeasured. The fixture is deliberately shaped so find_references returns
// a genuine multi-member identity bucket (two symbols defined by one file collapse
// to source_path|symbol|file_defines_symbol|related), so the guard's uniformity
// comparison actually executes on real data instead of passing vacuously over
// singleton buckets. A real-data negative control then proves the guard still fires
// when a matcher discriminates within that bucket.
//
// PLACEMENT (per the ticket's "decide placement" step): this is a plain Go test in
// internal/eval. `make ci-check` runs `go test ./...` (no -short gate), so the
// smoke test is already on the CI path without a dedicated make target. The fixture
// is in-memory, hits no network or embedder, and builds one tiny SQLite DB, so the
// runtime cost is negligible — a separate target would add surface area for no gain.
//
// Scope is coverage only: no product code changes, no corpus expansion.

const smokeProjectID = "debt038-smoke"

const smokeFixtureFile = "pkg/core/types.go"

// buildSmokeGraphDB indexes a minimal fixture repo into a real graph.db at
// dir/graph.db using the production cheap extractor, then closes the build handle
// so the eval client can open its own connection to the same database.
//
// The fixture is one Go file declaring a package, importing fmt, and defining two
// symbols (Alpha, Beta) carried as chunk symbol metadata. That single file is
// enough to produce, for find_references, a multi-member identity bucket (the two
// file_defines_symbol rows) plus singleton buckets for the package, import, and
// chunk edges — a representative real result set across the scoring pipeline.
func buildSmokeGraphDB(t *testing.T, dir string) {
	t.Helper()
	ctx := context.Background()

	repo, err := graph.OpenSQLiteRepository(filepath.Join(dir, "graph.db"))
	require.NoError(t, err, "open fixture graph repository")

	files := []graph.SourceFile{
		{
			Path:        smokeFixtureFile,
			Language:    "go",
			ContentType: graph.SourceContentTypeCode,
			Content: []byte("package core\n\n" +
				"import \"fmt\"\n\n" +
				"// Alpha is a fixture type.\n" +
				"type Alpha struct{}\n\n" +
				"// Beta is a fixture function.\n" +
				"func Beta() { fmt.Println(\"beta\") }\n"),
			Chunks: []graph.SourceChunk{
				{
					ID:        smokeFixtureFile + "#chunk0",
					FilePath:  smokeFixtureFile,
					Language:  "go",
					StartLine: 1,
					EndLine:   9,
					Symbols: []graph.SourceSymbol{
						{Name: "Alpha", Kind: "type", StartLine: 6, EndLine: 6, Signature: "type Alpha struct{}"},
						{Name: "Beta", Kind: "function", StartLine: 9, EndLine: 9, Signature: "func Beta()"},
					},
				},
			},
		},
	}

	// IndexCheapEdges records GraphStatusFresh when extraction is clean, which is
	// what makes the run servable (and therefore GraphToolMeasured) below.
	require.NoError(t, graph.IndexCheapEdges(ctx, repo, smokeProjectID, files, graph.CheapExtractorOptions{}),
		"index fixture cheap edges")
	// Close the build handle before the eval client opens its own; the client opens
	// dir/graph.db independently via NewSQLiteDirectGraphClient.
	require.NoError(t, repo.Close(), "close fixture graph repository")
}

// smokeCorpus is the dedicated fixture corpus. Every expected matcher keys on
// stable identity fields (source_path + relation) so it matches whole identity
// buckets — the safe shape the guard expects on a clean corpus.
const smokeCorpus = `
schema_version: 1
queries:
  - id: SMOKE-FR-Q01
    name: find references to the fixture file
    mode: find_references
    query: pkg/core/types.go
    subsets: [full]
    holdout: false
    source: debt-038 smoke fixture
    expected:
      - source_path: pkg/core/types.go
        relation: file_defines_symbol
        rationale: the file defines Alpha and Beta; the whole symbol bucket is relevant
  - id: SMOKE-ES-Q01
    name: explain the fixture symbol
    mode: explain_symbol
    query: Alpha
    subsets: [full]
    holdout: false
    source: debt-038 smoke fixture
    expected:
      - source_path: pkg/core/types.go
        relation: file_defines_symbol
        rationale: explaining Alpha surfaces its defining file
  - id: SMOKE-IA-Q01
    name: impact of the fixture file
    mode: impact_analysis
    query: pkg/core/types.go
    subsets: [full]
    holdout: false
    source: debt-038 smoke fixture
    expected:
      - source_path: pkg/core/types.go
        relation: file_defines_symbol
        rationale: changing the file impacts the symbols it defines
`

// TestDirectGraphEval_RealDataSmoke is the DEBT-038 coverage gate: it runs the full
// direct graph.query eval pipeline against a real graph.db built by the production
// extractor and asserts the precision-identity ambiguity guard does not trip and the
// run is GraphToolMeasured.
func TestDirectGraphEval_RealDataSmoke(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	buildSmokeGraphDB(t, dir)

	corpusPath := writeTempGraphCorpus(t, smokeCorpus)
	runner := NewDirectGraphRunner(NewSQLiteDirectGraphClient(dir, smokeProjectID))
	report, err := runner.Run(ctx, GraphOptions{
		CorpusPath: corpusPath,
		Subset:     GraphSubsetFull,
		Output:     "json",
		OutDir:     filepath.Join(dir, "out"),
		Command:    "debt-038 real-data graph-eval smoke test",
	})
	require.NoError(t, err, "real-data graph eval must run without infrastructure error")
	require.NotNil(t, report)
	require.Len(t, report.Queries, 3, "all three fixture cases must be evaluated")

	// AC: graph_tool_measured == true (guards against silent search-fallback /
	// unmeasured runs over an empty or unavailable graph).
	assert.True(t, report.GraphToolMeasured,
		"graph.query must be measured against the real client; unmeasured reason: %q", report.UnmeasuredReason)
	assert.Empty(t, report.UnmeasuredReason)

	// The fixture build must yield a genuinely fresh, available graph. Asserting
	// this at the snapshot level localizes a future IndexCheapEdges status
	// regression to the build step rather than letting it surface only as a
	// downstream measurement failure.
	require.NotEmpty(t, report.StatusSnapshots, "run must capture a graph status snapshot")
	assert.True(t, report.StatusSnapshots[0].Available, "fixture graph must be available")
	assert.Equal(t, graph.GraphStatusFresh, report.StatusSnapshots[0].Status,
		"a clean fixture build must produce a fresh graph")

	// AC: zero precision-identity ambiguity contract failures across the evaluated
	// cases, failing loudly with the offending identity if the invariant breaks.
	// Also assert every case served a healthy (servable) status, so the guard ran on
	// real evidence rather than being skipped by a transport error or empty graph.
	for _, q := range report.Queries {
		assert.NotContainsf(t, q.FailureReason, "precision identity ambiguity",
			"case %s tripped the precision-identity guard on real data: %s", q.ID, q.FailureReason)
		assert.Truef(t, graph.QueryServable(q.Status),
			"case %s status %q must be servable for the guard to be exercised", q.ID, q.Status)
		assert.Truef(t, directGraphResultMeasured(q),
			"case %s must be a measured graph.query result", q.ID)
	}

	// Non-vacuity: prove the guard's uniformity comparison actually executed on real
	// data by confirming find_references returned a genuine multi-member identity
	// bucket. Without this, a fixture of all-unique results would let the guard pass
	// trivially and the test would assert nothing about the dedup invariant.
	fr := graphQueryResultByID(t, report, "SMOKE-FR-Q01")
	require.NotEmpty(t, fr.Results, "find_references must return real results")
	bucketSize := maxIdentityBucketSize(fr.Results, fr.Mode)
	t.Logf("find_references over real graph: %d results, largest identity bucket = %d, measured=%t",
		fr.ResultCount, bucketSize, report.GraphToolMeasured)
	assert.GreaterOrEqual(t, bucketSize, 2,
		"find_references must return a real >=2-member identity bucket so the guard does real work")

	// Real-data negative control: the guard must still fire when a matcher
	// discriminates WITHIN a shared-identity bucket (DEBT-037's exact failure mode).
	// Alpha and Beta collapse to one identity; a graph_path_contains matcher pinned to
	// Alpha's volatile node id matches Alpha but not Beta, so their shared bucket
	// disagrees on relevance and the guard must return that identity.
	alphaID := fileDefinesSymbolNodeID(fr.Results, "Alpha")
	require.NotEmpty(t, alphaID, "fixture must define an Alpha file_defines_symbol result")
	discriminating := []GraphExpectedEvidence{{
		SourcePath:        smokeFixtureFile,
		Relation:          graph.EdgeKindFileDefinesSymbol,
		GraphPathContains: []string{alphaID},
		Rationale:         "negative control: discriminates within a shared-identity bucket",
	}}
	ambiguous := graphPrecisionIdentityAmbiguity(fr.Results, discriminating, nil, fr.Mode)
	t.Logf("negative control: discriminating matcher on Alpha (%s) -> guard returned identity %q", alphaID, ambiguous)
	assert.NotEmpty(t, ambiguous,
		"guard must detect ambiguity when a real bucket's members disagree on relevance")
}

// maxIdentityBucketSize returns the size of the largest per-mode identity bucket in
// results, mirroring how graphPrecisionIdentityAmbiguity and dedupeGraphResults group
// rows. A value >= 2 proves at least one bucket has collapsible duplicates.
func maxIdentityBucketSize(results []graph.QueryResult, mode string) int {
	buckets := make(map[string]int, len(results))
	largest := 0
	for _, r := range results {
		id := graphResultIdentity(r, mode)
		buckets[id]++
		if buckets[id] > largest {
			largest = buckets[id]
		}
	}
	return largest
}

// fileDefinesSymbolNodeID returns the node id of the file_defines_symbol result for
// the named symbol, used to craft the negative-control matcher.
func fileDefinesSymbolNodeID(results []graph.QueryResult, symbolName string) string {
	for _, r := range results {
		if r.Relation == graph.EdgeKindFileDefinesSymbol && r.Name == symbolName {
			return r.NodeID
		}
	}
	return ""
}
