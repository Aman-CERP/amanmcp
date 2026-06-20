package chunk

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/ledongthuc/pdf"
)

// PDFChunker extracts page text from in-memory PDF bytes and emits page-aware
// chunks. It is intentionally text-only: OCR, table structure, form fields, and
// layout reconstruction are out of scope, so scanned, encrypted, malformed, or
// otherwise unsupported PDFs return no chunks instead of failing the whole file.
type PDFChunker struct {
	options PDFChunkerOptions
}

// PDFChunkerOptions configures PDF chunking behavior.
type PDFChunkerOptions struct {
	MaxChunkTokens int // Maximum tokens per chunk (default: DefaultMaxChunkTokens)
}

// NewPDFChunker creates a PDF chunker with default options.
func NewPDFChunker() *PDFChunker {
	return NewPDFChunkerWithOptions(PDFChunkerOptions{})
}

// NewPDFChunkerWithOptions creates a PDF chunker with custom options.
func NewPDFChunkerWithOptions(opts PDFChunkerOptions) *PDFChunker {
	if opts.MaxChunkTokens == 0 {
		opts.MaxChunkTokens = DefaultMaxChunkTokens
	}
	return &PDFChunker{options: opts}
}

// Close releases chunker resources.
// PDFChunker is stateless, so this is a no-op for interface consistency.
func (c *PDFChunker) Close() {
	// No resources to release - PDFChunker is stateless.
}

// SupportedExtensions returns file extensions this chunker handles.
func (c *PDFChunker) SupportedExtensions() []string {
	return []string{".pdf"}
}

// Chunk splits a PDF into page-aware text chunks.
func (c *PDFChunker) Chunk(ctx context.Context, file *FileInput) ([]*Chunk, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if file == nil || len(file.Content) == 0 {
		return nil, nil
	}

	reader, err := pdf.NewReader(bytes.NewReader(file.Content), int64(len(file.Content)))
	if err != nil {
		return []*Chunk{}, nil
	}

	totalPages := reader.NumPage()
	if totalPages == 0 {
		return []*Chunk{}, nil
	}

	now := time.Now()
	chunks := make([]*Chunk, 0, totalPages)
	for pageNumber := 1; pageNumber <= totalPages; pageNumber++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		pageText, err := reader.Page(pageNumber).GetPlainText(nil)
		if err != nil {
			continue
		}

		pageText = normalizePDFText(pageText)
		if pageText == "" {
			continue
		}

		pageChunks := c.chunksForPage(file, pageText, pageNumber, totalPages, now)
		chunks = append(chunks, pageChunks...)
	}

	if len(chunks) == 0 {
		return []*Chunk{}, nil
	}
	return chunks, nil
}

func (c *PDFChunker) chunksForPage(file *FileInput, pageText string, pageNumber, totalPages int, now time.Time) []*Chunk {
	if estimateTokens(pageText) <= c.options.MaxChunkTokens {
		return []*Chunk{
			c.createPageChunk(file, pageText, pageNumber, totalPages, "page", 0, now),
		}
	}

	parts := splitPDFText(pageText, c.options.MaxChunkTokens)
	chunks := make([]*Chunk, 0, len(parts))
	for i, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		chunks = append(chunks, c.createPageChunk(file, part, pageNumber, totalPages, "part", i+1, now))
	}
	return chunks
}

func (c *PDFChunker) createPageChunk(
	file *FileInput,
	content string,
	pageNumber int,
	totalPages int,
	kind string,
	partNumber int,
	now time.Time,
) *Chunk {
	disambiguator := fmt.Sprintf("page%d", pageNumber)
	if kind == "part" {
		disambiguator = fmt.Sprintf("page%d_part%d", pageNumber, partNumber)
	}

	metadata := map[string]string{
		"content_type": "pdf",
		"chunker":      "pdf",
		"page_number":  fmt.Sprint(pageNumber),
		"page_start":   fmt.Sprint(pageNumber),
		"page_end":     fmt.Sprint(pageNumber),
		"total_pages":  fmt.Sprint(totalPages),
	}
	if kind == "part" {
		metadata["split_part"] = fmt.Sprint(partNumber)
	}

	return &Chunk{
		ID:          generateChunkIDWithDisambiguator(file.Path, content, disambiguator),
		FilePath:    file.Path,
		Content:     content,
		RawContent:  content,
		ContentType: ContentTypePDF,
		Language:    "pdf",
		Metadata:    metadata,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func normalizePDFText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.Join(strings.Fields(line), " ")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func splitPDFText(text string, maxChunkTokens int) []string {
	maxChars := maxChunkTokens * TokensPerChar
	if maxChars < TokensPerChar {
		maxChars = DefaultMaxChunkTokens * TokensPerChar
	}

	units := splitPDFTextUnits(text, maxChars)
	chunks := make([]string, 0, len(units))
	var current strings.Builder

	for _, unit := range units {
		unit = strings.TrimSpace(unit)
		if unit == "" {
			continue
		}

		if current.Len() == 0 {
			current.WriteString(unit)
			continue
		}

		candidate := current.String() + "\n\n" + unit
		if estimateTokens(candidate) > maxChunkTokens {
			chunks = append(chunks, current.String())
			current.Reset()
			current.WriteString(unit)
			continue
		}
		current.WriteString("\n\n")
		current.WriteString(unit)
	}

	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}
	return chunks
}

func splitPDFTextUnits(text string, maxChars int) []string {
	paragraphs := splitNonEmpty(text, "\n\n")
	units := make([]string, 0, len(paragraphs))

	for _, paragraph := range paragraphs {
		paragraph = strings.TrimSpace(paragraph)
		if paragraph == "" {
			continue
		}
		if len(paragraph) <= maxChars {
			units = append(units, paragraph)
			continue
		}

		for _, sentence := range splitSentences(paragraph) {
			sentence = strings.TrimSpace(sentence)
			if sentence == "" {
				continue
			}
			if len(sentence) <= maxChars {
				units = append(units, sentence)
				continue
			}
			units = append(units, splitLongText(sentence, maxChars)...)
		}
	}

	return units
}

func splitNonEmpty(text, sep string) []string {
	raw := strings.Split(text, sep)
	parts := make([]string, 0, len(raw))
	for _, part := range raw {
		part = strings.TrimSpace(part)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func splitSentences(text string) []string {
	var sentences []string
	start := 0
	for i, r := range text {
		if !isSentenceTerminator(r) {
			continue
		}
		end := i + len(string(r))
		if end >= len(text) || nextRuneIsSpace(text[end:]) {
			sentences = append(sentences, strings.TrimSpace(text[start:end]))
			start = end
		}
	}
	if start < len(text) {
		sentences = append(sentences, strings.TrimSpace(text[start:]))
	}
	return sentences
}

func isSentenceTerminator(r rune) bool {
	return r == '.' || r == '!' || r == '?'
}

func nextRuneIsSpace(text string) bool {
	for _, r := range text {
		return unicode.IsSpace(r)
	}
	return false
}

func splitLongText(text string, maxChars int) []string {
	if maxChars <= 0 {
		maxChars = DefaultMaxChunkTokens * TokensPerChar
	}

	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}

	parts := make([]string, 0, len(words))
	var current strings.Builder
	for _, word := range words {
		if len(word) > maxChars {
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
			parts = append(parts, splitLongWord(word, maxChars)...)
			continue
		}

		if current.Len() == 0 {
			current.WriteString(word)
			continue
		}

		if current.Len()+1+len(word) > maxChars {
			parts = append(parts, current.String())
			current.Reset()
			current.WriteString(word)
			continue
		}
		current.WriteByte(' ')
		current.WriteString(word)
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

func splitLongWord(word string, maxChars int) []string {
	if maxChars <= 0 {
		return []string{word}
	}

	parts := make([]string, 0, len(word)/maxChars+1)
	for start := 0; start < len(word); start += maxChars {
		end := start + maxChars
		if end > len(word) {
			end = len(word)
		}
		parts = append(parts, word[start:end])
	}
	return parts
}

var _ Chunker = (*PDFChunker)(nil)
