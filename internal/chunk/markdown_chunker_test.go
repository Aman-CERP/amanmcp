package chunk

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TS01: Header-Based Splitting
func TestMarkdownChunker_Chunk_HeaderBasedSplitting(t *testing.T) {
	chunker := NewMarkdownChunker()

	content := `# Title

Welcome to the project.

## Section 1

Content for section 1.

## Section 2

Content for section 2.
`

	file := &FileInput{
		Path:     "README.md",
		Content:  []byte(content),
		Language: "markdown",
	}

	chunks, err := chunker.Chunk(context.Background(), file)
	require.NoError(t, err)
	require.Len(t, chunks, 3, "Expected 3 chunks for 3 sections")

	// Check first chunk
	assert.Contains(t, chunks[0].Content, "# Title")
	assert.Contains(t, chunks[0].Content, "Welcome to the project")

	// Check second chunk
	assert.Contains(t, chunks[1].Content, "## Section 1")
	assert.Contains(t, chunks[1].Content, "Content for section 1")

	// Check third chunk
	assert.Contains(t, chunks[2].Content, "## Section 2")
	assert.Contains(t, chunks[2].Content, "Content for section 2")

	// All chunks should be markdown type
	for _, c := range chunks {
		assert.Equal(t, ContentTypeMarkdown, c.ContentType)
		assert.Equal(t, "markdown", c.Language)
		assert.Equal(t, "README.md", c.FilePath)
	}
}

// TS02: Preserve Code Blocks
func TestMarkdownChunker_Chunk_PreserveCodeBlocks(t *testing.T) {
	chunker := NewMarkdownChunker()

	content := "# Installation\n\nInstall using:\n\n```bash\nbrew install myapp\napt-get install myapp\nyum install myapp\n```\n\nThen run:\n\n```bash\nmyapp --version\n```\n"

	file := &FileInput{
		Path:     "INSTALL.md",
		Content:  []byte(content),
		Language: "markdown",
	}

	chunks, err := chunker.Chunk(context.Background(), file)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(chunks), 1)

	// The entire code block should be in one chunk
	found := false
	for _, c := range chunks {
		if strings.Contains(c.Content, "brew install") &&
			strings.Contains(c.Content, "apt-get install") &&
			strings.Contains(c.Content, "yum install") {
			found = true
			break
		}
	}
	assert.True(t, found, "Code block should be intact in one chunk")
}

// TS03: Header Path Tracking
func TestMarkdownChunker_Chunk_HeaderPathTracking(t *testing.T) {
	chunker := NewMarkdownChunker()

	content := `# Top

Intro.

## Middle

Middle content.

### Deep

Deep content.
`

	file := &FileInput{
		Path:     "docs.md",
		Content:  []byte(content),
		Language: "markdown",
	}

	chunks, err := chunker.Chunk(context.Background(), file)
	require.NoError(t, err)
	require.Len(t, chunks, 3)

	// Check header paths in metadata
	assert.Equal(t, "Top", chunks[0].Metadata["header_path"])
	assert.Equal(t, "Top > Middle", chunks[1].Metadata["header_path"])
	assert.Equal(t, "Top > Middle > Deep", chunks[2].Metadata["header_path"])

	// Check header levels
	assert.Equal(t, "1", chunks[0].Metadata["header_level"])
	assert.Equal(t, "2", chunks[1].Metadata["header_level"])
	assert.Equal(t, "3", chunks[2].Metadata["header_level"])

	// Check section titles
	assert.Equal(t, "Top", chunks[0].Metadata["section_title"])
	assert.Equal(t, "Middle", chunks[1].Metadata["section_title"])
	assert.Equal(t, "Deep", chunks[2].Metadata["section_title"])
}

// TS04: Frontmatter Extraction
func TestMarkdownChunker_Chunk_FrontmatterExtraction(t *testing.T) {
	chunker := NewMarkdownChunker()

	content := `---
title: My Document
author: John Doe
date: 2025-01-01
---

# Introduction

Welcome to the document.
`

	file := &FileInput{
		Path:     "doc.md",
		Content:  []byte(content),
		Language: "markdown",
	}

	chunks, err := chunker.Chunk(context.Background(), file)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(chunks), 2)

	// First chunk should be frontmatter
	assert.Contains(t, chunks[0].Content, "title: My Document")
	assert.Contains(t, chunks[0].Content, "author: John Doe")
	assert.Equal(t, "frontmatter", chunks[0].Metadata["type"])

	// Second chunk should be the introduction
	assert.Contains(t, chunks[1].Content, "# Introduction")
}

// TS05: Large Section Split
func TestMarkdownChunker_Chunk_LargeSectionSplit(t *testing.T) {
	chunker := NewMarkdownChunkerWithOptions(MarkdownChunkerOptions{
		MaxChunkTokens: 100, // Very small to force splitting
		OverlapTokens:  10,
	})

	// Create a large section with many paragraphs
	var sb strings.Builder
	sb.WriteString("# Large Section\n\n")
	for i := 0; i < 50; i++ {
		sb.WriteString("This is paragraph number ")
		sb.WriteString(strings.Repeat("word ", 20))
		sb.WriteString(".\n\n")
	}

	file := &FileInput{
		Path:     "large.md",
		Content:  []byte(sb.String()),
		Language: "markdown",
	}

	chunks, err := chunker.Chunk(context.Background(), file)
	require.NoError(t, err)
	require.Greater(t, len(chunks), 1, "Large section should be split into multiple chunks")

	// All chunks should have header context
	for i, c := range chunks {
		if i > 0 {
			// Continuation chunks should reference the section
			assert.Contains(t, c.Metadata["header_path"], "Large Section", "Chunk %d should have header context", i)
		}
	}
}

// TS06: Empty Section Handling
func TestMarkdownChunker_Chunk_EmptySectionHandling(t *testing.T) {
	chunker := NewMarkdownChunker()

	content := `# Header 1

Some intro content.

## Empty Section

## Section With Content

Some content here.
`

	file := &FileInput{
		Path:     "empty.md",
		Content:  []byte(content),
		Language: "markdown",
	}

	chunks, err := chunker.Chunk(context.Background(), file)
	require.NoError(t, err)

	// Empty sections should be handled gracefully (either skipped or minimal)
	// We should get chunks for Header 1 (with intro) and Section With Content
	// Empty Section is skipped because it has no content
	require.GreaterOrEqual(t, len(chunks), 2)

	// Find the section with content
	found := false
	for _, c := range chunks {
		if strings.Contains(c.Content, "Some content here") {
			found = true
			break
		}
	}
	assert.True(t, found, "Section with content should be present")

	// Header 1 should have its intro content
	introFound := false
	for _, c := range chunks {
		if strings.Contains(c.Content, "Some intro content") {
			introFound = true
			break
		}
	}
	assert.True(t, introFound, "Header 1 should include its intro content")
}

// TS07: No Headers Document
func TestMarkdownChunker_Chunk_NoHeadersDocument(t *testing.T) {
	chunker := NewMarkdownChunker()

	content := `First paragraph with some content.

Second paragraph with more content.

Third paragraph concluding the document.
`

	file := &FileInput{
		Path:     "plain.md",
		Content:  []byte(content),
		Language: "markdown",
	}

	chunks, err := chunker.Chunk(context.Background(), file)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(chunks), 1)

	// Content should be chunked by size/paragraph boundaries
	assert.Contains(t, chunks[0].Content, "First paragraph")
}

// Additional test: Nested headers reset properly
func TestMarkdownChunker_Chunk_NestedHeaderReset(t *testing.T) {
	chunker := NewMarkdownChunker()

	content := `# Top Level

## Subsection A

### Deep in A

## Subsection B

This should be under Top Level > Subsection B, not Top Level > Subsection A > Subsection B.
`

	file := &FileInput{
		Path:     "nested.md",
		Content:  []byte(content),
		Language: "markdown",
	}

	chunks, err := chunker.Chunk(context.Background(), file)
	require.NoError(t, err)

	// Find Subsection B chunk
	var subsectionB *Chunk
	for _, c := range chunks {
		if strings.Contains(c.Content, "Subsection B") && !strings.Contains(c.Content, "Deep in A") {
			subsectionB = c
			break
		}
	}

	require.NotNil(t, subsectionB, "Subsection B chunk should exist")
	assert.Equal(t, "Top Level > Subsection B", subsectionB.Metadata["header_path"])
}

// Test: Preserve tables as units
func TestMarkdownChunker_Chunk_PreserveTables(t *testing.T) {
	chunker := NewMarkdownChunker()

	content := `# Data

| Column A | Column B | Column C |
|----------|----------|----------|
| Value 1  | Value 2  | Value 3  |
| Value 4  | Value 5  | Value 6  |
| Value 7  | Value 8  | Value 9  |

After the table.
`

	file := &FileInput{
		Path:     "table.md",
		Content:  []byte(content),
		Language: "markdown",
	}

	chunks, err := chunker.Chunk(context.Background(), file)
	require.NoError(t, err)

	// Table should be intact in one chunk
	found := false
	for _, c := range chunks {
		if strings.Contains(c.Content, "Column A") &&
			strings.Contains(c.Content, "Value 1") &&
			strings.Contains(c.Content, "Value 9") {
			found = true
			break
		}
	}
	assert.True(t, found, "Table should be intact in one chunk")
}

// Test: Preserve lists as units
func TestMarkdownChunker_Chunk_PreserveLists(t *testing.T) {
	chunker := NewMarkdownChunker()

	content := `# Steps

Follow these steps:

1. First step
2. Second step
3. Third step
4. Fourth step

After the list.
`

	file := &FileInput{
		Path:     "list.md",
		Content:  []byte(content),
		Language: "markdown",
	}

	chunks, err := chunker.Chunk(context.Background(), file)
	require.NoError(t, err)

	// List should be intact
	found := false
	for _, c := range chunks {
		if strings.Contains(c.Content, "1. First") &&
			strings.Contains(c.Content, "4. Fourth") {
			found = true
			break
		}
	}
	assert.True(t, found, "List should be intact in one chunk")
}

// Test: MDX component handling
func TestMarkdownChunker_Chunk_MDXComponentHandling(t *testing.T) {
	chunker := NewMarkdownChunker()

	content := `# Getting Started

import { Button } from '@/components'

<Button onClick={() => alert('Hello')}>
  Click me!
</Button>

## Usage

<CodeExample language="tsx" title="example.tsx">
  const foo = 'bar';
</CodeExample>
`

	file := &FileInput{
		Path:     "docs.mdx",
		Content:  []byte(content),
		Language: "markdown",
	}

	chunks, err := chunker.Chunk(context.Background(), file)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(chunks), 1)

	// MDX components should be preserved as-is
	found := false
	for _, c := range chunks {
		if strings.Contains(c.Content, "<Button") &&
			strings.Contains(c.Content, "Click me!") &&
			strings.Contains(c.Content, "</Button>") {
			found = true
			break
		}
	}
	assert.True(t, found, "MDX component should be preserved intact")
}

// Test: Code block with metadata preserved
func TestMarkdownChunker_Chunk_CodeBlockMetadata(t *testing.T) {
	chunker := NewMarkdownChunker()

	content := "# Code Example\n\n```tsx {1-3} title=\"example.tsx\" showLineNumbers\nconst hello = 'world';\nconst foo = 'bar';\nconst baz = 'qux';\n```\n"

	file := &FileInput{
		Path:     "code.md",
		Content:  []byte(content),
		Language: "markdown",
	}

	chunks, err := chunker.Chunk(context.Background(), file)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(chunks), 1)

	// Check that code fence metadata is preserved
	found := false
	for _, c := range chunks {
		if strings.Contains(c.Content, "```tsx {1-3}") &&
			strings.Contains(c.Content, "title=\"example.tsx\"") &&
			strings.Contains(c.Content, "showLineNumbers") {
			found = true
			break
		}
	}
	assert.True(t, found, "Code block metadata should be preserved")
}

// Test: Deeply nested headers
func TestMarkdownChunker_Chunk_DeeplyNestedHeaders(t *testing.T) {
	chunker := NewMarkdownChunker()

	content := `# Level 1

## Level 2

### Level 3

#### Level 4

##### Level 5

###### Level 6

Content at level 6.
`

	file := &FileInput{
		Path:     "deep.md",
		Content:  []byte(content),
		Language: "markdown",
	}

	chunks, err := chunker.Chunk(context.Background(), file)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(chunks), 1)

	// Find the deepest chunk
	var deepest *Chunk
	for _, c := range chunks {
		if strings.Contains(c.Content, "Content at level 6") {
			deepest = c
			break
		}
	}

	require.NotNil(t, deepest)
	assert.Equal(t, "Level 1 > Level 2 > Level 3 > Level 4 > Level 5 > Level 6", deepest.Metadata["header_path"])
	assert.Equal(t, "6", deepest.Metadata["header_level"])
}

// Test: Empty file handling
func TestMarkdownChunker_Chunk_EmptyFile(t *testing.T) {
	chunker := NewMarkdownChunker()

	file := &FileInput{
		Path:     "empty.md",
		Content:  []byte(""),
		Language: "markdown",
	}

	chunks, err := chunker.Chunk(context.Background(), file)
	require.NoError(t, err)
	assert.Empty(t, chunks)
}

// Test: Whitespace only file
func TestMarkdownChunker_Chunk_WhitespaceOnlyFile(t *testing.T) {
	chunker := NewMarkdownChunker()

	file := &FileInput{
		Path:     "whitespace.md",
		Content:  []byte("   \n\n\t\t\n   "),
		Language: "markdown",
	}

	chunks, err := chunker.Chunk(context.Background(), file)
	require.NoError(t, err)
	assert.Empty(t, chunks)
}

// Test: Section context in continuation chunks
func TestMarkdownChunker_Chunk_SectionContextInContinuation(t *testing.T) {
	chunker := NewMarkdownChunkerWithOptions(MarkdownChunkerOptions{
		MaxChunkTokens: 50, // Very small to force splitting
		OverlapTokens:  5,
	})

	content := `# Section Title

` + strings.Repeat("This is a long paragraph with many words to fill up space. ", 30) + "\n"

	file := &FileInput{
		Path:     "context.md",
		Content:  []byte(content),
		Language: "markdown",
	}

	chunks, err := chunker.Chunk(context.Background(), file)
	require.NoError(t, err)

	if len(chunks) > 1 {
		// Continuation chunks should have section context
		for i, c := range chunks {
			assert.Contains(t, c.Metadata["header_path"], "Section Title", "Chunk %d should have header context", i)
		}
	}
}

// Test: SupportedExtensions
func TestMarkdownChunker_SupportedExtensions(t *testing.T) {
	chunker := NewMarkdownChunker()
	exts := chunker.SupportedExtensions()

	assert.Contains(t, exts, ".md")
	assert.Contains(t, exts, ".markdown")
	assert.Contains(t, exts, ".mdx")
}

// Test: Chunk IDs are unique
func TestMarkdownChunker_Chunk_UniqueIDs(t *testing.T) {
	chunker := NewMarkdownChunker()

	content := `# Section 1

Content 1.

# Section 2

Content 2.

# Section 3

Content 3.
`

	file := &FileInput{
		Path:     "unique.md",
		Content:  []byte(content),
		Language: "markdown",
	}

	chunks, err := chunker.Chunk(context.Background(), file)
	require.NoError(t, err)

	ids := make(map[string]bool)
	for _, c := range chunks {
		assert.NotEmpty(t, c.ID)
		assert.False(t, ids[c.ID], "Duplicate chunk ID: %s", c.ID)
		ids[c.ID] = true
	}
}

// Test: Line numbers are correct
func TestMarkdownChunker_Chunk_CorrectLineNumbers(t *testing.T) {
	chunker := NewMarkdownChunker()

	content := `# First

Line 3.

# Second

Line 7.
`

	file := &FileInput{
		Path:     "lines.md",
		Content:  []byte(content),
		Language: "markdown",
	}

	chunks, err := chunker.Chunk(context.Background(), file)
	require.NoError(t, err)
	require.Len(t, chunks, 2)

	// First section starts at line 1
	assert.Equal(t, 1, chunks[0].StartLine)

	// Second section starts at line 5 (# Second)
	assert.Equal(t, 5, chunks[1].StartLine)
}

// Benchmark: Chunk 10 sections
func BenchmarkMarkdownChunker_Chunk_10Sections(b *testing.B) {
	chunker := NewMarkdownChunker()

	var sb strings.Builder
	for i := 0; i < 10; i++ {
		sb.WriteString("# Section ")
		sb.WriteString(string(rune('A' + i)))
		sb.WriteString("\n\n")
		sb.WriteString(strings.Repeat("Content paragraph with some text. ", 10))
		sb.WriteString("\n\n")
	}

	file := &FileInput{
		Path:     "bench.md",
		Content:  []byte(sb.String()),
		Language: "markdown",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = chunker.Chunk(context.Background(), file)
	}
}

// Benchmark: Chunk 100 sections
func BenchmarkMarkdownChunker_Chunk_100Sections(b *testing.B) {
	chunker := NewMarkdownChunker()

	var sb strings.Builder
	for i := 0; i < 100; i++ {
		sb.WriteString("# Section ")
		sb.WriteString(strings.Repeat("X", 3))
		sb.WriteString("\n\n")
		sb.WriteString(strings.Repeat("Content paragraph with some text. ", 5))
		sb.WriteString("\n\n")
	}

	file := &FileInput{
		Path:     "bench_large.md",
		Content:  []byte(sb.String()),
		Language: "markdown",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = chunker.Chunk(context.Background(), file)
	}
}

// Test: Close method exists and is idempotent (DEBT-005)
func TestMarkdownChunker_Close(t *testing.T) {
	chunker := NewMarkdownChunker()

	// Close should be safe to call (no panic)
	chunker.Close()

	// Close should be idempotent (safe to call multiple times)
	chunker.Close()
}
