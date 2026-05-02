package search

import (
	"fmt"
	"strings"
)

var semanticRerankerClasses = map[string]struct{}{
	"semantic":                {},
	"mixed":                   {},
	"natural_language_intent": {},
	"docs_to_code":            {},
	"caller_callee":           {},
	"impact_analysis":         {},
	"cross_file_subsystem":    {},
	"test_to_implementation":  {},
	"decision_lookup":         {},
}

// Validate rejects unsupported reranker policies instead of silently falling back.
func (p RerankerPolicy) Validate() error {
	switch p.normalized() {
	case RerankerPolicyAuto, RerankerPolicyAlways, RerankerPolicyNever:
		return nil
	default:
		return fmt.Errorf("reranker policy must be one of auto, always, or never, got %q", p)
	}
}

func (p RerankerPolicy) normalized() RerankerPolicy {
	switch RerankerPolicy(strings.ToLower(strings.TrimSpace(string(p)))) {
	case "":
		return RerankerPolicyAuto
	case RerankerPolicyAlways:
		return RerankerPolicyAlways
	case RerankerPolicyNever:
		return RerankerPolicyNever
	default:
		return RerankerPolicy(strings.ToLower(strings.TrimSpace(string(p))))
	}
}

type RerankerPolicyDecision struct {
	Apply      bool
	Policy     RerankerPolicy
	SkipReason string
}

// DecideRerankerPolicy centralizes the product policy for the optional reranker.
func DecideRerankerPolicy(policy RerankerPolicy, query string, classification *QueryClassification) RerankerPolicyDecision {
	policy = policy.normalized()
	switch policy {
	case RerankerPolicyNever:
		return RerankerPolicyDecision{Policy: policy, SkipReason: RerankerSkipPolicyNever}
	case RerankerPolicyAlways:
		return RerankerPolicyDecision{Apply: true, Policy: policy}
	}

	reason := autoRerankerSkipReason(query, classification)
	if reason == RerankerSkipPolicyAutoSemantic {
		return RerankerPolicyDecision{Apply: true, Policy: policy}
	}
	return RerankerPolicyDecision{Policy: policy, SkipReason: reason}
}

func autoRerankerSkipReason(query string, classification *QueryClassification) string {
	if classification == nil || classification.Type == "" ||
		classification.ConfidenceState == QueryClassificationConfidenceUnavailable {
		return RerankerSkipPolicyAutoUnknownClass
	}

	queryClass := strings.ToLower(strings.TrimSpace(string(classification.Type)))
	switch queryClass {
	case "path_lookup":
		return RerankerSkipPolicyAutoPath
	case "quoted_string":
		return RerankerSkipPolicyAutoQuoted
	case "exact_identifier", "config_error", "error_code", "lexical":
		return lexicalRerankerSkipReason(query)
	case "negative_adversarial":
		return RerankerSkipPolicyAutoNegative
	}
	if _, ok := semanticRerankerClasses[queryClass]; ok {
		return RerankerSkipPolicyAutoSemantic
	}
	return RerankerSkipPolicyAutoUnknownClass
}

func lexicalRerankerSkipReason(query string) string {
	query = strings.TrimSpace(query)
	if quotedPattern.MatchString(query) {
		return RerankerSkipPolicyAutoQuoted
	}
	if filePathPattern.MatchString(query) {
		return RerankerSkipPolicyAutoPath
	}
	return RerankerSkipPolicyAutoLexical
}
