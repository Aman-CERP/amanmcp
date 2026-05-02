package secrets_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Aman-CERP/amanmcp/internal/secrets"
)

// Test fixtures use AWS-published canonical example values. They match the
// scanner's regex (so detection coverage is preserved) but are recognized by
// GitHub Push Protection as known-fake, so syncing this file to public
// repositories does not trigger secret-scanning blocks.
//   - https://docs.aws.amazon.com/IAM/latest/UserGuide/security-creds.html
const highEntropyToken = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"

func TestScanner_ScanContent_DetectsHighConfidencePatterns(t *testing.T) {
	scanner := secrets.NewScanner(secrets.DefaultPolicy())
	content := []byte(`package main

const githubToken = "ghp_1234567890abcdefghijklmnopqrstuvwxyzABCD"
const awsKey = "AKIAIOSFODNN7EXAMPLE"
`)

	result := scanner.ScanContent(secrets.ContentInput{
		Path:    "internal/service/config.go",
		Content: content,
		Source:  secrets.SourceIndex,
	})

	if len(result.Warnings) != 2 {
		t.Fatalf("warning count = %d, want 2: %#v", len(result.Warnings), result.Warnings)
	}

	detectors := map[string]bool{}
	for _, warning := range result.Warnings {
		if warning.FilePath != "internal/service/config.go" {
			t.Fatalf("warning file path = %q", warning.FilePath)
		}
		if warning.Confidence != secrets.ConfidenceHigh {
			t.Fatalf("warning confidence = %q, want high", warning.Confidence)
		}
		detectors[warning.DetectorID] = true
	}

	if !detectors["github-token"] {
		t.Fatalf("github-token detector missing: %#v", detectors)
	}
	if !detectors["aws-access-key-id"] {
		t.Fatalf("aws-access-key-id detector missing: %#v", detectors)
	}
}

func TestScanner_ScanContent_EntropyThresholdRejectsLowEntropyAssignment(t *testing.T) {
	scanner := secrets.NewScanner(secrets.DefaultPolicy())
	content := []byte(`api_key = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`)

	result := scanner.ScanContent(secrets.ContentInput{
		Path:    "config/settings.py",
		Content: content,
		Source:  secrets.SourceIndex,
	})

	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %#v, want none for low entropy placeholder", result.Warnings)
	}
}

func TestScanner_ScanContent_AllowlistSuppressesKnownFalsePositive(t *testing.T) {
	policy := secrets.DefaultPolicy()
	policy.Allowlist.Values = []string{"doc-only-token-" + highEntropyToken}
	scanner := secrets.NewScanner(policy)

	result := scanner.ScanContent(secrets.ContentInput{
		Path:    "docs/setup.md",
		Content: []byte(`token = "doc-only-token-` + highEntropyToken + `"`),
		Source:  secrets.SourceIndex,
	})

	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %#v, want none for allowlisted token", result.Warnings)
	}
}

func TestScanner_ScanContent_DefaultAllowlistDoesNotSuppressByPath(t *testing.T) {
	scanner := secrets.NewScanner(secrets.DefaultPolicy())

	result := scanner.ScanContent(secrets.ContentInput{
		Path:    "docs/example/config.go",
		Content: []byte(`token = "` + highEntropyToken + `"`),
		Source:  secrets.SourceIndex,
	})

	if len(result.Warnings) != 1 {
		t.Fatalf("warnings = %#v, want one warning; default value allowlist must not trust path names", result.Warnings)
	}
}

func TestScanner_ScanContent_ExplicitPathAllowlistSuppressesByPath(t *testing.T) {
	policy := secrets.DefaultPolicy()
	policy.Allowlist.PathPatterns = []string{`^docs/example/`}
	scanner := secrets.NewScanner(policy)

	result := scanner.ScanContent(secrets.ContentInput{
		Path:    "docs/example/config.go",
		Content: []byte(`token = "` + highEntropyToken + `"`),
		Source:  secrets.SourceIndex,
	})

	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %#v, want none for explicit path allowlist", result.Warnings)
	}
}

func TestScanner_GuardContent_RedactsTokenWithoutLeakingWarning(t *testing.T) {
	rawSecret := "tok_" + highEntropyToken
	scanner := secrets.NewScanner(secrets.DefaultPolicy())

	result := scanner.GuardContent(secrets.ContentInput{
		Path:    ".aman-pm/knowledge/learnings.md",
		Content: []byte("learning token = \"" + rawSecret + "\"\n"),
		Source:  secrets.SourcePMCaptureLearning,
	})

	if result.Blocked {
		t.Fatalf("result blocked = true, want redacted content")
	}
	if result.Action != secrets.ActionRedact {
		t.Fatalf("action = %q, want redact", result.Action)
	}
	if strings.Contains(string(result.Content), rawSecret) {
		t.Fatalf("redacted content leaked raw secret")
	}
	if !strings.Contains(string(result.Content), "[REDACTED:generic-secret-assignment]") {
		t.Fatalf("redacted content missing detector marker: %q", result.Content)
	}

	encoded, err := json.Marshal(result.Warnings)
	if err != nil {
		t.Fatalf("marshal warnings: %v", err)
	}
	for _, payload := range []string{string(encoded), result.Warnings[0].Error()} {
		if strings.Contains(payload, rawSecret) {
			t.Fatalf("warning payload leaked raw secret: %s", payload)
		}
		if strings.Contains(payload, highEntropyToken[:8]) || strings.Contains(payload, highEntropyToken[len(highEntropyToken)-8:]) {
			t.Fatalf("warning payload leaked reversible secret substring: %s", payload)
		}
	}
}

func TestScanner_GuardContent_SkipsPrivateKey(t *testing.T) {
	scanner := secrets.NewScanner(secrets.DefaultPolicy())
	begin := "-----BEGIN " + "PRIVATE KEY-----"
	end := "-----END " + "PRIVATE KEY-----"

	result := scanner.GuardContent(secrets.ContentInput{
		Path: "internal/service/key_fixture.go",
		Content: []byte(`package service

const key = ` + "`" + begin + `
MIIEvQIBADANBgkqhkiG9w0BAQEFAASC
` + end + "`" + `
`),
		Source: secrets.SourceIndex,
	})

	if !result.Blocked {
		t.Fatalf("blocked = false, want private key content skipped")
	}
	if result.Action != secrets.ActionSkip {
		t.Fatalf("action = %q, want skip", result.Action)
	}
	if len(result.Content) != 0 {
		t.Fatalf("content length = %d, want zero for skipped content", len(result.Content))
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("warnings = %d, want 1", len(result.Warnings))
	}
	if result.Warnings[0].DetectorID != "private-key-block" {
		t.Fatalf("detector = %q, want private-key-block", result.Warnings[0].DetectorID)
	}
}

func TestScanner_ScanMutationLikeContent_UsesSameAPI(t *testing.T) {
	scanner := secrets.NewScanner(secrets.DefaultPolicy())
	cases := []struct {
		name   string
		path   string
		source secrets.ContentSource
	}{
		{name: "capture learning", path: ".aman-pm/knowledge/learnings.md", source: secrets.SourcePMCaptureLearning},
		{name: "changelog fragment", path: ".aman-pm/changelog/unreleased.md", source: secrets.SourcePMAddChangelogFragment},
		{name: "file item", path: ".aman-pm/backlog/tasks/active/TASK-NEW.md", source: secrets.SourcePMFileItem},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			result := scanner.GuardContent(secrets.ContentInput{
				Path:    tt.path,
				Content: []byte("token: \"" + highEntropyToken + "\"\n"),
				Source:  tt.source,
			})

			if len(result.Warnings) != 1 {
				t.Fatalf("warnings = %d, want 1", len(result.Warnings))
			}
			if result.Warnings[0].FilePath != tt.path {
				t.Fatalf("file path = %q, want %q", result.Warnings[0].FilePath, tt.path)
			}
			if strings.Contains(string(result.Content), highEntropyToken) {
				t.Fatalf("mutation-like content leaked secret")
			}
		})
	}
}
