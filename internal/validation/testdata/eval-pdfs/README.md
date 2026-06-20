# PDF Eval Fixtures

These PDFs are tiny deterministic text fixtures for the search eval corpus.
They are intentionally plain, unencrypted, and text-extractable by
`github.com/ledongthuc/pdf` through `pdf.NewReader(bytes.NewReader(...), size)`.

The fixtures cover PDF-specific retrieval behavior without depending on local
PDF tooling, OCR, fonts outside PDF base fonts, timestamps, or generated
metadata:

- `technical-spec.pdf`: API and search-output contract terms.
- `rfc-pdf-context.pdf`: reader API and scanner guard terms.
- `design-doc-pdf-indexing.pdf`: scanner/indexer design and PDF citation terms.

Keep these files small and stable. If fixture content changes, update
`internal/validation/testdata/queries.yaml` expectations in the same change so
page-aware eval results remain deterministic.
