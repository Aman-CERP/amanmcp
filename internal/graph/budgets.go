package graph

import (
	"encoding/json"
	"fmt"

	"github.com/Aman-CERP/amanmcp/internal/config"
)

// TraversalBudgetReason names which traversal budget was exhausted.
type TraversalBudgetReason string

const (
	TraversalBudgetResults     TraversalBudgetReason = "results"
	TraversalBudgetNodes       TraversalBudgetReason = "nodes"
	TraversalBudgetPerEdgeKind TraversalBudgetReason = "per_edge_kind"
	TraversalBudgetTokens      TraversalBudgetReason = "tokens"
	TraversalBudgetDepth       TraversalBudgetReason = "depth"
)

// TraversalBudgetOverrides carries optional caller overrides within policy.
type TraversalBudgetOverrides struct {
	MaxResults     *int `json:"max_results,omitempty"`
	MaxNodes       *int `json:"max_nodes,omitempty"`
	MaxPerEdgeKind *int `json:"max_per_edge_kind,omitempty"`
	MaxTokens      *int `json:"max_tokens,omitempty"`
	MaxDepth       *int `json:"max_depth,omitempty"`
}

// TraversalBudget is the resolved per-query traversal limit set.
type TraversalBudget struct {
	MaxResults     int
	MaxNodes       int
	MaxPerEdgeKind int
	MaxTokens      int
	MaxDepth       int
}

func resolveTraversalBudgets(mode string, cfg config.GraphTraversalConfig, overrides TraversalBudgetOverrides) (TraversalBudget, error) {
	normalized := cfg
	config.NormalizeGraphTraversalConfig(&normalized)
	modeBudget := modeTraversalBudget(mode, normalized.Modes)
	budget := TraversalBudget{
		MaxResults:     modeBudget.MaxResults,
		MaxNodes:       modeBudget.MaxNodes,
		MaxPerEdgeKind: modeBudget.MaxPerEdgeKind,
		MaxTokens:      modeBudget.MaxTokens,
		MaxDepth:       modeBudget.MaxDepth,
	}
	if overrides.MaxResults != nil {
		if err := validateBudgetOverride("max_results", *overrides.MaxResults, normalized.Policy.MaxResults); err != nil {
			return TraversalBudget{}, err
		}
		budget.MaxResults = *overrides.MaxResults
	}
	if overrides.MaxNodes != nil {
		if err := validateBudgetOverride("max_nodes", *overrides.MaxNodes, normalized.Policy.MaxNodes); err != nil {
			return TraversalBudget{}, err
		}
		budget.MaxNodes = *overrides.MaxNodes
	}
	if overrides.MaxPerEdgeKind != nil {
		if err := validateBudgetOverride("max_per_edge_kind", *overrides.MaxPerEdgeKind, normalized.Policy.MaxPerEdgeKind); err != nil {
			return TraversalBudget{}, err
		}
		budget.MaxPerEdgeKind = *overrides.MaxPerEdgeKind
	}
	if overrides.MaxTokens != nil {
		if err := validateBudgetOverride("max_tokens", *overrides.MaxTokens, normalized.Policy.MaxTokens); err != nil {
			return TraversalBudget{}, err
		}
		budget.MaxTokens = *overrides.MaxTokens
	}
	if overrides.MaxDepth != nil {
		if err := validateBudgetOverride("max_depth", *overrides.MaxDepth, normalized.Policy.MaxDepth); err != nil {
			return TraversalBudget{}, err
		}
		budget.MaxDepth = *overrides.MaxDepth
	}
	return budget, nil
}

func modeTraversalBudget(mode string, modes config.GraphTraversalModes) config.GraphTraversalBudget {
	switch normalizeQueryMode(mode) {
	case QueryModeExplainSymbol:
		return modes.ExplainSymbol
	case QueryModeImpactAnalysis:
		return modes.ImpactAnalysis
	default:
		return modes.FindReferences
	}
}

func validateBudgetOverride(name string, value, policyMax int) error {
	if value <= 0 {
		return fmt.Errorf("%s must be positive, got %d: %w", name, value, ErrInvalidQueryParams)
	}
	if value > policyMax {
		return fmt.Errorf("%s must be <= policy max %d, got %d: %w", name, policyMax, value, ErrInvalidQueryParams)
	}
	return nil
}

func newTraversalBudgetWarning(reason TraversalBudgetReason, limit int) StatusWarning {
	return StatusWarning{
		Code:         WarningTraversalBudgetExhausted,
		Message:      fmt.Sprintf("traversal budget exhausted (%s, limit %d)", reason, limit),
		BudgetReason: reason,
		BudgetLimit:  limit,
	}
}

type applyBudgetsOutput struct {
	results  []QueryResult
	warnings []StatusWarning
}

func applyTraversalBudgets(results []QueryResult, budget TraversalBudget) applyBudgetsOutput {
	out := applyBudgetsOutput{results: results}
	if budget.MaxPerEdgeKind > 0 {
		results, exhausted := capResultsPerEdgeKind(results, budget.MaxPerEdgeKind)
		out.results = results
		if exhausted {
			out.warnings = append(out.warnings, newTraversalBudgetWarning(TraversalBudgetPerEdgeKind, budget.MaxPerEdgeKind))
		}
	}
	if budget.MaxTokens > 0 {
		results, exhausted := capResultsByTokens(out.results, budget.MaxTokens)
		out.results = results
		if exhausted {
			out.warnings = append(out.warnings, newTraversalBudgetWarning(TraversalBudgetTokens, budget.MaxTokens))
		}
	}
	if budget.MaxResults > 0 && len(out.results) > budget.MaxResults {
		out.results = out.results[:budget.MaxResults]
		out.warnings = append(out.warnings, newTraversalBudgetWarning(TraversalBudgetResults, budget.MaxResults))
	}
	return out
}

func capResultsPerEdgeKind(results []QueryResult, maxPerKind int) ([]QueryResult, bool) {
	counts := make(map[EdgeKind]int, len(results))
	capped := make([]QueryResult, 0, len(results))
	exhausted := false
	for _, result := range results {
		counts[result.Relation]++
		if counts[result.Relation] > maxPerKind {
			exhausted = true
			continue
		}
		capped = append(capped, result)
	}
	return capped, exhausted
}

func capResultsByTokens(results []QueryResult, maxTokens int) ([]QueryResult, bool) {
	if len(results) == 0 {
		return results, false
	}
	capped := make([]QueryResult, 0, len(results))
	exhausted := false
	for _, result := range results {
		next := append(capped, result)
		if estimateResultsJSONBytes(next) > maxTokens {
			exhausted = true
			break
		}
		capped = next
	}
	if len(capped) == 0 && len(results) > 0 {
		capped = results[:1]
		exhausted = true
	}
	return capped, exhausted
}

func estimateResultsJSONBytes(results []QueryResult) int {
	raw, err := json.Marshal(results)
	if err != nil {
		return 0
	}
	return len(raw)
}