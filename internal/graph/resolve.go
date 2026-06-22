package graph

import (
	"fmt"
	"sort"
	"strings"
)

// Resolution outcomes for a graph.query subject. They are the SSOT for the three
// ways a subject string can map onto the graph before any traversal runs.
const (
	// ResolutionResolved means the subject mapped to exactly one node (or one
	// container scope) and traversal proceeded from it.
	ResolutionResolved = "resolved"
	// ResolutionDisambiguationRequired means the subject matched more than one
	// distinct node at the best score tier; the query refuses to guess or merge
	// and returns the competing candidates instead.
	ResolutionDisambiguationRequired = "disambiguation_required"
	// ResolutionSubjectNotFound means nothing matched; near-miss hints (if any)
	// are returned so the caller can correct the subject.
	ResolutionSubjectNotFound = "subject_not_found"
)

const (
	// defaultSubjectHintLimit caps near-miss hints emitted on subject_not_found.
	defaultSubjectHintLimit = 5
	// maxHintEditDistance bounds the cheap edit-distance hint search so unrelated
	// names are never suggested. It is deliberately small: hints are a courtesy,
	// not a fuzzy search (that is explicitly a non-goal of GRA19).
	maxHintEditDistance = 3
)

// Candidate identifies one competing subject (on disambiguation_required) or one
// near-miss suggestion (on subject_not_found). QualifiedName is derived because
// the node model has no stored fully-qualified-name field.
type Candidate struct {
	SubjectID     string   `json:"subject_id"`
	QualifiedName string   `json:"qualified_name"`
	Kind          NodeKind `json:"kind"`
	SourcePath    string   `json:"source_path,omitempty"`
	Line          int      `json:"line,omitempty"`
	Hint          string   `json:"hint,omitempty"`
}

// subjectResolution is the internal result of resolveSubject. Seeds is populated
// only when Outcome == ResolutionResolved; Candidates carries disambiguation
// candidates or not-found hints otherwise. CandidatesTotal is the number of
// distinct subjects that matched before Candidates was capped to the limit, so
// the caller can surface a truncation warning for a broad/ambiguous subject.
type subjectResolution struct {
	Outcome         string
	Seeds           []Node
	Candidates      []Candidate
	CandidatesTotal int
}

// resolveOptions bounds the additive disambiguation/not-found output so a broad
// subject (e.g. a directory prefix matching hundreds of nodes) cannot bloat the
// response. CandidateLimit caps disambiguation candidates; HintLimit caps
// not-found near-miss hints.
type resolveOptions struct {
	CandidateLimit int
	HintLimit      int
}

// resolveSubject maps a query string onto the graph and decides whether to
// traverse, disambiguate, or report not-found. It is the single subject matcher
// for the exposed graph.query path, replacing the naive substring matcher and
// sharing nodeMatchScore (the relevance scorer used by Neighbors/Path) so the
// package has one ranking definition rather than two.
//
// Policy (exact-beats-fuzzy, deterministic):
//   - Score every mode-eligible node with nodeMatchScore and keep the matches.
//   - Take the minimum-score tier. Because (kind,key) is unique per project, each
//     node in that tier is a distinct subject.
//   - Exactly one subject  → resolved; seeds are that subject's traversal scope.
//   - More than one subject → disambiguation_required; no traversal.
//   - No matches           → subject_not_found with near-miss hints.
//
// The minimum-tier rule is what lets a path query resolve to its single file node
// (key-exact, score 0) even though the file's symbols/chunks also match that path
// (source_path-exact, score 1): the file outranks its members, so it is the lone
// subject, and seedScope then expands traversal back across the file's members.
func resolveSubject(nodes []Node, query, mode string, opts resolveOptions) subjectResolution {
	type scoredNode struct {
		node  Node
		score int
	}
	matched := make([]scoredNode, 0, len(nodes))
	for _, node := range nodes {
		if mode == QueryModeExplainSymbol && node.Kind != NodeKindSymbol {
			continue
		}
		if score, ok := nodeMatchScore(node, query); ok {
			matched = append(matched, scoredNode{node: node, score: score})
		}
	}
	if len(matched) == 0 {
		return subjectResolution{
			Outcome:    ResolutionSubjectNotFound,
			Candidates: nearestSubjectHints(nodes, query, mode, opts.HintLimit),
		}
	}

	sort.Slice(matched, func(i, j int) bool {
		if matched[i].score != matched[j].score {
			return matched[i].score < matched[j].score
		}
		if kindRank(matched[i].node.Kind) != kindRank(matched[j].node.Kind) {
			return kindRank(matched[i].node.Kind) < kindRank(matched[j].node.Kind)
		}
		return nodeSortKey(matched[i].node) < nodeSortKey(matched[j].node)
	})

	minScore := matched[0].score
	var tier []Node
	for _, m := range matched {
		if m.score != minScore {
			break // matched is score-ascending, so the tier is a prefix.
		}
		tier = append(tier, m.node)
	}

	if len(tier) == 1 {
		return subjectResolution{
			Outcome: ResolutionResolved,
			Seeds:   seedScope(tier[0], nodes),
		}
	}

	// tier is already sorted by (score, kindRank, nodeSortKey), so capping keeps
	// the most relevant, deterministically-ordered candidates. The full count is
	// reported so the caller can warn that the subject was broad/ambiguous.
	limit := opts.CandidateLimit
	if limit <= 0 {
		limit = defaultGraphQueryLimit
	}
	capped := tier
	if len(capped) > limit {
		capped = capped[:limit]
	}
	candidates := make([]Candidate, 0, len(capped))
	for _, node := range capped {
		candidates = append(candidates, newCandidate(node, disambiguationHint(node)))
	}
	return subjectResolution{
		Outcome:         ResolutionDisambiguationRequired,
		Candidates:      candidates,
		CandidatesTotal: len(tier),
	}
}

// seedScope expands a resolved subject into the seed set that traversal walks
// from. A file-like subject seeds its whole source-path scope (the file plus its
// symbols and chunks) because a file's references flow through its members' edges
// — this reproduces the prior single-file behavior exactly. Every other kind is
// already its own seed (a symbol's edges, an import's importers) and needs no
// expansion.
func seedScope(subject Node, nodes []Node) []Node {
	switch subject.Kind {
	case NodeKindFile, NodeKindTestFile, NodeKindDoc, NodeKindConfigFile:
		if subject.SourcePath == "" {
			return []Node{subject}
		}
		scoped := make([]Node, 0, 8)
		for _, node := range nodes {
			if node.SourcePath == subject.SourcePath {
				scoped = append(scoped, node)
			}
		}
		return scoped
	default:
		return []Node{subject}
	}
}

// newCandidate projects a node into a disambiguation/hint candidate with a
// derived qualified name and line.
func newCandidate(node Node, hint string) Candidate {
	return Candidate{
		SubjectID:     node.ID,
		QualifiedName: qualifiedName(node),
		Kind:          node.Kind,
		SourcePath:    node.SourcePath,
		Line:          node.StartLine,
		Hint:          hint,
	}
}

// qualifiedName derives a stable fully-qualified name for a node. Symbols use
// their natural key (path#name:line); everything else uses its key. The branches
// below are a defensive fallback for keyless nodes (every extractor-produced node
// carries a Key, so they are exercised only by synthetic/hand-built nodes).
func qualifiedName(node Node) string {
	if node.Key != "" {
		return node.Key
	}
	if node.Kind == NodeKindSymbol && node.SourcePath != "" {
		if node.StartLine > 0 {
			return fmt.Sprintf("%s#%s:%d", node.SourcePath, node.Name, node.StartLine)
		}
		return fmt.Sprintf("%s#%s", node.SourcePath, node.Name)
	}
	if node.SourcePath != "" {
		return node.SourcePath
	}
	return node.Name
}

// disambiguationHint renders a short human-readable disambiguator for a competing
// candidate, e.g. "method Search in internal/search/engine.go:42".
func disambiguationHint(node Node) string {
	descriptor := node.SymbolKind
	if descriptor == "" {
		descriptor = string(node.Kind)
	}
	label := node.Name
	if label == "" {
		label = node.Key
	}
	location := node.SourcePath
	if node.SourcePath != "" && node.StartLine > 0 {
		location = fmt.Sprintf("%s:%d", node.SourcePath, node.StartLine)
	}
	if location == "" {
		return fmt.Sprintf("%s %s", descriptor, label)
	}
	return fmt.Sprintf("%s %s in %s", descriptor, label, location)
}

// nearestSubjectHints returns up to limit cheap near-miss suggestions ranked by
// bounded edit distance to the query. It never performs a real fuzzy search
// (explicitly a non-goal): only candidates within maxHintEditDistance qualify, so
// a totally unrelated subject produces no hints and an empty graph produces none.
func nearestSubjectHints(nodes []Node, query, mode string, limit int) []Candidate {
	if limit <= 0 {
		limit = defaultSubjectHintLimit
	}
	// Compare like with like: hintField reduces a node's path to its basename, so
	// reduce the query the same way. This lets a full-path typo
	// (internal/search/engin.go) and a bare-basename typo (engin.go) both find the
	// real file, while a bare symbol query (no slash) is unchanged.
	target := strings.ToLower(lastPathSegment(strings.TrimSpace(query)))
	if target == "" {
		return nil
	}

	type scoredHint struct {
		node Node
		dist int
	}
	var hints []scoredHint
	for _, node := range nodes {
		if mode == QueryModeExplainSymbol && node.Kind != NodeKindSymbol {
			continue
		}
		candidate := strings.ToLower(hintField(node))
		if candidate == "" {
			continue
		}
		dist := boundedEditDistance(target, candidate, maxHintEditDistance)
		if dist < 0 {
			continue // beyond the bound — not a near miss.
		}
		hints = append(hints, scoredHint{node: node, dist: dist})
	}
	if len(hints) == 0 {
		return nil
	}

	sort.Slice(hints, func(i, j int) bool {
		if hints[i].dist != hints[j].dist {
			return hints[i].dist < hints[j].dist
		}
		if kindRank(hints[i].node.Kind) != kindRank(hints[j].node.Kind) {
			return kindRank(hints[i].node.Kind) < kindRank(hints[j].node.Kind)
		}
		return nodeSortKey(hints[i].node) < nodeSortKey(hints[j].node)
	})
	if len(hints) > limit {
		hints = hints[:limit]
	}

	candidates := make([]Candidate, 0, len(hints))
	for _, hint := range hints {
		qn := qualifiedName(hint.node)
		candidates = append(candidates, newCandidate(hint.node, fmt.Sprintf("did you mean %q?", qn)))
	}
	return candidates
}

// hintField picks the most query-comparable field for near-miss ranking: a
// symbol's bare name, otherwise the last path segment of the key/source_path so a
// short query like "engin.go" can be compared against "engine.go" rather than the
// full directory-qualified key.
func hintField(node Node) string {
	if node.Kind == NodeKindSymbol && node.Name != "" {
		return node.Name
	}
	base := node.Key
	if base == "" {
		base = node.SourcePath
	}
	if base == "" {
		return node.Name
	}
	return lastPathSegment(base)
}

// lastPathSegment returns the final "/"-delimited segment of s (its basename),
// or s unchanged when it has no slash. Used to compare query and node hints at
// the same path granularity.
func lastPathSegment(s string) string {
	if idx := strings.LastIndex(s, "/"); idx >= 0 && idx+1 < len(s) {
		return s[idx+1:]
	}
	return s
}

// boundedEditDistance returns the Levenshtein distance between a and b, or -1 if
// it exceeds maxDistance. The early-exit bound keeps the cost at O(len(a)*bound)
// for the common near-miss case and avoids scoring unrelated strings at all.
func boundedEditDistance(a, b string, maxDistance int) int {
	if a == b {
		return 0
	}
	la, lb := len(a), len(b)
	if abs(la-lb) > maxDistance {
		return -1
	}
	if la == 0 {
		if lb <= maxDistance {
			return lb
		}
		return -1
	}

	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		rowMin := curr[0]
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
			if curr[j] < rowMin {
				rowMin = curr[j]
			}
		}
		if rowMin > maxDistance {
			return -1 // no cell in this row can lead to a within-bound result.
		}
		prev, curr = curr, prev
	}
	if prev[lb] > maxDistance {
		return -1
	}
	return prev[lb]
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
