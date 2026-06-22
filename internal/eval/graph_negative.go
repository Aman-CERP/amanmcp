package eval

import (
	"fmt"
	"strings"

	"github.com/Aman-CERP/amanmcp/internal/graph"
)

// GraphNegativeExpectation declares the graceful outcome and prohibited behaviors
// for a negative_adversarial graph eval case.
type GraphNegativeExpectation struct {
	ExpectedResolution   string                      `json:"expected_resolution,omitempty" yaml:"expected_resolution,omitempty"`
	MinCandidates        int                         `json:"min_candidates,omitempty" yaml:"min_candidates,omitempty"`
	MaxResults           *int                        `json:"max_results,omitempty" yaml:"max_results,omitempty"`
	MinResults           int                         `json:"min_results,omitempty" yaml:"min_results,omitempty"`
	ExpectedWarningCodes []graph.WarningCode         `json:"expected_warning_codes,omitempty" yaml:"expected_warning_codes,omitempty"`
	ExpectedLabels       []GraphDegradationLabel     `json:"expected_labels,omitempty" yaml:"expected_labels,omitempty"`
	ProhibitedEvidence   []GraphExpectedEvidence     `json:"prohibited_evidence,omitempty" yaml:"prohibited_evidence,omitempty"`
}

// graphNegativeInput is the minimal projection needed to score a negative case.
type graphNegativeInput struct {
	Resolution     string
	CandidateCount int
	ResultCount    int
	Results        []graph.QueryResult
	WarningCodes   []graph.WarningCode
	DegradationLabels []GraphDegradationLabel
}

func validateGraphNegativeExpectation(neg GraphNegativeExpectation) error {
	if strings.TrimSpace(neg.ExpectedResolution) != "" &&
		neg.ExpectedResolution != graph.ResolutionResolved &&
		neg.ExpectedResolution != graph.ResolutionDisambiguationRequired &&
		neg.ExpectedResolution != graph.ResolutionSubjectNotFound {
		return fmt.Errorf("unsupported expected_resolution %q", neg.ExpectedResolution)
	}
	if neg.MinCandidates < 0 {
		return fmt.Errorf("min_candidates must be non-negative")
	}
	if neg.MinResults < 0 {
		return fmt.Errorf("min_results must be non-negative")
	}
	for _, code := range neg.ExpectedWarningCodes {
		if !allowedGraphWarningCodes[code] {
			return fmt.Errorf("unsupported expected warning code %q", code)
		}
	}
	for _, label := range neg.ExpectedLabels {
		if !isKnownGraphDegradationLabel(label) {
			return fmt.Errorf("unsupported expected degradation label %q", label)
		}
	}
	for _, prohibited := range neg.ProhibitedEvidence {
		if err := validateGraphExpectedEvidence(prohibited); err != nil {
			return fmt.Errorf("prohibited evidence: %w", err)
		}
		if !hasGraphExpectedMatcher(prohibited) {
			return fmt.Errorf("prohibited evidence without a matcher")
		}
	}
	if !hasGraphNegativeAssertion(neg) {
		return fmt.Errorf("negative expectation requires at least one assertion")
	}
	return nil
}

func hasGraphNegativeAssertion(neg GraphNegativeExpectation) bool {
	return strings.TrimSpace(neg.ExpectedResolution) != "" ||
		neg.MinCandidates > 0 ||
		neg.MaxResults != nil ||
		neg.MinResults > 0 ||
		len(neg.ExpectedWarningCodes) > 0 ||
		len(neg.ExpectedLabels) > 0 ||
		len(neg.ProhibitedEvidence) > 0
}

// scoreGraphNegativeCase returns whether a negative_adversarial case passed and
// a failure reason when it did not.
func scoreGraphNegativeCase(neg GraphNegativeExpectation, in graphNegativeInput) (bool, string) {
	if strings.TrimSpace(neg.ExpectedResolution) != "" && in.Resolution != neg.ExpectedResolution {
		return false, fmt.Sprintf("resolution %q != expected %q", in.Resolution, neg.ExpectedResolution)
	}
	if neg.MinCandidates > 0 && in.CandidateCount < neg.MinCandidates {
		return false, fmt.Sprintf("candidate count %d < min %d", in.CandidateCount, neg.MinCandidates)
	}
	if neg.MaxResults != nil && in.ResultCount > *neg.MaxResults {
		return false, fmt.Sprintf("result count %d > max %d (fabricated/merged guess)", in.ResultCount, *neg.MaxResults)
	}
	if neg.MinResults > 0 && in.ResultCount < neg.MinResults {
		return false, fmt.Sprintf("result count %d < min %d (silent empty)", in.ResultCount, neg.MinResults)
	}
	if missing := missingGraphWarningCodes(neg.ExpectedWarningCodes, in.WarningCodes); len(missing) > 0 {
		return false, fmt.Sprintf("missing expected warning codes: %s", strings.Join(graphWarningCodeStrings(missing), ", "))
	}
	for _, wantLabel := range neg.ExpectedLabels {
		if !containsGraphDegradationLabel(in.DegradationLabels, wantLabel) {
			return false, fmt.Sprintf("missing expected degradation label %q", wantLabel)
		}
	}
	for _, prohibited := range neg.ProhibitedEvidence {
		for _, result := range in.Results {
			if matchesGraphEvidence(prohibited, result) {
				return false, fmt.Sprintf("prohibited evidence matched: %s", graphExpectedEvidenceIdentity(prohibited))
			}
		}
	}
	return true, ""
}

func containsGraphDegradationLabel(labels []GraphDegradationLabel, want GraphDegradationLabel) bool {
	for _, label := range labels {
		if label == want {
			return true
		}
	}
	return false
}

func graphSubsetRequiresNegativeAdversarialCoverage(subset string) bool {
	subset = strings.TrimSpace(subset)
	return subset == GraphSubsetQuick || subset == GraphSubsetFull
}

func summarizeGraphNegativeAdversarial(results []DirectGraphQueryResult) (passCount, total int, passRate float64) {
	for _, result := range results {
		if graphExpectationClassOrDefault(result.ExpectationClass) != GraphExpectationClassNegativeAdversarial {
			continue
		}
		total++
		if result.Passed {
			passCount++
		}
	}
	if total > 0 {
		passRate = float64(passCount) / float64(total)
	}
	return passCount, total, passRate
}

func graphNegativeGateFailures(summary DirectGraphSummary, subset string) []string {
	if graphSubsetRequiresNegativeAdversarialCoverage(subset) && summary.NegativeAdversarialCount == 0 {
		return []string{fmt.Sprintf(
			"no negative_adversarial cases selected for gated subset %q",
			subset,
		)}
	}
	if summary.NegativeAdversarialCount == 0 {
		return nil
	}
	if summary.NegativeAdversarialPassRate >= 1.0 {
		return nil
	}
	return []string{fmt.Sprintf(
		"negative_adversarial_pass_rate %.2f < 1.00 (%d/%d passed)",
		summary.NegativeAdversarialPassRate,
		summary.NegativeAdversarialPassCount,
		summary.NegativeAdversarialCount,
	)}
}