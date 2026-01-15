package store

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/analysis"
	"github.com/blevesearch/bleve/v2/analysis/analyzer/custom"
	"github.com/blevesearch/bleve/v2/analysis/token/lowercase"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/registry"
	"github.com/blevesearch/bleve/v2/search"
)

const (
	// CodeTokenizerName is the name of our custom code tokenizer.
	CodeTokenizerName = "code_tokenizer"

	// CodeStopFilterName is the name of our custom stop word filter.
	CodeStopFilterName = "code_stop"

	// CodeAnalyzerName is the name of our custom code analyzer.
	CodeAnalyzerName = "code_analyzer"
)

func init() {
	// Register custom tokenizer
	_ = registry.RegisterTokenizer(CodeTokenizerName, codeTokenizerConstructor)

	// Register custom stop word filter
	_ = registry.RegisterTokenFilter(CodeStopFilterName, codeStopFilterConstructor)
}

// BleveBM25Index wraps Bleve v2 for BM25 keyword search.
type BleveBM25Index struct {
	mu       sync.RWMutex
	index    bleve.Index
	path     string
	config   BM25Config
	closed   bool
	stopWords map[string]struct{}
}

// BleveDocument is the document structure for Bleve indexing.
type BleveDocument struct {
	Content string `json:"content"`
}

// validateIndexIntegrity checks if a Bleve index is valid before opening.
// Returns nil if valid, error describing corruption if not.
// This helps detect and auto-recover from BUG-049 (index corruption after binary rebuild).
func validateIndexIntegrity(path string) error {
	// Check if index directory exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil // Index doesn't exist, will be created
	}

	// Check 1: index_meta.json exists and is non-empty
	metaPath := filepath.Join(path, "index_meta.json")
	info, err := os.Stat(metaPath)
	if os.IsNotExist(err) {
		// index_meta.json missing means index is incomplete/corrupted
		return fmt.Errorf("index_meta.json missing (corrupted index)")
	}
	if err != nil {
		return fmt.Errorf("cannot stat index_meta.json: %w", err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("index_meta.json is empty (corrupted)")
	}

	// Check 2: Validate JSON is parseable
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return fmt.Errorf("cannot read index_meta.json: %w", err)
	}
	var meta map[string]interface{}
	if err := json.Unmarshal(data, &meta); err != nil {
		return fmt.Errorf("index_meta.json is corrupt: %w", err)
	}

	return nil
}

// isCorruptionError checks if an error indicates Bleve index corruption.
func isCorruptionError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "unexpected end of JSON") ||
		strings.Contains(errStr, "error parsing mapping JSON") ||
		strings.Contains(errStr, "failed to load segment") ||
		strings.Contains(errStr, "error opening bolt") ||
		strings.Contains(errStr, "no such file or directory") ||
		err == bleve.ErrorIndexMetaCorrupt
}

// NewBleveBM25Index creates a new BM25 index.
// If path is empty, creates an in-memory index.
// Includes BUG-049 fix: validates index integrity before opening and auto-recovers from corruption.
func NewBleveBM25Index(path string, config BM25Config) (*BleveBM25Index, error) {
	indexMapping, err := createIndexMapping()
	if err != nil {
		return nil, fmt.Errorf("failed to create index mapping: %w", err)
	}

	var idx bleve.Index
	if path == "" {
		// In-memory index for testing
		idx, err = bleve.NewMemOnly(indexMapping)
	} else {
		// Create directory if needed
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
		}

		// BUG-049 Fix: Validate integrity before opening
		if validErr := validateIndexIntegrity(path); validErr != nil {
			slog.Warn("bm25_index_corrupted",
				slog.String("path", path),
				slog.String("error", validErr.Error()))

			// Auto-clear corrupted index
			if removeErr := os.RemoveAll(path); removeErr != nil {
				return nil, fmt.Errorf("BM25 index corrupted at %s and cannot remove: %w (original error: %v)", path, removeErr, validErr)
			}
			slog.Info("bm25_index_cleared",
				slog.String("path", path),
				slog.String("reason", "corruption detected, please reindex"))
		}

		// Try to open existing index first
		idx, err = bleve.Open(path)
		if err == bleve.ErrorIndexPathDoesNotExist {
			// Create new index
			idx, err = bleve.New(path, indexMapping)
		} else if err != nil && isCorruptionError(err) {
			// BUG-049 Fix: Handle corruption errors from Bleve.Open()
			slog.Warn("bm25_index_open_failed",
				slog.String("path", path),
				slog.String("error", err.Error()))

			// Clear and recreate
			if removeErr := os.RemoveAll(path); removeErr != nil {
				return nil, fmt.Errorf("BM25 index corrupted, cannot clear: %w (original: %v)", removeErr, err)
			}
			slog.Info("bm25_index_cleared",
				slog.String("path", path),
				slog.String("reason", "open failed with corruption, please reindex"))

			// Create fresh index
			idx, err = bleve.New(path, indexMapping)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create/open index: %w", err)
	}

	return &BleveBM25Index{
		index:     idx,
		path:      path,
		config:    config,
		stopWords: BuildStopWordMap(config.StopWords),
	}, nil
}

// createIndexMapping creates the Bleve index mapping with BM25 scoring.
func createIndexMapping() (*mapping.IndexMappingImpl, error) {
	// Create index mapping
	indexMapping := bleve.NewIndexMapping()

	// Register custom analyzer
	err := indexMapping.AddCustomAnalyzer(CodeAnalyzerName, map[string]interface{}{
		"type":      custom.Name,
		"tokenizer": CodeTokenizerName,
		"token_filters": []string{
			lowercase.Name,
			CodeStopFilterName,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to add custom analyzer: %w", err)
	}

	// Set as default analyzer
	indexMapping.DefaultAnalyzer = CodeAnalyzerName

	return indexMapping, nil
}

// Index adds documents to the index.
func (b *BleveBM25Index) Index(ctx context.Context, docs []*Document) error {
	if len(docs) == 0 {
		return nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return fmt.Errorf("index is closed")
	}

	batch := b.index.NewBatch()
	for _, doc := range docs {
		bleveDoc := BleveDocument{Content: doc.Content}
		if err := batch.Index(doc.ID, bleveDoc); err != nil {
			return fmt.Errorf("failed to index document %s: %w", doc.ID, err)
		}
	}

	if err := b.index.Batch(batch); err != nil {
		return fmt.Errorf("failed to execute batch: %w", err)
	}

	return nil
}

// Search returns documents matching query, scored by BM25.
func (b *BleveBM25Index) Search(ctx context.Context, queryStr string, limit int) ([]*BM25Result, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return nil, fmt.Errorf("index is closed")
	}

	// Handle empty query
	if queryStr == "" || strings.TrimSpace(queryStr) == "" {
		return []*BM25Result{}, nil
	}

	// Create match query (uses the analyzer)
	matchQuery := bleve.NewMatchQuery(queryStr)
	matchQuery.SetField("content")

	searchRequest := bleve.NewSearchRequest(matchQuery)
	searchRequest.Size = limit
	searchRequest.IncludeLocations = true // For matched terms

	result, err := b.index.SearchInContext(ctx, searchRequest)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	results := make([]*BM25Result, 0, len(result.Hits))
	for _, hit := range result.Hits {
		matchedTerms := extractMatchedTerms(hit)
		results = append(results, &BM25Result{
			DocID:        hit.ID,
			Score:        hit.Score,
			MatchedTerms: matchedTerms,
		})
	}

	return results, nil
}

// Delete removes documents from the index.
func (b *BleveBM25Index) Delete(ctx context.Context, docIDs []string) error {
	if len(docIDs) == 0 {
		return nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return fmt.Errorf("index is closed")
	}

	batch := b.index.NewBatch()
	for _, id := range docIDs {
		batch.Delete(id)
	}

	if err := b.index.Batch(batch); err != nil {
		return fmt.Errorf("failed to delete documents: %w", err)
	}

	return nil
}

// AllIDs returns all document IDs in the index.
// Used for consistency checking between stores.
func (b *BleveBM25Index) AllIDs() ([]string, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return nil, fmt.Errorf("index is closed")
	}

	// Use a MatchAllQuery to get all documents
	query := bleve.NewMatchAllQuery()
	docCount, _ := b.index.DocCount()

	// Create search request to get all IDs
	req := bleve.NewSearchRequest(query)
	req.Size = int(docCount)
	req.Fields = []string{} // Only need IDs, not content

	result, err := b.index.Search(req)
	if err != nil {
		return nil, fmt.Errorf("failed to search for all IDs: %w", err)
	}

	ids := make([]string, len(result.Hits))
	for i, hit := range result.Hits {
		ids[i] = hit.ID
	}

	return ids, nil
}

// Stats returns index statistics.
func (b *BleveBM25Index) Stats() *IndexStats {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return &IndexStats{}
	}

	docCount, _ := b.index.DocCount()

	return &IndexStats{
		DocumentCount: int(docCount),
		// Note: Bleve doesn't directly expose term count and avg doc length
		// These would require iterating through the index or tracking separately
	}
}

// Save persists the index to disk.
// For Bleve, this is a no-op as changes are persisted automatically.
func (b *BleveBM25Index) Save(path string) error {
	// Bleve persists automatically when using disk-based index
	return nil
}

// Load opens an existing index from disk.
func (b *BleveBM25Index) Load(path string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.index != nil && !b.closed {
		_ = b.index.Close()
	}

	idx, err := bleve.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open index: %w", err)
	}

	b.index = idx
	b.path = path
	b.closed = false

	return nil
}

// Close closes the index.
func (b *BleveBM25Index) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil
	}

	b.closed = true
	if b.index != nil {
		return b.index.Close()
	}
	return nil
}

// extractMatchedTerms extracts matched terms from search hit.
func extractMatchedTerms(hit *search.DocumentMatch) []string {
	terms := make(map[string]struct{})
	for field, locations := range hit.Locations {
		if field == "content" {
			for term := range locations {
				terms[term] = struct{}{}
			}
		}
	}

	result := make([]string, 0, len(terms))
	for term := range terms {
		result = append(result, term)
	}
	return result
}

// Verify interface implementation
var _ BM25Index = (*BleveBM25Index)(nil)

// codeTokenizerConstructor creates a new code tokenizer for Bleve.
func codeTokenizerConstructor(config map[string]interface{}, cache *registry.Cache) (analysis.Tokenizer, error) {
	return &bleveCodeTokenizer{}, nil
}

// bleveCodeTokenizer implements analysis.Tokenizer for code-aware tokenization.
type bleveCodeTokenizer struct{}

// Tokenize implements analysis.Tokenizer.
func (t *bleveCodeTokenizer) Tokenize(input []byte) analysis.TokenStream {
	text := string(input)
	tokens := TokenizeCode(text)

	result := make(analysis.TokenStream, 0, len(tokens))
	pos := 1
	offset := 0

	for _, token := range tokens {
		// Find token position in original text (case-insensitive search)
		start := strings.Index(strings.ToLower(text[offset:]), strings.ToLower(token))
		if start == -1 {
			start = offset
		} else {
			start += offset
		}
		end := start + len(token)

		result = append(result, &analysis.Token{
			Term:     []byte(token),
			Start:    start,
			End:      end,
			Position: pos,
			Type:     analysis.AlphaNumeric,
		})
		pos++
		if end <= len(text) {
			offset = end
		}
	}

	return result
}

// codeStopFilterConstructor creates a code stop word filter for Bleve.
func codeStopFilterConstructor(config map[string]interface{}, cache *registry.Cache) (analysis.TokenFilter, error) {
	return &bleveCodeStopFilter{
		stopWords: BuildStopWordMap(DefaultCodeStopWords),
	}, nil
}

// bleveCodeStopFilter implements analysis.TokenFilter for code stop words.
type bleveCodeStopFilter struct {
	stopWords map[string]struct{}
}

// Filter implements analysis.TokenFilter.
func (f *bleveCodeStopFilter) Filter(input analysis.TokenStream) analysis.TokenStream {
	result := make(analysis.TokenStream, 0, len(input))
	for _, token := range input {
		term := strings.ToLower(string(token.Term))
		if _, isStop := f.stopWords[term]; !isStop {
			result = append(result, token)
		}
	}
	return result
}
