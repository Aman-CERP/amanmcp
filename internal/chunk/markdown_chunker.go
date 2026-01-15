package chunk

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"
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

	// Extract frontmatter if present
	if frontmatterMatch := frontmatterPattern.FindStringSubmatch(remainingContent); frontmatterMatch != nil {
		frontmatter := frontmatterMatch[0]
		chunk := c.createFrontmatterChunk(file, frontmatter, now)
		chunks = append(chunks, chunk)
		remainingContent = remainingContent[len(frontmatter):]
	}

	// Find all headers with their positions
	sections := c.parseSections(remainingContent)

	if len(sections) == 0 {
		// No headers - chunk by paragraphs
		paragraphChunks := c.chunkByParagraphs(file, remainingContent, "", 1, now)
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
		sectionChunks := c.createSectionChunks(file, section, baseLineOffset, now)
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

// createFrontmatterChunk creates a chunk for YAML frontmatter
func (c *MarkdownChunker) createFrontmatterChunk(file *FileInput, content string, now time.Time) *Chunk {
	// Count lines in frontmatter
	lineCount := strings.Count(content, "\n")
	if lineCount == 0 {
		lineCount = 1
	}

	return &Chunk{
		ID:          generateChunkID(file.Path, content),
		FilePath:    file.Path,
		Content:     content,
		RawContent:  content,
		ContentType: ContentTypeMarkdown,
		Language:    "markdown",
		StartLine:   1,
		EndLine:     lineCount,
		Metadata: map[string]string{
			"type":         "frontmatter",
			"header_path":  "",
			"header_level": "0",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// createSectionChunks creates one or more chunks from a section
func (c *MarkdownChunker) createSectionChunks(file *FileInput, sec *section, baseLineOffset int, now time.Time) []*Chunk {
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
			ID:          generateChunkID(file.Path, content),
			FilePath:    file.Path,
			Content:     content,
			RawContent:  content,
			ContentType: ContentTypeMarkdown,
			Language:    "markdown",
			StartLine:   startLine,
			EndLine:     endLine,
			Metadata: map[string]string{
				"header_path":   sec.headerPath,
				"header_level":  strconv.Itoa(sec.headerLevel),
				"section_title": sec.headerTitle,
			},
			CreatedAt: now,
			UpdatedAt: now,
		}
		return []*Chunk{chunk}
	}

	// Section too large - split by paragraphs
	startLine := baseLineOffset + sec.startLine
	return c.splitLargeSection(file, sec, content, startLine, now)
}

// splitLargeSection splits a large section into multiple chunks
func (c *MarkdownChunker) splitLargeSection(file *FileInput, sec *section, content string, startLine int, now time.Time) []*Chunk {
	// Find atomic blocks (code blocks, tables, MDX components)
	atomicBlocks := c.findAtomicBlocks(content)

	// Split by paragraphs (blank lines) while preserving atomic blocks
	paragraphs := c.splitByParagraphs(content, atomicBlocks)

	var chunks []*Chunk
	var currentContent strings.Builder
	currentStartLine := startLine
	lineCount := 0

	for i, para := range paragraphs {
		paraLines := strings.Count(para, "\n") + 1
		paraTokens := estimateTokens(para)
		currentTokens := estimateTokens(currentContent.String())

		// If adding this paragraph would exceed the limit, create a chunk
		if currentContent.Len() > 0 && currentTokens+paraTokens > c.options.MaxChunkTokens {
			chunk := c.createChunkFromContent(file, sec, currentContent.String(), currentStartLine, lineCount, now)
			chunks = append(chunks, chunk)

			// Reset for next chunk (with some overlap context)
			currentContent.Reset()
			currentStartLine = startLine + lineCount

			// Add section context to continuation chunks
			if i > 0 {
				currentContent.WriteString("<!-- Section: ")
				currentContent.WriteString(sec.headerPath)
				currentContent.WriteString(" -->\n\n")
			}
		}

		currentContent.WriteString(para)
		currentContent.WriteString("\n\n")
		lineCount += paraLines + 1 // +1 for the blank line between paragraphs
	}

	// Create final chunk
	if currentContent.Len() > 0 {
		chunk := c.createChunkFromContent(file, sec, currentContent.String(), currentStartLine, lineCount, now)
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

// createChunkFromContent creates a chunk from content string
func (c *MarkdownChunker) createChunkFromContent(file *FileInput, sec *section, content string, startLine, lineCount int, now time.Time) *Chunk {
	content = strings.TrimRight(content, "\n ")

	return &Chunk{
		ID:          generateChunkID(file.Path, content),
		FilePath:    file.Path,
		Content:     content,
		RawContent:  content,
		ContentType: ContentTypeMarkdown,
		Language:    "markdown",
		StartLine:   startLine,
		EndLine:     startLine + lineCount,
		Metadata: map[string]string{
			"header_path":   sec.headerPath,
			"header_level":  strconv.Itoa(sec.headerLevel),
			"section_title": sec.headerTitle,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// chunkByParagraphs chunks content without headers by paragraphs
func (c *MarkdownChunker) chunkByParagraphs(file *FileInput, content, headerPath string, startLine int, now time.Time) []*Chunk {
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
			chunk := &Chunk{
				ID:          generateChunkID(file.Path, chunkContent),
				FilePath:    file.Path,
				Content:     chunkContent,
				RawContent:  chunkContent,
				ContentType: ContentTypeMarkdown,
				Language:    "markdown",
				StartLine:   currentStartLine,
				EndLine:     currentStartLine + lineCount,
				Metadata: map[string]string{
					"header_path":   headerPath,
					"header_level":  "0",
					"section_title": "",
				},
				CreatedAt: now,
				UpdatedAt: now,
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
		chunk := &Chunk{
			ID:          generateChunkID(file.Path, finalContent),
			FilePath:    file.Path,
			Content:     finalContent,
			RawContent:  finalContent,
			ContentType: ContentTypeMarkdown,
			Language:    "markdown",
			StartLine:   currentStartLine,
			EndLine:     currentStartLine + lineCount,
			Metadata: map[string]string{
				"header_path":   headerPath,
				"header_level":  "0",
				"section_title": "",
			},
			CreatedAt: now,
			UpdatedAt: now,
		}
		chunks = append(chunks, chunk)
	}

	return chunks
}
