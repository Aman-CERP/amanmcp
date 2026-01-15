package index

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Aman-CERP/amanmcp/internal/store"
)

// Default LLM context generator configuration.
const (
	DefaultContextModel   = "qwen3:0.6b"
	DefaultContextTimeout = 5 * time.Second
	DefaultContextHost    = "http://localhost:11434"
)

// LLMContextGenerator generates context using Ollama LLM.
// Uses a small, fast model optimized for context generation.
type LLMContextGenerator struct {
	client *http.Client
	config ContextGeneratorConfig
}

// generateRequest is the Ollama /api/generate request body.
type llmGenerateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

// generateResponse is the Ollama /api/generate response body.
type llmGenerateResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

// contextPromptTemplate is the prompt for context generation.
// Optimized for code comprehension and concise output.
const contextPromptTemplate = `You are analyzing code. Generate a 1-2 sentence context for this code chunk.

File: %s

Document context:
%s

Code chunk:
%s

Instructions:
- Describe what this code does and its purpose
- Be specific about function names and types
- Keep it under 100 tokens
- Output ONLY the context, no preamble

Context:`

// markdownPromptTemplate is the prompt for markdown context generation.
const markdownPromptTemplate = `You are analyzing documentation. Generate a 1-2 sentence context for this section.

Document: %s

Section content:
%s

Instructions:
- Summarize what this section explains
- Note its relationship to the document
- Keep it under 100 tokens
- Output ONLY the context, no preamble

Context:`

// NewLLMContextGenerator creates a new LLM-based context generator.
func NewLLMContextGenerator(config ContextGeneratorConfig) (*LLMContextGenerator, error) {
	// Apply defaults
	if config.OllamaHost == "" {
		config.OllamaHost = DefaultContextHost
	}
	if config.Model == "" {
		config.Model = DefaultContextModel
	}

	timeout := DefaultContextTimeout
	if config.Timeout != "" {
		parsed, err := time.ParseDuration(config.Timeout)
		if err == nil {
			timeout = parsed
		}
	}

	client := &http.Client{
		Timeout: timeout,
	}

	gen := &LLMContextGenerator{
		client: client,
		config: config,
	}

	return gen, nil
}

// GenerateContext generates context for a single chunk.
func (l *LLMContextGenerator) GenerateContext(
	ctx context.Context,
	chunk *store.Chunk,
	docContext string,
) (string, error) {
	if chunk == nil {
		return "", nil
	}

	// Build prompt based on content type
	var prompt string
	switch chunk.ContentType {
	case store.ContentTypeCode:
		prompt = fmt.Sprintf(contextPromptTemplate,
			chunk.FilePath,
			docContext,
			truncateContent(chunk.RawContent, 1500),
		)
	case store.ContentTypeMarkdown:
		prompt = fmt.Sprintf(markdownPromptTemplate,
			chunk.FilePath,
			truncateContent(chunk.RawContent, 1500),
		)
	default:
		// Use code template for other types
		prompt = fmt.Sprintf(contextPromptTemplate,
			chunk.FilePath,
			docContext,
			truncateContent(chunk.RawContent, 1500),
		)
	}

	// Make LLM request
	response, err := l.generate(ctx, prompt)
	if err != nil {
		return "", err
	}

	// Clean up response
	response = strings.TrimSpace(response)
	response = strings.TrimPrefix(response, "Context:")
	response = strings.TrimSpace(response)

	return response, nil
}

// GenerateBatch generates context for multiple chunks.
// Uses the same document context for all chunks (prompt caching optimization).
func (l *LLMContextGenerator) GenerateBatch(
	ctx context.Context,
	chunks []*store.Chunk,
	docContext string,
) ([]string, error) {
	results := make([]string, len(chunks))

	for i, chunk := range chunks {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		context, err := l.GenerateContext(ctx, chunk, docContext)
		if err != nil {
			slog.Debug("LLM context generation failed, using empty",
				slog.String("chunk_id", chunk.ID),
				slog.String("error", err.Error()))
			results[i] = ""
			continue
		}
		results[i] = context
	}

	return results, nil
}

// generate makes an LLM request to Ollama.
func (l *LLMContextGenerator) generate(ctx context.Context, prompt string) (string, error) {
	reqBody := llmGenerateRequest{
		Model:  l.config.Model,
		Prompt: prompt,
		Stream: false,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := l.config.OllamaHost + "/api/generate"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := l.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("execute request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	var genResp llmGenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&genResp); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	return genResp.Response, nil
}

// Available checks if Ollama is reachable.
func (l *LLMContextGenerator) Available(ctx context.Context) bool {
	// Try to hit the Ollama API
	url := l.config.OllamaHost + "/api/tags"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := l.client.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	return resp.StatusCode == http.StatusOK
}

// ModelName returns the model being used.
func (l *LLMContextGenerator) ModelName() string {
	return l.config.Model
}

// Close is a no-op for LLM generator.
func (l *LLMContextGenerator) Close() error {
	return nil
}

// truncateContent truncates content to maxLen characters.
func truncateContent(content string, maxLen int) string {
	if len(content) <= maxLen {
		return content
	}
	return content[:maxLen] + "\n... [truncated]"
}
