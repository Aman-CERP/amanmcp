package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultGraphTraversalConfig_HasStrictDefaults(t *testing.T) {
	cfg := DefaultGraphTraversalConfig()
	assert.Equal(t, PolicyGraphTraversalMaxResults, cfg.Policy.MaxResults)
	assert.Equal(t, DefaultGraphTraversalMaxResults, cfg.Modes.FindReferences.MaxResults)
	assert.Equal(t, DefaultGraphTraversalMaxNodes, cfg.Modes.FindReferences.MaxNodes)
	assert.Equal(t, DefaultGraphTraversalFindRefDepth, cfg.Modes.FindReferences.MaxDepth)
	assert.Equal(t, DefaultGraphTraversalImpactDepth, cfg.Modes.ImpactAnalysis.MaxDepth)
}

func TestValidateGraphTraversalConfig_RejectsModeAbovePolicy(t *testing.T) {
	cfg := DefaultGraphTraversalConfig()
	cfg.Modes.FindReferences.MaxResults = cfg.Policy.MaxResults + 1
	err := ValidateGraphTraversalConfig(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_results")
}

func TestNormalizeGraphTraversalConfig_FillsZeros(t *testing.T) {
	cfg := GraphTraversalConfig{}
	NormalizeGraphTraversalConfig(&cfg)
	require.NoError(t, ValidateGraphTraversalConfig(cfg))
	assert.Equal(t, DefaultGraphTraversalMaxResults, cfg.Modes.FindReferences.MaxResults)
}