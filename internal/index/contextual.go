// Package index provides contextual retrieval for enhanced RAG performance.
// CR-1: Contextual Retrieval - LLM-generated context for each chunk at index time.
//
// Based on Anthropic's research showing 67% reduction in retrieval errors.
// See: https://www.anthropic.com/news/contextual-retrieval
package index

import (
	"context"
	"fmt"
	"strings"

	"github.com/Aman-CERP/amanmcp/internal/store"
)

// ContextGenerator generates contextual descriptions for chunks.
// This enriches chunks with LLM-generated context before embedding,
// improving semantic search quality.
type ContextGenerator interface {
	// GenerateContext generates a 1-2 sentence context for a chunk.
	// The context situates the chunk within its parent document.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeouts
	//   - chunk: The chunk to generate context for
	//   - docContext: The parent document context (imports, headers, etc.)
	//
	// Returns the generated context string, or empty string on failure.
	GenerateContext(ctx context.Context, chunk *store.Chunk, docContext string) (string, error)

	// GenerateBatch generates context for multiple chunks from the same file.
	// This enables prompt caching optimization when processing many chunks.
	GenerateBatch(ctx context.Context, chunks []*store.Chunk, docContext string) ([]string, error)

	// Available checks if the generator is available and ready.
	Available(ctx context.Context) bool

	// ModelName returns the model identifier being used.
	ModelName() string

	// Close releases any resources held by the generator.
	Close() error
}

// ContextGeneratorConfig configures the context generator.
type ContextGeneratorConfig struct {
	// OllamaHost is the Ollama API endpoint.
	// Default: http://localhost:11434
	OllamaHost string

	// Model is the LLM model to use for context generation.
	// Default: qwen3:0.6b (small, fast model)
	Model string

	// Timeout is the per-chunk timeout for context generation.
	// Default: 5s
	Timeout string

	// BatchSize is the number of chunks to process in a batch.
	// Default: 8
	BatchSize int

	// FallbackOnly skips LLM and uses pattern-based fallback only.
	// Default: false
	FallbackOnly bool
}

// DefaultContextGeneratorConfig returns the default configuration.
func DefaultContextGeneratorConfig() ContextGeneratorConfig {
	return ContextGeneratorConfig{
		OllamaHost: "http://localhost:11434",
		Model:      "qwen3:0.6b",
		Timeout:    "5s",
		BatchSize:  8,
	}
}

// EnrichChunkWithContext prepends generated context to a chunk's content.
// This modifies chunk.Content in place.
//
// Format: "[Context]\n\n[Original Content]"
func EnrichChunkWithContext(chunk *store.Chunk, generatedContext string) {
	if generatedContext == "" || chunk == nil {
		return
	}

	// Prepend context to content for embedding
	chunk.Content = generatedContext + "\n\n" + chunk.RawContent

	// Store context in metadata for debugging/inspection
	if chunk.Metadata == nil {
		chunk.Metadata = make(map[string]string)
	}
	chunk.Metadata["contextual_context"] = generatedContext
}

// ExtractDocumentContext extracts document-level context for a file.
// For code files, this includes package declaration and imports.
// For markdown files, this includes the file path and section headers.
func ExtractDocumentContext(chunks []*store.Chunk) string {
	if len(chunks) == 0 {
		return ""
	}

	// Get file path from first chunk
	filePath := chunks[0].FilePath

	// For code files, use the Context field (imports/package)
	// For markdown files, build context from headers
	switch chunks[0].ContentType {
	case store.ContentTypeCode:
		// Code files have imports in Context field
		if chunks[0].Context != "" {
			return fmt.Sprintf("File: %s\n%s", filePath, chunks[0].Context)
		}
		return fmt.Sprintf("File: %s", filePath)

	case store.ContentTypeMarkdown:
		// For markdown, list section headers
		var headers []string
		headers = append(headers, fmt.Sprintf("Document: %s", filePath))
		for _, c := range chunks {
			if len(c.Symbols) > 0 && c.Symbols[0].Type == store.SymbolTypeFunction {
				// Section headers are stored as "function" symbols in markdown
				headers = append(headers, "- "+c.Symbols[0].Name)
			}
		}
		if len(headers) > 5 {
			headers = headers[:5] // Limit to first 5 headers
			headers = append(headers, "...")
		}
		return strings.Join(headers, "\n")

	default:
		return fmt.Sprintf("File: %s", filePath)
	}
}

// GroupChunksByFile groups chunks by their file path for batch processing.
func GroupChunksByFile(chunks []*store.Chunk) map[string][]*store.Chunk {
	grouped := make(map[string][]*store.Chunk)
	for _, c := range chunks {
		grouped[c.FilePath] = append(grouped[c.FilePath], c)
	}
	return grouped
}
