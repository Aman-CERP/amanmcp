package pmmutation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testNow = time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)

func TestCaptureLearning_UnchangedTokenSuccess(t *testing.T) {
	root := newMutationFixture(t)
	mutator := New(root, WithClock(func() time.Time { return testNow }))
	tokens, err := mutator.AcquireTokens(context.Background(), LearningPath)
	require.NoError(t, err)

	receipt, err := mutator.CaptureLearning(context.Background(), CaptureLearningRequest{
		What:       "File-scoped PM mutation tokens can protect append-only writes",
		Context:    "Sprint 14 runs multiple workers against source PM files",
		Action:     "Added core pmmutation tests before implementation",
		LockTokens: tokens,
	})

	require.NoError(t, err)
	require.Len(t, receipt.ChangedFiles, 1)
	assert.Equal(t, LearningPath, receipt.ChangedFiles[0].Path)
	assert.Equal(t, ValidationOK, receipt.Validation.Status)
	assert.Contains(t, receipt.SuggestedCommitMessage, "Authored-By: Niraj Kumar <nirajkvinit@gmail.com>")
	assert.NotEmpty(t, receipt.Validation.PostWriteTokens)

	content := readFile(t, root, LearningPath)
	assert.Contains(t, content, "Existing learning stays first")
	assert.Contains(t, content, "**Learning**: File-scoped PM mutation tokens can protect append-only writes")
	assert.True(t, strings.Index(content, "Existing learning stays first") < strings.Index(content, "File-scoped PM mutation tokens"))
}

func TestCaptureLearning_StaleTokenConflictReturnsNoChangedFiles(t *testing.T) {
	root := newMutationFixture(t)
	mutator := New(root, WithClock(func() time.Time { return testNow }))
	tokens, err := mutator.AcquireTokens(context.Background(), LearningPath)
	require.NoError(t, err)

	appendFile(t, root, LearningPath, "\n- Concurrent worker wrote first\n")
	afterConcurrentWrite := readFile(t, root, LearningPath)

	receipt, err := mutator.CaptureLearning(context.Background(), CaptureLearningRequest{
		What:       "This stale write must not land",
		Context:    "The file changed after preview",
		Action:     "Return a stale-read diagnostic",
		LockTokens: tokens,
	})

	require.ErrorIs(t, err, ErrConflict)
	assert.Empty(t, receipt.ChangedFiles)
	assert.Equal(t, ValidationConflict, receipt.Validation.Status)
	assert.True(t, diagnosticsContainCode(receipt.Diagnostics, "STALE_LOCK_TOKEN"))
	assert.Equal(t, afterConcurrentWrite, readFile(t, root, LearningPath))
}

func TestCaptureLearning_ConcurrentSameTokenAllowsOneWinner(t *testing.T) {
	root := newMutationFixture(t)
	mutator := New(root, WithClock(func() time.Time { return testNow }))
	tokens, err := mutator.AcquireTokens(context.Background(), LearningPath)
	require.NoError(t, err)

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, what := range []string{"first concurrent learning", "second concurrent learning"} {
		wg.Add(1)
		go func(what string) {
			defer wg.Done()
			<-start
			_, writeErr := mutator.CaptureLearning(context.Background(), CaptureLearningRequest{
				What:       what,
				Context:    "Two agents used the same pre-write token",
				Action:     "Only one write may commit",
				LockTokens: tokens,
			})
			errs <- writeErr
		}(what)
	}
	close(start)
	wg.Wait()
	close(errs)

	var successes, conflicts int
	for writeErr := range errs {
		switch {
		case writeErr == nil:
			successes++
		case errors.Is(writeErr, ErrConflict):
			conflicts++
		default:
			t.Fatalf("unexpected error: %v", writeErr)
		}
	}
	assert.Equal(t, 1, successes)
	assert.Equal(t, 1, conflicts)
}

func TestCaptureLearning_MissingTargetReturnsNoChangedFiles(t *testing.T) {
	root := newMutationFixture(t)
	require.NoError(t, os.Remove(filepath.Join(root, filepath.FromSlash(LearningPath))))
	mutator := New(root, WithClock(func() time.Time { return testNow }))
	tokens, err := mutator.AcquireTokens(context.Background(), LearningPath)
	require.NoError(t, err)
	require.False(t, tokens[0].Exists)

	receipt, err := mutator.CaptureLearning(context.Background(), CaptureLearningRequest{
		What:       "Missing target should not be silently created",
		Context:    "Append-only writes require an existing source file",
		Action:     "Fail before writing",
		LockTokens: tokens,
	})

	require.ErrorIs(t, err, ErrNotFound)
	assert.Empty(t, receipt.ChangedFiles)
	assert.Equal(t, ValidationUnavailable, receipt.Validation.Status)
	assert.NoFileExists(t, filepath.Join(root, filepath.FromSlash(LearningPath)))
}

func TestAddChangelogFragment_MissingSectionLeavesFileUnchanged(t *testing.T) {
	root := newMutationFixture(t)
	mutator := New(root, WithClock(func() time.Time { return testNow }))
	tokens, err := mutator.AcquireTokens(context.Background(), ChangelogPath)
	require.NoError(t, err)
	before := readFile(t, root, ChangelogPath)

	receipt, err := mutator.AddChangelogFragment(context.Background(), AddChangelogFragmentRequest{
		Section:    "Security",
		Summary:    "This valid section is absent in the fixture",
		LockTokens: tokens,
	})

	require.ErrorIs(t, err, ErrInvalidInput)
	assert.Empty(t, receipt.ChangedFiles)
	assert.Equal(t, ValidationInvalid, receipt.Validation.Status)
	assert.Equal(t, before, readFile(t, root, ChangelogPath))
}

func TestCaptureLearning_InvalidInputLeavesFileUnchanged(t *testing.T) {
	root := newMutationFixture(t)
	mutator := New(root, WithClock(func() time.Time { return testNow }))
	tokens, err := mutator.AcquireTokens(context.Background(), LearningPath)
	require.NoError(t, err)
	before := readFile(t, root, LearningPath)

	receipt, err := mutator.CaptureLearning(context.Background(), CaptureLearningRequest{
		What:       "Missing context must fail",
		Action:     "Do not write placeholders",
		LockTokens: tokens,
	})

	require.ErrorIs(t, err, ErrInvalidInput)
	assert.Empty(t, receipt.ChangedFiles)
	assert.Equal(t, ValidationInvalid, receipt.Validation.Status)
	assert.Equal(t, before, readFile(t, root, LearningPath))
}

func TestCaptureLearning_SecretLikeInputLeavesFileUnchanged(t *testing.T) {
	root := newMutationFixture(t)
	mutator := New(root, WithClock(func() time.Time { return testNow }))
	tokens, err := mutator.AcquireTokens(context.Background(), LearningPath)
	require.NoError(t, err)
	before := readFile(t, root, LearningPath)

	receipt, err := mutator.CaptureLearning(context.Background(), CaptureLearningRequest{
		What:       "Secret-like input must not be written",
		Context:    "token = \"hT9pL2qR7sV4xY8zA1bC3dE5fG6hJ7kL9mN0pQ2r\"",
		Action:     "Reject before write",
		LockTokens: tokens,
	})

	require.ErrorIs(t, err, ErrInvalidInput)
	assert.Empty(t, receipt.ChangedFiles)
	assert.Equal(t, ValidationInvalid, receipt.Validation.Status)
	assert.True(t, diagnosticsContainCode(receipt.Diagnostics, "SECRET_LIKE_CONTENT"))
	assert.Equal(t, before, readFile(t, root, LearningPath))
}

func TestCreateItem_UsesCallerIDAndRejectsPlaceholderDeliverables(t *testing.T) {
	root := newMutationFixture(t)
	mutator := New(root, WithClock(func() time.Time { return testNow }))
	req := validTaskRequest()

	preview, err := mutator.PreviewCreateItem(context.Background(), req)
	require.NoError(t, err)
	req.LockTokens = preview.LockTokens

	receipt, err := mutator.CreateItem(context.Background(), req)

	require.NoError(t, err)
	assert.Equal(t, []string{"TASK-SYN99"}, receipt.GeneratedIDs)
	require.Len(t, receipt.ChangedFiles, 1)
	assert.Equal(t, ".aman-pm/backlog/tasks/active/TASK-SYN99-mutation-core.md", receipt.ChangedFiles[0].Path)

	created := readFile(t, root, receipt.ChangedFiles[0].Path)
	assert.Contains(t, created, "id: TASK-SYN99")
	assert.Contains(t, created, "parent: FEAT-SYN8")
	assert.Contains(t, created, "- [ ] Proves deterministic caller-supplied IDs")
	assert.NotContains(t, strings.ToLower(created), "placeholder")

	badReq := validTaskRequest()
	badReq.ID = "TASK-SYN100"
	badReq.Deliverables = []string{"TODO placeholder deliverable"}
	badPreview, err := mutator.PreviewCreateItem(context.Background(), badReq)
	require.NoError(t, err)
	badReq.LockTokens = badPreview.LockTokens

	badReceipt, err := mutator.CreateItem(context.Background(), badReq)
	require.ErrorIs(t, err, ErrInvalidInput)
	assert.Empty(t, badReceipt.ChangedFiles)
	assert.NoFileExists(t, filepath.Join(root, ".aman-pm/backlog/tasks/active/TASK-SYN100-mutation-core.md"))
}

func TestCreateItem_RejectsDuplicateIDDifferentTitle(t *testing.T) {
	root := newMutationFixture(t)
	writeFile(t, root, ".aman-pm/backlog/tasks/active/TASK-SYN99-existing-title.md", `---
id: TASK-SYN99
type: task
status: ready
priority: P2
created: "2026-05-02"
---

# TASK-SYN99: Existing Title
`)
	mutator := New(root, WithClock(func() time.Time { return testNow }))
	req := validTaskRequest()
	req.Title = "Different Title"
	preview, err := mutator.PreviewCreateItem(context.Background(), req)
	require.NoError(t, err)
	req.LockTokens = preview.LockTokens

	receipt, err := mutator.CreateItem(context.Background(), req)

	require.ErrorIs(t, err, ErrInvalidInput)
	assert.Empty(t, receipt.ChangedFiles)
	assert.True(t, diagnosticsContainCode(receipt.Diagnostics, "DUPLICATE_ITEM_ID"))
	assert.NoFileExists(t, filepath.Join(root, filepath.FromSlash(preview.TargetPath)))
}

func TestCreateItem_AllowsLegitimateJSONBracesInValidationText(t *testing.T) {
	root := newMutationFixture(t)
	mutator := New(root, WithClock(func() time.Time { return testNow }))
	req := validTaskRequest()
	req.ID = "TASK-SYN101"
	req.Validation = []string{`Go test fixture accepts {"status":"ok"} as documented JSON evidence`}
	preview, err := mutator.PreviewCreateItem(context.Background(), req)
	require.NoError(t, err)
	req.LockTokens = preview.LockTokens

	receipt, err := mutator.CreateItem(context.Background(), req)

	require.NoError(t, err)
	assert.Equal(t, ValidationOK, receipt.Validation.Status)
}

func TestCreateItem_RejectsRulesYAMLStatusMismatch(t *testing.T) {
	root := newMutationFixture(t)
	writeFile(t, root, ".aman-pm/rules.yaml", `item_types:
  task:
    required_fields: [id, type, status, priority, created]
state_machine:
  all_statuses: [active]
  directory_mapping:
    defaults:
      active: active
conventions:
  priorities:
    P2: Medium
`)
	mutator := New(root, WithClock(func() time.Time { return testNow }))
	req := validTaskRequest()
	req.Status = "ready"
	preview, err := mutator.PreviewCreateItem(context.Background(), req)
	require.NoError(t, err)
	req.LockTokens = preview.LockTokens

	receipt, err := mutator.CreateItem(context.Background(), req)

	require.ErrorIs(t, err, ErrInvalidInput)
	assert.Empty(t, receipt.ChangedFiles)
	assert.Contains(t, receipt.Diagnostics[0].Message, "rules.yaml validation failed")
}

func TestCreateADRSkeleton_ProposedSkeletonUsesProvidedNumber(t *testing.T) {
	root := newMutationFixture(t)
	mutator := New(root, WithClock(func() time.Time { return testNow }))
	req := CreateADRRequest{
		Number:   41,
		Title:    "PM Mutation Receipts",
		Context:  "Mutation callers need auditable receipts before MCP handlers are exposed.",
		Decision: "Core mutation helpers return structured receipts for every write attempt.",
	}
	preview, err := mutator.PreviewCreateADR(context.Background(), req)
	require.NoError(t, err)
	req.LockTokens = preview.LockTokens

	receipt, err := mutator.CreateADRSkeleton(context.Background(), req)

	require.NoError(t, err)
	assert.Equal(t, []string{"ADR-041"}, receipt.GeneratedIDs)
	require.Len(t, receipt.ChangedFiles, 1)
	assert.Equal(t, ".aman-pm/decisions/ADR-041-pm-mutation-receipts.md", receipt.ChangedFiles[0].Path)

	created := readFile(t, root, receipt.ChangedFiles[0].Path)
	assert.Contains(t, created, "**Status:** Proposed")
	assert.NotContains(t, created, "**Status:** Accepted")
	assert.Contains(t, created, "**Date:** 2026-05-02")
	assert.Contains(t, created, "## Decision")
}

func TestConfirmRelease_MissingConfirmationNeverReleases(t *testing.T) {
	root := newMutationFixture(t)
	mutator := New(root, WithClock(func() time.Time { return testNow }))
	preflight, err := mutator.PreflightRelease(context.Background(), ReleasePreflightRequest{
		Version: "0.12.0",
	})
	require.NoError(t, err)
	before := snapshotFiles(t, root)

	receipt, err := mutator.ConfirmRelease(context.Background(), ReleaseConfirmationRequest{
		Version:         "0.12.0",
		Confirmed:       false,
		PreflightTokens: preflight.Validation.PreWriteTokens,
	})

	require.ErrorIs(t, err, ErrConfirmationRequired)
	assert.Empty(t, receipt.ChangedFiles)
	assert.False(t, receipt.Release.Performed)
	assert.True(t, diagnosticsContainCode(receipt.Diagnostics, "HUMAN_CONFIRMATION_REQUIRED"))
	assert.Equal(t, before, snapshotFiles(t, root))
}

func TestConfirmRelease_PreflightDriftAbortsNoRelease(t *testing.T) {
	root := newMutationFixture(t)
	mutator := New(root, WithClock(func() time.Time { return testNow }))
	preflight, err := mutator.PreflightRelease(context.Background(), ReleasePreflightRequest{
		Version: "0.12.0",
	})
	require.NoError(t, err)

	appendFile(t, root, ChangelogPath, "\n- Concurrent release note drift\n")
	afterDrift := snapshotFiles(t, root)

	receipt, err := mutator.ConfirmRelease(context.Background(), ReleaseConfirmationRequest{
		Version:         "0.12.0",
		Confirmed:       true,
		PreflightTokens: preflight.Validation.PreWriteTokens,
	})

	require.ErrorIs(t, err, ErrConflict)
	assert.Empty(t, receipt.ChangedFiles)
	assert.False(t, receipt.Release.Performed)
	assert.Equal(t, ValidationConflict, receipt.Validation.Status)
	assert.True(t, diagnosticsContainCode(receipt.Diagnostics, "PREFLIGHT_TOKEN_DRIFT"))
	assert.Equal(t, afterDrift, snapshotFiles(t, root))
}

func TestPlanStatusMove_RejectsIllegalTerminalTransition(t *testing.T) {
	root := newMutationFixture(t)
	mutator := New(root, WithClock(func() time.Time { return testNow }))

	_, err := mutator.PlanStatusMove(context.Background(), StatusMoveRequest{
		Type:       "task",
		SourcePath: ".aman-pm/backlog/tasks/done/TASK-DONE-fixture.md",
		FromStatus: "done",
		ToStatus:   "active",
	})

	require.True(t, errors.Is(err, ErrInvalidInput), "got %v", err)
}

func TestMoveItem_UpdatesStatusAndMovesFileWithDestinationLock(t *testing.T) {
	root := newMutationFixture(t)
	source := ".aman-pm/backlog/tasks/active/TASK-MOVE-fixture.md"
	target := ".aman-pm/backlog/tasks/done/TASK-MOVE-fixture.md"
	writeFile(t, root, source, `---
id: TASK-MOVE
type: task
status: active
priority: P2
created: "2026-05-02"
---

# TASK-MOVE: Fixture
`)
	mutator := New(root, WithClock(func() time.Time { return testNow }))
	tokens, err := mutator.AcquireTokens(context.Background(), source, target)
	require.NoError(t, err)

	receipt, err := mutator.MoveItem(context.Background(), StatusMoveRequest{
		Type:       "task",
		SourcePath: source,
		FromStatus: "active",
		ToStatus:   "done",
		LockTokens: tokens,
	})

	require.NoError(t, err)
	assert.Equal(t, ValidationOK, receipt.Validation.Status)
	require.Len(t, receipt.ChangedFiles, 2)
	assert.Equal(t, source, receipt.ChangedFiles[0].Path)
	assert.Equal(t, "delete", receipt.ChangedFiles[0].Operation)
	assert.Equal(t, target, receipt.ChangedFiles[1].Path)
	assert.Equal(t, "create", receipt.ChangedFiles[1].Operation)
	assert.NoFileExists(t, filepath.Join(root, filepath.FromSlash(source)))
	moved := readFile(t, root, target)
	assert.Contains(t, moved, "status: done")
	assert.NotContains(t, moved, "status: active")
}

func TestMoveItem_DestinationCollisionLeavesSourceUnchanged(t *testing.T) {
	root := newMutationFixture(t)
	source := ".aman-pm/backlog/tasks/active/TASK-MOVE-fixture.md"
	target := ".aman-pm/backlog/tasks/done/TASK-MOVE-fixture.md"
	writeFile(t, root, source, `---
id: TASK-MOVE
type: task
status: active
priority: P2
created: "2026-05-02"
---

# TASK-MOVE: Fixture
`)
	mutator := New(root, WithClock(func() time.Time { return testNow }))
	tokens, err := mutator.AcquireTokens(context.Background(), source, target)
	require.NoError(t, err)
	writeFile(t, root, target, "existing target\n")
	before := readFile(t, root, source)

	receipt, err := mutator.MoveItem(context.Background(), StatusMoveRequest{
		Type:       "task",
		SourcePath: source,
		FromStatus: "active",
		ToStatus:   "done",
		LockTokens: tokens,
	})

	require.ErrorIs(t, err, ErrConflict)
	assert.Empty(t, receipt.ChangedFiles)
	assert.Equal(t, before, readFile(t, root, source))
	assert.Equal(t, "existing target\n", readFile(t, root, target))
}

func TestMoveItem_LegalTransitionMatrixHandlesSameFolderAndCrossFolderMoves(t *testing.T) {
	for fromStatus, toStatuses := range transitions {
		for _, toStatus := range toStatuses {
			t.Run(fromStatus+"_to_"+toStatus, func(t *testing.T) {
				root := newMutationFixture(t)
				fromFolder, ok := statusFolder(fromStatus)
				require.True(t, ok)
				targetFolder, ok := statusFolder(toStatus)
				require.True(t, ok)
				source := fmt.Sprintf(".aman-pm/backlog/tasks/%s/TASK-MATRIX-%s-to-%s.md", fromFolder, fromStatus, toStatus)
				target := fmt.Sprintf(".aman-pm/backlog/tasks/%s/TASK-MATRIX-%s-to-%s.md", targetFolder, fromStatus, toStatus)
				writeFile(t, root, source, fmt.Sprintf(`---
id: TASK-MATRIX
type: task
status: %s
priority: P2
created: "2026-05-02"
---

# TASK-MATRIX: Fixture
`, fromStatus))
				mutator := New(root, WithClock(func() time.Time { return testNow }))
				tokens, err := mutator.AcquireTokens(context.Background(), source, target)
				require.NoError(t, err)

				receipt, err := mutator.MoveItem(context.Background(), StatusMoveRequest{
					Type:       "task",
					SourcePath: source,
					FromStatus: fromStatus,
					ToStatus:   toStatus,
					LockTokens: tokens,
				})

				require.NoError(t, err)
				assert.Equal(t, ValidationOK, receipt.Validation.Status)
				assert.FileExists(t, filepath.Join(root, filepath.FromSlash(target)))
				moved := readFile(t, root, target)
				assert.Contains(t, moved, "status: "+toStatus)
				assert.NotContains(t, moved, "status: "+fromStatus+"\n")
				if source == target {
					require.Len(t, receipt.ChangedFiles, 1)
					assert.Equal(t, "modify", receipt.ChangedFiles[0].Operation)
				} else {
					require.Len(t, receipt.ChangedFiles, 2)
					assert.NoFileExists(t, filepath.Join(root, filepath.FromSlash(source)))
				}
			})
		}
	}
}

func TestReplaceFrontmatterStatus_PreservesIndentedQuotedCRLFStatus(t *testing.T) {
	content := "---\r\n  status: \"active\"\r\npriority: P2\r\n---\r\n\r\n# Fixture\r\n"

	updated, err := replaceFrontmatterStatus(content, "active", "in_progress")

	require.NoError(t, err)
	assert.Contains(t, updated, "---\r\n  status: \"in_progress\"\r\npriority: P2\r\n---\r\n")
	assert.NotContains(t, updated, "status: \"active\"")
}

func TestPreflightRelease_BlocksIndexTagChangelogAndKnownPublicMirrorDrift(t *testing.T) {
	root := newReleaseFixture(t, "0.10.2")
	writeFile(t, root, ".aman-pm/index.yaml", `snapshot:
  version: "0.10.2" # v0.11.0 release status still needs public-mirror verification.
release_notes:
  - "OPEN FOLLOW-UP: verify public mirror v0.11.0 release/tag artifacts before release planning"
`)
	initGitFixture(t, root, "v0.10.2")
	mutator := New(root, WithClock(func() time.Time { return testNow }))

	receipt, err := mutator.PreflightRelease(context.Background(), ReleasePreflightRequest{
		Version: "0.11.0",
	})

	require.NoError(t, err)
	require.NotNil(t, receipt.Release)
	assert.Equal(t, ValidationBlocked, receipt.Validation.Status)
	assert.Contains(t, strings.Join(receipt.Release.Blockers, "\n"), "index version mismatch: index=0.10.2 requested=0.11.0")
	assert.Contains(t, strings.Join(receipt.Release.Blockers, "\n"), "VERSION mismatch: file=0.10.2 requested=0.11.0")
	assert.Contains(t, strings.Join(receipt.Release.Blockers, "\n"), "local tag missing: requested=v0.11.0 latest=v0.10.2")
	assert.Contains(t, strings.Join(receipt.Release.Blockers, "\n"), "missing evidence: .aman-pm/changelog/0.11/v0.11.0.md")
	assert.Contains(t, strings.Join(receipt.Release.Blockers, "\n"), "known release gap remains unresolved: v0.11.0 public mirror verification")
	assert.True(t, diagnosticsContainCode(receipt.Diagnostics, "RELEASE_INDEX_VERSION_MISMATCH"))
	assert.True(t, diagnosticsContainCode(receipt.Diagnostics, "RELEASE_TAG_MISSING"))
	assert.True(t, diagnosticsContainCode(receipt.Diagnostics, "KNOWN_RELEASE_GAP"))
}

func TestPreflightRelease_PassesWhenReleaseEvidenceIsConsistent(t *testing.T) {
	root := newReleaseFixture(t, "0.11.0")
	writeFile(t, root, ".aman-pm/changelog/0.11/v0.11.0.md", "# v0.11.0\n\n- Release evidence\n")
	writeFile(t, root, ".aman-pm/validation/release/ci-v0.11.0.md", "# CI Evidence\n\nmake ci-check passed\n")
	writeFile(t, root, ".aman-pm/validation/release/public-mirror-v0.11.0.md", "# Public Mirror Evidence\n\nFinal release verified\n")
	initGitFixture(t, root, "v0.11.0")
	mutator := New(root, WithClock(func() time.Time { return testNow }))

	receipt, err := mutator.PreflightRelease(context.Background(), ReleasePreflightRequest{
		Version: "v0.11.0",
	})

	require.NoError(t, err)
	require.NotNil(t, receipt.Release)
	assert.Equal(t, ValidationOK, receipt.Validation.Status)
	assert.Empty(t, receipt.Release.Blockers)
	assert.Contains(t, receipt.Release.EvidencePaths, ".aman-pm/changelog/0.11/v0.11.0.md")
	assert.Contains(t, receipt.Release.EvidencePaths, ".aman-pm/validation/release/public-mirror-v0.11.0.md")
}

func validTaskRequest() CreateItemRequest {
	return CreateItemRequest{
		ID:                 "TASK-SYN99",
		Type:               "task",
		Status:             "ready",
		Priority:           "P2",
		Parent:             "FEAT-SYN8",
		Title:              "Mutation Core",
		Context:            "Sprint 14 needs PM mutation core helpers before MCP handlers.",
		AcceptanceCriteria: []string{"Proves deterministic caller-supplied IDs"},
		Deliverables:       []string{"Core pmmutation package and tests"},
		Validation:         []string{"go test ./internal/pmmutation passes"},
		CreatedBy:          "ai",
	}
}

func newMutationFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, LearningPath, "# Learnings\n\n- **Learning**: Existing learning stays first / **Context**: fixture / **Action**: keep\n")
	writeFile(t, root, ChangelogPath, "# Unreleased Changes\n\n## Added\n\n- Existing Added entry\n\n## Fixed\n\n- Existing Fixed entry\n")
	writeFile(t, root, ".aman-pm/index.yaml", "version: 0.11.0\n")
	writeFile(t, root, "VERSION", "0.11.0\n")
	writeFile(t, root, ".aman-pm/backlog/tasks/done/TASK-DONE-fixture.md", "---\nid: TASK-DONE\ntype: task\nstatus: done\npriority: P2\ncreated: \"2026-05-02\"\n---\n\n# TASK-DONE: Fixture\n")
	return root
}

func newReleaseFixture(t *testing.T, version string) string {
	t.Helper()
	root := newMutationFixture(t)
	writeFile(t, root, ".aman-pm/index.yaml", fmt.Sprintf("snapshot:\n  version: %q\n", version))
	writeFile(t, root, "VERSION", version+"\n")
	return root
}

func initGitFixture(t *testing.T, root string, tags ...string) {
	t.Helper()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "AmanMCP Test")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "test fixture")
	for _, tag := range tags {
		runGit(t, root, "tag", tag)
	}
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v failed:\n%s", args, string(output))
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func appendFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	defer func() { require.NoError(t, file.Close()) }()
	_, err = file.WriteString(content)
	require.NoError(t, err)
}

func readFile(t *testing.T, root, rel string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	require.NoError(t, err)
	return string(content)
}

func snapshotFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	require.NoError(t, filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		require.NoError(t, err)
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		require.NoError(t, err)
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		files[filepath.ToSlash(rel)] = string(content)
		return nil
	}))
	return files
}

func diagnosticsContainCode(diagnostics []Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}
