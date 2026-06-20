package chunk

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// MarkdownChunkerOptions configures the markdown chunker behavior
type MarkdownChunkerOptions struct {
	MaxChunkTokens int // Maximum tokens per chunk (default: DefaultMaxChunkTokens)
	OverlapTokens  int // Overlap between chunks when splitting (default: DefaultOverlapTokens)
}

// MarkdownChunker implements header-based Markdown chunking
type MarkdownChunker struct {
	options MarkdownChunkerOptions
}

// Regex patterns for markdown parsing
var (
	// Matches headers: # Title, ## Title, etc.
	headerPattern = regexp.MustCompile(`(?m)^(#{1,6})\s+(.+)$`)

	// Matches frontmatter: ---\n...\n---
	frontmatterPattern = regexp.MustCompile(`(?s)^---\n(.+?)\n---\n*`)

	// Matches fenced code blocks (including metadata)
	codeBlockPattern = regexp.MustCompile("(?s)```[^`]*```")

	// Matches MDX self-closing components: <Component ... />
	mdxSelfClosingPattern = regexp.MustCompile(`<[A-Z][a-zA-Z0-9]*[^>]*/\s*>`)

	// Matches tables (header row with |)
	tablePattern = regexp.MustCompile(`(?m)^\|.+\|$(\n^\|[-:|]+\|$)?(\n^\|.+\|$)*`)
)

// NewMarkdownChunker creates a new markdown chunker with default options
func NewMarkdownChunker() *MarkdownChunker {
	return NewMarkdownChunkerWithOptions(MarkdownChunkerOptions{})
}

// NewMarkdownChunkerWithOptions creates a new markdown chunker with custom options
func NewMarkdownChunkerWithOptions(opts MarkdownChunkerOptions) *MarkdownChunker {
	if opts.MaxChunkTokens == 0 {
		opts.MaxChunkTokens = DefaultMaxChunkTokens
	}
	if opts.OverlapTokens == 0 {
		opts.OverlapTokens = DefaultOverlapTokens
	}
	return &MarkdownChunker{options: opts}
}

// Close releases chunker resources.
// MarkdownChunker is stateless, so this is a no-op for interface consistency with CodeChunker.
func (c *MarkdownChunker) Close() {
	// No resources to release - MarkdownChunker is stateless
}

// SupportedExtensions returns file extensions this chunker handles
func (c *MarkdownChunker) SupportedExtensions() []string {
	return []string{".md", ".markdown", ".mdx"}
}

// Chunk splits a markdown file into semantic chunks
func (c *MarkdownChunker) Chunk(ctx context.Context, file *FileInput) ([]*Chunk, error) {
	content := string(file.Content)

	// Handle empty or whitespace-only content
	if strings.TrimSpace(content) == "" {
		return nil, nil
	}

	var chunks []*Chunk
	now := time.Now()
	remainingContent := content
	var frontmatterMetadata map[string]string

	// Extract frontmatter if present
	if frontmatterMatch := frontmatterPattern.FindStringSubmatch(remainingContent); frontmatterMatch != nil {
		frontmatter := frontmatterMatch[0]
		parsedMetadata, parsed := parseFrontmatterMetadata(frontmatterMatch[1])
		if parsed {
			frontmatterMetadata = parsedMetadata
		}
		chunk := c.createFrontmatterChunk(file, frontmatter, frontmatterMetadata, parsed, now)
		chunks = append(chunks, chunk)
		remainingContent = remainingContent[len(frontmatter):]
	}

	// Find all headers with their positions
	sections := c.parseSections(remainingContent)

	if len(sections) == 0 {
		// No headers - chunk by paragraphs
		paragraphChunks := c.chunkByParagraphs(file, remainingContent, "", 1, frontmatterMetadata, now)
		chunks = append(chunks, paragraphChunks...)
		return chunks, nil
	}

	// Calculate base line offset (after frontmatter)
	baseLineOffset := 1
	if len(chunks) > 0 && chunks[0].Metadata["type"] == "frontmatter" {
		// Count lines in frontmatter
		baseLineOffset = strings.Count(content[:len(content)-len(remainingContent)], "\n") + 1
	}

	// Create chunks from sections
	for _, section := range sections {
		sectionChunks := c.createSectionChunks(file, section, baseLineOffset, frontmatterMetadata, now)
		chunks = append(chunks, sectionChunks...)
	}

	return chunks, nil
}

// section represents a markdown section with header info
type section struct {
	headerLevel int
	headerTitle string
	headerPath  string
	content     string
	startLine   int // Line number within the content (0-indexed)
}

// parseSections parses markdown content into sections
func (c *MarkdownChunker) parseSections(content string) []*section {
	lines := strings.Split(content, "\n")
	var sections []*section
	headerStack := make([]string, 6) // Stack for header hierarchy (levels 1-6)

	var currentSection *section
	var contentBuilder strings.Builder

	for lineNum, line := range lines {
		if match := headerPattern.FindStringSubmatch(line); match != nil {
			// Save previous section if exists
			if currentSection != nil {
				currentSection.content = contentBuilder.String()
				sections = append(sections, currentSection)
				contentBuilder.Reset()
			}

			level := len(match[1])
			title := strings.TrimSpace(match[2])

			// Update header stack (clear deeper levels)
			headerStack[level-1] = title
			for i := level; i < 6; i++ {
				headerStack[i] = ""
			}

			// Build header path
			var pathParts []string
			for i := 0; i < level; i++ {
				if headerStack[i] != "" {
					pathParts = append(pathParts, headerStack[i])
				}
			}
			headerPath := strings.Join(pathParts, " > ")

			currentSection = &section{
				headerLevel: level,
				headerTitle: title,
				headerPath:  headerPath,
				startLine:   lineNum,
			}
			contentBuilder.WriteString(line)
			contentBuilder.WriteString("\n")
		} else if currentSection != nil {
			contentBuilder.WriteString(line)
			contentBuilder.WriteString("\n")
		} else {
			// Content before any header
			contentBuilder.WriteString(line)
			contentBuilder.WriteString("\n")
		}
	}

	// Save last section
	if currentSection != nil {
		currentSection.content = contentBuilder.String()
		sections = append(sections, currentSection)
	}

	return sections
}

// createFrontmatterChunk creates a metadata carrier for YAML frontmatter.
func (c *MarkdownChunker) createFrontmatterChunk(file *FileInput, content string, frontmatterMetadata map[string]string, parsed bool, now time.Time) *Chunk {
	// Count lines in frontmatter
	lineCount := strings.Count(content, "\n")
	if lineCount == 0 {
		lineCount = 1
	}

	chunkContent := content
	if parsed {
		chunkContent = ""
	}
	metadata := map[string]string{
		"type":         "frontmatter",
		"header_path":  "",
		"header_level": "0",
	}
	copyMetadata(metadata, frontmatterMetadata)

	return &Chunk{
		ID:          generateChunkID(file.Path, content),
		FilePath:    file.Path,
		Content:     chunkContent,
		RawContent:  chunkContent,
		ContentType: ContentTypeMarkdown,
		Language:    "markdown",
		StartLine:   1,
		EndLine:     lineCount,
		Metadata:    metadata,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// createSectionChunks creates one or more chunks from a section
func (c *MarkdownChunker) createSectionChunks(file *FileInput, sec *section, baseLineOffset int, frontmatterMetadata map[string]string, now time.Time) []*Chunk {
	content := strings.TrimRight(sec.content, "\n")

	// Skip empty sections (only header, no content)
	// Return empty slice, not nil, for consistent API behavior (DEBT-012)
	trimmedContent := strings.TrimSpace(content)
	lines := strings.Split(trimmedContent, "\n")
	if len(lines) <= 1 && headerPattern.MatchString(trimmedContent) {
		// Only contains the header itself
		return []*Chunk{}
	}

	tokens := estimateTokens(content)

	if tokens <= c.options.MaxChunkTokens {
		// Section fits in one chunk
		startLine := baseLineOffset + sec.startLine
		endLine := startLine + strings.Count(content, "\n")

		chunk := &Chunk{
			ID:          generateChunkIDWithDisambiguator(file.Path, content, sec.headerPath),
			FilePath:    file.Path,
			Content:     content,
			RawContent:  content,
			ContentType: ContentTypeMarkdown,
			Language:    "markdown",
			StartLine:   startLine,
			EndLine:     endLine,
			Metadata:    c.sectionMetadata(sec, frontmatterMetadata),
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		return []*Chunk{chunk}
	}

	// Section too large - split by paragraphs
	startLine := baseLineOffset + sec.startLine
	return c.splitLargeSection(file, sec, content, startLine, frontmatterMetadata, now)
}

// splitLargeSection splits a large section into multiple chunks
func (c *MarkdownChunker) splitLargeSection(file *FileInput, sec *section, content string, startLine int, frontmatterMetadata map[string]string, now time.Time) []*Chunk {
	// Find atomic blocks (code blocks, tables, MDX components)
	atomicBlocks := c.findAtomicBlocks(content)

	// Split by paragraphs (blank lines) while preserving atomic blocks
	paragraphs := c.splitByParagraphs(content, atomicBlocks)

	var chunks []*Chunk
	var currentContent strings.Builder
	currentStartLine := startLine
	lineCount := 0

	for _, para := range paragraphs {
		paraLines := strings.Count(para, "\n") + 1
		paraTokens := estimateTokens(para)
		currentTokens := estimateTokens(currentContent.String())

		// If adding this paragraph would exceed the limit, create a chunk
		if currentContent.Len() > 0 && currentTokens+paraTokens > c.options.MaxChunkTokens {
			chunk := c.createChunkFromContent(file, sec, currentContent.String(), currentStartLine, lineCount, len(chunks), frontmatterMetadata, now)
			chunks = append(chunks, chunk)

			// Reset for next chunk (with some overlap context)
			currentContent.Reset()
			currentStartLine = startLine + lineCount
		}

		if paraTokens > c.options.MaxChunkTokens && !c.isAtomicParagraph(para) {
			segments := c.splitOversizedParagraph(para)
			for _, segment := range segments {
				segmentTokens := estimateTokens(segment)
				currentTokens = estimateTokens(currentContent.String())
				if currentContent.Len() > 0 && currentTokens+segmentTokens > c.options.MaxChunkTokens {
					chunk := c.createChunkFromContent(file, sec, currentContent.String(), currentStartLine, lineCount, len(chunks), frontmatterMetadata, now)
					chunks = append(chunks, chunk)
					currentContent.Reset()
					currentStartLine = startLine + lineCount
				}
				currentContent.WriteString(segment)
				currentContent.WriteString("\n\n")
			}
		} else {
			currentContent.WriteString(para)
			currentContent.WriteString("\n\n")
		}
		lineCount += paraLines + 1 // +1 for the blank line between paragraphs
	}

	// Create final chunk
	if currentContent.Len() > 0 {
		chunk := c.createChunkFromContent(file, sec, currentContent.String(), currentStartLine, lineCount, len(chunks), frontmatterMetadata, now)
		chunks = append(chunks, chunk)
	}

	return chunks
}

// findAtomicBlocks finds positions of blocks that shouldn't be split
func (c *MarkdownChunker) findAtomicBlocks(content string) [][]int {
	var blocks [][]int

	// Find code blocks
	blocks = append(blocks, codeBlockPattern.FindAllStringIndex(content, -1)...)

	// Find tables
	blocks = append(blocks, tablePattern.FindAllStringIndex(content, -1)...)

	// Find MDX self-closing components
	blocks = append(blocks, mdxSelfClosingPattern.FindAllStringIndex(content, -1)...)

	// Find MDX block components using a simpler approach
	// Match patterns like <Component>...</Component> for common component names
	blocks = append(blocks, c.findMDXBlockComponents(content)...)

	return blocks
}

// findMDXBlockComponents finds MDX block components without backreferences
func (c *MarkdownChunker) findMDXBlockComponents(content string) [][]int {
	var locs [][]int

	// Simple approach: find opening tags and their matching closing tags
	// Pattern: <ComponentName where ComponentName starts with uppercase
	openTagPattern := regexp.MustCompile(`<([A-Z][a-zA-Z0-9]*)[^/>]*>`)
	matches := openTagPattern.FindAllStringSubmatchIndex(content, -1)

	for _, match := range matches {
		if len(match) >= 4 {
			tagName := content[match[2]:match[3]]
			closeTag := "</" + tagName + ">"
			startPos := match[0]

			// Find the matching close tag
			closePos := strings.Index(content[match[1]:], closeTag)
			if closePos != -1 {
				endPos := match[1] + closePos + len(closeTag)
				locs = append(locs, []int{startPos, endPos})
			}
		}
	}

	return locs
}

// splitByParagraphs splits content by blank lines while preserving atomic blocks
func (c *MarkdownChunker) splitByParagraphs(content string, atomicBlocks [][]int) []string {
	// Simple split by blank lines for now
	// We protect atomic blocks by not splitting within them
	parts := strings.Split(content, "\n\n")

	var paragraphs []string
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			paragraphs = append(paragraphs, trimmed)
		}
	}

	// Merge atomic blocks that were split
	paragraphs = c.mergeAtomicBlocks(paragraphs)

	return paragraphs
}

// mergeAtomicBlocks merges paragraphs that are part of atomic blocks
func (c *MarkdownChunker) mergeAtomicBlocks(paragraphs []string) []string {
	var result []string
	var inCodeBlock bool
	var codeBlockBuilder strings.Builder

	for _, para := range paragraphs {
		if inCodeBlock {
			codeBlockBuilder.WriteString("\n\n")
			codeBlockBuilder.WriteString(para)
			if strings.Contains(para, "```") {
				// End of code block
				result = append(result, codeBlockBuilder.String())
				codeBlockBuilder.Reset()
				inCodeBlock = false
			}
			continue
		}

		// Check if paragraph starts a code block but doesn't end it
		openCount := strings.Count(para, "```")
		if openCount > 0 && openCount%2 == 1 {
			// Unclosed code block
			inCodeBlock = true
			codeBlockBuilder.WriteString(para)
			continue
		}

		result = append(result, para)
	}

	// Handle unclosed code block (shouldn't happen with valid markdown)
	if inCodeBlock {
		result = append(result, codeBlockBuilder.String())
	}

	return result
}

func (c *MarkdownChunker) isAtomicParagraph(paragraph string) bool {
	trimmed := strings.TrimSpace(paragraph)
	return strings.HasPrefix(trimmed, "```") ||
		strings.HasPrefix(trimmed, "|") ||
		mdxSelfClosingPattern.MatchString(trimmed) ||
		len(c.findMDXBlockComponents(trimmed)) > 0
}

func (c *MarkdownChunker) splitOversizedParagraph(paragraph string) []string {
	sentences := splitBySentenceBoundary(paragraph)
	if len(sentences) == 0 {
		return nil
	}

	var segments []string
	var current strings.Builder
	for _, sentence := range sentences {
		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			continue
		}
		if estimateTokens(sentence) > c.options.MaxChunkTokens {
			if current.Len() > 0 {
				segments = append(segments, strings.TrimSpace(current.String()))
				current.Reset()
			}
			segments = append(segments, c.splitOversizedTextByWords(sentence)...)
			continue
		}
		if current.Len() > 0 && estimateTokens(current.String()+" "+sentence) > c.options.MaxChunkTokens {
			segments = append(segments, strings.TrimSpace(current.String()))
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteString(" ")
		}
		current.WriteString(sentence)
	}
	if current.Len() > 0 {
		segments = append(segments, strings.TrimSpace(current.String()))
	}
	return segments
}

func (c *MarkdownChunker) splitOversizedTextByWords(text string) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}

	maxChars := c.options.MaxChunkTokens * TokensPerChar
	if maxChars <= 0 {
		maxChars = DefaultMaxChunkTokens * TokensPerChar
	}

	var segments []string
	var current strings.Builder
	for _, word := range words {
		if len(word) > maxChars {
			if current.Len() > 0 {
				segments = append(segments, strings.TrimSpace(current.String()))
				current.Reset()
			}
			segments = append(segments, splitLongWord(word, maxChars)...)
			continue
		}

		candidate := word
		if current.Len() > 0 {
			candidate = current.String() + " " + word
		}
		if current.Len() > 0 && estimateTokens(candidate) > c.options.MaxChunkTokens {
			segments = append(segments, strings.TrimSpace(current.String()))
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteString(" ")
		}
		current.WriteString(word)
	}
	if current.Len() > 0 {
		segments = append(segments, strings.TrimSpace(current.String()))
	}
	return segments
}

func splitBySentenceBoundary(text string) []string {
	var sentences []string
	start := 0
	for i, r := range text {
		if r != '.' && r != '!' && r != '?' {
			continue
		}
		end := i + len(string(r))
		if end < len(text) {
			next := text[end]
			if next != ' ' && next != '\n' && next != '\t' {
				continue
			}
		}
		sentences = append(sentences, strings.TrimSpace(text[start:end]))
		start = end
	}
	if start < len(text) {
		sentences = append(sentences, strings.TrimSpace(text[start:]))
	}
	return sentences
}

// createChunkFromContent creates a chunk from content string
func (c *MarkdownChunker) createChunkFromContent(file *FileInput, sec *section, content string, startLine, lineCount, ordinal int, frontmatterMetadata map[string]string, now time.Time) *Chunk {
	content = strings.TrimRight(content, "\n ")

	return &Chunk{
		ID:          generateChunkIDWithDisambiguator(file.Path, content, fmt.Sprintf("%s:%d", sec.headerPath, ordinal)),
		FilePath:    file.Path,
		Content:     content,
		RawContent:  content,
		ContentType: ContentTypeMarkdown,
		Language:    "markdown",
		StartLine:   startLine,
		EndLine:     startLine + lineCount,
		Metadata:    c.sectionMetadata(sec, frontmatterMetadata),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func (c *MarkdownChunker) sectionMetadata(sec *section, frontmatterMetadata map[string]string) map[string]string {
	metadata := map[string]string{
		"header_path":   sec.headerPath,
		"header_level":  strconv.Itoa(sec.headerLevel),
		"section_title": sec.headerTitle,
	}
	copyMetadata(metadata, frontmatterMetadata)
	return metadata
}

func copyMetadata(dst map[string]string, src map[string]string) {
	for key, value := range src {
		dst[key] = value
	}
}

func parseFrontmatterMetadata(body string) (map[string]string, bool) {
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(body), &root); err != nil {
		return nil, false
	}
	metadata := make(map[string]string)
	if len(root.Content) == 0 {
		return metadata, true
	}
	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return metadata, true
	}
	for i := 0; i+1 < len(doc.Content); i += 2 {
		key := strings.TrimSpace(doc.Content[i].Value)
		if key == "" {
			continue
		}
		value, ok := encodeFrontmatterValue(doc.Content[i+1])
		if !ok {
			return nil, false
		}
		metadata["fm."+key] = value
	}
	return metadata, true
}

func encodeFrontmatterValue(node *yaml.Node) (string, bool) {
	if node == nil {
		return "", true
	}
	if node.Kind == yaml.ScalarNode {
		return node.Value, true
	}
	value := normalizeYAMLNode(node)
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", false
	}
	return string(encoded), true
}

func normalizeYAMLNode(node *yaml.Node) any {
	if node == nil {
		return nil
	}
	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) == 0 {
			return nil
		}
		return normalizeYAMLNode(node.Content[0])
	case yaml.MappingNode:
		values := make(map[string]any, len(node.Content)/2)
		for i := 0; i+1 < len(node.Content); i += 2 {
			values[node.Content[i].Value] = normalizeYAMLNode(node.Content[i+1])
		}
		return values
	case yaml.SequenceNode:
		values := make([]any, 0, len(node.Content))
		for _, child := range node.Content {
			values = append(values, normalizeYAMLNode(child))
		}
		return values
	case yaml.ScalarNode:
		return node.Value
	case yaml.AliasNode:
		return normalizeYAMLNode(node.Alias)
	default:
		return node.Value
	}
}

// chunkByParagraphs chunks content without headers by paragraphs
func (c *MarkdownChunker) chunkByParagraphs(file *FileInput, content, headerPath string, startLine int, frontmatterMetadata map[string]string, now time.Time) []*Chunk {
	// Split by blank lines
	paragraphs := strings.Split(content, "\n\n")

	var chunks []*Chunk
	var currentContent strings.Builder
	currentStartLine := startLine
	lineCount := 0

	for _, para := range paragraphs {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}

		paraLines := strings.Count(para, "\n") + 1
		paraTokens := estimateTokens(para)
		currentTokens := estimateTokens(currentContent.String())

		// If adding this paragraph would exceed the limit, create a chunk
		if currentContent.Len() > 0 && currentTokens+paraTokens > c.options.MaxChunkTokens {
			chunkContent := currentContent.String()
			metadata := map[string]string{
				"header_path":   headerPath,
				"header_level":  "0",
				"section_title": "",
			}
			copyMetadata(metadata, frontmatterMetadata)
			chunk := &Chunk{
				ID:          generateChunkID(file.Path, chunkContent),
				FilePath:    file.Path,
				Content:     chunkContent,
				RawContent:  chunkContent,
				ContentType: ContentTypeMarkdown,
				Language:    "markdown",
				StartLine:   currentStartLine,
				EndLine:     currentStartLine + lineCount,
				Metadata:    metadata,
				CreatedAt:   now,
				UpdatedAt:   now,
			}
			chunks = append(chunks, chunk)

			currentContent.Reset()
			currentStartLine = startLine + lineCount
		}

		if currentContent.Len() > 0 {
			currentContent.WriteString("\n\n")
		}
		currentContent.WriteString(para)
		lineCount += paraLines + 1
	}

	// Create final chunk
	if currentContent.Len() > 0 {
		finalContent := currentContent.String()
		metadata := map[string]string{
			"header_path":   headerPath,
			"header_level":  "0",
			"section_title": "",
		}
		copyMetadata(metadata, frontmatterMetadata)
		chunk := &Chunk{
			ID:          generateChunkID(file.Path, finalContent),
			FilePath:    file.Path,
			Content:     finalContent,
			RawContent:  finalContent,
			ContentType: ContentTypeMarkdown,
			Language:    "markdown",
			StartLine:   currentStartLine,
			EndLine:     currentStartLine + lineCount,
			Metadata:    metadata,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		chunks = append(chunks, chunk)
	}

	return chunks
}
