package graph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
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

const maxTestImplementationTargets = 5

var (
	jsImportPattern     = regexp.MustCompile(`(?m)\bimport\s+(?:[^'";]+?\s+from\s+)?['"]([^'"]+)['"]`)
	jsExportFromPattern = regexp.MustCompile(`(?m)\bexport\s+[^'";]+?\s+from\s+['"]([^'"]+)['"]`)
	jsRequirePattern    = regexp.MustCompile(`(?m)\brequire\s*\(\s*['"]([^'"]+)['"]\s*\)`)
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
	Now          func() time.Time
	StaleAfter   time.Duration
	KnownSources []SourceFile
}

type extractionScope struct {
	nodes    map[string]Node
	edges    []Edge
	warnings []string
	errors   []string
}

// CheapExtractionSummary reports aggregate status for a cheap extraction run.
type CheapExtractionSummary struct {
	StartedAt     time.Time
	CompletedAt   time.Time
	HadWarnings   bool
	HadErrors     bool
	SourceVersion string
}

// IndexCheapEdges extracts deterministic local graph edges and writes them via Repository.
func IndexCheapEdges(ctx context.Context, repo Repository, projectID string, files []SourceFile, opts CheapExtractorOptions) error {
	summary, err := UpdateCheapEdgesWithSummary(ctx, repo, projectID, files, opts)
	if err != nil {
		return err
	}

	status := GraphStatusFresh
	message := ""
	if summary.HadErrors || summary.HadWarnings {
		status = GraphStatusPartial
		message = "cheap edge extraction completed with warnings or errors"
	}
	if err := repo.RecordBuild(ctx, BuildMetadata{
		ProjectID:     projectID,
		Kind:          BuildKindFull,
		Status:        status,
		StartedAt:     summary.StartedAt,
		CompletedAt:   summary.CompletedAt,
		SourceVersion: summary.SourceVersion,
		Message:       message,
	}); err != nil {
		return fmt.Errorf("record graph build metadata: %w", err)
	}
	return nil
}

// UpdateCheapEdges extracts deterministic local graph edges and replaces only
// the provided source scopes. It intentionally does not record full-build
// metadata so incremental callers can make their own freshness/status decision.
func UpdateCheapEdges(ctx context.Context, repo Repository, projectID string, files []SourceFile, opts CheapExtractorOptions) error {
	_, err := UpdateCheapEdgesWithSummary(ctx, repo, projectID, files, opts)
	return err
}

// UpdateCheapEdgesWithSummary extracts deterministic local graph edges and returns
// aggregate warning/error state for callers that record graph freshness metadata.
func UpdateCheapEdgesWithSummary(ctx context.Context, repo Repository, projectID string, files []SourceFile, opts CheapExtractorOptions) (CheapExtractionSummary, error) {
	if projectID == "" {
		return CheapExtractionSummary{}, fmt.Errorf("project_id is required")
	}
	now := time.Now().UTC
	if opts.Now != nil {
		now = opts.Now
	}
	started := now()
	summary := CheapExtractionSummary{
		StartedAt:     started,
		CompletedAt:   started,
		SourceVersion: sourceVersion(files),
	}
	pathSet := make(map[string]SourceFile, len(files)+len(opts.KnownSources))
	for _, source := range opts.KnownSources {
		if source.Path != "" {
			normalized := filepath.ToSlash(source.Path)
			source.Path = normalized
			pathSet[normalized] = source
		}
	}
	for _, file := range files {
		if file.Path != "" {
			normalized := filepath.ToSlash(file.Path)
			file.Path = normalized
			pathSet[normalized] = file
		}
	}

	for _, file := range files {
		file.Path = filepath.ToSlash(file.Path)
		scope := newExtractionScope()
		extractProjectContainment(projectID, file, scope)
		extractPackageAndImports(projectID, file, scope)
		extractSymbolEdges(projectID, file, scope)
		extractConfigKeys(projectID, file, scope)
		extractTestImplementationEdge(projectID, file, pathSet, scope)
		extractDocMentions(projectID, file, pathSet, scope)

		status := ExtractorStatusSuccess
		if len(scope.errors) > 0 {
			status = ExtractorStatusFailed
			summary.HadErrors = true
		} else if len(scope.warnings) > 0 {
			status = ExtractorStatusPartial
			summary.HadWarnings = true
		}
		nodes := scope.sortedNodes()
		edges := append([]Edge(nil), scope.edges...)
		sortEdgesByNaturalKey(edges)
		completed := now()
		if err := repo.ReplaceEdges(ctx, EdgeReplacement{
			ProjectID:  projectID,
			Extractor:  ExtractorCheap,
			SourcePath: file.Path,
			Nodes:      nodes,
			Edges:      edges,
			Run: ExtractorRun{
				Status:      status,
				StartedAt:   started,
				CompletedAt: completed,
				NodeCount:   len(nodes),
				EdgeCount:   len(edges),
				Warnings:    scope.warnings,
				Errors:      scope.errors,
			},
		}); err != nil {
			return summary, fmt.Errorf("replace cheap graph edges for %s: %w", file.Path, err)
		}
		summary.CompletedAt = completed
	}

	return summary, nil
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

func extractProjectContainment(projectID string, file SourceFile, scope *extractionScope) {
	projectNode := scope.addNode(Node{
		ProjectID: projectID,
		Kind:      NodeKindProject,
		Key:       projectID,
		Name:      projectID,
	})
	fileNode := extractFileNode(projectID, file, scope)
	scope.addEdge(Edge{
		ProjectID:  projectID,
		Kind:       EdgeKindProjectContainsFile,
		FromNodeID: projectNode.ID,
		ToNodeID:   fileNode.ID,
		Extractor:  ExtractorCheap,
		SourcePath: file.Path,
		Confidence: 1.0,
		Evidence:   edgeEvidence(file.Path, "indexed_source_path", 1, 1, file.Path, false),
	})
}

func extractFileNode(projectID string, file SourceFile, scope *extractionScope) Node {
	kind, metadata := classifySource(file)
	return scope.addNode(Node{
		ProjectID:  projectID,
		Kind:       kind,
		Key:        file.Path,
		SourcePath: file.Path,
		Name:       filepath.Base(file.Path),
		Language:   file.Language,
		Metadata:   metadata,
	})
}

func classifySource(file SourceFile) (NodeKind, map[string]string) {
	metadata := map[string]string{
		"content_type": string(file.ContentType),
	}
	switch {
	case isMarkdownFile(file):
		metadata["doc_type"] = "markdown"
		return NodeKindDoc, metadata
	case isConfigFile(file):
		metadata["config_format"] = configFormat(file.Path)
		return NodeKindConfigFile, metadata
	case isTestFile(file):
		metadata["test"] = "true"
		return NodeKindTestFile, metadata
	default:
		return NodeKindFile, metadata
	}
}

func extractPackageAndImports(projectID string, file SourceFile, scope *extractionScope) {
	if file.Language != "go" {
		extractLanguageImports(projectID, file, scope)
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
	packageKey := packageName
	if dir := path.Dir(file.Path); dir != "." && dir != "" {
		packageKey = dir + "#" + packageName
	}
	packageNode := scope.addNode(Node{
		ProjectID:  projectID,
		Kind:       NodeKindPackage,
		Key:        packageKey,
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
		Evidence:   edgeEvidence(file.Path, "go_package_declaration", packageLine, packageLine, "package "+packageName, false),
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
			Kind:       EdgeKindFileImports,
			FromNodeID: fileNode.ID,
			ToNodeID:   importNode.ID,
			Extractor:  ExtractorCheap,
			SourcePath: file.Path,
			Confidence: 0.95,
			Evidence:   edgeEvidence(file.Path, "go_import_declaration", imp.Line, imp.Line, imp.Path, false),
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

type importRef struct {
	Specifier string
	Line      int
	Method    string
}

func extractLanguageImports(projectID string, file SourceFile, scope *extractionScope) {
	imports := parseLanguageImports(file)
	if len(imports) == 0 {
		return
	}
	fileNode := extractFileNode(projectID, file, scope)
	for _, imp := range imports {
		importNode := scope.addNode(Node{
			ProjectID:  projectID,
			Kind:       NodeKindImport,
			Key:        importNodeKey(file.Language, imp.Specifier),
			SourcePath: file.Path,
			Name:       imp.Specifier,
			Language:   file.Language,
			StartLine:  imp.Line,
			EndLine:    imp.Line,
		})
		scope.addEdge(Edge{
			ProjectID:  projectID,
			Kind:       EdgeKindFileImports,
			FromNodeID: fileNode.ID,
			ToNodeID:   importNode.ID,
			Extractor:  ExtractorCheap,
			SourcePath: file.Path,
			Confidence: 0.9,
			Evidence:   edgeEvidence(file.Path, imp.Method, imp.Line, imp.Line, imp.Specifier, false),
		})
	}
}

func parseLanguageImports(file SourceFile) []importRef {
	switch normalizedLanguage(file.Language, file.Path) {
	case "typescript", "javascript":
		return parseJSImports(file)
	case "python":
		return parsePythonImports(file)
	default:
		return nil
	}
}

func parseJSImports(file SourceFile) []importRef {
	content := string(file.Content)
	var imports []importRef
	seen := map[string]bool{}
	addMatches := func(pattern *regexp.Regexp, method string) {
		for _, match := range pattern.FindAllStringSubmatchIndex(content, -1) {
			if len(match) < 4 || match[2] < 0 || match[3] < 0 {
				continue
			}
			specifier := content[match[2]:match[3]]
			if specifier == "" || seen[specifier+"|"+method] {
				continue
			}
			seen[specifier+"|"+method] = true
			imports = append(imports, importRef{
				Specifier: specifier,
				Line:      1 + strings.Count(content[:match[2]], "\n"),
				Method:    method,
			})
		}
	}
	addMatches(jsImportPattern, "js_import_declaration")
	addMatches(jsExportFromPattern, "js_export_from_declaration")
	addMatches(jsRequirePattern, "js_require_call")
	sort.Slice(imports, func(i, j int) bool {
		if imports[i].Line == imports[j].Line {
			return imports[i].Specifier < imports[j].Specifier
		}
		return imports[i].Line < imports[j].Line
	})
	return imports
}

func parsePythonImports(file SourceFile) []importRef {
	var imports []importRef
	for i, line := range strings.Split(string(file.Content), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "import "):
			specs := strings.Split(strings.TrimSpace(strings.TrimPrefix(trimmed, "import ")), ",")
			for _, spec := range specs {
				spec = strings.TrimSpace(strings.Split(spec, " as ")[0])
				if spec != "" {
					imports = append(imports, importRef{Specifier: spec, Line: i + 1, Method: "python_import_statement"})
				}
			}
		case strings.HasPrefix(trimmed, "from "):
			rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "from "))
			module, _, ok := strings.Cut(rest, " import ")
			module = strings.TrimSpace(module)
			if ok && module != "" {
				imports = append(imports, importRef{Specifier: module, Line: i + 1, Method: "python_from_import_statement"})
			}
		}
	}
	return imports
}

func importNodeKey(language, specifier string) string {
	language = strings.TrimSpace(language)
	if language == "" {
		return specifier
	}
	return language + ":" + specifier
}

func extractSymbolEdges(projectID string, file SourceFile, scope *extractionScope) {
	if len(file.Chunks) == 0 {
		return
	}
	fileNode := extractFileNode(projectID, file, scope)
	// DEBT-041: a single declaration can reach extraction more than once — e.g. when
	// a stale chunk and its re-indexed successor both survive in the store and are
	// rebuilt together, the same symbol arrives in two overlapping, line-drifted
	// chunks. Emitting a node per occurrence produced duplicate same-file symbol
	// nodes (relatedResults:241 and :243). canonicalSymbolChunks collapses those
	// drift duplicates to the declaration head while keeping genuinely distinct
	// same-name declarations (disjoint ranges) intact.
	keep := canonicalSymbolChunks(file.Chunks)
	for ci, chunk := range file.Chunks {
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
		for si, symbol := range chunk.Symbols {
			if symbol.Name == "" {
				continue
			}
			if !keep[symbolLocator{chunk: ci, symbol: si}] {
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
				Evidence:   edgeEvidence(file.Path, "chunk_symbol", symbol.StartLine, symbol.EndLine, symbol.Signature, false),
			})
			scope.addEdge(Edge{
				ProjectID:  projectID,
				Kind:       EdgeKindSymbolHasChunk,
				FromNodeID: symbolNode.ID,
				ToNodeID:   chunkNode.ID,
				Extractor:  ExtractorCheap,
				SourcePath: file.Path,
				Confidence: 1.0,
				Evidence:   edgeEvidence(file.Path, "chunk_symbol_membership", chunk.StartLine, chunk.EndLine, chunk.ID, false),
			})
		}
	}
}

// symbolLocator identifies one symbol by its position within a file's chunk set.
type symbolLocator struct {
	chunk  int
	symbol int
}

// canonicalSymbolChunks selects, for a single file's extraction, the set of
// (chunk, symbol) positions whose symbol node should be emitted (DEBT-041).
//
// It collapses a drift duplicate — the same declaration carried by a stale chunk
// and its re-indexed successor (identical name + symbol kind, overlapping line
// ranges) — to the occurrence with the lowest start line (the declaration head),
// while keeping genuinely distinct same-name declarations whose ranges are
// disjoint (e.g. two methods named Close in one file). Within one file, two
// symbols sharing a name and kind cannot both be valid declarations at overlapping
// lines, so range overlap is a reliable same-declaration signal. Symbols without
// line information already share a natural key (path#name:0) and collapse at the
// node layer, so they need no special handling here.
//
// Limitation: when the metadata store holds multiple chunk generations for a file
// (a stale chunk plus its re-indexed successor — DEBT-042), the lowest-start
// occurrence kept here is a deterministic representative, not a guaranteed-current
// one. If an edit shifted the declaration down, the stale generation has the lower
// line and wins, so the surviving node's line/snippet can be stale (graph.query may
// then cite a slightly-off location at "exact" confidence). This is undecidable at
// the graph layer — SourceChunk carries no generation/currency signal — and is the
// reason the real fix is store-side replace-by-file (DEBT-042): once a file has a
// single current generation, the dedup never fires and the surviving line is always
// current. This collapse is still strictly better than the prior duplicate nodes.
func canonicalSymbolChunks(chunks []SourceChunk) map[symbolLocator]bool {
	type candidate struct {
		loc       symbolLocator
		startLine int
		endLine   int
	}
	groups := map[string][]candidate{}
	var order []string
	for ci, chunk := range chunks {
		for si, sym := range chunk.Symbols {
			if sym.Name == "" {
				continue
			}
			key := sym.Name + "\x00" + sym.Kind
			if _, seen := groups[key]; !seen {
				order = append(order, key)
			}
			groups[key] = append(groups[key], candidate{
				loc:       symbolLocator{chunk: ci, symbol: si},
				startLine: sym.StartLine,
				endLine:   sym.EndLine,
			})
		}
	}

	keep := make(map[symbolLocator]bool)
	for _, key := range order {
		cands := groups[key]
		// Deterministic order: declaration head (lowest start line) first, then
		// chunk/symbol position, so the kept occurrence is stable regardless of how
		// the (possibly stale) chunks were ordered on input.
		sort.SliceStable(cands, func(i, j int) bool {
			if cands[i].startLine != cands[j].startLine {
				return cands[i].startLine < cands[j].startLine
			}
			if cands[i].loc.chunk != cands[j].loc.chunk {
				return cands[i].loc.chunk < cands[j].loc.chunk
			}
			return cands[i].loc.symbol < cands[j].loc.symbol
		})
		var kept []candidate
		for _, c := range cands {
			overlapsKept := false
			for _, k := range kept {
				if symbolRangesOverlap(c.startLine, c.endLine, k.startLine, k.endLine) {
					overlapsKept = true
					break
				}
			}
			if overlapsKept {
				continue // drift duplicate of an already-kept declaration
			}
			kept = append(kept, c)
			keep[c.loc] = true
		}
	}
	return keep
}

// symbolRangesOverlap reports whether two inclusive line ranges intersect. A
// zero/negative end line is normalized to a single-line range at the start.
func symbolRangesOverlap(aStart, aEnd, bStart, bEnd int) bool {
	if aEnd < aStart {
		aEnd = aStart
	}
	if bEnd < bStart {
		bEnd = bStart
	}
	return aStart <= bEnd && bStart <= aEnd
}

func extractConfigKeys(projectID string, file SourceFile, scope *extractionScope) {
	if !isConfigFile(file) {
		return
	}
	keys, err := configKeyRefs(file)
	if err != nil {
		scope.errors = append(scope.errors, fmt.Sprintf("parse config %s: %v", file.Path, err))
		return
	}
	fileNode := extractFileNode(projectID, file, scope)
	for _, key := range keys {
		configNode := scope.addNode(Node{
			ProjectID:  projectID,
			Kind:       NodeKindConfigKey,
			Key:        file.Path + "#" + key.Path,
			SourcePath: file.Path,
			Name:       key.Path,
			Language:   file.Language,
			StartLine:  key.Line,
			EndLine:    key.Line,
			Metadata: map[string]string{
				"key_path": key.Path,
			},
		})
		scope.addEdge(Edge{
			ProjectID:  projectID,
			Kind:       EdgeKindFileDefinesConfigKey,
			FromNodeID: fileNode.ID,
			ToNodeID:   configNode.ID,
			Extractor:  ExtractorCheap,
			SourcePath: file.Path,
			Confidence: 0.9,
			Evidence:   edgeEvidence(file.Path, key.Method, key.Line, key.Line, key.Path, false),
		})
	}
}

func isConfigFile(file SourceFile) bool {
	if file.ContentType == SourceContentTypeConfig {
		return true
	}
	switch strings.ToLower(filepath.Ext(file.Path)) {
	case ".yaml", ".yml", ".json", ".toml", ".env", ".properties":
		return true
	default:
		return false
	}
}

type configKeyRef struct {
	Path   string
	Line   int
	Method string
}

func configKeyRefs(file SourceFile) ([]configKeyRef, error) {
	switch strings.ToLower(filepath.Ext(file.Path)) {
	case ".yaml", ".yml":
		var root yaml.Node
		if err := yaml.Unmarshal(file.Content, &root); err != nil {
			return nil, err
		}
		keys := collectYAMLKeys(&root, nil)
		sortConfigKeyRefs(keys)
		return keys, nil
	case ".json":
		var value any
		if err := json.Unmarshal(file.Content, &value); err != nil {
			return nil, err
		}
		keys := collectJSONKeys(value, nil, string(file.Content))
		sortConfigKeyRefs(keys)
		return keys, nil
	case ".toml":
		keys := collectTOMLKeys(string(file.Content))
		sortConfigKeyRefs(keys)
		return keys, nil
	case ".env":
		keys := collectEnvKeys(string(file.Content))
		sortConfigKeyRefs(keys)
		return keys, nil
	case ".properties":
		keys := collectPropertiesKeys(string(file.Content))
		sortConfigKeyRefs(keys)
		return keys, nil
	default:
		return nil, nil
	}
}

func collectYAMLKeys(node *yaml.Node, prefix []string) []configKeyRef {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		return collectYAMLKeys(node.Content[0], prefix)
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}
	var keys []configKeyRef
	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]
		path := append(append([]string{}, prefix...), keyNode.Value)
		keys = append(keys, configKeyRef{Path: strings.Join(path, "."), Line: positiveLine(keyNode.Line), Method: "yaml_config_key_parse"})
		keys = append(keys, collectYAMLKeys(valueNode, path)...)
	}
	return keys
}

func collectJSONKeys(value any, prefix []string, content string) []configKeyRef {
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	var keys []configKeyRef
	for key, child := range object {
		path := append(append([]string{}, prefix...), key)
		keyPath := strings.Join(path, ".")
		keys = append(keys, configKeyRef{Path: keyPath, Line: lineForToken(content, `"`+key+`"`), Method: "json_config_key_parse"})
		keys = append(keys, collectJSONKeys(child, path, content)...)
	}
	return keys
}

func collectTOMLKeys(content string) []configKeyRef {
	var keys []configKeyRef
	var section []string
	for i, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.Contains(trimmed, "]") {
			name := strings.TrimSpace(strings.Trim(trimmed, "[]"))
			if name != "" {
				section = strings.Split(name, ".")
				keys = append(keys, configKeyRef{Path: strings.Join(section, "."), Line: i + 1, Method: "toml_config_key_parse"})
			}
			continue
		}
		if idx := strings.Index(trimmed, "="); idx > 0 {
			key := strings.TrimSpace(trimmed[:idx])
			path := append(append([]string{}, section...), key)
			keys = append(keys, configKeyRef{Path: strings.Join(path, "."), Line: i + 1, Method: "toml_config_key_parse"})
		}
	}
	return keys
}

func collectEnvKeys(content string) []configKeyRef {
	var keys []configKeyRef
	for i, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		trimmed = strings.TrimPrefix(trimmed, "export ")
		if idx := strings.Index(trimmed, "="); idx > 0 {
			key := strings.TrimSpace(trimmed[:idx])
			if key != "" {
				keys = append(keys, configKeyRef{Path: key, Line: i + 1, Method: "env_config_key_parse"})
			}
		}
	}
	return keys
}

func collectPropertiesKeys(content string) []configKeyRef {
	var keys []configKeyRef
	for i, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "!") {
			continue
		}
		idx := strings.IndexAny(trimmed, "=:")
		if idx <= 0 {
			fields := strings.Fields(trimmed)
			if len(fields) < 2 {
				continue
			}
			keys = append(keys, configKeyRef{Path: fields[0], Line: i + 1, Method: "properties_config_key_parse"})
			continue
		}
		key := strings.TrimSpace(trimmed[:idx])
		if key != "" {
			keys = append(keys, configKeyRef{Path: key, Line: i + 1, Method: "properties_config_key_parse"})
		}
	}
	return keys
}

func sortConfigKeyRefs(keys []configKeyRef) {
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Path == keys[j].Path {
			return keys[i].Line < keys[j].Line
		}
		return keys[i].Path < keys[j].Path
	})
}

func extractTestImplementationEdge(projectID string, file SourceFile, pathSet map[string]SourceFile, scope *extractionScope) {
	if !isTestFile(file) {
		return
	}
	candidates := testImplementationCandidates(file, pathSet)
	if len(candidates) == 0 {
		return
	}
	testNode := extractFileNode(projectID, file, scope)
	if len(candidates) > maxTestImplementationTargets {
		candidates = candidates[:maxTestImplementationTargets]
	}
	for _, candidate := range candidates {
		implNode := extractFileNode(projectID, candidate.Source, scope)
		scope.addEdge(Edge{
			ProjectID:  projectID,
			Kind:       EdgeKindTestCoversImplementation,
			FromNodeID: testNode.ID,
			ToNodeID:   implNode.ID,
			Extractor:  ExtractorCheap,
			SourcePath: file.Path,
			Confidence: testImplementationConfidence(candidate),
			Evidence:   edgeEvidence(file.Path, candidate.Method, candidate.Line, candidate.Line, candidate.Source.Path, true),
		})
	}
}

type implementationCandidate struct {
	Source SourceFile
	Method string
	Line   int
}

func testImplementationConfidence(candidate implementationCandidate) float64 {
	switch candidate.Method {
	case "test_import_reference":
		return 0.45
	default:
		return 0.72
	}
}

func testImplementationCandidates(file SourceFile, pathSet map[string]SourceFile) []implementationCandidate {
	candidates := map[string]implementationCandidate{}
	add := func(pathValue, method string, line int) {
		pathValue = filepath.ToSlash(path.Clean(pathValue))
		source, ok := pathSet[pathValue]
		if !ok || source.Path == file.Path || classifyKind(source) != NodeKindFile {
			return
		}
		if line <= 0 {
			line = 1
		}
		candidate := implementationCandidate{Source: source, Method: method, Line: line}
		if existing, ok := candidates[pathValue]; ok && testImplementationConfidence(existing) >= testImplementationConfidence(candidate) {
			return
		}
		candidates[pathValue] = candidate
	}

	for _, direct := range directImplementationPaths(file) {
		add(direct, "test_filename_convention", 1)
	}
	for _, imp := range parseLanguageImports(file) {
		for _, resolved := range resolveImportToKnownSources(file.Path, imp.Specifier, pathSet) {
			add(resolved, "test_import_reference", imp.Line)
		}
	}

	ordered := make([]implementationCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		ordered = append(ordered, candidate)
	}
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].Source.Path < ordered[j].Source.Path
	})
	return ordered
}

func directImplementationPaths(file SourceFile) []string {
	dir := path.Dir(file.Path)
	base := path.Base(file.Path)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	var names []string
	switch normalizedLanguage(file.Language, file.Path) {
	case "go":
		if strings.HasSuffix(stem, "_test") {
			names = append(names, strings.TrimSuffix(stem, "_test")+".go")
		}
	case "python":
		switch {
		case strings.HasPrefix(stem, "test_"):
			names = append(names, strings.TrimPrefix(stem, "test_")+".py")
		case strings.HasSuffix(stem, "_test"):
			names = append(names, strings.TrimSuffix(stem, "_test")+".py")
		}
	case "typescript", "javascript":
		for _, marker := range []string{".test", ".spec"} {
			if strings.HasSuffix(stem, marker) {
				names = append(names, strings.TrimSuffix(stem, marker)+ext)
			}
		}
	}
	var paths []string
	for _, name := range names {
		paths = append(paths, path.Join(dir, name))
		if strings.Contains(dir, "/__tests__") {
			paths = append(paths, path.Join(strings.Replace(dir, "/__tests__", "", 1), name))
		}
		for _, segment := range []string{"tests", "spec"} {
			if strings.Contains(dir, "/"+segment) {
				paths = append(paths, path.Join(strings.Replace(dir, "/"+segment, "", 1), name))
			}
			if strings.HasPrefix(dir, segment) {
				paths = append(paths, path.Join(strings.TrimPrefix(dir, segment+"/"), name))
				paths = append(paths, path.Join("src", strings.TrimPrefix(dir, segment+"/"), name))
			}
		}
	}
	return uniqueStrings(paths)
}

func extractDocMentions(projectID string, file SourceFile, pathSet map[string]SourceFile, scope *extractionScope) {
	if !isMarkdownFile(file) {
		return
	}
	docNode := extractFileNode(projectID, file, scope)
	for _, mention := range mentionedKnownPaths(string(file.Content), pathSet, file.Path) {
		targetFile := pathSet[mention.Path]
		targetNode := extractFileNode(projectID, targetFile, scope)
		scope.addEdge(Edge{
			ProjectID:  projectID,
			Kind:       EdgeKindDocMentionsFile,
			FromNodeID: docNode.ID,
			ToNodeID:   targetNode.ID,
			Extractor:  ExtractorCheap,
			SourcePath: file.Path,
			Confidence: mention.Confidence,
			Evidence:   edgeEvidence(file.Path, mention.Method, mention.Line, mention.Line, mention.Path, false),
		})
	}
	for _, mention := range mentionedKnownSymbols(string(file.Content), pathSet, file.Path) {
		mention.Node.ProjectID = projectID
		symbolNode := scope.addNode(mention.Node)
		scope.addEdge(Edge{
			ProjectID:  projectID,
			Kind:       EdgeKindDocMentionsSymbol,
			FromNodeID: docNode.ID,
			ToNodeID:   symbolNode.ID,
			Extractor:  ExtractorCheap,
			SourcePath: file.Path,
			Confidence: 0.72,
			Evidence:   edgeEvidence(file.Path, "markdown_inline_symbol_reference", mention.Line, mention.Line, mention.Node.Name, true),
		})
	}
	for _, mention := range mentionedKnownConfigKeys(string(file.Content), pathSet, file.Path) {
		mention.Node.ProjectID = projectID
		configNode := scope.addNode(mention.Node)
		scope.addEdge(Edge{
			ProjectID:  projectID,
			Kind:       EdgeKindDocMentionsConfigKey,
			FromNodeID: docNode.ID,
			ToNodeID:   configNode.ID,
			Extractor:  ExtractorCheap,
			SourcePath: file.Path,
			Confidence: 0.72,
			Evidence:   edgeEvidence(file.Path, "markdown_inline_config_key_reference", mention.Line, mention.Line, mention.Node.Name, true),
		})
	}
}

func isMarkdownFile(file SourceFile) bool {
	return file.ContentType == SourceContentTypeMarkdown || strings.EqualFold(filepath.Ext(file.Path), ".md")
}

type knownPathMention struct {
	Path       string
	Line       int
	Method     string
	Confidence float64
}

func mentionedKnownPaths(content string, pathSet map[string]SourceFile, self string) []knownPathMention {
	var mentions []knownPathMention
	for path := range pathSet {
		if path == self {
			continue
		}
		if mention, ok := knownPathMentionLine(content, path, self); ok {
			mentions = append(mentions, mention)
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

func knownPathMentionLine(content, targetPath, self string) (knownPathMention, bool) {
	markers := []struct {
		value      string
		method     string
		confidence float64
	}{
		{value: "`" + targetPath + "`", method: "markdown_inline_path_reference", confidence: 0.72},
		{value: "](" + targetPath + ")", method: "markdown_link_path_reference", confidence: 0.9},
		{value: "](" + targetPath + " ", method: "markdown_link_path_reference", confidence: 0.9},
		{value: "](" + targetPath + "\t", method: "markdown_link_path_reference", confidence: 0.9},
	}
	if rel, ok := relativePathFromDoc(self, targetPath); ok {
		markers = append(markers,
			struct {
				value      string
				method     string
				confidence float64
			}{value: "](" + rel + ")", method: "markdown_link_path_reference", confidence: 0.9},
			struct {
				value      string
				method     string
				confidence float64
			}{value: "](" + rel + " ", method: "markdown_link_path_reference", confidence: 0.9},
			struct {
				value      string
				method     string
				confidence float64
			}{value: "](" + rel + "\t", method: "markdown_link_path_reference", confidence: 0.9},
		)
	}
	for _, marker := range markers {
		if idx := strings.Index(content, marker.value); idx >= 0 {
			return knownPathMention{Path: targetPath, Line: 1 + strings.Count(content[:idx], "\n"), Method: marker.method, Confidence: marker.confidence}, true
		}
	}

	for i, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == targetPath {
			return knownPathMention{Path: targetPath, Line: i + 1, Method: "markdown_line_path_reference", Confidence: 0.72}, true
		}
	}
	return knownPathMention{}, false
}

type knownNodeMention struct {
	Node Node
	Line int
}

func mentionedKnownSymbols(content string, pathSet map[string]SourceFile, docPath string) []knownNodeMention {
	var mentions []knownNodeMention
	seen := map[string]bool{}
	symbolsByName := knownSymbolsByName(pathSet)
	for _, source := range sortedSources(pathSet) {
		for _, chunk := range source.Chunks {
			for _, symbol := range chunk.Symbols {
				if symbol.Name == "" {
					continue
				}
				if len(symbolsByName[symbol.Name]) != 1 {
					continue
				}
				line, ok := inlineCodeMentionLine(content, symbol.Name)
				if !ok {
					continue
				}
				symbolKey := fmt.Sprintf("%s#%s:%d", source.Path, symbol.Name, symbol.StartLine)
				if seen[symbolKey] {
					continue
				}
				seen[symbolKey] = true
				mentions = append(mentions, knownNodeMention{
					Line: line,
					Node: Node{
						ProjectID:  "", // filled by scope normalization caller.
						Kind:       NodeKindSymbol,
						Key:        symbolKey,
						SourcePath: source.Path,
						Name:       symbol.Name,
						Language:   source.Language,
						SymbolKind: symbol.Kind,
						StartLine:  symbol.StartLine,
						EndLine:    symbol.EndLine,
						Metadata: map[string]string{
							"signature": symbol.Signature,
						},
					},
				})
			}
		}
	}
	_ = docPath
	return mentions
}

func knownSymbolsByName(pathSet map[string]SourceFile) map[string][]SourceSymbol {
	byName := map[string][]SourceSymbol{}
	for _, source := range sortedSources(pathSet) {
		for _, chunk := range source.Chunks {
			for _, symbol := range chunk.Symbols {
				if symbol.Name == "" {
					continue
				}
				byName[symbol.Name] = append(byName[symbol.Name], symbol)
			}
		}
	}
	return byName
}

func mentionedKnownConfigKeys(content string, pathSet map[string]SourceFile, docPath string) []knownNodeMention {
	var mentions []knownNodeMention
	seen := map[string]bool{}
	for _, source := range sortedSources(pathSet) {
		if !isConfigFile(source) {
			continue
		}
		keys, err := configKeyRefs(source)
		if err != nil {
			continue
		}
		for _, key := range keys {
			line, ok := inlineCodeMentionLine(content, key.Path)
			if !ok {
				continue
			}
			nodeKey := source.Path + "#" + key.Path
			if seen[nodeKey] {
				continue
			}
			seen[nodeKey] = true
			mentions = append(mentions, knownNodeMention{
				Line: line,
				Node: Node{
					Kind:       NodeKindConfigKey,
					Key:        nodeKey,
					SourcePath: source.Path,
					Name:       key.Path,
					Language:   source.Language,
					StartLine:  key.Line,
					EndLine:    key.Line,
					Metadata: map[string]string{
						"key_path": key.Path,
					},
				},
			})
		}
	}
	_ = docPath
	return mentions
}

func inlineCodeMentionLine(content, token string) (int, bool) {
	if token == "" {
		return 0, false
	}
	marker := "`" + token + "`"
	if idx := strings.Index(content, marker); idx >= 0 {
		return 1 + strings.Count(content[:idx], "\n"), true
	}
	return 0, false
}

func sortedSources(pathSet map[string]SourceFile) []SourceFile {
	sources := make([]SourceFile, 0, len(pathSet))
	for _, source := range pathSet {
		source.Path = filepath.ToSlash(source.Path)
		sources = append(sources, source)
	}
	sort.Slice(sources, func(i, j int) bool {
		return sources[i].Path < sources[j].Path
	})
	return sources
}

func edgeEvidence(sourcePath, method string, lineStart, lineEnd int, snippet string, heuristic bool) Evidence {
	lineStart = positiveLine(lineStart)
	if lineEnd <= 0 {
		lineEnd = lineStart
	}
	if lineEnd < lineStart {
		lineEnd = lineStart
	}
	return Evidence{
		Method:     method,
		SourcePath: sourcePath,
		Snippet:    boundedSnippet(snippet),
		Line:       lineStart,
		LineStart:  lineStart,
		LineEnd:    lineEnd,
		Heuristic:  heuristic,
	}
}

func boundedSnippet(snippet string) string {
	snippet = strings.TrimSpace(snippet)
	if len(snippet) > 160 {
		return snippet[:160]
	}
	return snippet
}

func positiveLine(line int) int {
	if line <= 0 {
		return 1
	}
	return line
}

func lineForToken(content, token string) int {
	if token == "" {
		return 1
	}
	if idx := strings.Index(content, token); idx >= 0 {
		return 1 + strings.Count(content[:idx], "\n")
	}
	return 1
}

func classifyKind(file SourceFile) NodeKind {
	kind, _ := classifySource(file)
	return kind
}

func isTestFile(file SourceFile) bool {
	language := normalizedLanguage(file.Language, file.Path)
	base := path.Base(file.Path)
	stem := strings.TrimSuffix(base, path.Ext(base))
	dirParts := strings.Split(path.Dir(file.Path), "/")
	inTestDir := false
	for _, part := range dirParts {
		switch part {
		case "tests", "test", "spec", "__tests__":
			inTestDir = true
		}
	}
	switch language {
	case "go":
		return strings.HasSuffix(base, "_test.go")
	case "python":
		return strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py") || inTestDir
	case "typescript", "javascript":
		return strings.Contains(stem, ".test") || strings.Contains(stem, ".spec") || inTestDir
	default:
		return false
	}
}

func normalizedLanguage(language, filePath string) string {
	language = strings.ToLower(strings.TrimSpace(language))
	switch language {
	case "ts", "tsx", "typescriptreact":
		return "typescript"
	case "js", "jsx", "javascriptreact", "node":
		return "javascript"
	case "py":
		return "python"
	case "":
		switch strings.ToLower(path.Ext(filePath)) {
		case ".ts", ".tsx":
			return "typescript"
		case ".js", ".jsx", ".mjs", ".cjs":
			return "javascript"
		case ".py":
			return "python"
		}
	}
	return language
}

func configFormat(filePath string) string {
	ext := strings.TrimPrefix(strings.ToLower(path.Ext(filePath)), ".")
	if ext == "" && path.Base(filePath) == ".env" {
		return "env"
	}
	return ext
}

func relativePathFromDoc(docPath, targetPath string) (string, bool) {
	docDir := path.Dir(docPath)
	if docDir == "." || docDir == "" {
		return targetPath, true
	}
	rel, err := filepath.Rel(filepath.FromSlash(docDir), filepath.FromSlash(targetPath))
	if err != nil {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func resolveImportToKnownSources(importerPath, specifier string, pathSet map[string]SourceFile) []string {
	if specifier == "" {
		return nil
	}
	var candidates []string
	if strings.HasPrefix(specifier, ".") {
		base := path.Clean(path.Join(path.Dir(importerPath), specifier))
		candidates = append(candidates, base)
		for _, ext := range []string{".go", ".py", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"} {
			candidates = append(candidates, base+ext)
			candidates = append(candidates, path.Join(base, "index"+ext))
		}
	} else {
		modulePath := strings.ReplaceAll(specifier, ".", "/")
		candidates = append(candidates, modulePath)
		for _, ext := range []string{".py", ".ts", ".tsx", ".js", ".jsx", ".go"} {
			candidates = append(candidates, modulePath+ext)
			candidates = append(candidates, path.Join("src", modulePath+ext))
		}
	}
	var resolved []string
	for _, candidate := range uniqueStrings(candidates) {
		candidate = filepath.ToSlash(path.Clean(candidate))
		if _, ok := pathSet[candidate]; ok {
			resolved = append(resolved, candidate)
		}
	}
	sort.Strings(resolved)
	return resolved
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
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
