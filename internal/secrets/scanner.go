package secrets

import (
	"bytes"
	"fmt"
	"math"
	"regexp"
	"sort"
)

type detector struct {
	id              string
	expr            *regexp.Regexp
	valueGroup      int
	requiresEntropy bool
}

type finding struct {
	warning Warning
	start   int
	end     int
}

// Scanner detects high-confidence secrets without exposing matched values.
type Scanner struct {
	policy Policy
}

// NewScanner creates a policy-driven secret scanner.
func NewScanner(policy Policy) *Scanner {
	return &Scanner{policy: normalizePolicy(policy)}
}

// ScanContent returns sanitized warning metadata without mutating content.
func (s *Scanner) ScanContent(input ContentInput) Result {
	findings := s.find(input)
	return Result{
		Warnings: warningsFromFindings(findings),
		Action:   aggregateAction(findings),
		Blocked:  hasSkipFinding(findings),
	}
}

// GuardContent applies the scanner decision to content before it reaches sinks.
func (s *Scanner) GuardContent(input ContentInput) Result {
	if !s.policy.Enabled {
		return Result{Content: cloneBytes(input.Content), Action: ActionAllow}
	}

	findings := s.find(input)
	action := aggregateAction(findings)
	warnings := warningsFromFindings(findings)
	if action == ActionSkip {
		return Result{
			Warnings: warnings,
			Action:   ActionSkip,
			Blocked:  true,
		}
	}
	if action == ActionRedact {
		return Result{
			Content:  s.redact(input.Content, findings),
			Warnings: warnings,
			Action:   ActionRedact,
		}
	}
	return Result{
		Content: cloneBytes(input.Content),
		Action:  ActionAllow,
	}
}

func (s *Scanner) find(input ContentInput) []finding {
	if !s.policy.Enabled || len(input.Content) == 0 {
		return nil
	}

	var findings []finding
	for _, d := range defaultDetectors() {
		matches := d.expr.FindAllSubmatchIndex(input.Content, -1)
		for _, match := range matches {
			startIndex, endIndex := match[0], match[1]
			if d.valueGroup > 0 {
				groupStart := d.valueGroup * 2
				groupEnd := groupStart + 1
				if groupEnd >= len(match) || match[groupStart] < 0 || match[groupEnd] < 0 {
					continue
				}
				startIndex, endIndex = match[groupStart], match[groupEnd]
			}
			if startIndex < 0 || endIndex <= startIndex {
				continue
			}

			value := string(input.Content[startIndex:endIndex])
			if s.isAllowlisted(input.Path, value) {
				continue
			}
			if d.requiresEntropy && !s.hasEnoughEntropy(value) {
				continue
			}
			if overlapsAny(startIndex, endIndex, findings) {
				continue
			}

			action := s.actionFor(d.id)
			if action == ActionAllow {
				continue
			}
			findings = append(findings, finding{
				warning: Warning{
					FilePath:   input.Path,
					DetectorID: d.id,
					Confidence: ConfidenceHigh,
					Action:     action,
					Location:   locate(input.Content, startIndex, endIndex),
				},
				start: startIndex,
				end:   endIndex,
			})
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].start == findings[j].start {
			return findings[i].end < findings[j].end
		}
		return findings[i].start < findings[j].start
	})
	return findings
}

func defaultDetectors() []detector {
	return []detector{
		{
			id:   "private-key-block",
			expr: regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`),
		},
		{
			id:              "github-token",
			expr:            regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9_]{36,255}\b`),
			requiresEntropy: true,
		},
		{
			id:              "slack-token",
			expr:            regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{20,}\b`),
			requiresEntropy: true,
		},
		{
			id:              "openai-api-key",
			expr:            regexp.MustCompile(`\bsk-[A-Za-z0-9]{20,}\b`),
			requiresEntropy: true,
		},
		{
			id:   "aws-access-key-id",
			expr: regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`),
		},
		{
			id:              "bearer-token",
			expr:            regexp.MustCompile(`(?i)\bbearer\s+([A-Za-z0-9][A-Za-z0-9._~+/=-]{19,})`),
			valueGroup:      1,
			requiresEntropy: true,
		},
		{
			id:              "generic-secret-assignment",
			expr:            regexp.MustCompile(`(?i)\b(?:api[_-]?key|apikey|access[_-]?token|accesstoken|auth[_-]?token|authtoken|secret[_-]?key|secretkey|client[_-]?secret|clientsecret|private[_-]?key|privatekey|token)\b\s*[:=]\s*["'` + "`" + `]?([A-Za-z0-9][A-Za-z0-9._~+/=-]{19,})["'` + "`" + `]?`),
			valueGroup:      1,
			requiresEntropy: true,
		},
	}
}

func (s *Scanner) actionFor(detectorID string) Action {
	if action, ok := s.policy.DetectorActions[detectorID]; ok {
		return action
	}
	return ActionRedact
}

func (s *Scanner) hasEnoughEntropy(value string) bool {
	if len(value) < s.policy.MinTokenLength {
		return false
	}
	return ShannonEntropy(value) >= s.policy.MinEntropy
}

func (s *Scanner) isAllowlisted(path, value string) bool {
	for _, allowed := range s.policy.Allowlist.Values {
		if value == allowed {
			return true
		}
	}
	for _, pattern := range s.policy.Allowlist.Patterns {
		expr, err := regexp.Compile(pattern)
		if err != nil {
			continue
		}
		if expr.MatchString(value) {
			return true
		}
	}
	for _, pattern := range s.policy.Allowlist.PathPatterns {
		expr, err := regexp.Compile(pattern)
		if err != nil {
			continue
		}
		if expr.MatchString(path) {
			return true
		}
	}
	return false
}

func (s *Scanner) redact(content []byte, findings []finding) []byte {
	if len(findings) == 0 {
		return cloneBytes(content)
	}

	var out bytes.Buffer
	cursor := 0
	for _, f := range findings {
		if f.warning.Action != ActionRedact {
			continue
		}
		if f.start < cursor {
			continue
		}
		out.Write(content[cursor:f.start])
		out.WriteString(s.redactionMarker(f.warning.DetectorID))
		cursor = f.end
	}
	out.Write(content[cursor:])
	return out.Bytes()
}

func (s *Scanner) redactionMarker(detectorID string) string {
	return fmt.Sprintf(s.policy.RedactionText, detectorID)
}

// RedactionMarker returns a non-reversible replacement marker.
func RedactionMarker(detectorID string) string {
	return fmt.Sprintf("[REDACTED:%s]", detectorID)
}

// ShannonEntropy calculates Shannon entropy per byte for detector candidates.
func ShannonEntropy(value string) float64 {
	if value == "" {
		return 0
	}

	counts := make(map[rune]int)
	for _, r := range value {
		counts[r]++
	}

	var entropy float64
	length := float64(len([]rune(value)))
	for _, count := range counts {
		p := float64(count) / length
		entropy -= p * math.Log2(p)
	}
	return entropy
}

func aggregateAction(findings []finding) Action {
	action := ActionAllow
	for _, f := range findings {
		if f.warning.Action == ActionSkip {
			return ActionSkip
		}
		if f.warning.Action == ActionRedact {
			action = ActionRedact
		}
	}
	return action
}

func hasSkipFinding(findings []finding) bool {
	return aggregateAction(findings) == ActionSkip
}

func warningsFromFindings(findings []finding) []Warning {
	if len(findings) == 0 {
		return nil
	}
	warnings := make([]Warning, len(findings))
	for i, f := range findings {
		warnings[i] = f.warning
	}
	return warnings
}

func overlapsAny(start, end int, findings []finding) bool {
	for _, f := range findings {
		if start < f.end && end > f.start {
			return true
		}
	}
	return false
}

func locate(content []byte, start, end int) Location {
	line, col := 1, 1
	for i := 0; i < start && i < len(content); i++ {
		if content[i] == '\n' {
			line++
			col = 1
			continue
		}
		col++
	}

	endLine, endCol := line, col
	for i := start; i < end && i < len(content); i++ {
		if content[i] == '\n' {
			endLine++
			endCol = 1
			continue
		}
		endCol++
	}

	return Location{
		Line:      line,
		Column:    col,
		EndLine:   endLine,
		EndColumn: endCol,
	}
}

func cloneBytes(content []byte) []byte {
	if content == nil {
		return nil
	}
	cloned := make([]byte, len(content))
	copy(cloned, content)
	return cloned
}
