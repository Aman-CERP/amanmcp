package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/pmresource"
)

func TestServer_RegisterPMResources_ExposesFrozenURIs(t *testing.T) {
	root := writePMResourceMCPFixture(t)
	srv, err := NewServer(&MockSearchEngine{}, &MockMetadataStore{}, &MockEmbedder{}, config.NewConfig(), root)
	require.NoError(t, err)
	require.NoError(t, srv.RegisterPMResources())

	ctx := context.Background()
	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()
	serverSession, err := srv.MCPServer().Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	defer serverSession.Close()

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "pm-resource-test-client", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	defer clientSession.Close()

	list, err := clientSession.ListResources(ctx, nil)
	require.NoError(t, err)
	seen := make(map[string]bool)
	for _, resource := range list.Resources {
		seen[resource.URI] = true
	}
	for _, uri := range pmresource.ResourceURIs() {
		assert.True(t, seen[uri], "missing registered PM resource %s", uri)
	}

	result, err := clientSession.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: pmresource.URIBacklogOpen})
	require.NoError(t, err)
	require.Len(t, result.Contents, 1)
	assert.Equal(t, pmresource.URIBacklogOpen, result.Contents[0].URI)
	assert.Equal(t, "application/json", result.Contents[0].MIMEType)

	var envelope struct {
		SchemaVersion string `json:"schema_version"`
		ResourceURI   string `json:"resource_uri"`
		Validation    struct {
			Status string `json:"status"`
		} `json:"validation"`
	}
	require.NoError(t, json.Unmarshal([]byte(result.Contents[0].Text), &envelope))
	assert.Equal(t, pmresource.SchemaVersion, envelope.SchemaVersion)
	assert.Equal(t, pmresource.URIBacklogOpen, envelope.ResourceURI)
	assert.Equal(t, string(pmresource.ValidationOK), envelope.Validation.Status)
}

func TestServer_RegisterPMResources_UnknownURIIsNotFound(t *testing.T) {
	root := writePMResourceMCPFixture(t)
	srv, err := NewServer(&MockSearchEngine{}, &MockMetadataStore{}, &MockEmbedder{}, config.NewConfig(), root)
	require.NoError(t, err)
	require.NoError(t, srv.RegisterPMResources())

	ctx := context.Background()
	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()
	serverSession, err := srv.MCPServer().Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	defer serverSession.Close()

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "pm-resource-test-client", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	defer clientSession.Close()

	_, err = clientSession.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: "pm://unknown/resource"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Resource not found")
}

func writePMResourceMCPFixture(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	writeMCPFixtureFile(t, root, ".aman-pm/rules.yaml", `version: "1.0"
state_machine:
  all_statuses: [ready, done, resolved, cancelled]
  categories:
    planning: [ready]
    terminal: [done, resolved, cancelled]
  directory_mapping:
    defaults:
      ready: active
      done: done
      resolved: resolved
      cancelled: cancelled
item_types:
  task:
    required_fields: [id, type, status, priority, created]
conventions:
  backlog_root: ".aman-pm/backlog"
  ac_section_heading: "## Acceptance Criteria"
`)
	writeMCPFixtureFile(t, root, ".aman-pm/backlog/tasks/active/TASK-001-fixture.md", `---
id: TASK-001
type: task
status: ready
priority: P1
created: "2026-05-02"
---

# TASK-LEGACY: Fixture task

## Acceptance Criteria

- [ ] Exercise PM resource fixture
`)
	writeMCPFixtureFile(t, root, ".aman-pm/backlog/index.yaml", `items:
  - id: "TASK-001"
    type: "task"
    title: "Fixture task"
    status: "ready"
    priority: "P1"
    file: "tasks/active/TASK-001-fixture.md"
`)
	writeMCPFixtureFile(t, root, ".aman-pm/index.yaml", `snapshot:
  generated: "2026-05-02T10:00:00Z"
metrics:
  generated_at: "2026-05-02T10:00:00Z"
  total_items: 1
`)
	writeMCPFixtureFile(t, root, ".aman-pm/sprints/active/13/items.yaml", "sprint: 13\nitems: []\n")
	writeMCPFixtureFile(t, root, ".aman-pm/decisions/ADR-001-fixture.md", "# Fixture Decision\n\n**Status:** Accepted\n")
	writeMCPFixtureFile(t, root, ".aman-pm/changelog/unreleased.md", "# Unreleased Changes\n\n## Added\n\n- PM resources fixture\n")
	writeMCPFixtureFile(t, root, ".amanmcp/amanpm-read-model.sqlite", "fixture read model\n")

	fresh := time.Date(2026, 5, 2, 11, 0, 0, 0, time.UTC)
	require.NoError(t, filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		return os.Chtimes(path, fresh, fresh)
	}))

	return root
}

func writeMCPFixtureFile(t *testing.T, root, relPath, content string) {
	t.Helper()
	path := filepath.Join(root, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}
