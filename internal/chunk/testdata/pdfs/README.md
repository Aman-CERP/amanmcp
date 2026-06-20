# PDF Chunker Fixtures

These fixtures are intentionally tiny, deterministic PDFs used by
`internal/chunk/pdf_test.go`.

- `simple.pdf`: one text page.
- `multipage.pdf`: two text pages with distinct page content.
- `large-section.pdf`: one oversized text page for split behavior.
- `scanned.pdf`: one page without text content, representing image-only or scanned PDFs.
- `encrypted.pdf`: minimal password-protected PDF that `github.com/ledongthuc/pdf`
  rejects with `pdf.ErrInvalidPassword`.

They were generated with a hermetic Go standard-library script that writes
minimal PDF objects, streams, xref offsets, trailers, and `%%EOF` markers.
Regenerate them by running an equivalent script from the repository root that:

1. Creates `internal/chunk/testdata/pdfs`.
2. Writes each PDF object in ascending object-id order.
3. Records byte offsets for the xref table.
4. Emits a trailer rooted at object `1 0 R`.
5. Adds `/Encrypt` and `/ID` trailer entries only for `encrypted.pdf`.

Do not use external PDF tools for these fixtures; keeping generation in pure Go
prevents local toolchain differences from changing test bytes.
