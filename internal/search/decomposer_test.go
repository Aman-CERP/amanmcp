package search

import (
	"testing"
)

// TestShouldDecompose tests the decomposition eligibility detection.
func TestShouldDecompose(t *testing.T) {
	d := NewPatternDecomposer()

	tests := []struct {
		name     string
		query    string
		expected bool
		reason   string
	}{
		// Should decompose: generic noun+function patterns (the failing queries)
		{
			name:     "Search function - should decompose",
			query:    "Search function",
			expected: true,
			reason:   "Generic 2-word noun+function pattern",
		},
		{
			name:     "Index function - should decompose",
			query:    "Index function",
			expected: true,
			reason:   "Generic 2-word noun+function pattern",
		},
		{
			name:     "search function lowercase - should decompose",
			query:    "search function",
			expected: true,
			reason:   "Case-insensitive pattern matching",
		},
		{
			name:     "Index method - should decompose",
			query:    "Index method",
			expected: true,
			reason:   "method is synonym of function",
		},
		{
			name:     "How does RRF fusion work - should decompose",
			query:    "How does RRF fusion work",
			expected: true,
			reason:   "How does X work pattern for generic queries",
		},

		// Should NOT decompose: specific identifiers
		{
			name:     "camelCase identifier - skip",
			query:    "OllamaEmbedder",
			expected: false,
			reason:   "Specific identifier, already targeted",
		},
		{
			name:     "PascalCase identifier - skip",
			query:    "SearchEngine",
			expected: false,
			reason:   "Specific identifier, already targeted",
		},
		{
			name:     "snake_case identifier - skip",
			query:    "bm25_search",
			expected: false,
			reason:   "Specific identifier, already targeted",
		},

		// Should NOT decompose: file paths
		{
			name:     "file path - skip",
			query:    "internal/search/engine.go",
			expected: false,
			reason:   "File path is already specific",
		},
		{
			name:     "relative path - skip",
			query:    "search/fusion.go",
			expected: false,
			reason:   "File path is already specific",
		},

		// Should NOT decompose: quoted phrases
		{
			name:     "quoted phrase - skip",
			query:    `"exact match"`,
			expected: false,
			reason:   "Quoted phrases are for exact match",
		},

		// Should NOT decompose: single words
		{
			name:     "single word - skip",
			query:    "Search",
			expected: false,
			reason:   "Single words don't benefit from decomposition",
		},

		// Should NOT decompose: 4+ word natural language (already semantic)
		{
			name:     "long question - skip",
			query:    "Where is the vector store implementation",
			expected: false,
			reason:   "Long natural language already works with semantic search",
		},
		{
			name:     "detailed how question - skip",
			query:    "How do I add a new language parser",
			expected: false,
			reason:   "Detailed questions already semantic-optimized",
		},

		// Edge cases
		{
			name:     "empty query - skip",
			query:    "",
			expected: false,
			reason:   "Empty queries can't be decomposed",
		},
		{
			name:     "whitespace only - skip",
			query:    "   ",
			expected: false,
			reason:   "Whitespace-only treated as empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := d.ShouldDecompose(tt.query)
			if got != tt.expected {
				t.Errorf("ShouldDecompose(%q) = %v, want %v (%s)",
					tt.query, got, tt.expected, tt.reason)
			}
		})
	}
}

// TestDecompose tests the query decomposition logic.
func TestDecompose(t *testing.T) {
	d := NewPatternDecomposer()

	tests := []struct {
		name            string
		query           string
		minSubQueries   int
		mustContain     []string // At least these terms should appear in sub-queries
		mustNotContain  []string // These should NOT appear (e.g., docs-only terms)
	}{
		{
			name:          "Search function decomposition",
			query:         "Search function",
			minSubQueries: 3,
			mustContain:   []string{"func Search", "Search"}, // Go pattern + original noun
			mustNotContain: []string{},
		},
		{
			name:          "Index function decomposition",
			query:         "Index function",
			minSubQueries: 3,
			mustContain:   []string{"func Index", "Index"}, // Go pattern + original noun
			mustNotContain: []string{},
		},
		{
			name:          "How does RRF fusion work",
			query:         "How does RRF fusion work",
			minSubQueries: 2,
			mustContain:   []string{"RRF", "fusion"}, // Key terms extracted
			mustNotContain: []string{},
		},
		{
			name:          "non-decomposable returns original",
			query:         "OllamaEmbedder",
			minSubQueries: 1,
			mustContain:   []string{"OllamaEmbedder"}, // Original query preserved
			mustNotContain: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subQueries := d.Decompose(tt.query)

			// Check minimum sub-queries
			if len(subQueries) < tt.minSubQueries {
				t.Errorf("Decompose(%q) returned %d sub-queries, want at least %d",
					tt.query, len(subQueries), tt.minSubQueries)
			}

			// Collect all query strings
			allQueries := make(map[string]bool)
			for _, sq := range subQueries {
				allQueries[sq.Query] = true
			}

			// Check mustContain
			for _, term := range tt.mustContain {
				found := false
				for q := range allQueries {
					if q == term || containsSubstring(q, term) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Decompose(%q) should contain %q in sub-queries, got %v",
						tt.query, term, subQueries)
				}
			}

			// Check mustNotContain
			for _, term := range tt.mustNotContain {
				for q := range allQueries {
					if q == term {
						t.Errorf("Decompose(%q) should NOT contain %q in sub-queries",
							tt.query, term)
					}
				}
			}
		})
	}
}

// TestSubQueryWeights verifies that sub-query weights are reasonable.
func TestSubQueryWeights(t *testing.T) {
	d := NewPatternDecomposer()

	subQueries := d.Decompose("Search function")

	for _, sq := range subQueries {
		if sq.Weight <= 0 {
			t.Errorf("SubQuery %q has non-positive weight: %f", sq.Query, sq.Weight)
		}
		if sq.Weight > 2.0 {
			t.Errorf("SubQuery %q has unexpectedly high weight: %f", sq.Query, sq.Weight)
		}
	}
}

// TestDecomposeIdempotent verifies decomposing an already-decomposed query.
func TestDecomposeIdempotent(t *testing.T) {
	d := NewPatternDecomposer()

	// Non-decomposable query should return itself
	query := "OllamaEmbedder"
	subQueries := d.Decompose(query)

	if len(subQueries) != 1 {
		t.Errorf("Expected 1 sub-query for non-decomposable query, got %d", len(subQueries))
	}
	if subQueries[0].Query != query {
		t.Errorf("Expected original query %q, got %q", query, subQueries[0].Query)
	}
}

// Helper function to check substring containment.
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
		findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestGoMethodReceiverPatterns verifies FEAT-QI4: Go method receiver syntax patterns.
// These patterns match Go methods like "func (e *Engine) Search(" which standard
// patterns like "func Search" fail to match due to BM25 tokenization.
func TestGoMethodReceiverPatterns(t *testing.T) {
	d := NewPatternDecomposer()

	tests := []struct {
		name           string
		query          string
		expectedPatterns []string // Patterns that should be generated
	}{
		{
			name:  "Search function generates Go method patterns",
			query: "Search function",
			expectedPatterns: []string{
				") Search(",    // FEAT-QI4: Method receiver pattern
				"Search(ctx",   // FEAT-QI4: Context parameter pattern
				"func (search", // FEAT-QI4: Partial method signature
				"func Search",  // Existing pattern
			},
		},
		{
			name:  "Index function generates Go method patterns",
			query: "Index function",
			expectedPatterns: []string{
				") Index(",    // FEAT-QI4: Method receiver pattern
				"Index(ctx",   // FEAT-QI4: Context parameter pattern
				"func (index", // FEAT-QI4: Partial method signature
				"func Index",  // Existing pattern
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subQueries := d.Decompose(tt.query)

			// Collect all generated patterns
			patterns := make(map[string]bool)
			for _, sq := range subQueries {
				patterns[sq.Query] = true
			}

			// Verify expected patterns are present
			for _, expected := range tt.expectedPatterns {
				if !patterns[expected] {
					t.Errorf("Decompose(%q) missing expected pattern %q\nGot patterns: %v",
						tt.query, expected, mapKeys(patterns))
				}
			}

			// Verify method receiver pattern has highest weight
			var receiverWeight float64
			for _, sq := range subQueries {
				if sq.Query == ") Search(" || sq.Query == ") Index(" {
					receiverWeight = sq.Weight
					break
				}
			}
			if receiverWeight == 0 {
				t.Error("Method receiver pattern not found")
			}
			// It should be the highest weight pattern
			for _, sq := range subQueries {
				if sq.Weight > receiverWeight {
					t.Errorf("Method receiver pattern (weight %.1f) should have highest weight, but %q has %.1f",
						receiverWeight, sq.Query, sq.Weight)
					break
				}
			}
		})
	}
}

// mapKeys returns the keys of a map as a slice for easier debugging.
func mapKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
