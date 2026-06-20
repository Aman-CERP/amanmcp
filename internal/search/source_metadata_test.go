package search

import (
	"testing"
	"time"

	"github.com/Aman-CERP/amanmcp/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeriveSourceMetadata_ReviewCorpusClassification(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "vend feedback", path: "vend_feedback/gpt/f39-review.md"},
		{name: "improvements dump", path: "improvements_dump/search-authority-notes.md"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta := DeriveSourceMetadata(SourceMetadataInput{
				Path:        tt.path,
				ContentType: store.ContentTypeMarkdown,
			})

			assert.Equal(t, SourceClassReviewCorpus, meta.SourceClass)
			assert.Equal(t, AuthorityAdvisory, meta.Authority)
			assert.Equal(t, ProfileReviewCorpus, meta.Profile)
			assert.Equal(t, tt.path, meta.SourcePath)
			assert.False(t, meta.Generated)
			assert.False(t, meta.Stale)
		})
	}
}

func TestParseProfile_RejectsUnknownProfile(t *testing.T) {
	profile, err := ParseProfile("review-corpus")
	require.NoError(t, err)
	assert.Equal(t, ProfileReviewCorpus, profile)

	_, err = ParseProfile("unsafe")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown search profile")
}

func TestDeriveSourceMetadata_ArchiveClassification(t *testing.T) {
	meta := DeriveSourceMetadata(SourceMetadataInput{
		Path:        "archive/sprint-09/old-decision.md",
		ContentType: store.ContentTypeMarkdown,
	})

	assert.Equal(t, SourceClassArchived, meta.SourceClass)
	assert.Equal(t, AuthorityArchived, meta.Authority)
	assert.Equal(t, ProfileArchive, meta.Profile)
	assert.False(t, meta.Generated)
}

func TestDeriveSourceMetadata_UnknownConservativeFallback(t *testing.T) {
	meta := DeriveSourceMetadata(SourceMetadataInput{
		Path: "scratch/blob.bin",
	})

	assert.Equal(t, SourceClassUnknown, meta.SourceClass)
	assert.Equal(t, AuthorityUnknown, meta.Authority)
	assert.Equal(t, ProfileProjectMemory, meta.Profile)
	assert.False(t, meta.Generated)
	assert.False(t, meta.Stale)
	assert.NotEmpty(t, meta.FreshnessReason)
	assert.Less(t, MetadataPriority(meta), MetadataPriority(SourceMetadata{
		SourceClass: SourceClassDocs,
		Authority:   AuthorityActive,
		Profile:     ProfileProjectMemory,
	}))
}

func TestDeriveSourceMetadata_DecisionFreshnessAndAuthority(t *testing.T) {
	modified := time.Date(2026, 5, 1, 10, 30, 0, 0, time.UTC)

	active := DeriveSourceMetadata(SourceMetadataInput{
		Path:         ".aman-pm/decisions/ADR-039-roadmap-execution-substrate.md",
		ContentType:  store.ContentTypeMarkdown,
		LastModified: modified,
		SourceHash:   "active-hash",
		Content: `---
status: accepted
supersedes:
  - ADR-030
---
# ADR-039`,
	})
	generated := DeriveSourceMetadata(SourceMetadataInput{
		Path:        ".aman-pm/validation/decision-index.md",
		ContentType: store.ContentTypeMarkdown,
		Content:     "**Status:** accepted",
	})
	advisory := DeriveSourceMetadata(SourceMetadataInput{
		Path:        "vend_feedback/decision-review.md",
		ContentType: store.ContentTypeMarkdown,
		Content:     "ADR-039 should be accepted",
	})
	superseded := DeriveSourceMetadata(SourceMetadataInput{
		Path:        ".aman-pm/decisions/ADR-030-old-approach.md",
		ContentType: store.ContentTypeMarkdown,
		Content: `---
status: superseded
superseded_by: ADR-039
---
# ADR-030`,
	})

	require.Equal(t, SourceClassADR, active.SourceClass)
	assert.Equal(t, AuthorityAuthoritative, active.Authority)
	assert.Equal(t, DecisionStatusAccepted, active.DecisionStatus)
	assert.Equal(t, []string{"ADR-030"}, active.Supersedes)
	require.NotNil(t, active.CurrentAsOf)
	assert.Equal(t, modified, *active.CurrentAsOf)

	assert.Equal(t, SourceClassGenerated, generated.SourceClass)
	assert.Equal(t, AuthorityGenerated, generated.Authority)
	assert.True(t, generated.Generated)

	assert.Equal(t, SourceClassReviewCorpus, advisory.SourceClass)
	assert.Equal(t, AuthorityAdvisory, advisory.Authority)

	assert.Equal(t, DecisionStatusSuperseded, superseded.DecisionStatus)
	assert.Equal(t, AuthorityArchived, superseded.Authority)
	assert.Equal(t, []string{"ADR-039"}, superseded.SupersededBy)
	assert.True(t, superseded.Stale)
	assert.NotEmpty(t, superseded.FreshnessReason)

	assert.Greater(t, MetadataPriority(active), MetadataPriority(generated))
	assert.Greater(t, MetadataPriority(active), MetadataPriority(advisory))
	assert.Greater(t, MetadataPriority(active), MetadataPriority(superseded))
}

func TestDeriveSourceMetadata_ParsesSupersededByFromStatusLine(t *testing.T) {
	meta := DeriveSourceMetadata(SourceMetadataInput{
		Path:        ".aman-pm/decisions/ADR-030-old-approach.md",
		ContentType: store.ContentTypeMarkdown,
		Content: `# ADR-030

Status: Superseded by ADR-039
`,
	})

	assert.Equal(t, SourceClassADR, meta.SourceClass)
	assert.Equal(t, DecisionStatusSuperseded, meta.DecisionStatus)
	assert.Equal(t, []string{"ADR-039"}, meta.SupersededBy)
	assert.Equal(t, AuthorityArchived, meta.Authority)
	assert.True(t, meta.Stale)
}

func TestDeriveSourceMetadata_UsesFrontmatterPMSignals(t *testing.T) {
	tests := []struct {
		name          string
		metadata      map[string]string
		wantClass     SourceClass
		wantAuthority Authority
		wantProfile   Profile
	}{
		{
			name: "active P0 bug is authoritative PM memory",
			metadata: map[string]string{
				"fm.type":     "bug",
				"fm.status":   "active",
				"fm.priority": "P0",
			},
			wantClass:     SourceClassPMItem,
			wantAuthority: AuthorityAuthoritative,
			wantProfile:   ProfileProjectMemory,
		},
		{
			name: "resolved task is archived",
			metadata: map[string]string{
				"fm.type":   "task",
				"fm.status": "resolved",
			},
			wantClass:     SourceClassArchived,
			wantAuthority: AuthorityArchived,
			wantProfile:   ProfileArchive,
		},
		{
			name: "feature type marks PM item",
			metadata: map[string]string{
				"fm.type": "feature",
			},
			wantClass:     SourceClassPMItem,
			wantAuthority: AuthorityActive,
			wantProfile:   ProfileProjectMemory,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta := DeriveSourceMetadata(SourceMetadataInput{
				Path:        ".aman-pm/backlog/tasks/active/TASK-DOC05.md",
				ContentType: store.ContentTypeMarkdown,
				Metadata:    tt.metadata,
			})

			assert.Equal(t, tt.wantClass, meta.SourceClass)
			assert.Equal(t, tt.wantAuthority, meta.Authority)
			assert.Equal(t, tt.wantProfile, meta.Profile)
		})
	}
}

func TestDeriveSourceMetadata_SupersededByDowngradesContradictoryAcceptedStatus(t *testing.T) {
	meta := DeriveSourceMetadata(SourceMetadataInput{
		Path:        ".aman-pm/decisions/ADR-030-old-approach.md",
		ContentType: store.ContentTypeMarkdown,
		Content: `---
status: accepted
superseded_by: ADR-039
---
# ADR-030`,
	})

	assert.Equal(t, DecisionStatusSuperseded, meta.DecisionStatus)
	assert.Equal(t, AuthorityArchived, meta.Authority)
	assert.True(t, meta.Stale)
	assert.Contains(t, meta.FreshnessReason, "superseded")
}

func TestDecisionScopePrefixes_DerivesScopesFromADRMetadataRules(t *testing.T) {
	scopes := DecisionScopePrefixes(MetadataRules{Rules: []MetadataRule{
		{Pattern: ".aman-pm/decisions/ADR-*.md", SourceClass: SourceClassADR},
		{Pattern: "docs/reference/decisions/**", SourceClass: SourceClassADR},
		{Pattern: "docs/**", SourceClass: SourceClassDocs},
		{Pattern: ".aman-pm/decisions/ADR-*.md", SourceClass: SourceClassADR},
	}})

	assert.Equal(t, []string{
		".aman-pm/decisions",
		"docs/reference/decisions",
	}, scopes)
}

func TestProfileEligibility_ProjectMemoryExcludesReviewCorpusAndArchive(t *testing.T) {
	results := []*SearchResult{
		{Chunk: &store.Chunk{ID: "doc", FilePath: "docs/runbook.md", ContentType: store.ContentTypeMarkdown}},
		{Chunk: &store.Chunk{ID: "review", FilePath: "vend_feedback/f39-review.md", ContentType: store.ContentTypeMarkdown}},
		{Chunk: &store.Chunk{ID: "archive", FilePath: "archive/old-plan.md", ContentType: store.ContentTypeMarkdown}},
	}

	filtered, mismatches := ApplyProfileEligibility(results, SearchOptions{Profile: ProfileProjectMemory})

	require.Len(t, filtered, 1)
	assert.Equal(t, "doc", filtered[0].Chunk.ID)
	require.Len(t, mismatches, 2)
	assert.Equal(t, ProfileReviewCorpus, mismatches[0].RequiredProfile)
	assert.Equal(t, ProfileArchive, mismatches[1].RequiredProfile)
	assert.Contains(t, mismatches[0].Action, "review-corpus")
	assert.Contains(t, mismatches[1].Action, "archive")
}

func TestProfileEligibility_ExplicitReviewCorpusIncludesAdvisoryMaterial(t *testing.T) {
	results := []*SearchResult{
		{Chunk: &store.Chunk{ID: "review", FilePath: "improvements_dump/authority.md", ContentType: store.ContentTypeMarkdown}},
		{Chunk: &store.Chunk{ID: "doc", FilePath: "README.md", ContentType: store.ContentTypeMarkdown}},
	}

	filtered, mismatches := ApplyProfileEligibility(results, SearchOptions{Profile: ProfileReviewCorpus})

	require.Len(t, filtered, 1)
	assert.Equal(t, "review", filtered[0].Chunk.ID)
	assert.Equal(t, AuthorityAdvisory, filtered[0].SourceMetadata.Authority)
	require.Len(t, mismatches, 1)
	assert.Equal(t, ProfileProjectMemory, mismatches[0].RequiredProfile)
}

func TestProfileEligibility_DefaultProfileDoesNotReincludeExcludedPaths(t *testing.T) {
	notIndexable := false
	meta := DeriveSourceMetadata(SourceMetadataInput{
		Path:        "vend_feedback/excluded-by-amanmcp.md",
		ContentType: store.ContentTypeMarkdown,
		Indexable:   &notIndexable,
	})

	eligibility := ExplainProfileEligibility(meta, ProfileReviewCorpus)

	assert.False(t, eligibility.Eligible)
	assert.Equal(t, "path is not indexable", eligibility.Reason)
	assert.Contains(t, eligibility.Action, ".gitignore")
	assert.Contains(t, eligibility.Action, ".amanmcp.yaml")
}

func TestApplyAuthorityBoost_PrioritizesActiveAuthoritativeMaterial(t *testing.T) {
	results := []*SearchResult{
		{
			Chunk: &store.Chunk{ID: "advisory", FilePath: "vend_feedback/review.md"},
			Score: 0.95,
			SourceMetadata: SourceMetadata{
				SourceClass: SourceClassReviewCorpus,
				Authority:   AuthorityAdvisory,
				Profile:     ProfileReviewCorpus,
			},
		},
		{
			Chunk: &store.Chunk{ID: "active", FilePath: "docs/runbook.md"},
			Score: 0.90,
			SourceMetadata: SourceMetadata{
				SourceClass: SourceClassDocs,
				Authority:   AuthorityActive,
				Profile:     ProfileProjectMemory,
			},
		},
	}

	boosted := ApplyAuthorityBoost(results)

	require.Len(t, boosted, 2)
	assert.Equal(t, "active", boosted[0].Chunk.ID)
	assert.Equal(t, 0.90, boosted[0].Score)
}

func TestProfileEligibility_CustomRulesAffectEligibility(t *testing.T) {
	rules := ProfileRules{Profiles: map[Profile]ProfileRule{
		ProfileProjectMemory: {
			SourceClasses:        []SourceClass{SourceClassDocs},
			ExcludeSourceClasses: []SourceClass{SourceClassReviewCorpus},
		},
		ProfileReviewCorpus: {
			Include:       []string{"custom_reviews/**"},
			SourceClasses: []SourceClass{SourceClassReviewCorpus},
			Authorities:   []Authority{AuthorityAdvisory},
		},
	}}
	meta := DeriveSourceMetadata(SourceMetadataInput{
		Path:        "custom_reviews/f39-review.md",
		ContentType: store.ContentTypeMarkdown,
		Rules: MetadataRules{Rules: []MetadataRule{
			{Pattern: "custom_reviews/**", SourceClass: SourceClassReviewCorpus, Authority: AuthorityAdvisory, Profile: ProfileReviewCorpus},
		}},
	})

	projectEligibility := ExplainProfileEligibilityWithRules(meta, ProfileProjectMemory, rules)
	reviewEligibility := ExplainProfileEligibilityWithRules(meta, ProfileReviewCorpus, rules)

	assert.False(t, projectEligibility.Eligible)
	assert.Equal(t, ProfileReviewCorpus, projectEligibility.RequiredProfile)
	assert.True(t, reviewEligibility.Eligible)
}

func TestApplyFilters_DecisionModesSeparateCurrentAndHistory(t *testing.T) {
	results := []*SearchResult{
		{
			Chunk: &store.Chunk{
				ID:          "active",
				FilePath:    ".aman-pm/decisions/ADR-039-current.md",
				ContentType: store.ContentTypeMarkdown,
				Content:     "status: accepted",
			},
			Score: 0.8,
		},
		{
			Chunk: &store.Chunk{
				ID:          "superseded",
				FilePath:    ".aman-pm/decisions/ADR-030-old.md",
				ContentType: store.ContentTypeMarkdown,
				Content:     "status: superseded\nsuperseded_by: ADR-039",
			},
			Score: 0.9,
		},
		{
			Chunk: &store.Chunk{
				ID:          "runbook",
				FilePath:    "docs/runbook.md",
				ContentType: store.ContentTypeMarkdown,
				Content:     "decision notes",
			},
			Score: 1.0,
		},
	}

	current := ApplyFilters(cloneSearchResults(results), SearchOptions{
		Filter:  "docs",
		Profile: ProfileProjectMemory,
		Mode:    SearchModeDecisions,
	})
	require.Len(t, current, 1)
	assert.Equal(t, "active", current[0].Chunk.ID)

	history := ApplyFilters(cloneSearchResults(results), SearchOptions{
		Filter: "docs",
		Mode:   SearchModeDecisionHistory,
	})
	require.Len(t, history, 2)
	assert.Equal(t, "active", history[0].Chunk.ID)
	assert.Equal(t, "superseded", history[1].Chunk.ID)
}

func cloneSearchResults(in []*SearchResult) []*SearchResult {
	out := make([]*SearchResult, len(in))
	for i, result := range in {
		copyResult := *result
		out[i] = &copyResult
	}
	return out
}
