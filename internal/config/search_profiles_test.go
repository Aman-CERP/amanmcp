package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Aman-CERP/amanmcp/internal/search"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewConfig_SearchProfiles_ReturnsF39Defaults(t *testing.T) {
	cfg := NewConfig()

	require.Contains(t, cfg.Search.Profiles, "code")
	require.Contains(t, cfg.Search.Profiles, "project-memory")
	require.Contains(t, cfg.Search.Profiles, "review-corpus")
	require.Contains(t, cfg.Search.Profiles, "archive")

	assert.Contains(t, cfg.Search.Profiles["code"].SourceClasses, "source_code")
	assert.Contains(t, cfg.Search.Profiles["code"].SourceClasses, "test")
	assert.Contains(t, cfg.Search.Profiles["project-memory"].SourceClasses, "adr")
	assert.Contains(t, cfg.Search.Profiles["project-memory"].ExcludeSourceClasses, "review_corpus")
	assert.Contains(t, cfg.Search.Profiles["review-corpus"].Include, "vend_feedback/**")
	assert.Contains(t, cfg.Search.Profiles["review-corpus"].Include, "improvements_dump/**")
	assert.Contains(t, cfg.Search.Profiles["archive"].Include, "archive/**")
}

func TestLoad_SearchProfiles_ExtendsDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	configContent := `
version: 1
search:
  profiles:
    review-corpus:
      include:
        - custom_reviews/**
    archive:
      include:
        - historical/**
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".amanmcp.yaml"), []byte(configContent), 0o644))

	cfg, err := Load(tmpDir)

	require.NoError(t, err)
	require.Contains(t, cfg.Search.Profiles, "review-corpus")
	assert.Contains(t, cfg.Search.Profiles["review-corpus"].Include, "vend_feedback/**")
	assert.Contains(t, cfg.Search.Profiles["review-corpus"].Include, "custom_reviews/**")
	assert.Contains(t, cfg.Search.Profiles["archive"].Include, "archive/**")
	assert.Contains(t, cfg.Search.Profiles["archive"].Include, "historical/**")
}

func TestSearchProfileRules_ConvertsCustomProfileIncludes(t *testing.T) {
	tmpDir := t.TempDir()
	configContent := `
version: 1
search:
  profiles:
    review-corpus:
      include:
        - custom_reviews/**
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".amanmcp.yaml"), []byte(configContent), 0o644))
	cfg, err := Load(tmpDir)
	require.NoError(t, err)

	meta := search.DeriveSourceMetadata(search.SourceMetadataInput{
		Path:        "custom_reviews/f39-review.md",
		ContentType: "markdown",
		Rules:       cfg.SearchMetadataRules(),
	})
	eligibility := search.ExplainProfileEligibilityWithRules(meta, search.ProfileReviewCorpus, cfg.SearchProfileRules())

	assert.Equal(t, search.SourceClassReviewCorpus, meta.SourceClass)
	assert.Equal(t, search.AuthorityAdvisory, meta.Authority)
	assert.Equal(t, search.ProfileReviewCorpus, meta.Profile)
	assert.True(t, eligibility.Eligible)
}
