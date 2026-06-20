package graph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"time"
)

// SchemaVersion is the current disposable graph overlay schema version.
const SchemaVersion = 3

const (
	// DefaultStaleAfter is the named freshness window used by graph query/status
	// callers when no project-specific value is configured.
	DefaultStaleAfter = 24 * time.Hour
	// DefaultStalePurgeAfter is the named retention window for stale graph edges
	// in coordinator maintenance paths.
	DefaultStalePurgeAfter = 7 * 24 * time.Hour
)

const (
	// ExtractorCheap identifies deterministic local edge extractors.
	ExtractorCheap = "cheap"
)

// NodeKind identifies the graph primitive represented by a node.
type NodeKind string

const (
	NodeKindProject    NodeKind = "project"
	NodeKindFile       NodeKind = "file"
	NodeKindTestFile   NodeKind = "test_file"
	NodeKindDoc        NodeKind = "doc"
	NodeKindConfigFile NodeKind = "config_file"
	NodeKindPackage    NodeKind = "package"
	NodeKindImport     NodeKind = "import"
	NodeKindSymbol     NodeKind = "symbol"
	NodeKindChunk      NodeKind = "chunk"
	NodeKindConfigKey  NodeKind = "config_key"
)

// EdgeKind identifies the relationship represented by an edge.
type EdgeKind string

const (
	EdgeKindProjectContainsFile      EdgeKind = "project_contains_file"
	EdgeKindFileDeclaresPackage      EdgeKind = "file_declares_package"
	EdgeKindFileImports              EdgeKind = "file_imports"
	EdgeKindPackageImports           EdgeKind = "package_imports"
	EdgeKindFileDefinesSymbol        EdgeKind = "file_defines_symbol"
	EdgeKindSymbolHasChunk           EdgeKind = "symbol_has_chunk"
	EdgeKindFileDefinesConfigKey     EdgeKind = "file_defines_config_key"
	EdgeKindTestCoversImplementation EdgeKind = "test_covers_implementation"
	EdgeKindDocMentionsFile          EdgeKind = "doc_mentions_file"
	EdgeKindDocMentionsSymbol        EdgeKind = "doc_mentions_symbol"
	EdgeKindDocMentionsConfigKey     EdgeKind = "doc_mentions_config_key"
	EdgeKindDocMentionsPath          EdgeKind = "doc_mentions_path"
)

// ConfidenceLabel is a compact, stable confidence bucket for status output.
type ConfidenceLabel string

const (
	ConfidenceHigh   ConfidenceLabel = "high"
	ConfidenceMedium ConfidenceLabel = "medium"
	ConfidenceLow    ConfidenceLabel = "low"
	ConfidenceExact  ConfidenceLabel = "exact"
)

// GraphStatus is the stored or derived graph health state.
type GraphStatus string

const (
	GraphStatusUnavailable  GraphStatus = "unavailable"
	GraphStatusIncompatible GraphStatus = "incompatible"
	GraphStatusEmpty        GraphStatus = "empty"
	GraphStatusFresh        GraphStatus = "fresh"
	GraphStatusStale        GraphStatus = "stale"
	GraphStatusPartial      GraphStatus = "partial"
	GraphStatusFailed       GraphStatus = "failed"
)

// BuildKind identifies whether build metadata came from a full rebuild or an
// incremental source update.
type BuildKind string

const (
	BuildKindFull        BuildKind = "full"
	BuildKindIncremental BuildKind = "incremental"
)

// FreshnessState explains whether the graph build is recent enough to trust.
type FreshnessState string

const (
	FreshnessUnknown FreshnessState = "unknown"
	FreshnessFresh   FreshnessState = "fresh"
	FreshnessStale   FreshnessState = "stale"
)

// ExtractorStatus summarizes one extractor/source run.
type ExtractorStatus string

const (
	ExtractorStatusSuccess ExtractorStatus = "success"
	ExtractorStatusPartial ExtractorStatus = "partial"
	ExtractorStatusFailed  ExtractorStatus = "failed"
)

// WarningCode is a stable machine-readable graph_status warning code.
type WarningCode string

const (
	WarningGraphUnavailable   WarningCode = "graph_unavailable"
	WarningSchemaIncompatible WarningCode = "schema_incompatible"
	WarningGraphStale         WarningCode = "graph_stale"
	WarningGraphStaleEdges    WarningCode = "graph_stale_edges"
	WarningExtractorFailed    WarningCode = "extractor_failed"
	WarningExtractorPartial   WarningCode = "extractor_partial"
	WarningBuildFailed        WarningCode = "build_failed"
)

// Node is a typed graph entity. Key is the stable natural key within kind/project.
type Node struct {
	ID         string            `json:"id"`
	ProjectID  string            `json:"project_id"`
	Kind       NodeKind          `json:"kind"`
	Key        string            `json:"key"`
	SourcePath string            `json:"source_path,omitempty"`
	Name       string            `json:"name,omitempty"`
	Language   string            `json:"language,omitempty"`
	SymbolKind string            `json:"symbol_kind,omitempty"`
	StartLine  int               `json:"start_line,omitempty"`
	EndLine    int               `json:"end_line,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	CreatedAt  time.Time         `json:"created_at,omitempty"`
	UpdatedAt  time.Time         `json:"updated_at,omitempty"`
}

// Evidence explains why an edge exists. Heuristic must be true for inferred edges.
type Evidence struct {
	Method     string `json:"method"`
	SourcePath string `json:"source_path,omitempty"`
	Snippet    string `json:"snippet,omitempty"`
	Line       int    `json:"line,omitempty"`
	LineStart  int    `json:"line_start,omitempty"`
	LineEnd    int    `json:"line_end,omitempty"`
	Heuristic  bool   `json:"heuristic,omitempty"`
}

// Edge is a typed relationship between two existing nodes.
type Edge struct {
	ID              string          `json:"id"`
	ProjectID       string          `json:"project_id"`
	Kind            EdgeKind        `json:"kind"`
	FromNodeID      string          `json:"from_node_id"`
	ToNodeID        string          `json:"to_node_id"`
	Extractor       string          `json:"extractor"`
	SourcePath      string          `json:"source_path"`
	SourceVersion   string          `json:"source_version,omitempty"`
	Evidence        Evidence        `json:"evidence"`
	Confidence      float64         `json:"confidence"`
	ConfidenceLabel ConfidenceLabel `json:"confidence_label"`
	Stale           bool            `json:"stale,omitempty"`
	CreatedAt       time.Time       `json:"created_at,omitempty"`
	UpdatedAt       time.Time       `json:"updated_at,omitempty"`
}

// NaturalKey returns the replacement/idempotency key used for deterministic rebuilds.
func (e Edge) NaturalKey() string {
	return strings.Join([]string{
		e.ProjectID,
		e.Extractor,
		e.SourcePath,
		string(e.Kind),
		e.FromNodeID,
		e.ToNodeID,
	}, "|")
}

// NodeQuery filters node reads.
type NodeQuery struct {
	ProjectID string
	Kind      NodeKind
}

// EdgeQuery filters edge reads.
type EdgeQuery struct {
	ProjectID    string
	Kind         EdgeKind
	Extractor    string
	SourcePath   string
	ExcludeStale bool
	OnlyStale    bool
}

// EdgeReplacement atomically replaces all edges for one extractor/source scope.
type EdgeReplacement struct {
	ProjectID  string
	Extractor  string
	SourcePath string
	Nodes      []Node
	Edges      []Edge
	Run        ExtractorRun
}

// BuildMetadata records the latest graph build state for a project.
type BuildMetadata struct {
	ProjectID     string      `json:"project_id"`
	Kind          BuildKind   `json:"kind,omitempty"`
	Status        GraphStatus `json:"status"`
	StartedAt     time.Time   `json:"started_at,omitempty"`
	CompletedAt   time.Time   `json:"completed_at,omitempty"`
	SourceVersion string      `json:"source_version,omitempty"`
	Message       string      `json:"message,omitempty"`
}

// ExtractorRun records the latest extractor/source run state for status snapshots.
type ExtractorRun struct {
	ProjectID    string          `json:"project_id"`
	Extractor    string          `json:"extractor"`
	SourcePath   string          `json:"source_path"`
	Status       ExtractorStatus `json:"status"`
	StartedAt    time.Time       `json:"started_at,omitempty"`
	CompletedAt  time.Time       `json:"completed_at,omitempty"`
	NodeCount    int             `json:"node_count"`
	EdgeCount    int             `json:"edge_count"`
	WarningCount int             `json:"warning_count"`
	ErrorCount   int             `json:"error_count"`
	Warnings     []string        `json:"warnings,omitempty"`
	Errors       []string        `json:"errors,omitempty"`
}

// CountSummary is a compact count distribution.
type CountSummary struct {
	Total  int            `json:"total"`
	ByKind map[string]int `json:"by_kind,omitempty"`
}

// Freshness is the build freshness contract exposed by graph_status.
type Freshness struct {
	State             FreshnessState `json:"state"`
	StartedAt         string         `json:"started_at,omitempty"`
	CompletedAt       string         `json:"completed_at,omitempty"`
	AgeSeconds        int64          `json:"age_seconds,omitempty"`
	StaleAfterSeconds int64          `json:"stale_after_seconds,omitempty"`
	SourceVersion     string         `json:"source_version,omitempty"`
}

// BuildTiming exposes compact build timestamps in graph_status.
type BuildTiming struct {
	StartedAt     string `json:"started_at,omitempty"`
	CompletedAt   string `json:"completed_at,omitempty"`
	SourceVersion string `json:"source_version,omitempty"`
}

// ExtractorSummary is the compact status contract for one extractor/source run.
type ExtractorSummary struct {
	Name         string          `json:"name"`
	SourcePath   string          `json:"source_path,omitempty"`
	Status       ExtractorStatus `json:"status"`
	NodeCount    int             `json:"node_count,omitempty"`
	EdgeCount    int             `json:"edge_count,omitempty"`
	WarningCount int             `json:"warning_count,omitempty"`
	ErrorCount   int             `json:"error_count,omitempty"`
	Message      string          `json:"message,omitempty"`
	CompletedAt  string          `json:"completed_at,omitempty"`
}

// StatusWarning is a compact machine-readable degradation warning.
type StatusWarning struct {
	Code       WarningCode `json:"code"`
	Message    string      `json:"message"`
	Extractor  string      `json:"extractor,omitempty"`
	SourcePath string      `json:"source_path,omitempty"`
}

// StatusSnapshot is the graph_status resource payload.
type StatusSnapshot struct {
	Available             bool               `json:"available"`
	SchemaVersion         int                `json:"schema_version"`
	Status                GraphStatus        `json:"status"`
	GeneratedAt           time.Time          `json:"generated_at"`
	Freshness             Freshness          `json:"freshness"`
	LastFullBuild         *BuildTiming       `json:"last_full_build,omitempty"`
	LastIncrementalUpdate *BuildTiming       `json:"last_incremental_update,omitempty"`
	Nodes                 CountSummary       `json:"nodes"`
	Edges                 CountSummary       `json:"edges"`
	ActiveEdges           CountSummary       `json:"active_edges"`
	StaleEdges            CountSummary       `json:"stale_edges"`
	Extractors            []ExtractorSummary `json:"extractors,omitempty"`
	Confidence            map[string]int     `json:"confidence"`
	Warnings              []StatusWarning    `json:"warnings,omitempty"`
}

// StatusOptions controls status derivation without rescanning project files.
type StatusOptions struct {
	ProjectID  string
	Now        time.Time
	StaleAfter time.Duration
}

// StatusProvider exposes graph status snapshots to MCP without leaking storage details.
type StatusProvider interface {
	Snapshot(ctx context.Context, opts StatusOptions) (*StatusSnapshot, error)
}

// Repository is the graph storage contract used by extractors and MCP status.
type Repository interface {
	StatusProvider

	UpsertNode(ctx context.Context, node Node) (Node, error)
	UpsertEdge(ctx context.Context, edge Edge) (Edge, error)
	ReplaceEdges(ctx context.Context, replacement EdgeReplacement) error
	MarkEdgesToSourceStale(ctx context.Context, projectID, sourcePath string) error
	PurgeStaleEdges(ctx context.Context, projectID string, olderThan time.Time) (int, error)
	ListNodes(ctx context.Context, query NodeQuery) ([]Node, error)
	ListEdges(ctx context.Context, query EdgeQuery) ([]Edge, error)
	RecordBuild(ctx context.Context, metadata BuildMetadata) error
	RecordExtractorRun(ctx context.Context, run ExtractorRun) error
	Reset(ctx context.Context) error
	Close() error
}

func normalizeNode(node Node) (Node, error) {
	if node.ProjectID == "" {
		return Node{}, fmt.Errorf("project_id is required")
	}
	if node.Kind == "" {
		return Node{}, fmt.Errorf("node kind is required")
	}
	if node.Key == "" {
		return Node{}, fmt.Errorf("node key is required")
	}
	if node.ID == "" {
		node.ID = nodeID(node.ProjectID, node.Kind, node.Key)
	}
	if node.Metadata == nil {
		node.Metadata = map[string]string{}
	}
	return node, nil
}

func normalizeEdge(edge Edge) (Edge, error) {
	if edge.ProjectID == "" {
		return Edge{}, fmt.Errorf("project_id is required")
	}
	if edge.Kind == "" {
		return Edge{}, fmt.Errorf("edge kind is required")
	}
	if edge.FromNodeID == "" || edge.ToNodeID == "" {
		return Edge{}, fmt.Errorf("edge endpoints are required")
	}
	if edge.Extractor == "" {
		return Edge{}, fmt.Errorf("edge extractor is required")
	}
	if edge.SourcePath == "" {
		return Edge{}, fmt.Errorf("edge source_path is required")
	}
	if err := validateConfidence(edge.Confidence); err != nil {
		return Edge{}, err
	}
	if edge.Evidence.SourcePath == "" {
		edge.Evidence.SourcePath = edge.SourcePath
	}
	if edge.Evidence.LineStart == 0 && edge.Evidence.Line != 0 {
		edge.Evidence.LineStart = edge.Evidence.Line
	}
	if edge.Evidence.LineEnd == 0 && edge.Evidence.LineStart != 0 {
		edge.Evidence.LineEnd = edge.Evidence.LineStart
	}
	if edge.Evidence.Line == 0 && edge.Evidence.LineStart != 0 {
		edge.Evidence.Line = edge.Evidence.LineStart
	}
	edge.ConfidenceLabel = confidenceLabelFor(edge.Confidence)
	if edge.ID == "" {
		edge.ID = edgeID(edge.NaturalKey())
	}
	return edge, nil
}

func validateConfidence(confidence float64) error {
	if math.IsNaN(confidence) || confidence < 0 || confidence > 1 {
		return fmt.Errorf("confidence must be between 0 and 1: %v", confidence)
	}
	return nil
}

func confidenceLabelFor(confidence float64) ConfidenceLabel {
	switch {
	case confidence == 1:
		return ConfidenceExact
	case confidence >= 0.9:
		return ConfidenceHigh
	case confidence >= 0.7:
		return ConfidenceMedium
	default:
		return ConfidenceLow
	}
}

func nodeID(projectID string, kind NodeKind, key string) string {
	return fmt.Sprintf("node:%s:%s:%s", kind, projectID, key)
}

func edgeID(naturalKey string) string {
	sum := sha256.Sum256([]byte(naturalKey))
	return "edge:" + hex.EncodeToString(sum[:])[:24]
}

func effectiveNow(now time.Time) time.Time {
	if now.IsZero() {
		return time.Now().UTC()
	}
	return now.UTC()
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
