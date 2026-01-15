# ADR-032: Query Pattern Telemetry

**Status:** Accepted
**Date:** 2026-01-06
**Deciders:** AmanMCP Team
**Category:** Performance & Optimization (030-039)

---

## Context

### Problem Statement

AmanMCP lacks visibility into how users search. Without data on query patterns, we cannot:

1. **Identify zero-result queries** - Which queries fail to return results? These indicate vocabulary mismatch opportunities (per RCA-010)
2. **Understand query type distribution** - Are users mostly doing lexical, semantic, or mixed queries?
3. **Track latency trends** - Are there slow query patterns that need optimization?
4. **Build synonym dictionaries** - Which terms are commonly searched? These inform query expansion (QI-1)

### Motivation from Roadmap

This is item **AI-6** from the strategic roadmap (`archive/roadmap/strategic-improvements-2026.md`). It was upgraded to P1 Phase 1 because:

- Observability is a prerequisite for data-driven optimization
- Query telemetry enables measurement of roadmap improvements (CR-1, QI-1, RR-1)
- Privacy-first local storage aligns with AmanMCP philosophy

### Research Foundation

- [Weaviate AXN paper](https://weaviate.io/papers/axn) shows 5-54% recall improvement from query-adaptive approaches
- Query pattern analysis is standard practice for search optimization

---

## Decision

**We will implement local-only query pattern telemetry** with the following design:

### Storage Design: Aggregated + In-Memory

| Data Type | Storage | Rationale |
|-----------|---------|-----------|
| Query type counts | SQLite (daily aggregates) | Low storage, trend analysis |
| Top query terms | SQLite (LRU, max 100) | Synonym dictionary input |
| Zero-result queries | SQLite (circular buffer, max 100) | Expansion opportunities |
| Latency histogram | SQLite (daily buckets) | Performance trending |

**Why aggregated, not raw events:**
- Bounded storage growth (raw events would grow unbounded)
- Still provides actionable insights
- Privacy-friendly (no query content persisted for successful queries)

### Architecture Pattern

```
Search Request
     ↓
Engine.Search()
     ↓ (after results computed)
QueryMetrics.Record() ───┐
     ↓                   │
Return Results           │
                         ↓
              [In-Memory Cache]
                    │
              (periodic flush)
                    ↓
              [SQLite Tables]
```

### Key Design Choices

1. **Post-search recording** - Zero latency impact on search path
2. **In-memory aggregation** - Flush to SQLite every 60 seconds
3. **Optional injection** - Via `WithMetrics` EngineOption, disabled by default in tests
4. **LRU for top terms** - Using existing `hashicorp/golang-lru` pattern
5. **Circular buffer for zero-results** - Simple bounded FIFO

---

## Rationale

### Alternatives Considered

| Option | Pros | Cons |
|--------|------|------|
| **Raw event logging** | Full history, flexible analysis | Unbounded growth, privacy concern |
| **External telemetry (StatsD)** | Standard tooling | External dependency, configuration |
| **Log-based only** | Simple | Hard to aggregate, no persistence |
| **Aggregated SQLite** | Bounded, private, persistent | Less granular | **SELECTED**

### Why SQLite?

1. **Already used** - MetadataStore uses SQLite, schema extension is natural
2. **Local-first** - Aligns with privacy philosophy
3. **Queryable** - Can expose via MCP resource
4. **Durable** - Survives process restarts

### Why Not Prometheus/StatsD?

- Would require external dependencies
- Over-engineered for single-user local tool
- Doesn't align with "It Just Works" philosophy

---

## Consequences

### Positive

1. **Data-driven optimization** - Can measure impact of roadmap improvements
2. **Zero-result visibility** - Direct input for query expansion (QI-1)
3. **Top terms list** - Informs synonym dictionary (QI-1)
4. **Latency trends** - Identify slow query patterns
5. **Privacy-first** - All data stays local

### Negative

1. **Storage overhead** - ~100KB for telemetry tables
2. **Flush latency** - Brief SQLite write every 60s
3. **Code complexity** - New package, new integration points

### Mitigations

1. Storage is bounded by design (LRU, circular buffer)
2. Flush is async, non-blocking
3. Package is self-contained with clear interface

---

## Implementation

### New Package: `internal/telemetry`

| File | Purpose |
|------|---------|
| `query_metrics.go` | Core QueryMetrics collector |
| `query_metrics_test.go` | Unit tests |
| `store.go` | SQLite persistence |
| `store_test.go` | Persistence tests |

### SQLite Schema

```sql
-- Query type frequency (aggregated daily)
CREATE TABLE IF NOT EXISTS query_type_stats (
    date TEXT NOT NULL,
    query_type TEXT NOT NULL,
    count INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (date, query_type)
);

-- Top query terms
CREATE TABLE IF NOT EXISTS query_terms (
    term TEXT PRIMARY KEY,
    count INTEGER NOT NULL DEFAULT 1,
    last_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Zero-result queries (circular buffer)
CREATE TABLE IF NOT EXISTS zero_result_queries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    query TEXT NOT NULL,
    timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Latency histogram
CREATE TABLE IF NOT EXISTS query_latency_stats (
    date TEXT NOT NULL,
    bucket TEXT NOT NULL,
    count INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (date, bucket)
);
```

### Integration Points

| Component | Change |
|-----------|--------|
| `search/engine.go` | Add `WithMetrics` option, record after Search |
| `mcp/server.go` | Add `query_metrics` MCP resource |
| `cmd/stats.go` | Add `amanmcp stats queries` CLI command |

### Exposure

1. **MCP Resource** - `query_metrics` resource for AI clients
2. **CLI Command** - `amanmcp stats queries [--json] [--days N]`
3. **No external export** - Privacy by design

---

## Validation Criteria

This ADR is successfully implemented when:

- [ ] `QueryMetrics.Record()` captures query events
- [ ] Aggregated data persists to SQLite
- [ ] `amanmcp stats queries` displays metrics
- [ ] MCP `query_metrics` resource returns JSON
- [ ] Zero latency impact on search (<1ms overhead)
- [ ] All tests pass: `make ci-check`
- [ ] Feature spec F32 created and validated

---

## References

- [AI-6 in Strategic Roadmap](../roadmap/strategic-improvements-2026.md#ai-6-query-pattern-telemetry-p3)
- [Weaviate AXN Paper](https://weaviate.io/papers/axn) - Query-adaptive retrieval
- [RCA-010 Vocabulary Mismatch](../postmortems/RCA-010-semantic-search-vocabulary-mismatch.md) - Zero-result root cause
- [F32 Query Telemetry Feature](../specs/features/F32-query-telemetry.md)

---

## Changelog

| Date | Change |
|------|--------|
| 2026-01-06 | Initial acceptance |
