package config

import (
	"fmt"

	"github.com/Aman-CERP/amanmcp/internal/search"
)

const (
	profileCode          = "code"
	profileProjectMemory = "project-memory"
	profileReviewCorpus  = "review-corpus"
	profileArchive       = "archive"
)

var validProfileSourceClasses = map[string]bool{
	"source_code":   true,
	"test":          true,
	"docs":          true,
	"adr":           true,
	"pm_item":       true,
	"generated":     true,
	"archived":      true,
	"review_corpus": true,
	"config":        true,
	"raw_evidence":  true,
	"unknown":       true,
}

var validProfileAuthorities = map[string]bool{
	"authoritative": true,
	"active":        true,
	"generated":     true,
	"archived":      true,
	"advisory":      true,
	"raw_evidence":  true,
	"unknown":       true,
}

func defaultSearchProfiles() map[string]SearchProfileConfig {
	return map[string]SearchProfileConfig{
		profileCode: {
			Include:              []string{"cmd/**", "internal/**", "pkg/**", "configs/**", ".amanmcp.yaml", ".gitignore"},
			SourceClasses:        []string{"source_code", "test", "config"},
			ExcludeSourceClasses: []string{"review_corpus", "archived", "raw_evidence"},
		},
		profileProjectMemory: {
			Include: []string{
				"README.md",
				"docs/**",
				".aman-pm/product/**",
				".aman-pm/backlog/**",
				".aman-pm/sprints/active/**",
				".aman-pm/decisions/**",
			},
			Exclude:              []string{"vend_feedback/**", "improvements_dump/**", "archive/**", "**/*.log"},
			SourceClasses:        []string{"docs", "adr", "pm_item", "generated", "config", "unknown"},
			ExcludeSourceClasses: []string{"review_corpus", "archived", "raw_evidence"},
		},
		profileReviewCorpus: {
			Include:       []string{"vend_feedback/**", "improvements_dump/**"},
			SourceClasses: []string{"review_corpus", "raw_evidence"},
			Authorities:   []string{"advisory", "raw_evidence"},
		},
		profileArchive: {
			Include:       []string{"archive/**"},
			SourceClasses: []string{"archived"},
			Authorities:   []string{"archived"},
		},
	}
}

func mergeSearchProfiles(base, overlay map[string]SearchProfileConfig) map[string]SearchProfileConfig {
	merged := cloneSearchProfiles(base)
	if merged == nil {
		merged = make(map[string]SearchProfileConfig, len(overlay))
	}

	for name, profile := range overlay {
		existing := merged[name]
		existing.Include = appendStringSet(existing.Include, profile.Include...)
		existing.Exclude = appendStringSet(existing.Exclude, profile.Exclude...)
		existing.SourceClasses = appendStringSet(existing.SourceClasses, profile.SourceClasses...)
		existing.ExcludeSourceClasses = appendStringSet(existing.ExcludeSourceClasses, profile.ExcludeSourceClasses...)
		existing.Authorities = appendStringSet(existing.Authorities, profile.Authorities...)
		existing.ExcludeAuthorities = appendStringSet(existing.ExcludeAuthorities, profile.ExcludeAuthorities...)
		merged[name] = existing
	}

	return merged
}

func cloneSearchProfiles(in map[string]SearchProfileConfig) map[string]SearchProfileConfig {
	if len(in) == 0 {
		return nil
	}

	out := make(map[string]SearchProfileConfig, len(in))
	for name, profile := range in {
		out[name] = SearchProfileConfig{
			Include:              append([]string(nil), profile.Include...),
			Exclude:              append([]string(nil), profile.Exclude...),
			SourceClasses:        append([]string(nil), profile.SourceClasses...),
			ExcludeSourceClasses: append([]string(nil), profile.ExcludeSourceClasses...),
			Authorities:          append([]string(nil), profile.Authorities...),
			ExcludeAuthorities:   append([]string(nil), profile.ExcludeAuthorities...),
		}
	}
	return out
}

func appendStringSet(base []string, values ...string) []string {
	if len(values) == 0 {
		return base
	}

	seen := make(map[string]struct{}, len(base)+len(values))
	for _, value := range base {
		seen[value] = struct{}{}
	}
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		base = append(base, value)
		seen[value] = struct{}{}
	}
	return base
}

func validateSearchProfiles(profiles map[string]SearchProfileConfig) error {
	for name, profile := range profiles {
		if name == "" {
			return fmt.Errorf("search.profiles contains an empty profile name")
		}
		if err := validateProfileValues(name, "source_classes", profile.SourceClasses, validProfileSourceClasses); err != nil {
			return err
		}
		if err := validateProfileValues(name, "exclude_source_classes", profile.ExcludeSourceClasses, validProfileSourceClasses); err != nil {
			return err
		}
		if err := validateProfileValues(name, "authorities", profile.Authorities, validProfileAuthorities); err != nil {
			return err
		}
		if err := validateProfileValues(name, "exclude_authorities", profile.ExcludeAuthorities, validProfileAuthorities); err != nil {
			return err
		}
	}
	return nil
}

func validateProfileValues(profileName, field string, values []string, valid map[string]bool) error {
	for _, value := range values {
		if value == "" {
			return fmt.Errorf("search.profiles.%s.%s contains an empty value", profileName, field)
		}
		if !valid[value] {
			return fmt.Errorf("search.profiles.%s.%s contains unknown value %q", profileName, field, value)
		}
	}
	return nil
}

// SearchProfileRules converts configured search profiles into runtime profile
// eligibility rules. Config validation guarantees enum strings are known.
func (c *Config) SearchProfileRules() search.ProfileRules {
	out := search.ProfileRules{Profiles: make(map[search.Profile]search.ProfileRule, len(c.Search.Profiles))}
	for name, profile := range c.Search.Profiles {
		parsed, err := search.ParseProfile(name)
		if err != nil {
			continue
		}
		out.Profiles[parsed] = search.ProfileRule{
			Include:              append([]string(nil), profile.Include...),
			Exclude:              append([]string(nil), profile.Exclude...),
			SourceClasses:        toSearchSourceClasses(profile.SourceClasses),
			ExcludeSourceClasses: toSearchSourceClasses(profile.ExcludeSourceClasses),
			Authorities:          toSearchAuthorities(profile.Authorities),
			ExcludeAuthorities:   toSearchAuthorities(profile.ExcludeAuthorities),
		}
	}
	if len(out.Profiles) == 0 {
		return search.DefaultProfileRules()
	}
	return out
}

// SearchMetadataRules adds profile include extensions to the built-in metadata
// classifier so user profile additions affect runtime source classification.
func (c *Config) SearchMetadataRules() search.MetadataRules {
	rules := search.DefaultMetadataRules()
	defaults := defaultSearchProfiles()
	for name, profile := range c.Search.Profiles {
		parsed, err := search.ParseProfile(name)
		if err != nil {
			continue
		}
		defaultIncludes := make(map[string]struct{})
		if defaultProfile, ok := defaults[name]; ok {
			for _, pattern := range defaultProfile.Include {
				defaultIncludes[pattern] = struct{}{}
			}
		}
		for _, pattern := range profile.Include {
			if _, isDefault := defaultIncludes[pattern]; isDefault {
				continue
			}
			sourceClass, authority := profileMetadataDefaults(parsed, profile)
			rules.Rules = append(rules.Rules, search.MetadataRule{
				Pattern:     pattern,
				SourceClass: sourceClass,
				Authority:   authority,
				Profile:     parsed,
				Generated:   sourceClass == search.SourceClassGenerated,
				Stale:       sourceClass == search.SourceClassArchived,
			})
		}
	}
	return rules
}

func toSearchSourceClasses(values []string) []search.SourceClass {
	out := make([]search.SourceClass, 0, len(values))
	for _, value := range values {
		out = append(out, search.SourceClass(value))
	}
	return out
}

func toSearchAuthorities(values []string) []search.Authority {
	out := make([]search.Authority, 0, len(values))
	for _, value := range values {
		out = append(out, search.Authority(value))
	}
	return out
}

func profileMetadataDefaults(profile search.Profile, cfg SearchProfileConfig) (search.SourceClass, search.Authority) {
	classes := toSearchSourceClasses(cfg.SourceClasses)
	authorities := toSearchAuthorities(cfg.Authorities)
	sourceClass := defaultSourceClassForProfile(profile)
	authority := defaultAuthorityForProfile(profile)
	if len(classes) == 1 {
		sourceClass = classes[0]
	}
	if len(authorities) == 1 {
		authority = authorities[0]
	}
	return sourceClass, authority
}

func defaultSourceClassForProfile(profile search.Profile) search.SourceClass {
	switch profile {
	case search.ProfileCode:
		return search.SourceClassSourceCode
	case search.ProfileReviewCorpus:
		return search.SourceClassReviewCorpus
	case search.ProfileArchive:
		return search.SourceClassArchived
	default:
		return search.SourceClassDocs
	}
}

func defaultAuthorityForProfile(profile search.Profile) search.Authority {
	switch profile {
	case search.ProfileCode:
		return search.AuthorityActive
	case search.ProfileReviewCorpus:
		return search.AuthorityAdvisory
	case search.ProfileArchive:
		return search.AuthorityArchived
	default:
		return search.AuthorityActive
	}
}
