package index

import (
	"context"
	"fmt"
	"strings"

	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/store"
)

// PatternContextGenerator generates context using pattern-based rules.
// This is the fallback when LLM is unavailable or for fast processing.
//
// It extracts context from:
// - File path
// - Symbol names and types
// - Doc comments
type PatternContextGenerator struct {
	cfg *config.Config
}

// NewPatternContextGenerator creates a new pattern-based context generator.
func NewPatternContextGenerator(cfg *config.Config) *PatternContextGenerator {
	return &PatternContextGenerator{cfg: cfg}
}

// GenerateContext generates context for a chunk using pattern rules.
func (p *PatternContextGenerator) GenerateContext(
	ctx context.Context,
	chunk *store.Chunk,
	docContext string,
) (string, error) {
	if chunk == nil {
		return "", nil
	}

	// RCA-015: Skip context for code chunks when CodeChunks is disabled.
	// This improves vector search quality by embedding raw code without prefixes.
	if chunk.ContentType == store.ContentTypeCode && p.cfg != nil && !p.cfg.Contextual.CodeChunks {
		return "", nil
	}

	var parts []string

	// Add file path context
	parts = append(parts, fmt.Sprintf("From file: %s", chunk.FilePath))

	// Add symbol info if available
	if len(chunk.Symbols) > 0 {
		sym := chunk.Symbols[0]
		parts = append(parts, fmt.Sprintf("Defines: %s %s", sym.Type, sym.Name))

		// Add doc comment if present (first sentence only)
		if sym.DocComment != "" {
			firstSentence := extractFirstSentence(sym.DocComment)
			if firstSentence != "" {
				parts = append(parts, fmt.Sprintf("Purpose: %s", firstSentence))
			}
		}
	}

	// Add language context for code files
	if chunk.ContentType == store.ContentTypeCode && chunk.Language != "" {
		parts = append(parts, fmt.Sprintf("Language: %s", chunk.Language))
	}

	return strings.Join(parts, ". ") + ".", nil
}

// GenerateBatch generates context for multiple chunks.
func (p *PatternContextGenerator) GenerateBatch(
	ctx context.Context,
	chunks []*store.Chunk,
	docContext string,
) ([]string, error) {
	results := make([]string, len(chunks))
	for i, chunk := range chunks {
		context, err := p.GenerateContext(ctx, chunk, docContext)
		if err != nil {
			return nil, err
		}
		results[i] = context
	}
	return results, nil
}

// Available always returns true for pattern generator.
func (p *PatternContextGenerator) Available(ctx context.Context) bool {
	return true
}

// ModelName returns the model identifier.
func (p *PatternContextGenerator) ModelName() string {
	return "pattern-based"
}

// Close is a no-op for pattern generator.
func (p *PatternContextGenerator) Close() error {
	return nil
}

// extractFirstSentence extracts the first sentence from text.
func extractFirstSentence(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	// Remove common doc comment prefixes
	text = strings.TrimPrefix(text, "//")
	text = strings.TrimPrefix(text, "/*")
	text = strings.TrimSuffix(text, "*/")
	text = strings.TrimSpace(text)

	// Find end of first sentence
	for i, r := range text {
		if r == '.' || r == '\n' {
			sentence := strings.TrimSpace(text[:i+1])
			// Remove trailing period if we're going to add our own
			return strings.TrimSuffix(sentence, ".")
		}
	}

	// No sentence end found, return first 100 chars
	if len(text) > 100 {
		return text[:100] + "..."
	}
	return text
}

// HybridContextGenerator combines LLM and pattern-based generators.
// Uses LLM when available, falls back to pattern generator otherwise.
type HybridContextGenerator struct {
	llm     ContextGenerator // Can be nil if LLM unavailable
	pattern *PatternContextGenerator
	cfg     *config.Config
}

// NewHybridContextGenerator creates a new hybrid generator.
// If llm is nil, only pattern-based generation is used.
func NewHybridContextGenerator(llm ContextGenerator, cfg *config.Config) *HybridContextGenerator {
	return &HybridContextGenerator{
		llm:     llm,
		pattern: NewPatternContextGenerator(cfg),
		cfg:     cfg,
	}
}

// GenerateContext generates context, preferring LLM if available.
func (h *HybridContextGenerator) GenerateContext(
	ctx context.Context,
	chunk *store.Chunk,
	docContext string,
) (string, error) {
	// RCA-015: Skip context for code chunks when CodeChunks is disabled.
	if chunk != nil && chunk.ContentType == store.ContentTypeCode && h.cfg != nil && !h.cfg.Contextual.CodeChunks {
		return "", nil
	}

	// Try LLM first if available
	if h.llm != nil && h.llm.Available(ctx) {
		context, err := h.llm.GenerateContext(ctx, chunk, docContext)
		if err == nil && context != "" {
			return context, nil
		}
		// Fall through to pattern generator on error
	}

	// Use pattern generator as fallback
	return h.pattern.GenerateContext(ctx, chunk, docContext)
}

// GenerateBatch generates context for multiple chunks.
func (h *HybridContextGenerator) GenerateBatch(
	ctx context.Context,
	chunks []*store.Chunk,
	docContext string,
) ([]string, error) {
	// Try LLM first if available
	if h.llm != nil && h.llm.Available(ctx) {
		contexts, err := h.llm.GenerateBatch(ctx, chunks, docContext)
		if err == nil {
			return contexts, nil
		}
		// Fall through to pattern generator on error
	}

	// Use pattern generator as fallback
	return h.pattern.GenerateBatch(ctx, chunks, docContext)
}

// Available returns true if any generator is available.
func (h *HybridContextGenerator) Available(ctx context.Context) bool {
	return h.pattern.Available(ctx) || (h.llm != nil && h.llm.Available(ctx))
}

// ModelName returns the model identifier.
func (h *HybridContextGenerator) ModelName() string {
	if h.llm != nil {
		return h.llm.ModelName() + "+pattern"
	}
	return h.pattern.ModelName()
}

// Close releases resources.
func (h *HybridContextGenerator) Close() error {
	if h.llm != nil {
		return h.llm.Close()
	}
	return nil
}
