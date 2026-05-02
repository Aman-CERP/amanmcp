package index

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Aman-CERP/amanmcp/internal/config"
)

const runnerSecretValue = "tok_hT9pL2qR7sV4xY8zA1bC3dE5fG6hJ7kL9mN0pQ2r"

func TestRunner_RedactsSecretsBeforeChunkEmbeddingAndBM25(t *testing.T) {
	tmpDir := t.TempDir()
	requireWriteFile(t, filepath.Join(tmpDir, "config.go"), `package main

const accessToken = "`+runnerSecretValue+`"
`)

	renderer := &MockRenderer{}
	metadata := &MockMetadataStore{}
	bm25 := &MockBM25Index{}
	embedder := &MockEmbedder{}

	runner, err := NewRunner(RunnerDependencies{
		Renderer:        renderer,
		Config:          config.NewConfig(),
		Metadata:        metadata,
		BM25:            bm25,
		Vector:          &MockVectorStore{},
		Embedder:        embedder,
		CodeChunker:     &MockChunker{},
		MarkdownChunker: &MockChunker{},
	})
	if err != nil {
		t.Fatalf("NewRunner() error: %v", err)
	}

	result, err := runner.Run(context.Background(), RunnerConfig{
		RootDir: tmpDir,
		DataDir: filepath.Join(tmpDir, ".amanmcp"),
	})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	if result.Warnings != 1 {
		t.Fatalf("warnings = %d, want 1", result.Warnings)
	}
	if len(metadata.ChunksSaved) != 1 {
		t.Fatalf("chunks saved = %d, want 1", len(metadata.ChunksSaved))
	}

	payloads := []string{
		metadata.ChunksSaved[0].Content,
		strings.Join(embedder.BatchTexts, "\n"),
		bm25.Documents[0].Content,
		renderer.ErrorEvents[0].Err.Error(),
	}
	for _, payload := range payloads {
		if strings.Contains(payload, runnerSecretValue) {
			t.Fatalf("raw secret leaked into indexed payload: %q", payload)
		}
	}
	if !strings.Contains(metadata.ChunksSaved[0].Content, "[REDACTED:generic-secret-assignment]") {
		t.Fatalf("chunk content was not redacted: %q", metadata.ChunksSaved[0].Content)
	}
	if metadata.ChunksSaved[0].Metadata["secret_scan_action"] != "redact" {
		t.Fatalf("secret_scan_action metadata = %q, want redact", metadata.ChunksSaved[0].Metadata["secret_scan_action"])
	}
}

func TestRunner_SkipsPrivateKeyBeforeChunking(t *testing.T) {
	tmpDir := t.TempDir()
	begin := "-----BEGIN " + "PRIVATE KEY-----"
	end := "-----END " + "PRIVATE KEY-----"
	requireWriteFile(t, filepath.Join(tmpDir, "config.go"), `package main

const privateKey = `+"`"+begin+`
MIIEvQIBADANBgkqhkiG9w0BAQEFAASC
`+end+"`"+`
`)

	renderer := &MockRenderer{}
	metadata := &MockMetadataStore{}
	embedder := &MockEmbedder{}
	bm25 := &MockBM25Index{}

	runner, err := NewRunner(RunnerDependencies{
		Renderer:        renderer,
		Config:          config.NewConfig(),
		Metadata:        metadata,
		BM25:            bm25,
		Vector:          &MockVectorStore{},
		Embedder:        embedder,
		CodeChunker:     &MockChunker{},
		MarkdownChunker: &MockChunker{},
	})
	if err != nil {
		t.Fatalf("NewRunner() error: %v", err)
	}

	result, err := runner.Run(context.Background(), RunnerConfig{
		RootDir: tmpDir,
		DataDir: filepath.Join(tmpDir, ".amanmcp"),
	})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	if result.Chunks != 0 {
		t.Fatalf("chunks = %d, want 0", result.Chunks)
	}
	if result.Warnings != 1 {
		t.Fatalf("warnings = %d, want 1", result.Warnings)
	}
	if metadata.SaveChunksCalled {
		t.Fatalf("SaveChunks called for skipped private-key file")
	}
	if embedder.EmbedBatchCalled {
		t.Fatalf("EmbedBatch called for skipped private-key file")
	}
	if bm25.IndexCalled {
		t.Fatalf("BM25 Index called for skipped private-key file")
	}
	if strings.Contains(renderer.ErrorEvents[0].Err.Error(), "MIIEvQIB") {
		t.Fatalf("warning leaked private key content: %s", renderer.ErrorEvents[0].Err.Error())
	}
}

func TestRunner_DoesNotSecretScanGitignoredFiles(t *testing.T) {
	tmpDir := t.TempDir()
	requireWriteFile(t, filepath.Join(tmpDir, ".gitignore"), "ignored.go\n")
	requireWriteFile(t, filepath.Join(tmpDir, "ignored.go"), `package main

const accessToken = "`+runnerSecretValue+`"
`)
	requireWriteFile(t, filepath.Join(tmpDir, "visible.go"), "package main\nfunc main() {}\n")

	renderer := &MockRenderer{}
	metadata := &MockMetadataStore{}
	embedder := &MockEmbedder{}

	runner, err := NewRunner(RunnerDependencies{
		Renderer:        renderer,
		Config:          config.NewConfig(),
		Metadata:        metadata,
		BM25:            &MockBM25Index{},
		Vector:          &MockVectorStore{},
		Embedder:        embedder,
		CodeChunker:     &MockChunker{},
		MarkdownChunker: &MockChunker{},
	})
	if err != nil {
		t.Fatalf("NewRunner() error: %v", err)
	}

	result, err := runner.Run(context.Background(), RunnerConfig{
		RootDir: tmpDir,
		DataDir: filepath.Join(tmpDir, ".amanmcp"),
	})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	if result.Warnings != 0 {
		t.Fatalf("warnings = %d, want 0 because ignored file should not be scanned", result.Warnings)
	}
	if len(renderer.ErrorEvents) != 0 {
		t.Fatalf("renderer warnings = %#v, want none", renderer.ErrorEvents)
	}
	if strings.Contains(strings.Join(embedder.BatchTexts, "\n"), runnerSecretValue) {
		t.Fatalf("ignored file secret reached embeddings")
	}
	for _, file := range metadata.FilesSaved {
		if file.Path == "ignored.go" {
			t.Fatalf("ignored file was saved to metadata")
		}
	}
}

func requireWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
