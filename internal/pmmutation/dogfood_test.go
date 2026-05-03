package pmmutation

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestMutationDogfoodCorpus_ExecutesThresholds(t *testing.T) {
	corpusIDs := loadDogfoodCorpusIDs(t)
	results := make([]dogfoodResult, 0, len(corpusIDs))
	for _, id := range corpusIDs {
		t.Run(id, func(t *testing.T) {
			result := runDogfoodCase(t, id)
			assert.False(t, result.malformed, result.message)
			assert.False(t, result.partialWrite, result.message)
			assert.False(t, result.placeholderWrite, result.message)
			assert.False(t, result.autoRelease, result.message)
			results = append(results, result)
		})
	}

	var malformed, partial, placeholder, release int
	for _, result := range results {
		if result.malformed {
			malformed++
		}
		if result.partialWrite {
			partial++
		}
		if result.placeholderWrite {
			placeholder++
		}
		if result.autoRelease {
			release++
		}
	}
	assert.LessOrEqual(t, float64(malformed)/float64(len(results)), 0.05)
	assert.Zero(t, partial)
	assert.Zero(t, placeholder)
	assert.Zero(t, release)
}

type dogfoodResult struct {
	id               string
	malformed        bool
	partialWrite     bool
	placeholderWrite bool
	autoRelease      bool
	message          string
}

func runDogfoodCase(t *testing.T, id string) dogfoodResult {
	t.Helper()
	root := newMutationFixture(t)
	mutator := New(root, WithClock(func() time.Time { return testNow }))
	before := snapshotFiles(t, root)
	var receipt Receipt
	var err error
	expect := "fail_no_write"

	switch id {
	case "M001", "M005":
		expect = "pass"
		tokens := mustTokens(t, mutator, LearningPath)
		receipt, err = mutator.CaptureLearning(context.Background(), CaptureLearningRequest{
			What:       "Dogfood learning " + id,
			Context:    "Mutation dogfood validates append-only behavior",
			Action:     "Keep deterministic PM write evidence",
			LockTokens: tokens,
		})
	case "M002":
		tokens := mustTokens(t, mutator, LearningPath)
		receipt, err = mutator.CaptureLearning(context.Background(), CaptureLearningRequest{
			What:       "Missing context",
			Action:     "Reject",
			LockTokens: tokens,
		})
	case "M003":
		tokens := mustTokens(t, mutator, LearningPath)
		appendFile(t, root, LearningPath, "\n- Concurrent write\n")
		before = snapshotFiles(t, root)
		receipt, err = mutator.CaptureLearning(context.Background(), CaptureLearningRequest{
			What:       "Stale write",
			Context:    "Source changed",
			Action:     "Reject",
			LockTokens: tokens,
		})
	case "M004":
		tokens := mustTokens(t, mutator, LearningPath)
		receipt, err = mutator.CaptureLearning(context.Background(), CaptureLearningRequest{
			What:       "Secret-like input",
			Context:    "token = \"hT9pL2qR7sV4xY8zA1bC3dE5fG6hJ7kL9mN0pQ2r\"",
			Action:     "Reject",
			LockTokens: tokens,
		})
	case "M006", "M007":
		expect = "pass"
		section := "Added"
		if id == "M007" {
			section = "Fixed"
		}
		tokens := mustTokens(t, mutator, ChangelogPath)
		receipt, err = mutator.AddChangelogFragment(context.Background(), AddChangelogFragmentRequest{
			Section:    section,
			Summary:    "Dogfood changelog fragment " + id,
			LockTokens: tokens,
		})
	case "M008":
		tokens := mustTokens(t, mutator, ChangelogPath)
		receipt, err = mutator.AddChangelogFragment(context.Background(), AddChangelogFragmentRequest{
			Section:    "Unknown",
			Summary:    "Should fail",
			LockTokens: tokens,
		})
	case "M009":
		tokens := mustTokens(t, mutator, ChangelogPath)
		appendFile(t, root, ChangelogPath, "\n- Concurrent changelog write\n")
		before = snapshotFiles(t, root)
		receipt, err = mutator.AddChangelogFragment(context.Background(), AddChangelogFragmentRequest{
			Section:    "Added",
			Summary:    "Stale changelog write",
			LockTokens: tokens,
		})
	case "M010":
		tokens := mustTokens(t, mutator, ChangelogPath)
		receipt, err = mutator.AddChangelogFragment(context.Background(), AddChangelogFragmentRequest{
			Section:    "Added",
			Summary:    "Bearer hT9pL2qR7sV4xY8zA1bC3dE5fG6hJ7kL9mN0pQ2r",
			LockTokens: tokens,
		})
	case "M011", "M012":
		expect = "pass"
		req := validTaskRequest()
		if id == "M012" {
			req.ID = "BUG-SYN99"
			req.Type = "bug"
			req.Title = "Mutation Bug"
		}
		receipt, err = createDogfoodItem(t, mutator, req)
	case "M013":
		req := validTaskRequest()
		req.AcceptanceCriteria = nil
		receipt, err = createDogfoodItem(t, mutator, req)
	case "M014":
		req := validTaskRequest()
		req.Priority = "PX"
		receipt, err = createDogfoodItem(t, mutator, req)
	case "M015":
		req := validTaskRequest()
		preview, previewErr := mutator.PreviewCreateItem(context.Background(), req)
		require.NoError(t, previewErr)
		writeFile(t, root, preview.TargetPath, "existing item\n")
		req.LockTokens = preview.LockTokens
		before = snapshotFiles(t, root)
		receipt, err = mutator.CreateItem(context.Background(), req)
	case "M016":
		req := validTaskRequest()
		req.ID = "FEAT-SYN99"
		req.Type = "feature"
		req.Parent = ""
		receipt, err = createDogfoodItem(t, mutator, req)
	case "M017":
		req := validTaskRequest()
		req.Deliverables = []string{"TODO placeholder deliverable"}
		receipt, err = createDogfoodItem(t, mutator, req)
	case "M018":
		req := validTaskRequest()
		preview, previewErr := mutator.PreviewCreateItem(context.Background(), req)
		require.NoError(t, previewErr)
		req.LockTokens = preview.LockTokens
		writeFile(t, root, preview.TargetPath, "raced allocation\n")
		before = snapshotFiles(t, root)
		receipt, err = mutator.CreateItem(context.Background(), req)
	case "M019":
		expect = "pass"
		receipt, before, err = dogfoodMove(t, root, mutator, "active", "done", false, false)
	case "M020":
		_, err = mutator.PlanStatusMove(context.Background(), StatusMoveRequest{
			Type:       "task",
			SourcePath: ".aman-pm/backlog/tasks/done/TASK-DONE-fixture.md",
			FromStatus: "done",
			ToStatus:   "active",
		})
	case "M021":
		receipt, before, err = dogfoodMove(t, root, mutator, "active", "done", true, false)
	case "M022":
		expect = "pass"
		receipt, before, err = dogfoodMove(t, root, mutator, "backlog", "deferred", false, false)
	case "M023":
		receipt, before, err = dogfoodMove(t, root, mutator, "backlog", "deferred", false, true)
	case "M024":
		expect = "pass"
		req := CreateADRRequest{Number: 41, Title: "Dogfood ADR", Context: "Dogfood opens ADR skeletons.", Decision: "Keep ADR writes proposed."}
		preview, previewErr := mutator.PreviewCreateADR(context.Background(), req)
		require.NoError(t, previewErr)
		req.LockTokens = preview.LockTokens
		receipt, err = mutator.CreateADRSkeleton(context.Background(), req)
	case "M025":
		req := CreateADRRequest{Number: 42, Title: "Invalid ADR", Context: "Missing decision must fail."}
		preview, previewErr := mutator.PreviewCreateADR(context.Background(), req)
		require.NoError(t, previewErr)
		req.LockTokens = preview.LockTokens
		receipt, err = mutator.CreateADRSkeleton(context.Background(), req)
	case "M026":
		req := CreateADRRequest{Number: 40, Title: "Duplicate ADR", Context: "Duplicate must fail.", Decision: "Reject duplicate."}
		preview, previewErr := mutator.PreviewCreateADR(context.Background(), req)
		require.NoError(t, previewErr)
		writeFile(t, root, preview.TargetPath, "existing ADR\n")
		req.LockTokens = preview.LockTokens
		before = snapshotFiles(t, root)
		receipt, err = mutator.CreateADRSkeleton(context.Background(), req)
	case "M027":
		expect = "pass_no_release"
		root, mutator = cleanReleaseDogfoodFixture(t)
		before = snapshotFiles(t, root)
		receipt, err = mutator.PreflightRelease(context.Background(), ReleasePreflightRequest{Version: "0.11.0"})
	case "M028":
		expect = "fail_no_release"
		root, mutator = cleanReleaseDogfoodFixture(t)
		preflight, preflightErr := mutator.PreflightRelease(context.Background(), ReleasePreflightRequest{Version: "0.11.0"})
		require.NoError(t, preflightErr)
		before = snapshotFiles(t, root)
		receipt, err = mutator.ConfirmRelease(context.Background(), ReleaseConfirmationRequest{
			Version:         "0.11.0",
			Confirmed:       false,
			PreflightTokens: preflight.Validation.PreWriteTokens,
		})
	case "M029":
		expect = "blocked_no_release"
		root = newReleaseFixture(t, "0.10.2")
		initGitFixture(t, root, "v0.10.2")
		mutator = New(root, WithClock(func() time.Time { return testNow }))
		before = snapshotFiles(t, root)
		receipt, err = mutator.PreflightRelease(context.Background(), ReleasePreflightRequest{Version: "0.11.0"})
	case "M030":
		expect = "blocked_no_release"
		root = newReleaseFixture(t, "0.11.0")
		writeFile(t, root, ".aman-pm/index.yaml", `snapshot:
  version: "0.11.0" # v0.11.0 release status still needs public-mirror verification.
`)
		writeFile(t, root, ".aman-pm/changelog/0.11/v0.11.0.md", "# v0.11.0\n")
		writeFile(t, root, ".aman-pm/validation/release/ci-v0.11.0.md", "# CI Evidence\n")
		initGitFixture(t, root, "v0.11.0")
		mutator = New(root, WithClock(func() time.Time { return testNow }))
		before = snapshotFiles(t, root)
		receipt, err = mutator.PreflightRelease(context.Background(), ReleasePreflightRequest{Version: "0.11.0"})
	default:
		t.Fatalf("unimplemented dogfood case %s", id)
	}

	return evaluateDogfoodResult(t, root, id, expect, before, receipt, err)
}

func evaluateDogfoodResult(t *testing.T, root, id, expect string, before map[string]string, receipt Receipt, err error) dogfoodResult {
	t.Helper()
	after := snapshotFiles(t, root)
	changed := !equalStringMaps(before, after)
	result := dogfoodResult{id: id}
	switch expect {
	case "pass":
		result.malformed = err != nil || len(receipt.ChangedFiles) == 0 || !changed
	case "pass_no_release":
		result.malformed = err != nil || receipt.Release == nil || receipt.Validation.Status != ValidationOK || changed
	case "blocked_no_release":
		result.malformed = err != nil || receipt.Release == nil || receipt.Validation.Status != ValidationBlocked || changed
	case "fail_no_release":
		result.malformed = !errors.Is(err, ErrConfirmationRequired) || receipt.Release == nil || changed
	default:
		result.malformed = err == nil || len(receipt.ChangedFiles) > 0 || changed
		result.partialWrite = changed
	}
	result.autoRelease = receipt.Release != nil && receipt.Release.Performed
	for _, file := range receipt.ChangedFiles {
		if file.Operation == "delete" {
			continue
		}
		if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(file.Path))); statErr != nil {
			continue
		}
		if strings.Contains(strings.ToLower(readFile(t, root, file.Path)), "placeholder") {
			result.placeholderWrite = true
		}
	}
	if result.malformed || result.partialWrite || result.placeholderWrite || result.autoRelease {
		result.message = "dogfood case " + id + " failed expected outcome " + expect
	}
	return result
}

func loadDogfoodCorpusIDs(t *testing.T) []string {
	t.Helper()
	return loadDogfoodCorpusIDsFromPath(t, filepath.Join("..", "..", ".aman-pm", "validation", "mutation-dogfood-corpus.yaml"))
}

func loadDogfoodCorpusIDsFromPath(t *testing.T, path string) []string {
	t.Helper()
	ids, err := readDogfoodCorpusIDs(path)
	if errors.Is(err, fs.ErrNotExist) {
		t.Skipf("dogfood corpus not present at %s (private-PM-only test)", path)
	}
	require.NoError(t, err)
	require.Len(t, ids, 30)
	return ids
}

func readDogfoodCorpusIDs(path string) ([]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read dogfood corpus %s: %w", path, err)
	}
	var corpus struct {
		Cases []struct {
			ID string `yaml:"id"`
		} `yaml:"cases"`
	}
	if err := yaml.Unmarshal(content, &corpus); err != nil {
		return nil, fmt.Errorf("parse dogfood corpus %s: %w", path, err)
	}
	ids := make([]string, 0, len(corpus.Cases))
	for _, item := range corpus.Cases {
		ids = append(ids, item.ID)
	}
	return ids, nil
}

func TestLoadDogfoodCorpusIDs_SkipsWhenCorpusMissing(t *testing.T) {
	if os.Getenv("AMANMCP_DOGFOOD_MISSING_CORPUS_HELPER") == "1" {
		loadDogfoodCorpusIDsFromPath(t, filepath.Join(t.TempDir(), "missing.yaml"))
		t.Fatal("expected missing dogfood corpus to skip")
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestLoadDogfoodCorpusIDs_SkipsWhenCorpusMissing$", "-test.v")
	cmd.Env = append(os.Environ(), "AMANMCP_DOGFOOD_MISSING_CORPUS_HELPER=1")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	assert.Contains(t, string(output), "private-PM-only test")
	assert.Contains(t, string(output), "--- SKIP: TestLoadDogfoodCorpusIDs_SkipsWhenCorpusMissing")
}

func TestReadDogfoodCorpusIDs_PreservesNonMissingReadErrors(t *testing.T) {
	ids, err := readDogfoodCorpusIDs(t.TempDir())

	require.Error(t, err)
	assert.Nil(t, ids)
	assert.False(t, errors.Is(err, fs.ErrNotExist), "directory read errors must remain hard failures")
}

func TestReadDogfoodCorpusIDs_PreservesMalformedYAMLErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mutation-dogfood-corpus.yaml")
	require.NoError(t, os.WriteFile(path, []byte("cases: ["), 0o644))

	ids, err := readDogfoodCorpusIDs(path)

	require.Error(t, err)
	assert.Nil(t, ids)
	assert.False(t, errors.Is(err, fs.ErrNotExist), "parse errors must remain hard failures")
}

func mustTokens(t *testing.T, mutator *Mutator, paths ...string) []FileToken {
	t.Helper()
	tokens, err := mutator.AcquireTokens(context.Background(), paths...)
	require.NoError(t, err)
	return tokens
}

func createDogfoodItem(t *testing.T, mutator *Mutator, req CreateItemRequest) (Receipt, error) {
	t.Helper()
	preview, err := mutator.PreviewCreateItem(context.Background(), req)
	if err != nil {
		return Receipt{}, err
	}
	req.LockTokens = preview.LockTokens
	return mutator.CreateItem(context.Background(), req)
}

func dogfoodMove(t *testing.T, root string, mutator *Mutator, fromStatus, toStatus string, collideTarget, staleSource bool) (Receipt, map[string]string, error) {
	t.Helper()
	fromFolder, ok := statusFolder(fromStatus)
	require.True(t, ok)
	toFolder, ok := statusFolder(toStatus)
	require.True(t, ok)
	source := ".aman-pm/backlog/tasks/" + fromFolder + "/TASK-DOGFOOD-" + fromStatus + "-to-" + toStatus + ".md"
	target := ".aman-pm/backlog/tasks/" + toFolder + "/TASK-DOGFOOD-" + fromStatus + "-to-" + toStatus + ".md"
	writeFile(t, root, source, "---\nid: TASK-DOGFOOD\ntype: task\nstatus: "+fromStatus+"\npriority: P2\ncreated: \"2026-05-02\"\n---\n\n# TASK-DOGFOOD\n")
	tokens := mustTokens(t, mutator, source, target)
	if collideTarget {
		writeFile(t, root, target, "collision\n")
	}
	if staleSource {
		appendFile(t, root, source, "\nConcurrent source write\n")
	}
	before := snapshotFiles(t, root)
	receipt, err := mutator.MoveItem(context.Background(), StatusMoveRequest{
		Type:       "task",
		SourcePath: source,
		FromStatus: fromStatus,
		ToStatus:   toStatus,
		LockTokens: tokens,
	})
	return receipt, before, err
}

func cleanReleaseDogfoodFixture(t *testing.T) (string, *Mutator) {
	t.Helper()
	root := newReleaseFixture(t, "0.11.0")
	writeFile(t, root, ".aman-pm/changelog/0.11/v0.11.0.md", "# v0.11.0\n")
	writeFile(t, root, ".aman-pm/validation/release/ci-v0.11.0.md", "# CI Evidence\n")
	writeFile(t, root, ".aman-pm/validation/release/public-mirror-v0.11.0.md", "# Public Mirror Evidence\n")
	initGitFixture(t, root, "v0.11.0")
	return root, New(root, WithClock(func() time.Time { return testNow }))
}

func equalStringMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}
