package graph

import "sort"

type targetDedupBucket struct {
	best           QueryResult
	suppressed     int
	otherRelations map[EdgeKind]struct{}
}

// deduplicateResultsByTarget collapses multiple paths to the same target node
// into one representative result, counting suppressed paths and relations.
func deduplicateResultsByTarget(results []QueryResult) []QueryResult {
	if len(results) <= 1 {
		return results
	}
	buckets := make(map[string]*targetDedupBucket, len(results))
	order := make([]string, 0, len(results))
	for _, result := range results {
		entry, ok := buckets[result.NodeID]
		if !ok {
			buckets[result.NodeID] = &targetDedupBucket{
				best:           result,
				otherRelations: map[EdgeKind]struct{}{},
			}
			order = append(order, result.NodeID)
			continue
		}
		if preferResult(result, entry.best) {
			recordSuppressedRelation(entry, entry.best.Relation, result.Relation)
			entry.best = result
		} else {
			recordSuppressedRelation(entry, result.Relation, entry.best.Relation)
		}
		entry.suppressed++
	}
	out := make([]QueryResult, 0, len(order))
	for _, nodeID := range order {
		entry := buckets[nodeID]
		entry.best.AdditionalPathsCount = entry.suppressed
		if len(entry.otherRelations) > 0 {
			relations := make([]EdgeKind, 0, len(entry.otherRelations))
			for kind := range entry.otherRelations {
				relations = append(relations, kind)
			}
			sort.Slice(relations, func(i, j int) bool { return relations[i] < relations[j] })
			entry.best.AdditionalRelations = relations
		}
		out = append(out, entry.best)
	}
	return out
}

func recordSuppressedRelation(entry *targetDedupBucket, suppressed, representative EdgeKind) {
	if suppressed == representative {
		return
	}
	entry.otherRelations[suppressed] = struct{}{}
}

// preferResult chooses the representative path for one target. Higher confidence
// wins; ties prefer non-heuristic, shorter explanation, then stable fields.
func preferResult(candidate, incumbent QueryResult) bool {
	if candidate.Confidence != incumbent.Confidence {
		return candidate.Confidence > incumbent.Confidence
	}
	if candidate.Heuristic != incumbent.Heuristic {
		return !candidate.Heuristic
	}
	if len(candidate.Path.Explanation) != len(incumbent.Path.Explanation) {
		return len(candidate.Path.Explanation) < len(incumbent.Path.Explanation)
	}
	if candidate.SourcePath != incumbent.SourcePath {
		return candidate.SourcePath < incumbent.SourcePath
	}
	if candidate.EvidenceMethod != incumbent.EvidenceMethod {
		return candidate.EvidenceMethod < incumbent.EvidenceMethod
	}
	return candidate.Relation < incumbent.Relation
}