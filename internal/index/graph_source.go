package index

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/Aman-CERP/amanmcp/internal/chunk"
	"github.com/Aman-CERP/amanmcp/internal/graph"
	"github.com/Aman-CERP/amanmcp/internal/scanner"
	"github.com/Aman-CERP/amanmcp/internal/secrets"
	"github.com/Aman-CERP/amanmcp/internal/store"
)

// MetadataGraphSourceConfig configures graph source reconstruction from an
// existing metadata index for graph-only rebuilds.
type MetadataGraphSourceConfig struct {
	RootDir       string
	ProjectID     string
	Metadata      store.MetadataStore
	SecretScanner *secrets.Scanner
}

// BuildGraphSourcesFromMetadata reconstructs cheap graph extractor inputs from
// an existing search index without re-chunking or re-embedding source files.
func BuildGraphSourcesFromMetadata(ctx context.Context, cfg MetadataGraphSourceConfig) ([]graph.SourceFile, error) {
	if cfg.RootDir == "" {
		return nil, fmt.Errorf("root directory is required")
	}
	if cfg.ProjectID == "" {
		return nil, fmt.Errorf("project ID is required")
	}
	if cfg.Metadata == nil {
		return nil, fmt.Errorf("metadata store is required")
	}
	secretScanner := cfg.SecretScanner
	if secretScanner == nil {
		secretScanner = secrets.NewScanner(secrets.DefaultPolicy())
	}

	var sources []graph.SourceFile
	cursor := ""
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		files, nextCursor, err := cfg.Metadata.ListFiles(ctx, cfg.ProjectID, cursor, 500)
		if err != nil {
			return nil, fmt.Errorf("list indexed files for graph build: %w", err)
		}
		for _, file := range files {
			if _, ok := graphContentTypeFromString(file.ContentType); !ok {
				continue
			}
			content, err := readIndexedSourceFile(cfg.RootDir, file.Path)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					slog.Warn("graph_metadata_source_missing",
						slog.String("project_id", cfg.ProjectID),
						slog.String("file", file.Path),
						slog.String("action", "skip"))
					continue
				}
				return nil, err
			}
			guarded, blocked := guardGraphSourceContent(secretScanner, file.Path, store.ContentType(file.ContentType), content)
			if blocked {
				continue
			}
			chunks, err := cfg.Metadata.GetChunksByFile(ctx, file.ID)
			if err != nil {
				return nil, fmt.Errorf("load graph chunks for %s: %w", file.Path, err)
			}
			source, ok := graphSourceFromStoreFile(file, guarded, chunks)
			if ok {
				sources = append(sources, source)
			}
		}
		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}
	return sources, nil
}

func readIndexedSourceFile(rootDir, relativePath string) ([]byte, error) {
	cleaned := filepath.Clean(filepath.FromSlash(relativePath))
	if filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("indexed source path escapes project root: %s", relativePath)
	}
	content, err := os.ReadFile(filepath.Join(rootDir, cleaned))
	if err != nil {
		return nil, fmt.Errorf("read indexed source file %s: %w", relativePath, err)
	}
	return content, nil
}

func graphSourceFromChunkedFile(file *scanner.FileInfo, content []byte, chunks []*chunk.Chunk) (graph.SourceFile, bool) {
	if file == nil {
		return graph.SourceFile{}, false
	}
	contentType, ok := graphContentTypeFromString(string(file.ContentType))
	if !ok {
		return graph.SourceFile{}, false
	}
	return graph.SourceFile{
		Path:        filepath.ToSlash(file.Path),
		Language:    file.Language,
		ContentType: contentType,
		Content:     append([]byte(nil), content...),
		Chunks:      graphChunksFromChunks(chunks),
	}, true
}

func graphSourceFromStoreFile(file *store.File, content []byte, chunks []*store.Chunk) (graph.SourceFile, bool) {
	if file == nil {
		return graph.SourceFile{}, false
	}
	contentType, ok := graphContentTypeFromString(file.ContentType)
	if !ok {
		return graph.SourceFile{}, false
	}
	return graph.SourceFile{
		Path:        filepath.ToSlash(file.Path),
		Language:    file.Language,
		ContentType: contentType,
		Content:     append([]byte(nil), content...),
		Chunks:      graphChunksFromStoreChunks(chunks),
	}, true
}

func graphContentTypeFromString(contentType string) (graph.SourceContentType, bool) {
	switch scanner.ContentType(contentType) {
	case scanner.ContentTypeCode:
		return graph.SourceContentTypeCode, true
	case scanner.ContentTypeMarkdown:
		return graph.SourceContentTypeMarkdown, true
	case scanner.ContentTypeConfig:
		return graph.SourceContentTypeConfig, true
	default:
		return "", false
	}
}

func graphChunksFromChunks(chunks []*chunk.Chunk) []graph.SourceChunk {
	if len(chunks) == 0 {
		return nil
	}
	sources := make([]graph.SourceChunk, 0, len(chunks))
	for _, c := range chunks {
		if c == nil {
			continue
		}
		sources = append(sources, graph.SourceChunk{
			ID:        c.ID,
			FilePath:  filepath.ToSlash(c.FilePath),
			Language:  c.Language,
			StartLine: c.StartLine,
			EndLine:   c.EndLine,
			Symbols:   graphSymbolsFromChunkSymbols(c.Symbols),
		})
	}
	return sources
}

func graphChunksFromStoreChunks(chunks []*store.Chunk) []graph.SourceChunk {
	if len(chunks) == 0 {
		return nil
	}
	sources := make([]graph.SourceChunk, 0, len(chunks))
	for _, c := range chunks {
		if c == nil {
			continue
		}
		sources = append(sources, graph.SourceChunk{
			ID:        c.ID,
			FilePath:  filepath.ToSlash(c.FilePath),
			Language:  c.Language,
			StartLine: c.StartLine,
			EndLine:   c.EndLine,
			Symbols:   graphSymbolsFromStoreSymbols(c.Symbols),
		})
	}
	return sources
}

func graphSymbolsFromChunkSymbols(symbols []*chunk.Symbol) []graph.SourceSymbol {
	if len(symbols) == 0 {
		return nil
	}
	out := make([]graph.SourceSymbol, 0, len(symbols))
	for _, s := range symbols {
		if s == nil {
			continue
		}
		out = append(out, graph.SourceSymbol{
			Name:      s.Name,
			Kind:      string(s.Type),
			StartLine: s.StartLine,
			EndLine:   s.EndLine,
			Signature: s.Signature,
		})
	}
	return out
}

func graphSymbolsFromStoreSymbols(symbols []*store.Symbol) []graph.SourceSymbol {
	if len(symbols) == 0 {
		return nil
	}
	out := make([]graph.SourceSymbol, 0, len(symbols))
	for _, s := range symbols {
		if s == nil {
			continue
		}
		out = append(out, graph.SourceSymbol{
			Name:      s.Name,
			Kind:      string(s.Type),
			StartLine: s.StartLine,
			EndLine:   s.EndLine,
			Signature: s.Signature,
		})
	}
	return out
}

func guardGraphSourceContent(secretScanner *secrets.Scanner, path string, contentType store.ContentType, content []byte) ([]byte, bool) {
	if secretScanner == nil || contentType == store.ContentTypePDF {
		return content, false
	}
	result := secretScanner.GuardContent(secrets.ContentInput{
		Path:    path,
		Content: content,
		Source:  secrets.SourceIndex,
	})
	return result.Content, result.Blocked
}
