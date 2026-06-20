package chunk

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPDFChunker_SupportedExtensions_ReturnsPDF(t *testing.T) {
	chunker := NewPDFChunker()

	assert.Equal(t, []string{".pdf"}, chunker.SupportedExtensions())
}

func TestPDFChunker_Chunk_SimplePDFProducesPageMetadata(t *testing.T) {
	file := pdfFixtureInput(t, "simple.pdf")
	chunker := NewPDFChunker()

	chunks, err := chunker.Chunk(context.Background(), file)

	require.NoError(t, err)
	require.Len(t, chunks, 1)

	chunk := chunks[0]
	assert.Equal(t, ContentTypePDF, chunk.ContentType)
	assert.Equal(t, "pdf", chunk.Language)
	assert.Contains(t, chunk.Content, "AmanMCP PDF chunker simple fixture")
	assert.Equal(t, chunk.Content, chunk.RawContent)
	assert.Equal(t, generateChunkIDWithDisambiguator(file.Path, chunk.Content, "page1"), chunk.ID)
	assert.Equal(t, map[string]string{
		"content_type": "pdf",
		"chunker":      "pdf",
		"page_number":  "1",
		"page_start":   "1",
		"page_end":     "1",
		"total_pages":  "1",
	}, chunk.Metadata)
}

func TestPDFChunker_Chunk_MultipagePDFKeepsPageAwareChunks(t *testing.T) {
	file := pdfFixtureInput(t, "multipage.pdf")
	chunker := NewPDFChunker()

	chunks, err := chunker.Chunk(context.Background(), file)

	require.NoError(t, err)
	require.Len(t, chunks, 2)

	assert.Contains(t, chunks[0].Content, "First page alpha content")
	assert.Contains(t, chunks[1].Content, "Second page beta content")
	for i, chunk := range chunks {
		page := strconv.Itoa(i + 1)
		assert.Equal(t, page, chunk.Metadata["page_number"])
		assert.Equal(t, page, chunk.Metadata["page_start"])
		assert.Equal(t, page, chunk.Metadata["page_end"])
		assert.Equal(t, "2", chunk.Metadata["total_pages"])
		assert.Equal(t, generateChunkIDWithDisambiguator(file.Path, chunk.Content, "page"+page), chunk.ID)
	}
}

func TestPDFChunker_Chunk_LargePageSplitsWithinTokenBudgetAndStableIDs(t *testing.T) {
	file := pdfFixtureInput(t, "large-section.pdf")
	chunker := NewPDFChunkerWithOptions(PDFChunkerOptions{MaxChunkTokens: 120})

	firstRun, err := chunker.Chunk(context.Background(), file)
	require.NoError(t, err)
	require.Greater(t, len(firstRun), 1)

	secondRun, err := chunker.Chunk(context.Background(), file)
	require.NoError(t, err)
	require.Len(t, secondRun, len(firstRun))

	seenIDs := make(map[string]struct{}, len(firstRun))
	for i, chunk := range firstRun {
		assert.LessOrEqual(t, estimateTokens(chunk.Content), 120, "chunk %d exceeded token budget", i+1)
		assert.Equal(t, "1", chunk.Metadata["page_number"])
		assert.Equal(t, "1", chunk.Metadata["page_start"])
		assert.Equal(t, "1", chunk.Metadata["page_end"])
		assert.Equal(t, "1", chunk.Metadata["total_pages"])

		wantID := generateChunkIDWithDisambiguator(file.Path, chunk.Content, "page1_part"+strconv.Itoa(i+1))
		assert.Equal(t, wantID, chunk.ID)
		assert.Equal(t, chunk.ID, secondRun[i].ID)
		seenIDs[chunk.ID] = struct{}{}
	}
	assert.Len(t, seenIDs, len(firstRun), "split chunk IDs must be unique")
}

func TestPDFChunker_Chunk_ScannedAndEncryptedPDFsReturnEmptyChunks(t *testing.T) {
	chunker := NewPDFChunker()

	for _, name := range []string{"scanned.pdf", "encrypted.pdf"} {
		t.Run(name, func(t *testing.T) {
			chunks, err := chunker.Chunk(context.Background(), pdfFixtureInput(t, name))

			require.NoError(t, err)
			assert.Empty(t, chunks)
		})
	}
}

func pdfFixtureInput(t *testing.T, name string) *FileInput {
	t.Helper()

	path := filepath.Join("testdata", "pdfs", name)
	content, err := os.ReadFile(filepath.Join("testdata", "pdfs", name))
	require.NoError(t, err)

	return &FileInput{
		Path:     path,
		Content:  content,
		Language: "pdf",
	}
}
