package store

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBM25DocumentContent_IncludesFilePathForLookup(t *testing.T) {
	got := BM25DocumentContent("internal/docs/spec.pdf", "PDF_SPEC_API contract")

	assert.Contains(t, got, "File path: internal/docs/spec.pdf")
	assert.Contains(t, got, "PDF_SPEC_API contract")
}

func TestBM25DocumentContent_DoesNotDuplicateExistingPath(t *testing.T) {
	content := "From file: internal/docs/spec.pdf\n\nPDF_SPEC_API contract"

	got := BM25DocumentContent("internal/docs/spec.pdf", content)

	assert.Equal(t, content, got)
	assert.Equal(t, 1, strings.Count(got, "internal/docs/spec.pdf"))
}
