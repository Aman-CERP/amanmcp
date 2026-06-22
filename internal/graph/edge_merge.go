package graph

import "strings"

// relationshipKey identifies the semantic relationship between two nodes,
// ignoring extractor and source_path so competing edges can be merged.
func relationshipKey(edge Edge) string {
	return strings.Join([]string{
		edge.FromNodeID,
		edge.ToNodeID,
		string(edge.Kind),
	}, "|")
}

// mergeCompetingEdges collapses edges sharing (from, to, kind) to the single
// best edge. Different kinds are preserved. Order of first-seen keys is kept.
func mergeCompetingEdges(edges []Edge) []Edge {
	if len(edges) <= 1 {
		return edges
	}
	best := make(map[string]Edge, len(edges))
	order := make([]string, 0, len(edges))
	for _, edge := range edges {
		key := relationshipKey(edge)
		existing, ok := best[key]
		if !ok {
			best[key] = edge
			order = append(order, key)
			continue
		}
		if preferEdge(edge, existing) {
			best[key] = edge
		}
	}
	merged := make([]Edge, 0, len(order))
	for _, key := range order {
		merged = append(merged, best[key])
	}
	return merged
}

// preferEdge reports whether candidate should replace incumbent for the same
// (from, to, kind). Higher confidence wins; ties prefer non-heuristic, then id.
func preferEdge(candidate, incumbent Edge) bool {
	if candidate.Confidence != incumbent.Confidence {
		return candidate.Confidence > incumbent.Confidence
	}
	if candidate.Evidence.Heuristic != incumbent.Evidence.Heuristic {
		return !candidate.Evidence.Heuristic
	}
	return candidate.ID < incumbent.ID
}