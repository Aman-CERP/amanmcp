package eval

import (
	"fmt"
	"os"

	"github.com/Aman-CERP/amanmcp/internal/search"
	"gopkg.in/yaml.v3"
)

var allowedTools = map[string]bool{
	"search":      true,
	"search_code": true,
	"search_docs": true,
}

var allowedClasses = map[string]bool{
	"exact_identifier":        true,
	"path_lookup":             true,
	"quoted_string":           true,
	"config_error":            true,
	"natural_language_intent": true,
	"caller_callee":           true,
	"impact_analysis":         true,
	"docs_to_code":            true,
	"test_to_implementation":  true,
	"adr_to_code":             true,
	"cross_file_subsystem":    true,
	"negative_adversarial":    true,
}

var allowedJobs = map[string]bool{
	"code":            true,
	"project_memory":  true,
	"decision_lookup": true,
	"pm_inspection":   true,
	"exact_lookup":    true,
	"general":         true,
}

type rawCorpus struct {
	Queries  []Query `yaml:"queries"`
	Tier1    []Query `yaml:"tier1"`
	Tier2    []Query `yaml:"tier2"`
	Negative []Query `yaml:"negative"`
	Graded   []Query `yaml:"graded"`
}

func LoadCorpus(path string) (Corpus, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Corpus{}, fmt.Errorf("failed to read corpus %s: %w", path, err)
	}

	var raw rawCorpus
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return Corpus{}, fmt.Errorf("failed to parse corpus %s: %w", path, err)
	}

	var corpus Corpus
	corpus.Queries = append(corpus.Queries, raw.Queries...)
	corpus.Queries = append(corpus.Queries, legacyQueries(raw.Tier1, "1")...)
	corpus.Queries = append(corpus.Queries, legacyQueries(raw.Tier2, "2")...)
	corpus.Queries = append(corpus.Queries, legacyQueries(raw.Negative, "negative")...)
	corpus.Queries = append(corpus.Queries, legacyQueries(raw.Graded, "graded")...)

	if len(corpus.Queries) == 0 {
		return Corpus{}, fmt.Errorf("corpus %s contains no queries", path)
	}
	if err := ValidateCorpus(corpus); err != nil {
		return Corpus{}, err
	}
	return corpus, nil
}

func legacyQueries(queries []Query, tier string) []Query {
	for i := range queries {
		queries[i].Tier = tier
		if queries[i].Class == "" {
			queries[i].Class = legacyClass(queries[i], tier)
		}
		if queries[i].Job == "" {
			queries[i].Job = legacyJob(queries[i], tier)
		}
		if queries[i].Source == "" {
			queries[i].Source = "dogfood-tier" + tier
			if tier == "negative" {
				queries[i].Source = "dogfood-negative"
			}
		}
		if queries[i].ExpectedResults == nil && len(queries[i].Expected) > 0 {
			queries[i].ExpectedResults = make([]ExpectedResult, 0, len(queries[i].Expected))
			for _, expected := range queries[i].Expected {
				queries[i].ExpectedResults = append(queries[i].ExpectedResults, ExpectedResult{
					Path:      expected,
					Grade:     3,
					Rationale: "legacy expected path",
				})
			}
		}
	}
	return queries
}

func legacyClass(query Query, tier string) string {
	if tier == "negative" {
		return "negative_adversarial"
	}
	if query.Tool == "search_docs" {
		return "docs_to_code"
	}
	if query.Tool == "search_code" {
		return "exact_identifier"
	}
	return "natural_language_intent"
}

func legacyJob(query Query, tier string) string {
	if tier == "negative" {
		return "general"
	}
	if query.Tool == "search_docs" {
		return "project_memory"
	}
	if query.Tool == "search_code" {
		return "exact_lookup"
	}
	return "code"
}

func ValidateCorpus(corpus Corpus) error {
	seen := make(map[string]bool, len(corpus.Queries))
	for i, query := range corpus.Queries {
		if query.ID == "" {
			return fmt.Errorf("query at index %d missing id", i)
		}
		if seen[query.ID] {
			return fmt.Errorf("duplicate query id %q", query.ID)
		}
		seen[query.ID] = true
		if query.Name == "" {
			return fmt.Errorf("query %s missing name", query.ID)
		}
		if query.Query == "" && query.Class != "negative_adversarial" {
			return fmt.Errorf("query %s missing query text", query.ID)
		}
		if !allowedTools[query.Tool] {
			return fmt.Errorf("query %s uses unsupported tool %q", query.ID, query.Tool)
		}
		if _, err := search.ParseProfile(query.Profile); err != nil {
			return fmt.Errorf("query %s has invalid profile: %w", query.ID, err)
		}
		if _, err := search.ParseMode(query.Mode); err != nil {
			return fmt.Errorf("query %s has invalid mode: %w", query.ID, err)
		}
		for _, scope := range query.Scope {
			if scope == "" {
				return fmt.Errorf("query %s has empty scope", query.ID)
			}
		}
		if !allowedClasses[query.Class] {
			return fmt.Errorf("query %s uses unsupported class %q", query.ID, query.Class)
		}
		if !allowedJobs[query.Job] {
			return fmt.Errorf("query %s uses unsupported job %q", query.ID, query.Job)
		}
		if len(query.ExpectedResults) == 0 && query.Class != "negative_adversarial" {
			return fmt.Errorf("query %s missing expected evidence", query.ID)
		}
		for _, expected := range query.ExpectedResults {
			if expected.Path == "" {
				return fmt.Errorf("query %s has expected result without path", query.ID)
			}
			if expected.Grade < 0 || expected.Grade > 3 {
				return fmt.Errorf("query %s has invalid grade %d", query.ID, expected.Grade)
			}
			if expected.Page < 0 || expected.PageStart < 0 || expected.PageEnd < 0 {
				return fmt.Errorf("query %s has negative page expectation", query.ID)
			}
			if expected.PageStart > 0 && expected.PageEnd > 0 && expected.PageEnd < expected.PageStart {
				return fmt.Errorf("query %s has page_end before page_start", query.ID)
			}
		}
	}
	return nil
}
