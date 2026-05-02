package graph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// SourceContentType identifies the source file family for cheap extraction.
type SourceContentType string

const (
	SourceContentTypeCode     SourceContentType = "code"
	SourceContentTypeMarkdown SourceContentType = "markdown"
	SourceContentTypeConfig   SourceContentType = "config"
)

// SourceFile is the extractor input contract. It is intentionally smaller than store.Chunk.
type SourceFile struct {
	Path        string
	Language    string
	ContentType SourceContentType
	Content     []byte
	Chunks      []SourceChunk
}

// SourceChunk is the chunk metadata needed for symbol->chunk edges.
type SourceChunk struct {
	ID        string
	FilePath  string
	Language  string
	StartLine int
	EndLine   int
	Symbols   []SourceSymbol
}

// SourceSymbol is the symbol metadata needed for cheap symbol edges.
type SourceSymbol struct {
	Name      string
	Kind      string
	StartLine int
	EndLine   int
	Signature string
}

// CheapExtractorOptions controls deterministic extraction metadata.
type CheapExtractorOptions struct {
	Now        func() time.Time
	StaleAfter time.Duration
}

type extractionScope struct {
	nodes    map[string]Node
	edges    []Edge
	warnings []string
	errors   []string
}

// IndexCheapEdges extracts deterministic local graph edges and writes them via Repository.
func IndexCheapEdges(ctx context.Context, repo Repository, projectID string, files []SourceFile, opts CheapExtractorOptions) error {
	if projectID == "" {
		return fmt.Errorf("project_id is required")
	}
	now := time.Now().UTC
	if opts.Now != nil {
		now = opts.Now
	}
	started := now()
	pathSet := make(map[string]SourceFile, len(files))
	for _, file := range files {
		if file.Path != "" {
			normalized := filepath.ToSlash(file.Path)
			file.Path = normalized
			pathSet[normalized] = file
		}
	}

	hadWarnings := false
	hadErrors := false
	for _, file := range files {
		file.Path = filepath.ToSlash(file.Path)
		scope := newExtractionScope()
		extractFileNode(projectID, file, scope)
		extractGoPackageAndImports(projectID, file, scope)
		extractSymbolEdges(projectID, file, scope)
		extractConfigKeys(projectID, file, scope)
		extractTestImplementationEdge(projectID, file, pathSet, scope)
		extractDocMentions(projectID, file, pathSet, scope)

		status := ExtractorStatusSuccess
		if len(scope.errors) > 0 {
			status = ExtractorStatusFailed
			hadErrors = true
		} else if len(scope.warnings) > 0 {
			status = ExtractorStatusPartial
			hadWarnings = true
		}
		nodes := scope.sortedNodes()
		edges := append([]Edge(nil), scope.edges...)
		sortEdgesByNaturalKey(edges)
		if err := repo.ReplaceEdges(ctx, EdgeReplacement{
			ProjectID:  projectID,
			Extractor:  ExtractorCheap,
			SourcePath: file.Path,
			Nodes:      nodes,
			Edges:      edges,
			Run: ExtractorRun{
				Status:      status,
				StartedAt:   started,
				CompletedAt: now(),
				NodeCount:   len(nodes),
				EdgeCount:   len(edges),
				Warnings:    scope.warnings,
				Errors:      scope.errors,
			},
		}); err != nil {
			return fmt.Errorf("replace cheap graph edges for %s: %w", file.Path, err)
		}
	}

	status := GraphStatusFresh
	message := ""
	if hadErrors || hadWarnings {
		status = GraphStatusPartial
		message = "cheap edge extraction completed with warnings or errors"
	}
	if err := repo.RecordBuild(ctx, BuildMetadata{
		ProjectID:     projectID,
		Status:        status,
		StartedAt:     started,
		CompletedAt:   now(),
		SourceVersion: sourceVersion(files),
		Message:       message,
	}); err != nil {
		return fmt.Errorf("record graph build metadata: %w", err)
	}
	return nil
}

func newExtractionScope() *extractionScope {
	return &extractionScope{nodes: map[string]Node{}}
}

func (s *extractionScope) addNode(node Node) Node {
	normalized, err := normalizeNode(node)
	if err != nil {
		s.errors = append(s.errors, err.Error())
		return Node{}
	}
	s.nodes[normalized.ID] = normalized
	return normalized
}

func (s *extractionScope) addEdge(edge Edge) {
	normalized, err := normalizeEdge(edge)
	if err != nil {
		s.errors = append(s.errors, err.Error())
		return
	}
	s.edges = append(s.edges, normalized)
}

func (s *extractionScope) sortedNodes() []Node {
	nodes := make([]Node, 0, len(s.nodes))
	for _, node := range s.nodes {
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].ID < nodes[j].ID
	})
	return nodes
}

func extractFileNode(projectID string, file SourceFile, scope *extractionScope) Node {
	return scope.addNode(Node{
		ProjectID:  projectID,
		Kind:       NodeKindFile,
		Key:        file.Path,
		SourcePath: file.Path,
		Name:       filepath.Base(file.Path),
		Language:   file.Language,
	})
}

func extractGoPackageAndImports(projectID string, file SourceFile, scope *extractionScope) {
	if file.Language != "go" {
		return
	}
	lines := strings.Split(string(file.Content), "\n")
	packageName := ""
	packageLine := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "package ") {
			packageName = strings.TrimSpace(strings.TrimPrefix(trimmed, "package "))
			packageLine = i + 1
			break
		}
	}
	if packageName == "" {
		return
	}

	fileNode := extractFileNode(projectID, file, scope)
	packageNode := scope.addNode(Node{
		ProjectID:  projectID,
		Kind:       NodeKindPackage,
		Key:        packageName,
		SourcePath: file.Path,
		Name:       packageName,
		Language:   "go",
		StartLine:  packageLine,
		EndLine:    packageLine,
	})
	scope.addEdge(Edge{
		ProjectID:  projectID,
		Kind:       EdgeKindFileDeclaresPackage,
		FromNodeID: fileNode.ID,
		ToNodeID:   packageNode.ID,
		Extractor:  ExtractorCheap,
		SourcePath: file.Path,
		Confidence: 1.0,
		Evidence: Evidence{
			Method:  "go_package_declaration",
			Line:    packageLine,
			Snippet: "package " + packageName,
		},
	})

	for _, imp := range parseGoImports(lines) {
		importNode := scope.addNode(Node{
			ProjectID:  projectID,
			Kind:       NodeKindImport,
			Key:        imp.Path,
			SourcePath: file.Path,
			Name:       imp.Path,
			Language:   "go",
			StartLine:  imp.Line,
			EndLine:    imp.Line,
		})
		scope.addEdge(Edge{
			ProjectID:  projectID,
			Kind:       EdgeKindPackageImports,
			FromNodeID: packageNode.ID,
			ToNodeID:   importNode.ID,
			Extractor:  ExtractorCheap,
			SourcePath: file.Path,
			Confidence: 0.95,
			Evidence: Evidence{
				Method:  "go_import_declaration",
				Line:    imp.Line,
				Snippet: imp.Path,
			},
		})
	}
}

type goImport struct {
	Path string
	Line int
}

func parseGoImports(lines []string) []goImport {
	var imports []goImport
	inBlock := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "import ("):
			inBlock = true
			continue
		case inBlock && trimmed == ")":
			inBlock = false
			continue
		case inBlock:
			if path := quotedImportPath(trimmed); path != "" {
				imports = append(imports, goImport{Path: path, Line: i + 1})
			}
		case strings.HasPrefix(trimmed, "import "):
			if path := quotedImportPath(strings.TrimSpace(strings.TrimPrefix(trimmed, "import "))); path != "" {
				imports = append(imports, goImport{Path: path, Line: i + 1})
			}
		}
	}
	return imports
}

func quotedImportPath(value string) string {
	value = strings.TrimSpace(value)
	if idx := strings.Index(value, "//"); idx >= 0 {
		value = strings.TrimSpace(value[:idx])
	}
	fields := strings.Fields(value)
	if len(fields) > 0 {
		value = fields[len(fields)-1]
	}
	return strings.Trim(strings.Trim(value, "`"), `"`)
}

func extractSymbolEdges(projectID string, file SourceFile, scope *extractionScope) {
	if len(file.Chunks) == 0 {
		return
	}
	fileNode := extractFileNode(projectID, file, scope)
	for _, chunk := range file.Chunks {
		if chunk.ID == "" {
			continue
		}
		chunkPath := chunk.FilePath
		if chunkPath == "" {
			chunkPath = file.Path
		}
		chunkNode := scope.addNode(Node{
			ProjectID:  projectID,
			Kind:       NodeKindChunk,
			Key:        chunk.ID,
			SourcePath: chunkPath,
			Name:       chunk.ID,
			Language:   chunk.Language,
			StartLine:  chunk.StartLine,
			EndLine:    chunk.EndLine,
		})
		for _, symbol := range chunk.Symbols {
			if symbol.Name == "" {
				continue
			}
			symbolKey := fmt.Sprintf("%s#%s:%d", file.Path, symbol.Name, symbol.StartLine)
			symbolNode := scope.addNode(Node{
				ProjectID:  projectID,
				Kind:       NodeKindSymbol,
				Key:        symbolKey,
				SourcePath: file.Path,
				Name:       symbol.Name,
				Language:   file.Language,
				SymbolKind: symbol.Kind,
				StartLine:  symbol.StartLine,
				EndLine:    symbol.EndLine,
				Metadata: map[string]string{
					"signature": symbol.Signature,
				},
			})
			scope.addEdge(Edge{
				ProjectID:  projectID,
				Kind:       EdgeKindFileDefinesSymbol,
				FromNodeID: fileNode.ID,
				ToNodeID:   symbolNode.ID,
				Extractor:  ExtractorCheap,
				SourcePath: file.Path,
				Confidence: 0.95,
				Evidence: Evidence{
					Method:  "chunk_symbol",
					Line:    symbol.StartLine,
					Snippet: symbol.Signature,
				},
			})
			scope.addEdge(Edge{
				ProjectID:  projectID,
				Kind:       EdgeKindSymbolHasChunk,
				FromNodeID: symbolNode.ID,
				ToNodeID:   chunkNode.ID,
				Extractor:  ExtractorCheap,
				SourcePath: file.Path,
				Confidence: 1.0,
				Evidence: Evidence{
					Method: "chunk_symbol_membership",
					Line:   chunk.StartLine,
				},
			})
		}
	}
}

func extractConfigKeys(projectID string, file SourceFile, scope *extractionScope) {
	if !isConfigFile(file) {
		return
	}
	keys, err := configKeys(file)
	if err != nil {
		scope.errors = append(scope.errors, fmt.Sprintf("parse config %s: %v", file.Path, err))
		return
	}
	fileNode := extractFileNode(projectID, file, scope)
	for _, key := range keys {
		configNode := scope.addNode(Node{
			ProjectID:  projectID,
			Kind:       NodeKindConfigKey,
			Key:        file.Path + "#" + key,
			SourcePath: file.Path,
			Name:       key,
			Language:   file.Language,
		})
		scope.addEdge(Edge{
			ProjectID:  projectID,
			Kind:       EdgeKindFileDefinesConfigKey,
			FromNodeID: fileNode.ID,
			ToNodeID:   configNode.ID,
			Extractor:  ExtractorCheap,
			SourcePath: file.Path,
			Confidence: 0.9,
			Evidence: Evidence{
				Method:  "config_key_parse",
				Snippet: key,
			},
		})
	}
}

func isConfigFile(file SourceFile) bool {
	if file.ContentType == SourceContentTypeConfig {
		return true
	}
	switch strings.ToLower(filepath.Ext(file.Path)) {
	case ".yaml", ".yml", ".json", ".toml":
		return true
	default:
		return false
	}
}

func configKeys(file SourceFile) ([]string, error) {
	switch strings.ToLower(filepath.Ext(file.Path)) {
	case ".yaml", ".yml":
		var root yaml.Node
		if err := yaml.Unmarshal(file.Content, &root); err != nil {
			return nil, err
		}
		keys := collectYAMLKeys(&root, nil)
		sort.Strings(keys)
		return keys, nil
	case ".json":
		var value any
		if err := json.Unmarshal(file.Content, &value); err != nil {
			return nil, err
		}
		keys := collectJSONKeys(value, nil)
		sort.Strings(keys)
		return keys, nil
	case ".toml":
		keys := collectTOMLKeys(string(file.Content))
		sort.Strings(keys)
		return keys, nil
	default:
		return nil, nil
	}
}

func collectYAMLKeys(node *yaml.Node, prefix []string) []string {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		return collectYAMLKeys(node.Content[0], prefix)
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}
	var keys []string
	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]
		path := append(append([]string{}, prefix...), keyNode.Value)
		keys = append(keys, strings.Join(path, "."))
		keys = append(keys, collectYAMLKeys(valueNode, path)...)
	}
	return keys
}

func collectJSONKeys(value any, prefix []string) []string {
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	var keys []string
	for key, child := range object {
		path := append(append([]string{}, prefix...), key)
		keys = append(keys, strings.Join(path, "."))
		keys = append(keys, collectJSONKeys(child, path)...)
	}
	return keys
}

func collectTOMLKeys(content string) []string {
	var keys []string
	var section []string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.Contains(trimmed, "]") {
			name := strings.TrimSpace(strings.Trim(trimmed, "[]"))
			if name != "" {
				section = strings.Split(name, ".")
				keys = append(keys, strings.Join(section, "."))
			}
			continue
		}
		if idx := strings.Index(trimmed, "="); idx > 0 {
			key := strings.TrimSpace(trimmed[:idx])
			path := append(append([]string{}, section...), key)
			keys = append(keys, strings.Join(path, "."))
		}
	}
	return keys
}

func extractTestImplementationEdge(projectID string, file SourceFile, pathSet map[string]SourceFile, scope *extractionScope) {
	if !strings.HasSuffix(file.Path, "_test.go") {
		return
	}
	implPath := strings.TrimSuffix(file.Path, "_test.go") + ".go"
	if _, ok := pathSet[implPath]; !ok {
		return
	}
	testNode := extractFileNode(projectID, file, scope)
	implFile := pathSet[implPath]
	implNode := extractFileNode(projectID, implFile, scope)
	scope.addEdge(Edge{
		ProjectID:  projectID,
		Kind:       EdgeKindTestCoversImplementation,
		FromNodeID: testNode.ID,
		ToNodeID:   implNode.ID,
		Extractor:  ExtractorCheap,
		SourcePath: file.Path,
		Confidence: 0.82,
		Evidence: Evidence{
			Method:    "go_test_filename_convention",
			Snippet:   implPath,
			Heuristic: true,
		},
	})
}

func extractDocMentions(projectID string, file SourceFile, pathSet map[string]SourceFile, scope *extractionScope) {
	if !isMarkdownFile(file) {
		return
	}
	mentioned := mentionedKnownPaths(string(file.Content), pathSet, file.Path)
	if len(mentioned) == 0 {
		return
	}
	docNode := extractFileNode(projectID, file, scope)
	for _, mention := range mentioned {
		targetFile := pathSet[mention.Path]
		targetNode := extractFileNode(projectID, targetFile, scope)
		scope.addEdge(Edge{
			ProjectID:  projectID,
			Kind:       EdgeKindDocMentionsPath,
			FromNodeID: docNode.ID,
			ToNodeID:   targetNode.ID,
			Extractor:  ExtractorCheap,
			SourcePath: file.Path,
			Confidence: 0.8,
			Evidence: Evidence{
				Method:    "markdown_known_path_mention",
				Snippet:   mention.Path,
				Line:      mention.Line,
				Heuristic: true,
			},
		})
	}
}

func isMarkdownFile(file SourceFile) bool {
	return file.ContentType == SourceContentTypeMarkdown || strings.EqualFold(filepath.Ext(file.Path), ".md")
}

type knownPathMention struct {
	Path string
	Line int
}

func mentionedKnownPaths(content string, pathSet map[string]SourceFile, self string) []knownPathMention {
	var mentions []knownPathMention
	for path := range pathSet {
		if path == self {
			continue
		}
		if line, ok := knownPathMentionLine(content, path); ok {
			mentions = append(mentions, knownPathMention{Path: path, Line: line})
		}
	}
	sort.Slice(mentions, func(i, j int) bool {
		if mentions[i].Path == mentions[j].Path {
			return mentions[i].Line < mentions[j].Line
		}
		return mentions[i].Path < mentions[j].Path
	})
	return mentions
}

func knownPathMentionLine(content, path string) (int, bool) {
	for _, marker := range []string{
		"`" + path + "`",
		"](" + path + ")",
		"](" + path + " ",
		"](" + path + "\t",
	} {
		if idx := strings.Index(content, marker); idx >= 0 {
			return 1 + strings.Count(content[:idx], "\n"), true
		}
	}

	for i, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == path {
			return i + 1, true
		}
	}
	return 0, false
}

func sourceVersion(files []SourceFile) string {
	normalized := append([]SourceFile(nil), files...)
	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i].Path < normalized[j].Path
	})
	hash := sha256.New()
	for _, file := range normalized {
		_, _ = hash.Write([]byte(filepath.ToSlash(file.Path)))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(file.Content)
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))[:24]
}
