package pmresource

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var fixedTime = time.Date(2026, 5, 2, 10, 30, 0, 0, time.UTC)

func TestReader_Read_AllResourcesUseSharedEnvelope(t *testing.T) {
	root := newPMFixture(t, pmFixtureOptions{})
	reader := NewReader(root, WithClock(func() time.Time { return fixedTime }))

	for _, uri := range ResourceURIs() {
		t.Run(uri, func(t *testing.T) {
			env, err := reader.Read(context.Background(), uri)

			require.NoError(t, err)
			require.NotNil(t, env)
			assert.Equal(t, SchemaVersion, env.SchemaVersion)
			assert.Equal(t, uri, env.ResourceURI)
			assert.Equal(t, fixedTime.Format(time.RFC3339), env.GeneratedAt)
			assert.Equal(t, ValidationOK, env.Validation.Status)
			assert.True(t, env.Authority.Authoritative)
			assert.NotEmpty(t, env.SourcePaths)
			assert.True(t, env.Derivation.ReadOnly)
			assert.NotNil(t, env.Data)
		})
	}
}

func TestReader_Read_InvalidPMStateIsVisible(t *testing.T) {
	root := newPMFixture(t, pmFixtureOptions{InvalidStatus: true})
	reader := NewReader(root, WithClock(func() time.Time { return fixedTime }))

	env, err := reader.Read(context.Background(), URIComplyState)

	require.NoError(t, err)
	assert.Equal(t, ValidationInvalid, env.Validation.Status)
	assert.False(t, env.Authority.Authoritative)
	assert.NotEmpty(t, env.Diagnostics)

	data := envelopeData(t, env)
	assert.EqualValues(t, 1, data["violation_count"])
	assert.EqualValues(t, 1, data["blocking_violation_count"])
}

func TestReader_Read_StaleReadModelDegradesCounters(t *testing.T) {
	root := newPMFixture(t, pmFixtureOptions{})
	readModel := filepath.Join(root, ".amanmcp", "amanpm-read-model.sqlite")
	oldTime := fixedTime.Add(-24 * time.Hour)
	require.NoError(t, os.Chtimes(readModel, oldTime, oldTime))

	reader := NewReader(root, WithClock(func() time.Time { return fixedTime }))
	env, err := reader.Read(context.Background(), URISubstrateCounters)

	require.NoError(t, err)
	assert.Equal(t, ValidationStale, env.Validation.Status)
	assert.False(t, env.Authority.Authoritative)
	assert.True(t, diagnosticsContain(env.Diagnostics, "READ_MODEL_STALE"))
}

func TestReader_Read_UnavailableWhenPMRootIsMissing(t *testing.T) {
	reader := NewReader(t.TempDir(), WithClock(func() time.Time { return fixedTime }))

	env, err := reader.Read(context.Background(), URIBacklogOpen)

	require.NoError(t, err)
	assert.Equal(t, ValidationUnavailable, env.Validation.Status)
	assert.False(t, env.Authority.Authoritative)
	assert.True(t, diagnosticsContain(env.Diagnostics, "PM_ROOT_UNAVAILABLE"))
}

func TestReader_Read_EmptyBacklogIsDistinctFromUnavailable(t *testing.T) {
	root := newPMFixture(t, pmFixtureOptions{EmptyBacklog: true})
	reader := NewReader(root, WithClock(func() time.Time { return fixedTime }))

	env, err := reader.Read(context.Background(), URIBacklogOpen)

	require.NoError(t, err)
	assert.Equal(t, ValidationOK, env.Validation.Status)
	assert.True(t, env.Authority.Authoritative)

	data := envelopeData(t, env)
	assert.EqualValues(t, 0, data["count"])
	assert.Equal(t, true, data["empty"])
}

func TestReader_Read_IsReadOnly(t *testing.T) {
	root := newPMFixture(t, pmFixtureOptions{})
	before := fileModTimes(t, root)
	reader := NewReader(root, WithClock(func() time.Time { return fixedTime }))

	for _, uri := range ResourceURIs() {
		_, err := reader.Read(context.Background(), uri)
		require.NoError(t, err, uri)
	}

	assert.Equal(t, before, fileModTimes(t, root))
}

func TestReader_Read_ReusesFreshCacheWithinTTL(t *testing.T) {
	root := newPMFixture(t, pmFixtureOptions{})
	now := fixedTime
	reader := NewReader(root, WithClock(func() time.Time { return now }))

	env, err := reader.Read(context.Background(), URIBacklogOpen)
	require.NoError(t, err)
	assert.EqualValues(t, 1, envelopeData(t, env)["count"])

	writeFile(t, root, ".aman-pm/backlog/tasks/active/TASK-002-fixture.md", `---
id: TASK-002
type: task
status: ready
priority: P1
parent: FEAT-001
sprint: 13
created: "2026-05-02"
---

# TASK-002: Second fixture task

## Acceptance Criteria

- [ ] Exercise PM resource cache invalidation
`)
	writeFile(t, root, ".aman-pm/backlog/index.yaml", `items:
  - id: "TASK-001"
    type: "task"
    title: "Fixture task"
    status: "ready"
    priority: "P1"
    file: "tasks/active/TASK-001-fixture.md"
  - id: "TASK-002"
    type: "task"
    title: "Second fixture task"
    status: "ready"
    priority: "P1"
    file: "tasks/active/TASK-002-fixture.md"
`)

	env, err = reader.Read(context.Background(), URIBacklogOpen)
	require.NoError(t, err)
	assert.EqualValues(t, 1, envelopeData(t, env)["count"], "fresh cache should hide source mutations inside the TTL")

	now = fixedTime.Add(3 * time.Second)
	env, err = reader.Read(context.Background(), URIBacklogOpen)
	require.NoError(t, err)
	assert.EqualValues(t, 2, envelopeData(t, env)["count"], "expired cache should refresh source files")
}

func envelopeData(t *testing.T, env *Envelope) map[string]any {
	t.Helper()
	payload, err := json.Marshal(env.Data)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(payload, &out))
	return out
}

func diagnosticsContain(diagnostics []Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

type pmFixtureOptions struct {
	EmptyBacklog  bool
	InvalidStatus bool
}

func newPMFixture(t *testing.T, opts pmFixtureOptions) string {
	t.Helper()

	root := t.TempDir()
	writeFile(t, root, ".aman-pm/rules.yaml", minimalRulesYAML)
	writeFile(t, root, ".aman-pm/counters.yaml", "version: \"1.0\"\n")
	writeFile(t, root, ".aman-pm/index.yaml", `snapshot:
  generated: "2026-05-02T10:00:00Z"
metrics:
  generated_at: "2026-05-02T10:00:00Z"
  total_items: 1
  by_type:
    task: 1
  by_status:
    ready: 1
  by_priority:
    P1: 1
  active_breakdown:
    tasks_active: 1
  by_sprint:
    sprint_13:
      status: active
`)
	writeFile(t, root, ".aman-pm/sprints/active/13/items.yaml", `sprint: 13
goal: "PM resources"
started: "2026-05-02"
items:
  - id: "TASK-001"
    title: "Fixture task"
    type: task
    status: ready
    priority: P1
`)
	writeFile(t, root, ".aman-pm/decisions/ADR-001-fixture.md", `# Fixture Decision

**Status:** Accepted
**Supersedes:** None
**Superseded by:** None
`)
	writeFile(t, root, ".aman-pm/changelog/unreleased.md", `# Unreleased Changes

## Added

- **PM resources**: Add read-only resources for fixture coverage
`)

	indexItems := "items: []\n"
	if !opts.EmptyBacklog {
		status := "ready"
		if opts.InvalidStatus {
			status = "mystery"
		}
		writeFile(t, root, ".aman-pm/backlog/tasks/active/TASK-001-fixture.md", `---
id: TASK-001
type: task
status: `+status+`
priority: P1
parent: FEAT-001
sprint: 13
created: "2026-05-02"
---

# TASK-LEGACY: Fixture task

## Acceptance Criteria

- [ ] Exercise PM resource fixture
`)
		indexItems = `items:
  - id: "TASK-001"
    type: "task"
    title: "Fixture task"
    status: "` + status + `"
    priority: "P1"
    file: "tasks/active/TASK-001-fixture.md"
`
	} else {
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".aman-pm/backlog/tasks/active"), 0o755))
	}
	writeFile(t, root, ".aman-pm/backlog/index.yaml", indexItems)
	writeFile(t, root, ".amanmcp/amanpm-read-model.sqlite", "fixture read model\n")

	fresh := fixedTime.Add(1 * time.Hour)
	require.NoError(t, filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		return os.Chtimes(path, fresh, fresh)
	}))

	return root
}

func writeFile(t *testing.T, root, relPath, content string) {
	t.Helper()
	path := filepath.Join(root, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func fileModTimes(t *testing.T, root string) map[string]time.Time {
	t.Helper()
	out := make(map[string]time.Time)
	require.NoError(t, filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		out[rel] = info.ModTime()
		return nil
	}))
	return out
}

const minimalRulesYAML = `version: "1.0"
state_machine:
  all_statuses: [backlog, ready, active, in_progress, blocked, review, deferred, done, resolved, validated, cancelled]
  categories:
    planning: [backlog, ready]
    execution: [active, in_progress, blocked, review]
    deferred: [deferred]
    terminal: [done, resolved, validated, cancelled]
  directory_mapping:
    defaults:
      backlog: active
      ready: active
      active: active
      in_progress: active
      blocked: active
      review: active
      deferred: deferred
      done: done
      resolved: resolved
      validated: done
      cancelled: cancelled
item_types:
  task:
    required_fields: [id, type, status, priority, created]
    requires_acceptance_criteria: true
  feature:
    required_fields: [id, type, status, priority, created]
    requires_acceptance_criteria: true
conventions:
  backlog_root: ".aman-pm/backlog"
  ac_section_heading: "## Acceptance Criteria"
`
