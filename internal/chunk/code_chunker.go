package chunk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// CodeChunkerOptions configures the code chunker behavior
type CodeChunkerOptions struct {
	MaxChunkTokens int // Maximum tokens per chunk (default: DefaultMaxChunkTokens)
	OverlapTokens  int // Overlap between chunks when splitting (default: DefaultOverlapTokens)
}

// CodeChunker implements AST-aware code chunking using tree-sitter
type CodeChunker struct {
	parser    *Parser
	extractor *SymbolExtractor
	registry  *LanguageRegistry
	options   CodeChunkerOptions
}

// NewCodeChunker creates a new code chunker with default options
func NewCodeChunker() *CodeChunker {
	return NewCodeChunkerWithOptions(CodeChunkerOptions{})
}

// NewCodeChunkerWithOptions creates a new code chunker with custom options
func NewCodeChunkerWithOptions(opts CodeChunkerOptions) *CodeChunker {
	if opts.MaxChunkTokens == 0 {
		opts.MaxChunkTokens = DefaultMaxChunkTokens
	}
	if opts.OverlapTokens == 0 {
		opts.OverlapTokens = DefaultOverlapTokens
	}

	registry := DefaultRegistry()
	return &CodeChunker{
		parser:    NewParserWithRegistry(registry),
		extractor: NewSymbolExtractorWithRegistry(registry),
		registry:  registry,
		options:   opts,
	}
}

// Close releases chunker resources
func (c *CodeChunker) Close() {
	if c.parser != nil {
		c.parser.Close()
	}
}

// SupportedExtensions returns file extensions this chunker handles
func (c *CodeChunker) SupportedExtensions() []string {
	return c.registry.SupportedExtensions()
}

// Chunk splits a file into semantic chunks
func (c *CodeChunker) Chunk(ctx context.Context, file *FileInput) ([]*Chunk, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}

	// Check if language is supported
	_, supported := c.registry.GetByName(file.Language)
	if !supported {
		// Fall back to line-based chunking
		return c.chunkByLines(file)
	}

	// Parse the file
	tree, err := c.parser.Parse(ctx, file.Content, file.Language)
	if err != nil {
		// Fall back to line-based chunking on parse error
		return c.chunkByLines(file)
	}

	// Extract context (package declaration, imports)
	fileContext := c.extractFileContext(tree, file.Content, file.Language)

	// Enrich context with file path marker for better embedding quality
	fileContext = c.enrichContextWithFilePath(file.Path, file.Language, fileContext)

	// Find symbol nodes (functions, classes, methods, types)
	symbolNodes := c.findSymbolNodes(tree, file.Language)

	if len(symbolNodes) == 0 {
		return nil, nil
	}

	// Create chunks from symbol nodes
	chunks := make([]*Chunk, 0, len(symbolNodes))
	now := time.Now()

	for _, node := range symbolNodes {
		nodeChunks := c.createChunksFromNode(node, tree, file, fileContext, now)
		chunks = append(chunks, nodeChunks...)
	}

	return chunks, nil
}

// symbolNodeInfo holds a symbol node with its extracted symbol info
type symbolNodeInfo struct {
	node   *Node
	symbol *Symbol
}

// findSymbolNodes finds all top-level symbol-defining nodes
func (c *CodeChunker) findSymbolNodes(tree *Tree, language string) []*symbolNodeInfo {
	// Return empty slice, not nil, for consistent API behavior (DEBT-012)
	config, ok := c.registry.GetByName(language)
	if !ok {
		return []*symbolNodeInfo{}
	}

	var symbolNodes []*symbolNodeInfo

	// Build set of symbol-defining node types
	symbolTypes := make(map[string]SymbolType)
	for _, t := range config.FunctionTypes {
		symbolTypes[t] = SymbolTypeFunction
	}
	for _, t := range config.MethodTypes {
		symbolTypes[t] = SymbolTypeMethod
	}
	for _, t := range config.ClassTypes {
		symbolTypes[t] = SymbolTypeClass
	}
	for _, t := range config.InterfaceTypes {
		symbolTypes[t] = SymbolTypeInterface
	}
	for _, t := range config.TypeDefTypes {
		symbolTypes[t] = SymbolTypeType
	}
	for _, t := range config.ConstantTypes {
		symbolTypes[t] = SymbolTypeConstant
	}
	for _, t := range config.VariableTypes {
		symbolTypes[t] = SymbolTypeVariable
	}

	// Walk tree to find symbol nodes
	tree.Root.Walk(func(n *Node) bool {
		// For JS/TS lexical_declaration/variable_declaration, check for arrow functions first
		// Arrow functions should be typed as Function, not Constant
		if n.Type == "lexical_declaration" || n.Type == "variable_declaration" {
			sym := c.extractor.extractSpecialSymbol(n, tree.Source, language)
			if sym != nil {
				// It's an arrow function or function expression
				symbolNodes = append(symbolNodes, &symbolNodeInfo{
					node:   n,
					symbol: sym,
				})
				return true // Already handled, don't process as constant
			}
			// Not an arrow function - fall through to check as constant/variable
		}

		// Check if this is a symbol-defining node type
		if symType, isSymbol := symbolTypes[n.Type]; isSymbol {
			sym := c.extractSymbol(n, tree, symType, language)
			if sym != nil {
				symbolNodes = append(symbolNodes, &symbolNodeInfo{
					node:   n,
					symbol: sym,
				})
			}
		}
		return true
	})

	return symbolNodes
}

// extractSymbol extracts symbol info from a node
func (c *CodeChunker) extractSymbol(n *Node, tree *Tree, symType SymbolType, language string) *Symbol {
	config, _ := c.registry.GetByName(language)
	name := c.extractor.extractName(n, tree.Source, config, language)
	if name == "" {
		return nil
	}

	docComment := c.extractDocComment(n, tree.Source, language)

	return &Symbol{
		Name:       name,
		Type:       symType,
		StartLine:  int(n.StartPoint.Row) + 1,
		EndLine:    int(n.EndPoint.Row) + 1,
		DocComment: docComment,
	}
}

// extractDocComment extracts doc comment for a node, looking for multi-line comments
func (c *CodeChunker) extractDocComment(n *Node, source []byte, language string) string {
	// Find the start of the current line
	lineStart := int(n.StartByte)
	for lineStart > 0 && source[lineStart-1] != '\n' {
		lineStart--
	}

	// Look for comment on preceding lines
	if lineStart <= 1 {
		return ""
	}

	// Collect comment lines working backwards
	var commentLines []string
	pos := lineStart - 1 // Start before the newline

	for pos > 0 {
		// Find start of previous line
		prevLineEnd := pos
		pos--
		for pos > 0 && source[pos] != '\n' {
			pos--
		}
		prevLineStart := pos
		if pos > 0 {
			prevLineStart++ // Skip the newline
		}

		prevLine := strings.TrimSpace(string(source[prevLineStart:prevLineEnd]))

		// Check for single-line comments
		switch language {
		case "go", "typescript", "tsx", "javascript", "jsx":
			if strings.HasPrefix(prevLine, "//") {
				commentLines = append([]string{strings.TrimPrefix(prevLine, "//")}, commentLines...)
				continue
			}
		case "python":
			if strings.HasPrefix(prevLine, "#") {
				commentLines = append([]string{strings.TrimPrefix(prevLine, "#")}, commentLines...)
				continue
			}
		}

		// Stop if we hit a non-comment line (unless empty)
		if prevLine != "" {
			break
		}
	}

	if len(commentLines) == 0 {
		return ""
	}

	return strings.TrimSpace(strings.Join(commentLines, "\n"))
}

// createChunksFromNode creates one or more chunks from a symbol node
func (c *CodeChunker) createChunksFromNode(info *symbolNodeInfo, tree *Tree, file *FileInput, fileContext string, now time.Time) []*Chunk {
	node := info.node
	rawContent := string(tree.Source[node.StartByte:node.EndByte])

	// Include doc comment in raw content if it exists
	rawContentWithDoc := rawContent
	if info.symbol.DocComment != "" {
		// Find where the doc comment is in the source
		rawContentWithDoc = c.getRawContentWithDocComment(node, tree.Source, info.symbol.DocComment)
	}

	tokens := estimateTokens(rawContentWithDoc)

	if tokens <= c.options.MaxChunkTokens {
		// Small enough to be a single chunk
		chunk := c.createChunk(file, rawContentWithDoc, fileContext, info.symbol, now)
		return []*Chunk{chunk}
	}

	// Need to split large symbol
	return c.splitLargeSymbol(info, tree, file, fileContext, now)
}

// getRawContentWithDocComment gets raw content including doc comment
func (c *CodeChunker) getRawContentWithDocComment(n *Node, source []byte, docComment string) string {
	// Find start of doc comment (before the node)
	lineStart := int(n.StartByte)
	for lineStart > 0 && source[lineStart-1] != '\n' {
		lineStart--
	}

	// Count back through comment lines
	docLines := strings.Count(docComment, "\n") + 1
	for i := 0; i < docLines && lineStart > 0; i++ {
		lineStart--
		for lineStart > 0 && source[lineStart-1] != '\n' {
			lineStart--
		}
	}

	return string(source[lineStart:n.EndByte])
}

// splitLargeSymbol splits a large symbol into multiple chunks
func (c *CodeChunker) splitLargeSymbol(info *symbolNodeInfo, tree *Tree, file *FileInput, fileContext string, now time.Time) []*Chunk {
	node := info.node
	content := string(tree.Source[node.StartByte:node.EndByte])

	// Try to split at logical boundaries (child symbols for classes)
	if info.symbol.Type == SymbolTypeClass {
		// For classes, try to split by methods
		methodChunks := c.splitClassByMethods(info, tree, file, fileContext, now)
		if len(methodChunks) > 0 {
			return methodChunks
		}
	}

	// Fall back to line-based splitting with overlap
	return c.splitByLines(content, info.symbol, file, fileContext, now, int(node.StartPoint.Row)+1)
}

// splitClassByMethods splits a class into method-based chunks
func (c *CodeChunker) splitClassByMethods(info *symbolNodeInfo, tree *Tree, file *FileInput, fileContext string, now time.Time) []*Chunk {
	// This is a placeholder - in practice we'd walk the class node
	// to find method children and create individual chunks for each
	return nil // Will fall through to line splitting
}

// splitByLines splits content into line-based chunks with overlap
func (c *CodeChunker) splitByLines(content string, symbol *Symbol, file *FileInput, fileContext string, now time.Time, startLine int) []*Chunk {
	lines := strings.Split(content, "\n")
	// Return empty slice, not nil, for consistent API behavior (DEBT-012)
	if len(lines) == 0 {
		return []*Chunk{}
	}

	// Calculate lines per chunk (roughly)
	// TokensPerChar = 4, so ~128 chars = 32 tokens per line average
	// For 300 tokens, that's about 9-10 lines, but we'll use more conservative estimate
	maxLinesPerChunk := (c.options.MaxChunkTokens * TokensPerChar) / 80 // Assume 80 chars per line average
	if maxLinesPerChunk < 20 {
		maxLinesPerChunk = 20
	}

	overlapLines := (c.options.OverlapTokens * TokensPerChar) / 80
	if overlapLines < 2 {
		overlapLines = 2
	}

	var chunks []*Chunk
	for i := 0; i < len(lines); {
		end := i + maxLinesPerChunk
		if end > len(lines) {
			end = len(lines)
		}

		chunkContent := strings.Join(lines[i:end], "\n")
		chunkStartLine := startLine + i
		chunkEndLine := startLine + end - 1

		// Create a sub-symbol for this chunk
		subSymbol := &Symbol{
			Name:      fmt.Sprintf("%s_part%d", symbol.Name, len(chunks)+1),
			Type:      symbol.Type,
			StartLine: chunkStartLine,
			EndLine:   chunkEndLine,
		}

		// For the first chunk, also register the parent symbol.
		// This ensures queries for "Search method" can find split symbols
		// that are stored as "Search_part1", "Search_part2", etc.
		// (See RCA-013: Split Symbol Discovery)
		symbols := []*Symbol{subSymbol}
		if len(chunks) == 0 {
			// Add parent symbol to first chunk for discoverability
			parentSymbol := &Symbol{
				Name:      symbol.Name,
				Type:      symbol.Type,
				StartLine: symbol.StartLine,
				EndLine:   symbol.EndLine,
			}
			symbols = append(symbols, parentSymbol)
		}

		chunk := &Chunk{
			ID:          generateChunkID(file.Path, chunkContent),
			FilePath:    file.Path,
			Content:     combineContextAndContent(fileContext, chunkContent),
			RawContent:  chunkContent,
			Context:     fileContext,
			ContentType: ContentTypeCode,
			Language:    file.Language,
			StartLine:   chunkStartLine,
			EndLine:     chunkEndLine,
			Symbols:     symbols,
			Metadata:    make(map[string]string),
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		chunks = append(chunks, chunk)

		// Move forward, accounting for overlap
		i = end - overlapLines
		if i <= 0 || end >= len(lines) {
			break
		}
	}

	return chunks
}

// createChunk creates a single chunk from content
func (c *CodeChunker) createChunk(file *FileInput, rawContent, fileContext string, symbol *Symbol, now time.Time) *Chunk {
	return &Chunk{
		ID:          generateChunkID(file.Path, rawContent),
		FilePath:    file.Path,
		Content:     combineContextAndContent(fileContext, rawContent),
		RawContent:  rawContent,
		Context:     fileContext,
		ContentType: ContentTypeCode,
		Language:    file.Language,
		StartLine:   symbol.StartLine,
		EndLine:     symbol.EndLine,
		Symbols:     []*Symbol{symbol},
		Metadata:    make(map[string]string),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// extractFileContext extracts package declaration and imports from a file
func (c *CodeChunker) extractFileContext(tree *Tree, source []byte, language string) string {
	var parts []string

	switch language {
	case "go":
		parts = c.extractGoContext(tree, source)
	case "typescript", "tsx":
		parts = c.extractTSContext(tree, source)
	case "javascript", "jsx":
		parts = c.extractJSContext(tree, source)
	case "python":
		parts = c.extractPythonContext(tree, source)
	}

	return strings.Join(parts, "\n\n")
}

func (c *CodeChunker) extractGoContext(tree *Tree, source []byte) []string {
	var parts []string

	// Find package clause
	for _, node := range tree.Root.Children {
		if node.Type == "package_clause" {
			parts = append(parts, node.GetContent(source))
			break
		}
	}

	// Find import declarations
	for _, node := range tree.Root.Children {
		if node.Type == "import_declaration" {
			parts = append(parts, node.GetContent(source))
		}
	}

	return parts
}

func (c *CodeChunker) extractTSContext(tree *Tree, source []byte) []string {
	return c.extractJSContext(tree, source) // Same for TS/TSX
}

func (c *CodeChunker) extractJSContext(tree *Tree, source []byte) []string {
	var parts []string

	// Find import statements
	for _, node := range tree.Root.Children {
		if node.Type == "import_statement" {
			parts = append(parts, node.GetContent(source))
		}
	}

	return parts
}

func (c *CodeChunker) extractPythonContext(tree *Tree, source []byte) []string {
	var parts []string

	// Find import statements
	for _, node := range tree.Root.Children {
		if node.Type == "import_statement" || node.Type == "import_from_statement" {
			parts = append(parts, node.GetContent(source))
		}
	}

	return parts
}

// chunkByLines is the fallback for unsupported languages
func (c *CodeChunker) chunkByLines(file *FileInput) ([]*Chunk, error) {
	content := string(file.Content)
	if strings.TrimSpace(content) == "" {
		return nil, nil
	}

	lines := strings.Split(content, "\n")
	linesPerChunk := 128 // ~512 tokens at 4 chars per token, 80 chars per line
	overlapLines := 16   // ~64 tokens overlap

	var chunks []*Chunk
	now := time.Now()

	for i := 0; i < len(lines); {
		end := i + linesPerChunk
		if end > len(lines) {
			end = len(lines)
		}

		chunkContent := strings.Join(lines[i:end], "\n")
		startLine := i + 1 // 1-indexed
		endLine := end     // Inclusive

		chunk := &Chunk{
			ID:          generateChunkID(file.Path, chunkContent),
			FilePath:    file.Path,
			Content:     chunkContent,
			RawContent:  chunkContent,
			Context:     "",
			ContentType: ContentTypeText,
			Language:    file.Language,
			StartLine:   startLine,
			EndLine:     endLine,
			Symbols:     nil,
			Metadata:    make(map[string]string),
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		chunks = append(chunks, chunk)

		// Move forward with overlap
		i = end - overlapLines
		if i <= 0 || end >= len(lines) {
			break
		}
	}

	return chunks, nil
}

// generateChunkID generates a content-addressable chunk ID from file path and content.
// The ID is derived from filePath and content hash, making it stable across line number
// shifts while preserving file context. This is critical for checkpoint/resume to work
// correctly when files are modified between indexing sessions (BUG-052).
//
// Properties:
//   - Same content in same file = same ID (stable across line shifts)
//   - Different content in same file = different ID (triggers re-embedding)
//   - Same content in different files = different IDs (preserves file context)
func generateChunkID(filePath string, content string) string {
	// Hash the content first
	contentHash := sha256.Sum256([]byte(content))
	contentHashStr := hex.EncodeToString(contentHash[:])[:16]

	// Combine with file path for uniqueness per file
	input := fmt.Sprintf("%s:%s", filePath, contentHashStr)
	hash := sha256.Sum256([]byte(input))
	return hex.EncodeToString(hash[:])[:16]
}

// estimateTokens estimates the number of tokens in content
func estimateTokens(content string) int {
	return len(content) / TokensPerChar
}

// combineContextAndContent combines context and raw content into full content
func combineContextAndContent(context, rawContent string) string {
	if context == "" {
		return rawContent
	}
	return context + "\n\n" + rawContent
}

// enrichContextWithFilePath prepends a file path marker to the context.
// This helps embedding models understand file location and scope.
// The marker format is language-appropriate (// for Go/JS/TS, # for Python).
func (c *CodeChunker) enrichContextWithFilePath(filePath, language, existingContext string) string {
	if filePath == "" {
		return existingContext
	}

	// Use language-appropriate comment syntax
	var marker string
	switch language {
	case "python":
		marker = fmt.Sprintf("# File: %s", filePath)
	default:
		// Go, TypeScript, JavaScript, etc. use //
		marker = fmt.Sprintf("// File: %s", filePath)
	}

	if existingContext == "" {
		return marker
	}
	return marker + "\n" + existingContext
}
