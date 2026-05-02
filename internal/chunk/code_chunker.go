package chunk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/Aman-CERP/amanmcp/internal/language"
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
	registry := NewLanguageRegistry()
	return NewCodeChunkerWithRegistry(opts, registry)
}

// NewCodeChunkerWithLanguageDefinitions creates a chunker using validated
// built-ins plus user language definitions.
func NewCodeChunkerWithLanguageDefinitions(opts CodeChunkerOptions, defs []language.Definition) (*CodeChunker, error) {
	registry, err := NewLanguageRegistryFromDefinitions(defs)
	if err != nil {
		return nil, err
	}
	return NewCodeChunkerWithRegistry(opts, registry), nil
}

// NewCodeChunkerWithRegistry creates a chunker with a test-isolated registry.
func NewCodeChunkerWithRegistry(opts CodeChunkerOptions, registry *LanguageRegistry) *CodeChunker {
	if opts.MaxChunkTokens == 0 {
		opts.MaxChunkTokens = DefaultMaxChunkTokens
	}
	if opts.OverlapTokens == 0 {
		opts.OverlapTokens = DefaultOverlapTokens
	}
	if registry == nil {
		registry = NewLanguageRegistry()
	}
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

	config, supported := c.registry.ResolveForFile(file.Path, file.Language)
	if !supported {
		// Fall back to line-based chunking
		return c.chunkByLines(file, "legacy_fallback")
	}
	if config.LineFallback {
		return c.chunkByLines(file, config.ConfigSource)
	}

	// Parse the file
	tree, err := c.parser.Parse(ctx, file.Content, config.Name)
	if err != nil {
		// Fall back to line-based chunking on parse error
		return c.chunkByLines(file, config.ConfigSource)
	}

	// Extract context (package declaration, imports)
	fileContext := c.extractFileContext(tree, file.Content, config.Name)

	// Enrich context with file path marker for better embedding quality
	fileContext = c.enrichContextWithFilePath(file.Path, config.Name, fileContext)

	// Find symbol nodes (functions, classes, methods, types)
	symbolNodes := c.findSymbolNodes(tree, config.Name)

	if len(symbolNodes) == 0 {
		return nil, nil
	}

	// Create chunks from symbol nodes
	chunks := make([]*Chunk, 0, len(symbolNodes))
	now := time.Now()

	for _, node := range symbolNodes {
		nodeChunks := c.createChunksFromNode(node, tree, file, fileContext, config, now)
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
	symbolTypes := symbolTypesForConfig(config)
	for _, child := range tree.Root.Children {
		c.collectSymbolNodes(child, tree, language, symbolTypes, &symbolNodes)
	}
	return symbolNodes
}

func symbolTypesForConfig(config *LanguageConfig) map[string]SymbolType {
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
	return symbolTypes
}

func (c *CodeChunker) collectSymbolNodes(n *Node, tree *Tree, language string, symbolTypes map[string]SymbolType, symbolNodes *[]*symbolNodeInfo) {
	// For JS/TS lexical_declaration/variable_declaration, check for arrow functions first.
	if n.Type == "lexical_declaration" || n.Type == "variable_declaration" {
		sym := c.extractor.extractSpecialSymbol(n, tree.Source, language)
		if sym != nil {
			*symbolNodes = append(*symbolNodes, &symbolNodeInfo{node: n, symbol: sym})
			return
		}
	}

	if symType, isSymbol := symbolTypes[n.Type]; isSymbol {
		sym := c.extractSymbol(n, tree, symType, language)
		if sym != nil {
			*symbolNodes = append(*symbolNodes, &symbolNodeInfo{node: n, symbol: sym})
			return
		}
	}

	for _, child := range n.Children {
		c.collectSymbolNodes(child, tree, language, symbolTypes, symbolNodes)
	}
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
func (c *CodeChunker) createChunksFromNode(info *symbolNodeInfo, tree *Tree, file *FileInput, fileContext string, config *LanguageConfig, now time.Time) []*Chunk {
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
		chunk := c.createChunk(file, rawContentWithDoc, fileContext, info.symbol, config, now)
		return []*Chunk{chunk}
	}

	// Need to split large symbol
	return c.splitLargeSymbol(info, tree, file, fileContext, config, now)
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

const maxCASTSplitDepth = 8

// splitLargeSymbol splits a large symbol into multiple chunks
func (c *CodeChunker) splitLargeSymbol(info *symbolNodeInfo, tree *Tree, file *FileInput, fileContext string, config *LanguageConfig, now time.Time) []*Chunk {
	return c.splitSymbolRecursive(info, nil, tree, file, fileContext, config, now, 0)
}

func (c *CodeChunker) splitSymbolRecursive(info *symbolNodeInfo, ancestors []*Symbol, tree *Tree, file *FileInput, fileContext string, config *LanguageConfig, now time.Time, depth int) []*Chunk {
	node := info.node
	content := string(tree.Source[node.StartByte:node.EndByte])

	childSymbols := c.findSemanticChildSymbols(info, tree, config)
	if len(childSymbols) > 0 && depth < maxCASTSplitDepth {
		return c.splitBySemanticChildren(info, ancestors, childSymbols, tree, file, fileContext, config, now, depth)
	}

	// Fall back to line-based splitting with overlap
	reason := "no_semantic_children"
	if depth >= maxCASTSplitDepth {
		reason = "max_ast_split_depth"
	}
	chunks := c.splitByLines(content, info.symbol, file, fileContext, config, now, int(node.StartPoint.Row)+1, reason)
	if len(ancestors) > 0 {
		c.placeParentSymbolsOnce(chunks, ancestors...)
		for _, chunk := range chunks {
			chunk.Metadata["parent_symbol"] = ancestors[len(ancestors)-1].Name
		}
	}
	return chunks
}

func (c *CodeChunker) findSemanticChildSymbols(parent *symbolNodeInfo, tree *Tree, config *LanguageConfig) []*symbolNodeInfo {
	var children []*symbolNodeInfo
	symbolTypes := symbolTypesForConfig(config)
	for _, child := range parent.node.Children {
		c.collectSymbolNodes(child, tree, tree.Language, symbolTypes, &children)
	}
	return children
}

func (c *CodeChunker) splitBySemanticChildren(parent *symbolNodeInfo, ancestors []*Symbol, children []*symbolNodeInfo, tree *Tree, file *FileInput, fileContext string, config *LanguageConfig, now time.Time, depth int) []*Chunk {
	chunks := make([]*Chunk, 0, len(children))
	parentSymbols := appendSymbol(ancestors, parent.symbol)
	parentPlaced := false
	for _, child := range children {
		rawContent := string(tree.Source[child.node.StartByte:child.node.EndByte])
		if child.symbol.DocComment != "" {
			rawContent = c.getRawContentWithDocComment(child.node, tree.Source, child.symbol.DocComment)
		}
		var produced []*Chunk
		if estimateTokens(rawContent) > c.options.MaxChunkTokens {
			produced = c.splitSymbolRecursive(child, parentSymbols, tree, file, fileContext, config, now, depth+1)
		} else {
			produced = []*Chunk{c.createSplitChunk(file, rawContent, fileContext, child.symbol, parent.symbol, config, now, false)}
		}
		if !parentPlaced && len(produced) > 0 {
			c.placeParentSymbolsOnce(produced, parentSymbols...)
			parentPlaced = true
		}
		for _, chunk := range produced {
			chunk.Metadata["parent_symbol"] = parent.symbol.Name
		}
		chunks = append(chunks, produced...)
	}
	return chunks
}

func appendSymbol(symbols []*Symbol, symbol *Symbol) []*Symbol {
	out := make([]*Symbol, 0, len(symbols)+1)
	out = append(out, symbols...)
	out = append(out, symbol)
	return out
}

func (c *CodeChunker) placeParentSymbolsOnce(chunks []*Chunk, parents ...*Symbol) {
	if len(chunks) == 0 {
		return
	}
	for _, parent := range parents {
		c.placeParentSymbol(chunks[0], parent)
	}
}

func (c *CodeChunker) placeParentSymbol(chunk *Chunk, parent *Symbol) {
	if chunk == nil || parent == nil {
		return
	}
	for _, sym := range chunk.Symbols {
		if sym.Name == parent.Name && sym.Type == parent.Type {
			return
		}
	}
	chunk.Symbols = append(chunk.Symbols, &Symbol{
		Name:      parent.Name,
		Type:      parent.Type,
		StartLine: parent.StartLine,
		EndLine:   parent.EndLine,
	})
}

// splitByLines splits content into line-based chunks with overlap
func (c *CodeChunker) splitByLines(content string, symbol *Symbol, file *FileInput, fileContext string, config *LanguageConfig, now time.Time, startLine int, reason string) []*Chunk {
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	// Return empty slice, not nil, for consistent API behavior (DEBT-012)
	if len(lines) == 0 {
		return []*Chunk{}
	}

	overlapLines := (c.options.OverlapTokens * TokensPerChar) / 80
	if overlapLines < 2 {
		overlapLines = 2
	}

	var chunks []*Chunk
	for i := 0; i < len(lines); {
		if estimateTokens(lines[i]) > c.options.MaxChunkTokens {
			lineChunks := c.splitLongLine(lines[i], symbol, file, fileContext, config, now, startLine+i, reason, len(chunks))
			chunks = append(chunks, lineChunks...)
			i++
			continue
		}

		end := i
		var chunkContent string
		for end < len(lines) {
			candidateLines := lines[i : end+1]
			candidate := strings.Join(candidateLines, "\n")
			if estimateTokens(candidate) > c.options.MaxChunkTokens && end > i {
				break
			}
			chunkContent = candidate
			end++
			if estimateTokens(candidate) >= c.options.MaxChunkTokens {
				break
			}
		}
		if chunkContent == "" {
			chunkContent = lines[i]
			end = i + 1
		}
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
			ID:          generateChunkIDWithDisambiguator(file.Path, chunkContent, subSymbol.Name),
			FilePath:    file.Path,
			Content:     combineContextAndContent(fileContext, chunkContent),
			RawContent:  chunkContent,
			Context:     fileContext,
			ContentType: ContentTypeCode,
			Language:    file.Language,
			StartLine:   chunkStartLine,
			EndLine:     chunkEndLine,
			Symbols:     symbols,
			Metadata: map[string]string{
				"chunk_provenance":       "ast",
				"split_strategy":         "line_fallback",
				"split_reason":           reason,
				"parent_symbol":          symbol.Name,
				"language_config_source": config.ConfigSource,
			},
			CreatedAt: now,
			UpdatedAt: now,
		}
		chunks = append(chunks, chunk)

		// Move forward, accounting for overlap
		next := end - overlapLines
		if next <= i {
			next = end
		}
		if end >= len(lines) {
			break
		}
		i = next
	}

	return chunks
}

func (c *CodeChunker) splitLongLine(line string, symbol *Symbol, file *FileInput, fileContext string, config *LanguageConfig, now time.Time, lineNumber int, reason string, existingChunks int) []*Chunk {
	maxChars := c.options.MaxChunkTokens * TokensPerChar
	if maxChars < 1 {
		maxChars = TokensPerChar
	}

	chunks := make([]*Chunk, 0, (len(line)/maxChars)+1)
	for start := 0; start < len(line); start += maxChars {
		end := start + maxChars
		if end > len(line) {
			end = len(line)
		}
		chunkContent := line[start:end]
		partNumber := existingChunks + len(chunks) + 1
		subSymbol := &Symbol{
			Name:      fmt.Sprintf("%s_part%d", symbol.Name, partNumber),
			Type:      symbol.Type,
			StartLine: lineNumber,
			EndLine:   lineNumber,
		}
		symbols := []*Symbol{subSymbol}
		if existingChunks == 0 && len(chunks) == 0 {
			symbols = append(symbols, &Symbol{
				Name:      symbol.Name,
				Type:      symbol.Type,
				StartLine: symbol.StartLine,
				EndLine:   symbol.EndLine,
			})
		}
		chunks = append(chunks, &Chunk{
			ID:          generateChunkIDWithDisambiguator(file.Path, chunkContent, subSymbol.Name),
			FilePath:    file.Path,
			Content:     combineContextAndContent(fileContext, chunkContent),
			RawContent:  chunkContent,
			Context:     fileContext,
			ContentType: ContentTypeCode,
			Language:    file.Language,
			StartLine:   lineNumber,
			EndLine:     lineNumber,
			Symbols:     symbols,
			Metadata: map[string]string{
				"chunk_provenance":       "ast",
				"split_strategy":         "line_fallback",
				"split_reason":           reason,
				"parent_symbol":          symbol.Name,
				"language_config_source": config.ConfigSource,
			},
			CreatedAt: now,
			UpdatedAt: now,
		})
	}
	return chunks
}

// createChunk creates a single chunk from content
func (c *CodeChunker) createChunk(file *FileInput, rawContent, fileContext string, symbol *Symbol, config *LanguageConfig, now time.Time) *Chunk {
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
		Metadata: map[string]string{
			"chunk_provenance":       "ast",
			"language_config_source": config.ConfigSource,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func (c *CodeChunker) createSplitChunk(file *FileInput, rawContent, fileContext string, symbol, parent *Symbol, config *LanguageConfig, now time.Time, includeParentSymbol bool) *Chunk {
	symbols := []*Symbol{symbol}
	if includeParentSymbol {
		symbols = append(symbols, &Symbol{
			Name:      parent.Name,
			Type:      parent.Type,
			StartLine: parent.StartLine,
			EndLine:   parent.EndLine,
		})
	}
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
		Symbols:     symbols,
		Metadata: map[string]string{
			"chunk_provenance":       "ast",
			"split_strategy":         "ast_recursive",
			"split_reason":           "max_tokens_exceeded",
			"parent_symbol":          parent.Name,
			"language_config_source": config.ConfigSource,
		},
		CreatedAt: now,
		UpdatedAt: now,
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
func (c *CodeChunker) chunkByLines(file *FileInput, configSource string) ([]*Chunk, error) {
	content := string(file.Content)
	if strings.TrimSpace(content) == "" {
		return nil, nil
	}

	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return []*Chunk{}, nil
	}

	overlapLines := (c.options.OverlapTokens * TokensPerChar) / 80
	if overlapLines < 2 {
		overlapLines = 2
	}

	var chunks []*Chunk
	now := time.Now()

	for i := 0; i < len(lines); {
		if estimateTokens(lines[i]) > c.options.MaxChunkTokens {
			chunks = append(chunks, c.splitFallbackLongLine(file, lines[i], configSource, now, i+1, len(chunks))...)
			i++
			continue
		}

		end := i
		var chunkContent string
		for end < len(lines) {
			candidate := strings.Join(lines[i:end+1], "\n")
			if estimateTokens(candidate) > c.options.MaxChunkTokens && end > i {
				break
			}
			chunkContent = candidate
			end++
			if estimateTokens(candidate) >= c.options.MaxChunkTokens {
				break
			}
		}
		if chunkContent == "" {
			chunkContent = lines[i]
			end = i + 1
		}

		startLine := i + 1 // 1-indexed
		endLine := end     // Inclusive

		chunks = append(chunks, createLineFallbackChunk(file, chunkContent, configSource, now, startLine, endLine, "token_window", fmt.Sprintf("line_fallback_part%d", len(chunks)+1)))

		// Move forward with overlap
		next := end - overlapLines
		if next <= i {
			next = end
		}
		if end >= len(lines) {
			break
		}
		i = next
	}

	return chunks, nil
}

func (c *CodeChunker) splitFallbackLongLine(file *FileInput, line, configSource string, now time.Time, lineNumber, existingChunks int) []*Chunk {
	maxChars := c.options.MaxChunkTokens * TokensPerChar
	if maxChars < 1 {
		maxChars = TokensPerChar
	}
	chunks := make([]*Chunk, 0, (len(line)/maxChars)+1)
	for start := 0; start < len(line); start += maxChars {
		end := start + maxChars
		if end > len(line) {
			end = len(line)
		}
		disambiguator := fmt.Sprintf("line_fallback_part%d", existingChunks+len(chunks)+1)
		chunks = append(chunks, createLineFallbackChunk(file, line[start:end], configSource, now, lineNumber, lineNumber, "long_line", disambiguator))
	}
	return chunks
}

func createLineFallbackChunk(file *FileInput, chunkContent, configSource string, now time.Time, startLine, endLine int, reason, disambiguator string) *Chunk {
	return &Chunk{
		ID:          generateChunkIDWithDisambiguator(file.Path, chunkContent, disambiguator),
		FilePath:    file.Path,
		Content:     chunkContent,
		RawContent:  chunkContent,
		Context:     "",
		ContentType: ContentTypeText,
		Language:    file.Language,
		StartLine:   startLine,
		EndLine:     endLine,
		Symbols:     nil,
		Metadata: map[string]string{
			"chunk_provenance":       "line_fallback",
			"split_reason":           reason,
			"language_config_source": configSource,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
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
	return generateChunkIDWithDisambiguator(filePath, content, "")
}

func generateChunkIDWithDisambiguator(filePath string, content string, disambiguator string) string {
	// Hash the content first
	contentHash := sha256.Sum256([]byte(content))
	contentHashStr := hex.EncodeToString(contentHash[:])[:16]

	// Combine with file path for uniqueness per file
	input := fmt.Sprintf("%s:%s", filePath, contentHashStr)
	if disambiguator != "" {
		input = fmt.Sprintf("%s:%s:%s", filePath, contentHashStr, disambiguator)
	}
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
