package secrets

import "fmt"

// Action describes how secret-bearing content should be handled.
type Action string

const (
	ActionAllow  Action = "allow"
	ActionRedact Action = "redact"
	ActionSkip   Action = "skip"
)

// Confidence is the detector confidence label exposed in warnings.
type Confidence string

const (
	ConfidenceHigh Confidence = "high"
)

// ContentSource identifies the caller path that is using the scanner.
type ContentSource string

const (
	SourceIndex                  ContentSource = "index"
	SourcePMCaptureLearning      ContentSource = "pm.capture_learning"
	SourcePMAddChangelogFragment ContentSource = "pm.add_changelog_fragment"
	SourcePMFileItem             ContentSource = "pm.file_item"
)

// ContentInput is the stable scanner API for file and mutation-written content.
type ContentInput struct {
	Path    string
	Content []byte
	Source  ContentSource
}

// Location is a redacted source location. It intentionally contains no raw
// matched bytes and no byte offsets that could reconstruct a secret value.
type Location struct {
	Line      int `json:"line"`
	Column    int `json:"column"`
	EndLine   int `json:"end_line"`
	EndColumn int `json:"end_column"`
}

func (l Location) String() string {
	return fmt.Sprintf("%d:%d-%d:%d", l.Line, l.Column, l.EndLine, l.EndColumn)
}

// Warning is safe to serialize, log, and show to users.
type Warning struct {
	FilePath   string     `json:"file_path"`
	DetectorID string     `json:"detector_id"`
	Confidence Confidence `json:"confidence"`
	Action     Action     `json:"action"`
	Location   Location   `json:"location"`
}

func (w Warning) Error() string {
	return fmt.Sprintf("secret_scan file=%s detector_id=%s confidence=%s action=%s location=%s",
		w.FilePath, w.DetectorID, w.Confidence, w.Action, w.Location.String())
}

// Result is the scanner decision. Content is only populated by GuardContent.
type Result struct {
	Content  []byte
	Warnings []Warning
	Action   Action
	Blocked  bool
}

// Allowlist configures known-safe false positives.
type Allowlist struct {
	Values       []string
	Patterns     []string
	PathPatterns []string
}

// Policy is deliberately standalone so PM mutation packages can reuse the same
// scanner without depending on index internals.
type Policy struct {
	Enabled         bool
	MinEntropy      float64
	MinTokenLength  int
	RedactionText   string
	DetectorActions map[string]Action
	Allowlist       Allowlist
}

// DefaultPolicy fails closed for private keys and redacts high-confidence token
// spans before content reaches indexing or embedding sinks.
func DefaultPolicy() Policy {
	return Policy{
		Enabled:        true,
		MinEntropy:     3.5,
		MinTokenLength: 20,
		RedactionText:  "[REDACTED:%s]",
		DetectorActions: map[string]Action{
			"aws-access-key-id":         ActionRedact,
			"bearer-token":              ActionRedact,
			"generic-secret-assignment": ActionRedact,
			"github-token":              ActionRedact,
			"openai-api-key":            ActionRedact,
			"private-key-block":         ActionSkip,
			"slack-token":               ActionRedact,
		},
		Allowlist: Allowlist{
			Patterns: []string{
				`(?i)\b(example|placeholder|changeme|dummy|fake|redacted)\b`,
			},
		},
	}
}

func normalizePolicy(policy Policy) Policy {
	defaults := DefaultPolicy()
	if policy.MinEntropy == 0 {
		policy.MinEntropy = defaults.MinEntropy
	}
	if policy.MinTokenLength == 0 {
		policy.MinTokenLength = defaults.MinTokenLength
	}
	if policy.RedactionText == "" {
		policy.RedactionText = defaults.RedactionText
	}
	mergedActions := make(map[string]Action, len(defaults.DetectorActions)+len(policy.DetectorActions))
	for detectorID, action := range defaults.DetectorActions {
		mergedActions[detectorID] = action
	}
	for detectorID, action := range policy.DetectorActions {
		mergedActions[detectorID] = action
	}
	policy.DetectorActions = mergedActions
	if len(policy.Allowlist.Patterns) == 0 {
		policy.Allowlist.Patterns = append([]string(nil), defaults.Allowlist.Patterns...)
	}
	return policy
}
