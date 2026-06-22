package graph

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixtureNodes builds a small, deterministic node set that mirrors the real
// graph's field semantics: file nodes carry Key==SourcePath==path, symbol nodes
// carry Key==`path#name:line`. Two distinct `Search` symbols live in different
// packages (the canonical disambiguation case), and one file plus its in-file
// symbols/chunks share a source_path (the file-scope case).
func fixtureNodes() []Node {
	return []Node{
		// File subject: file node + two in-file symbols + one chunk, all sharing
		// source_path. A path query must resolve to this single file subject and
		// seed its whole scope.
		{ID: "n-file", Kind: NodeKindFile, Key: "internal/search/engine.go", SourcePath: "internal/search/engine.go", Name: "engine.go"},
		{ID: "n-engine-sym", Kind: NodeKindSymbol, Key: "internal/search/engine.go#Engine:20", SourcePath: "internal/search/engine.go", Name: "Engine", SymbolKind: "type", StartLine: 20},
		{ID: "n-search-a", Kind: NodeKindSymbol, Key: "internal/search/engine.go#Search:42", SourcePath: "internal/search/engine.go", Name: "Search", SymbolKind: "method", StartLine: 42},
		{ID: "n-chunk", Kind: NodeKindChunk, Key: "internal/search/engine.go#chunk:1", SourcePath: "internal/search/engine.go", Name: ""},

		// A second, distinct `Search` symbol in another package — together with
		// n-search-a this makes a bare `Search` query ambiguous.
		{ID: "n-search-b", Kind: NodeKindSymbol, Key: "pkg/searcher/searcher.go#Search:9", SourcePath: "pkg/searcher/searcher.go", Name: "Search", SymbolKind: "function", StartLine: 9},

		// A symbol whose name only *contains* the query token (fuzzy/substring).
		{ID: "n-searchopts", Kind: NodeKindSymbol, Key: "internal/search/options.go#SearchOptions:3", SourcePath: "internal/search/options.go", Name: "SearchOptions", SymbolKind: "type", StartLine: 3},

		// An import node (deduped to one node per package by the schema).
		{ID: "n-import", Kind: NodeKindImport, Key: "github.com/example/proj/internal/config", SourcePath: "internal/app/main.go", Name: "config"},
	}
}

// testResolveOpts uses the production defaults so resolver tests exercise the
// same candidate/hint bounds as QueryService.Query.
var testResolveOpts = resolveOptions{
	CandidateLimit: defaultGraphQueryLimit,
	HintLimit:      defaultSubjectHintLimit,
}

func seedIDs(seeds []Node) []string {
	ids := make([]string, 0, len(seeds))
	for _, s := range seeds {
		ids = append(ids, s.ID)
	}
	return ids
}

func candidateIDs(cands []Candidate) []string {
	ids := make([]string, 0, len(cands))
	for _, c := range cands {
		ids = append(ids, c.SubjectID)
	}
	return ids
}

// TestResolveSubject_Policy is the table-driven contract for the unified subject
// resolver: exact-beats-fuzzy, the minimum-score tier decides how many distinct
// subjects matched, exactly one ⇒ resolved (with the right traversal scope),
// more than one ⇒ disambiguation_required, and zero matches ⇒ subject_not_found.
func TestResolveSubject_Policy(t *testing.T) {
	nodes := fixtureNodes()

	t.Run("single exact symbol resolves to that one symbol", func(t *testing.T) {
		got := resolveSubject(nodes, "Engine", QueryModeExplainSymbol, testResolveOpts)
		require.Equal(t, ResolutionResolved, got.Outcome)
		assert.Equal(t, []string{"n-engine-sym"}, seedIDs(got.Seeds))
		assert.Empty(t, got.Candidates)
	})

	t.Run("multiple exact symbols in different packages disambiguate", func(t *testing.T) {
		got := resolveSubject(nodes, "Search", QueryModeExplainSymbol, testResolveOpts)
		require.Equal(t, ResolutionDisambiguationRequired, got.Outcome)
		assert.Empty(t, got.Seeds)
		// Both Search definitions surface; the substring-only SearchOptions does not.
		assert.ElementsMatch(t, []string{"n-search-a", "n-search-b"}, candidateIDs(got.Candidates))
		for _, c := range got.Candidates {
			assert.NotEmpty(t, c.QualifiedName, "candidate must carry a qualified name")
			assert.Equal(t, NodeKindSymbol, c.Kind)
			assert.NotEmpty(t, c.SourcePath)
			assert.Positive(t, c.Line)
		}
	})

	t.Run("exact match is preferred over substring (no disambiguation against containers)", func(t *testing.T) {
		// `Search` is an exact Name on n-search-a/b (score 2). SearchOptions only
		// contains it (score >= 8) and must never force a disambiguation here.
		got := resolveSubject(nodes, "Search", QueryModeExplainSymbol, testResolveOpts)
		require.Equal(t, ResolutionDisambiguationRequired, got.Outcome)
		assert.NotContains(t, candidateIDs(got.Candidates), "n-searchopts")
	})

	t.Run("fuzzy-only single match resolves", func(t *testing.T) {
		// "SearchOpt" matches only SearchOptions, and only as a substring.
		got := resolveSubject(nodes, "SearchOpt", QueryModeExplainSymbol, testResolveOpts)
		require.Equal(t, ResolutionResolved, got.Outcome)
		assert.Equal(t, []string{"n-searchopts"}, seedIDs(got.Seeds))
	})

	t.Run("fuzzy-only multiple matches disambiguate", func(t *testing.T) {
		// A path-decoupled set: "lpha" is a substring of both names but appears in
		// neither file path, so the only matches are fuzzy (substring) and there is
		// no exact tier to break the tie.
		local := []Node{
			{ID: "h-alpha", Kind: NodeKindSymbol, Key: "internal/x/foo.go#Alpha:1", SourcePath: "internal/x/foo.go", Name: "Alpha", StartLine: 1},
			{ID: "h-alphabet", Kind: NodeKindSymbol, Key: "internal/y/bar.go#Alphabet:2", SourcePath: "internal/y/bar.go", Name: "Alphabet", StartLine: 2},
		}
		got := resolveSubject(local, "lpha", QueryModeExplainSymbol, testResolveOpts)
		require.Equal(t, ResolutionDisambiguationRequired, got.Outcome)
		assert.ElementsMatch(t, []string{"h-alpha", "h-alphabet"}, candidateIDs(got.Candidates))
	})

	t.Run("zero match returns subject_not_found with near-miss hints", func(t *testing.T) {
		got := resolveSubject(nodes, "Searcz", QueryModeExplainSymbol, testResolveOpts)
		require.Equal(t, ResolutionSubjectNotFound, got.Outcome)
		assert.Empty(t, got.Seeds)
		require.NotEmpty(t, got.Candidates, "a near miss like Searcz must surface hints")
		assert.NotEmpty(t, got.Candidates[0].Hint, "hint text must explain the near miss")
	})

	t.Run("empty graph yields not_found with no hints", func(t *testing.T) {
		got := resolveSubject(nil, "anything", QueryModeFindReferences, testResolveOpts)
		require.Equal(t, ResolutionSubjectNotFound, got.Outcome)
		assert.Empty(t, got.Candidates)
	})

	t.Run("explain_symbol restricts candidates to symbol kind", func(t *testing.T) {
		// A path query under explain_symbol must not resolve to the file node; only
		// symbol-kind nodes are eligible.
		got := resolveSubject(nodes, "internal/search/engine.go", QueryModeExplainSymbol, testResolveOpts)
		for _, c := range got.Candidates {
			assert.Equal(t, NodeKindSymbol, c.Kind)
		}
		for _, s := range got.Seeds {
			assert.Equal(t, NodeKindSymbol, s.Kind, "explain_symbol seeds must be symbols only")
		}
	})

	t.Run("path query resolves to the file node and seeds the whole file scope", func(t *testing.T) {
		got := resolveSubject(nodes, "internal/search/engine.go", QueryModeFindReferences, testResolveOpts)
		require.Equal(t, ResolutionResolved, got.Outcome)
		// File subject ⇒ seeds are every node sharing the file's source_path, not
		// the lone file node (the expected evidence lives on the symbols' edges).
		assert.ElementsMatch(t,
			[]string{"n-file", "n-engine-sym", "n-search-a", "n-chunk"},
			seedIDs(got.Seeds),
		)
	})

	t.Run("import path resolves to the single import node", func(t *testing.T) {
		got := resolveSubject(nodes, "github.com/example/proj/internal/config", QueryModeFindReferences, testResolveOpts)
		require.Equal(t, ResolutionResolved, got.Outcome)
		assert.Equal(t, []string{"n-import"}, seedIDs(got.Seeds))
	})
}

// TestResolveSubject_CapsDisambiguationCandidates proves a broad subject (a
// directory prefix that fuzzy-matches many nodes) does not bloat the response:
// candidates are capped to CandidateLimit and CandidatesTotal reports the full
// count so the caller can warn and the user can narrow the query.
func TestResolveSubject_CapsDisambiguationCandidates(t *testing.T) {
	var nodes []Node
	for i := range 25 {
		nodes = append(nodes, Node{
			ID:         fmt.Sprintf("n-%02d", i),
			Kind:       NodeKindSymbol,
			Key:        fmt.Sprintf("internal/widget/w%02d.go#Widget:%d", i, i+1),
			SourcePath: fmt.Sprintf("internal/widget/w%02d.go", i),
			Name:       "Widget",
			StartLine:  i + 1,
		})
	}
	got := resolveSubject(nodes, "Widget", QueryModeExplainSymbol, resolveOptions{CandidateLimit: 10, HintLimit: defaultSubjectHintLimit})

	require.Equal(t, ResolutionDisambiguationRequired, got.Outcome)
	require.Len(t, got.Candidates, 10, "candidates must be capped to CandidateLimit")
	assert.Equal(t, 25, got.CandidatesTotal, "the full distinct-subject count must be reported for truncation")
	// Determinism: equal-score candidates are ordered by nodeSortKey (source_path),
	// so the cap deterministically keeps the lowest-sorting subjects, not a random
	// subset. This makes capped MCP responses reproducible.
	want := []string{"n-00", "n-01", "n-02", "n-03", "n-04", "n-05", "n-06", "n-07", "n-08", "n-09"}
	assert.Equal(t, want, candidateIDs(got.Candidates), "capped candidates must be the deterministic lowest-sorted prefix")
}

// TestQualifiedName_DerivesSymbolFQN proves the derived qualified name is the
// stable symbol key (path#name:line) for symbols and the natural key otherwise,
// including the keyless-node fallbacks that derive a name from source_path/name.
func TestQualifiedName_DerivesSymbolFQN(t *testing.T) {
	sym := Node{Kind: NodeKindSymbol, Key: "internal/search/engine.go#Search:42", SourcePath: "internal/search/engine.go", Name: "Search", StartLine: 42}
	assert.Equal(t, "internal/search/engine.go#Search:42", qualifiedName(sym))

	file := Node{Kind: NodeKindFile, Key: "internal/search/engine.go", SourcePath: "internal/search/engine.go", Name: "engine.go"}
	assert.Equal(t, "internal/search/engine.go", qualifiedName(file))

	// Keyless fallbacks (synthetic nodes): symbol with line, symbol without line,
	// and a keyless file falling back to source_path.
	keylessSym := Node{Kind: NodeKindSymbol, SourcePath: "internal/search/engine.go", Name: "Search", StartLine: 42}
	assert.Equal(t, "internal/search/engine.go#Search:42", qualifiedName(keylessSym))

	keylessSymNoLine := Node{Kind: NodeKindSymbol, SourcePath: "internal/search/engine.go", Name: "Search"}
	assert.Equal(t, "internal/search/engine.go#Search", qualifiedName(keylessSymNoLine))

	keylessFile := Node{Kind: NodeKindFile, SourcePath: "internal/search/engine.go", Name: "engine.go"}
	assert.Equal(t, "internal/search/engine.go", qualifiedName(keylessFile))
}

// TestBoundedEditDistance covers the cheap near-miss metric directly, including
// the empty-string branches and the over-bound early exits that the integration
// near-miss path does not reach.
func TestBoundedEditDistance(t *testing.T) {
	cases := []struct {
		a, b string
		max  int
		want int
	}{
		{"", "", 3, 0},
		{"Search", "Search", 3, 0},
		{"Search", "Searcz", 3, 1},
		{"", "abc", 3, 3},
		{"", "abcd", 3, -1}, // length delta exceeds the bound
		{"kitten", "sitting", 3, 3},
		{"kitten", "sitting", 2, -1}, // true distance 3 exceeds bound 2
		{"abc", "abcde", 2, 2},       // exactly at bound
		{"abc", "abcdef", 2, -1},     // one over bound
	}
	for _, tc := range cases {
		assert.Equalf(t, tc.want, boundedEditDistance(tc.a, tc.b, tc.max),
			"boundedEditDistance(%q,%q,%d)", tc.a, tc.b, tc.max)
	}
}

// TestResolveSubject_PathNearMissHints covers hintField's path-basename branch:
// a misspelled file path surfaces the real file as a not-found hint.
func TestResolveSubject_PathNearMissHints(t *testing.T) {
	nodes := []Node{
		{ID: "n-file", Kind: NodeKindFile, Key: "internal/search/engine.go", SourcePath: "internal/search/engine.go", Name: "engine.go"},
	}
	got := resolveSubject(nodes, "internal/search/engin.go", QueryModeFindReferences, testResolveOpts)
	require.Equal(t, ResolutionSubjectNotFound, got.Outcome)
	require.NotEmpty(t, got.Candidates, "a misspelled path must surface the real file as a hint")
	assert.Equal(t, "n-file", got.Candidates[0].SubjectID)
	assert.NotEmpty(t, got.Candidates[0].Hint)
}
