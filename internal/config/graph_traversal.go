package config

import "fmt"

const (
	DefaultGraphTraversalMaxResults      = 10
	PolicyGraphTraversalMaxResults       = 50
	DefaultGraphTraversalMaxNodes        = 2000
	PolicyGraphTraversalMaxNodes         = 10000
	DefaultGraphTraversalMaxPerEdgeKind  = 10
	PolicyGraphTraversalMaxPerEdgeKind   = 50
	DefaultGraphTraversalMaxTokens       = 16384
	PolicyGraphTraversalMaxTokens        = 65536
	DefaultGraphTraversalMaxDepth        = 5
	PolicyGraphTraversalMaxDepth         = 10
	DefaultGraphTraversalFindRefDepth    = 3
	DefaultGraphTraversalExplainDepth    = 1
	DefaultGraphTraversalImpactDepth     = 4
)

// GraphConfig configures product graph behavior.
type GraphConfig struct {
	Traversal GraphTraversalConfig `yaml:"traversal" json:"traversal"`
}

// GraphTraversalConfig holds per-mode traversal budgets and policy upper bounds.
type GraphTraversalConfig struct {
	Policy GraphTraversalBudget `yaml:"policy" json:"policy"`
	Modes  GraphTraversalModes  `yaml:"modes" json:"modes"`
}

// GraphTraversalModes holds per-query-mode default budgets.
type GraphTraversalModes struct {
	FindReferences GraphTraversalBudget `yaml:"find_references" json:"find_references"`
	ExplainSymbol  GraphTraversalBudget `yaml:"explain_symbol" json:"explain_symbol"`
	ImpactAnalysis GraphTraversalBudget `yaml:"impact_analysis" json:"impact_analysis"`
}

// GraphTraversalBudget is one set of traversal limits. Zero values are replaced
// with mode defaults during resolution; policy bounds reject caller overrides.
type GraphTraversalBudget struct {
	MaxResults     int `yaml:"max_results" json:"max_results"`
	MaxNodes       int `yaml:"max_nodes" json:"max_nodes"`
	MaxPerEdgeKind int `yaml:"max_per_edge_kind" json:"max_per_edge_kind"`
	MaxTokens      int `yaml:"max_tokens" json:"max_tokens"`
	MaxDepth       int `yaml:"max_depth" json:"max_depth"`
}

// DefaultGraphTraversalConfig returns built-in traversal budgets and policy caps.
func DefaultGraphTraversalConfig() GraphTraversalConfig {
	policy := GraphTraversalBudget{
		MaxResults:     PolicyGraphTraversalMaxResults,
		MaxNodes:       PolicyGraphTraversalMaxNodes,
		MaxPerEdgeKind: PolicyGraphTraversalMaxPerEdgeKind,
		MaxTokens:      PolicyGraphTraversalMaxTokens,
		MaxDepth:       PolicyGraphTraversalMaxDepth,
	}
	return GraphTraversalConfig{
		Policy: policy,
		Modes: GraphTraversalModes{
			FindReferences: GraphTraversalBudget{
				MaxResults:     DefaultGraphTraversalMaxResults,
				MaxNodes:       DefaultGraphTraversalMaxNodes,
				MaxPerEdgeKind: DefaultGraphTraversalMaxPerEdgeKind,
				MaxTokens:      DefaultGraphTraversalMaxTokens,
				MaxDepth:       DefaultGraphTraversalFindRefDepth,
			},
			ExplainSymbol: GraphTraversalBudget{
				MaxResults:     DefaultGraphTraversalMaxResults,
				MaxNodes:       DefaultGraphTraversalMaxNodes,
				MaxPerEdgeKind: DefaultGraphTraversalMaxPerEdgeKind,
				MaxTokens:      DefaultGraphTraversalMaxTokens,
				MaxDepth:       DefaultGraphTraversalExplainDepth,
			},
			ImpactAnalysis: GraphTraversalBudget{
				MaxResults:     DefaultGraphTraversalMaxResults,
				MaxNodes:       DefaultGraphTraversalMaxNodes,
				MaxPerEdgeKind: DefaultGraphTraversalMaxPerEdgeKind,
				MaxTokens:      DefaultGraphTraversalMaxTokens,
				MaxDepth:       DefaultGraphTraversalImpactDepth,
			},
		},
	}
}

// NormalizeGraphTraversalConfig fills zero fields with defaults and policy caps.
func NormalizeGraphTraversalConfig(cfg *GraphTraversalConfig) {
	if cfg == nil {
		return
	}
	defaults := DefaultGraphTraversalConfig()
	if cfg.Policy.MaxResults <= 0 {
		cfg.Policy.MaxResults = defaults.Policy.MaxResults
	}
	if cfg.Policy.MaxNodes <= 0 {
		cfg.Policy.MaxNodes = defaults.Policy.MaxNodes
	}
	if cfg.Policy.MaxPerEdgeKind <= 0 {
		cfg.Policy.MaxPerEdgeKind = defaults.Policy.MaxPerEdgeKind
	}
	if cfg.Policy.MaxTokens <= 0 {
		cfg.Policy.MaxTokens = defaults.Policy.MaxTokens
	}
	if cfg.Policy.MaxDepth <= 0 {
		cfg.Policy.MaxDepth = defaults.Policy.MaxDepth
	}
	normalizeTraversalModeBudget(&cfg.Modes.FindReferences, defaults.Modes.FindReferences, cfg.Policy)
	normalizeTraversalModeBudget(&cfg.Modes.ExplainSymbol, defaults.Modes.ExplainSymbol, cfg.Policy)
	normalizeTraversalModeBudget(&cfg.Modes.ImpactAnalysis, defaults.Modes.ImpactAnalysis, cfg.Policy)
}

func normalizeTraversalModeBudget(mode *GraphTraversalBudget, defaults, policy GraphTraversalBudget) {
	if mode.MaxResults <= 0 {
		mode.MaxResults = defaults.MaxResults
	}
	if mode.MaxNodes <= 0 {
		mode.MaxNodes = defaults.MaxNodes
	}
	if mode.MaxPerEdgeKind <= 0 {
		mode.MaxPerEdgeKind = defaults.MaxPerEdgeKind
	}
	if mode.MaxTokens <= 0 {
		mode.MaxTokens = defaults.MaxTokens
	}
	if mode.MaxDepth <= 0 {
		mode.MaxDepth = defaults.MaxDepth
	}
}

// ValidateGraphTraversalConfig rejects impossible traversal budgets. Missing
// zero fields are filled from defaults first; explicit over-policy values fail.
func ValidateGraphTraversalConfig(cfg GraphTraversalConfig) error {
	normalized := cfg
	NormalizeGraphTraversalConfig(&normalized)
	if err := validateTraversalPolicyBounds(normalized.Policy); err != nil {
		return err
	}
	checks := []struct {
		path  string
		value int
		max   int
	}{
		{"graph.traversal.policy.max_results", normalized.Policy.MaxResults, PolicyGraphTraversalMaxResults},
		{"graph.traversal.policy.max_nodes", normalized.Policy.MaxNodes, PolicyGraphTraversalMaxNodes},
		{"graph.traversal.policy.max_per_edge_kind", normalized.Policy.MaxPerEdgeKind, PolicyGraphTraversalMaxPerEdgeKind},
		{"graph.traversal.policy.max_tokens", normalized.Policy.MaxTokens, PolicyGraphTraversalMaxTokens},
		{"graph.traversal.policy.max_depth", normalized.Policy.MaxDepth, PolicyGraphTraversalMaxDepth},
	}
	for _, check := range checks {
		if check.value <= 0 {
			return fmt.Errorf("%s must be positive, got %d", check.path, check.value)
		}
	}
	modeChecks := []struct {
		path string
		mode GraphTraversalBudget
	}{
		{"graph.traversal.modes.find_references", normalized.Modes.FindReferences},
		{"graph.traversal.modes.explain_symbol", normalized.Modes.ExplainSymbol},
		{"graph.traversal.modes.impact_analysis", normalized.Modes.ImpactAnalysis},
	}
	for _, check := range modeChecks {
		if err := validateModeTraversalBudget(check.path, check.mode, normalized.Policy); err != nil {
			return err
		}
	}
	return nil
}

func validateTraversalPolicyBounds(policy GraphTraversalBudget) error {
	checks := []struct {
		path  string
		value int
		max   int
	}{
		{"graph.traversal.policy.max_results", policy.MaxResults, PolicyGraphTraversalMaxResults},
		{"graph.traversal.policy.max_nodes", policy.MaxNodes, PolicyGraphTraversalMaxNodes},
		{"graph.traversal.policy.max_per_edge_kind", policy.MaxPerEdgeKind, PolicyGraphTraversalMaxPerEdgeKind},
		{"graph.traversal.policy.max_tokens", policy.MaxTokens, PolicyGraphTraversalMaxTokens},
		{"graph.traversal.policy.max_depth", policy.MaxDepth, PolicyGraphTraversalMaxDepth},
	}
	for _, check := range checks {
		if check.value > check.max {
			return fmt.Errorf("%s must be <= %d, got %d", check.path, check.max, check.value)
		}
	}
	return nil
}

func validateModeTraversalBudget(path string, mode, policy GraphTraversalBudget) error {
	fields := []struct {
		name  string
		value int
	}{
		{"max_results", mode.MaxResults},
		{"max_nodes", mode.MaxNodes},
		{"max_per_edge_kind", mode.MaxPerEdgeKind},
		{"max_tokens", mode.MaxTokens},
		{"max_depth", mode.MaxDepth},
	}
	for _, field := range fields {
		if field.value <= 0 {
			return fmt.Errorf("%s.%s must be positive, got %d", path, field.name, field.value)
		}
	}
	if mode.MaxResults > policy.MaxResults {
		return fmt.Errorf("%s.max_results must be <= policy max %d, got %d", path, policy.MaxResults, mode.MaxResults)
	}
	if mode.MaxNodes > policy.MaxNodes {
		return fmt.Errorf("%s.max_nodes must be <= policy max %d, got %d", path, policy.MaxNodes, mode.MaxNodes)
	}
	if mode.MaxPerEdgeKind > policy.MaxPerEdgeKind {
		return fmt.Errorf("%s.max_per_edge_kind must be <= policy max %d, got %d", path, policy.MaxPerEdgeKind, mode.MaxPerEdgeKind)
	}
	if mode.MaxTokens > policy.MaxTokens {
		return fmt.Errorf("%s.max_tokens must be <= policy max %d, got %d", path, policy.MaxTokens, mode.MaxTokens)
	}
	if mode.MaxDepth > policy.MaxDepth {
		return fmt.Errorf("%s.max_depth must be <= policy max %d, got %d", path, policy.MaxDepth, mode.MaxDepth)
	}
	return nil
}