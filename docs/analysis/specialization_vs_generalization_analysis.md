# Specialization vs Generalization Analysis

**Date:** 2026-01-14
**Author:** AI Analysis (Claude Opus 4.5)
**Status:** Critical Review Required
**Verdict:** We have been overfitting to our own codebase.

---

## Executive Summary

After a thorough audit of the AmanMCP codebase, I must deliver an uncomfortable truth: **we have been building a Go-specific search tool optimized for this exact codebase, not a generalizable code search solution.**

The evidence is pervasive. From hardcoded Go syntax patterns to path conventions specific to Go projects, from validation queries that reference this project's exact file paths to domain-specific boosting for "Search" and "Index" nouns—we have systematically "taught to the test" rather than solving the underlying problem.

This analysis documents the evidence, explains why it happened, and proposes a path forward.

---

## Part 1: The Evidence

### 1.1 Go-Specific Syntax Patterns (decomposer.go)

The query decomposer is hardcoded for Go syntax:

```go
// Lines 187-207: All patterns are Go-specific
{Query: ") " + capitalNoun + "(", Weight: 1.5, Hint: "code"},  // Go method receiver
{Query: capitalNoun + "(ctx", Weight: 1.4, Hint: "code"},      // Go context param
{Query: "func " + capitalNoun, Weight: 1.2, Hint: "code"},     // Go func keyword
{Query: "func (" + lowerNoun, Weight: 1.1, Hint: "code"},      // Go receiver syntax
```

**What other languages use:**

| Language | Function Definition Syntax |
|----------|---------------------------|
| Python | `def search(self, ...)` |
| JavaScript | `search(...) { }` or `search = () => {}` |
| TypeScript | `search(query: string): Result[]` |
| Java | `public void search(...)` |
| Rust | `fn search(&self, ...)` or `pub fn search(...)` |
| C# | `public void Search(...)` |
| Ruby | `def search(...)` |

**The patterns we generate would find NOTHING in these languages.**

### 1.2 Hardcoded Domain Knowledge (decomposer.go:211-222)

We literally hardcoded knowledge about THIS codebase:

```go
switch strings.ToLower(noun) {
case "search":
    subQueries = append(subQueries,
        SubQuery{Query: "engine.go Search", Weight: 1.1, Hint: "code"},
        SubQuery{Query: "Engine Search", Weight: 1.0, Hint: "code"},
    )
case "index":
    subQueries = append(subQueries,
        SubQuery{Query: "Coordinator", Weight: 1.0, Hint: "code"},
        SubQuery{Query: "index/", Weight: 0.9, Hint: "code"},
    )
}
```

This is not search engineering. This is memorizing the answers to the test.

### 1.3 Go Project Structure Assumptions (options.go:296-305)

The path boosting assumes Go project conventions:

```go
// IsImplementationPath checks if a path is implementation code (internal/).
func IsImplementationPath(filePath string) bool {
    return strings.HasPrefix(filePath, "internal/") ||
        strings.Contains(filePath, "/internal/")
}

// IsWrapperPath checks if a path is CLI wrapper code (cmd/).
func IsWrapperPath(filePath string) bool {
    return strings.HasPrefix(filePath, "cmd/") ||
        strings.Contains(filePath, "/cmd/")
}
```

**What other ecosystems use:**

| Ecosystem | Implementation Path | Entry Point Path |
|-----------|---------------------|------------------|
| Go | `internal/` | `cmd/` |
| Python | `src/`, `lib/`, `package_name/` | `scripts/`, `bin/` |
| JavaScript | `src/`, `lib/` | `bin/`, `cli/` |
| Java/Maven | `src/main/java/` | (same) |
| Rust | `src/` | `src/bin/` |
| Ruby | `lib/` | `bin/`, `exe/` |

A Python project with `src/search/engine.py` would get NO boost.
A Java project with `src/main/java/com/example/Search.java` would get NO boost.

### 1.4 Test File Detection (options.go:218-243)

The test detection covers Go, JavaScript, and Python—three languages:

```go
if strings.HasSuffix(filePath, "_test.go") { return true }           // Go
if strings.Contains(filePath, ".test.") ||
   strings.Contains(filePath, ".spec.") { return true }               // JS/TS
if strings.HasPrefix(fileName, "test_") &&
   strings.HasSuffix(fileName, ".py") { return true }                 // Python
```

**Missing coverage:**

| Language | Test Convention | Detected? |
|----------|-----------------|-----------|
| C# | `*Tests.cs`, `*Spec.cs` | NO |
| Java | `*Test.java` (JUnit), `*IT.java` | NO |
| Rust | `tests/`, `#[cfg(test)]` | NO |
| Ruby | `*_spec.rb`, `spec/` | NO |
| Kotlin | `*Test.kt` | NO |
| PHP | `*Test.php` (PHPUnit) | NO |
| Swift | `*Tests.swift` | NO |

### 1.5 Validation Queries Reference This Codebase (spec.md)

The validation specification expects EXACT paths from THIS project:

```markdown
| 7 | Search function | search_code | internal/search/engine.go | FAIL |
| 8 | Index function | search_code | internal/index/ | FAIL |
| 11 | OllamaEmbedder | search_code | internal/embed/ollama.go | PASS |
```

These are not abstract quality metrics. They are regression tests for THIS codebase.

### 1.6 File Extension Assumptions (decomposer.go:74, :258)

```go
// Assumes .go files are the target
filePathPattern: regexp.MustCompile(`...\.go|ts|tsx|js|jsx|py|md|json|yaml|yml)$`)

// Generates .go file hints
subQueries = append(subQueries, SubQuery{
    Query:  strings.ToLower(word) + ".go",  // Hardcoded .go
    Weight: 1.1,
    Hint:   "code",
})
```

---

## Part 2: Why This Happened

### 2.1 The TDD Trap

We followed TDD religiously. But our tests were:
- Validation queries about THIS codebase
- Expectations of specific file paths in THIS project
- Success metrics tied to THIS project's structure

TDD doesn't prevent overfitting—it can CAUSE it when the tests themselves are too narrow.

### 2.2 Dogfooding Bias

Dogfooding is valuable, but it creates a selection bias:
- We only see bugs in OUR usage patterns
- We only measure success against OUR queries
- We only optimize for OUR project structure

A team using AmanMCP on a Python monorepo would have completely different failures.

### 2.3 Incremental Optimization

Each bug fix optimized for the specific failure:
- BUG-066: Wrapper outranks implementation → Add `internal/` boost (Go-specific)
- FEAT-QI4: Test files outrank implementation → Add `_test.go` penalty (Go-specific)
- FEAT-QI3: "Search function" fails → Add Go method patterns (Go-specific)

We never asked: "Will this work for a Java project?"

### 2.4 The Pressure to Ship

Sprint goals, validation targets, "green CI"—these create pressure to fix the immediate problem. Research and generalization feel like scope creep when you're trying to hit 100% Tier 1.

---

## Part 3: The Cost of Specialization

### 3.1 Limited Market

AmanMCP, as currently implemented, is primarily useful for:
- Go projects
- With `internal/` and `cmd/` conventions
- Where "function" means Go methods

This excludes:
- Python/JavaScript/Java/Rust/Ruby ecosystems
- Non-conventional Go projects
- Polyglot monorepos (the most complex use case)

### 3.2 Technical Debt

Every hardcoded pattern is debt:
- 7 Go-specific patterns in decomposer
- 2 hardcoded domain terms ("search", "index")
- 2 Go path conventions (`internal/`, `cmd/`)
- 3 language test file patterns (partial coverage)

Future language support requires:
- Rewriting decomposer for each language
- Adding path conventions for each ecosystem
- Extending test file patterns
- New validation suites per language

### 3.3 False Confidence

We achieved 67% Tier 1 validation. But that 67% is:
- On our own project
- Using our own conventions
- With patterns tuned to our queries

On a Python Django project, we might be at 10%.

---

## Part 4: What Generalization Would Look Like

### 4.1 Language-Aware Pattern Engine

Instead of hardcoded patterns, a configurable engine:

```yaml
# .amanmcp.yaml (project config)
language_patterns:
  go:
    function_patterns:
      - pattern: ") {Name}("
        weight: 1.5
        description: "Go method receiver"
      - pattern: "func {Name}"
        weight: 1.2
        description: "Go function"
    implementation_paths: ["internal/"]
    wrapper_paths: ["cmd/"]
    test_patterns: ["_test.go"]

  python:
    function_patterns:
      - pattern: "def {name}"
        weight: 1.3
        description: "Python function"
      - pattern: "def {name}(self"
        weight: 1.5
        description: "Python method"
    implementation_paths: ["src/", "lib/"]
    wrapper_paths: ["scripts/", "bin/"]
    test_patterns: ["test_*.py", "*_test.py"]
```

### 4.2 Automatic Language Detection

```go
// Detect project languages from file extensions
func DetectLanguages(root string) []string {
    // Scan, count extensions, return top languages
}

// Load patterns for detected languages
func LoadPatterns(languages []string) PatternSet {
    // Merge patterns from all detected languages
}
```

### 4.3 Semantic AST Indexing

Instead of regex patterns, use tree-sitter to extract semantic metadata:

```go
type SemanticMetadata struct {
    SymbolType  string   // "function", "class", "method", "interface"
    Name        string   // "Search", "Config", etc.
    Visibility  string   // "public", "private", "internal"
    IsTest      bool     // Determined from AST, not filename
    Language    string   // "go", "python", "typescript"
    Signature   string   // Full function signature
}
```

This would allow queries like:
- "Search function" → Filter by SymbolType="function", Name contains "Search"
- Works identically for Go, Python, JavaScript, Java

### 4.4 Project-Agnostic Validation

Instead of:
```
| Query | Expected | Status |
| Search function | internal/search/engine.go | FAIL |
```

Use:
```
| Query | Expected Criteria | Status |
| {Term} function | SymbolType=function, Name={Term}, IsTest=false, rank < 3 | ? |
```

Run validation against multiple test projects:
- `test-fixtures/go-project/`
- `test-fixtures/python-project/`
- `test-fixtures/typescript-project/`
- `test-fixtures/java-maven-project/`
- `test-fixtures/polyglot-monorepo/`

---

## Part 5: Recommendations

### 5.1 Immediate (Sprint 7)

1. **Document the limitations** in README.md:
   > AmanMCP is currently optimized for Go projects following standard conventions (`internal/`, `cmd/`). Support for other languages is limited.

2. **Make path patterns configurable** in `.amanmcp.yaml`:
   ```yaml
   path_scoring:
     implementation: ["internal/", "src/", "lib/"]
     wrapper: ["cmd/", "scripts/", "bin/"]
   ```

3. **Add more test file patterns** to cover major languages (C#, Java, Rust, Ruby).

### 5.2 Short-term (Sprint 8-9)

1. **Implement language detection** to auto-load appropriate patterns.

2. **Refactor decomposer** to be pattern-driven, not hardcoded:
   ```go
   type LanguagePatterns struct {
       FunctionPatterns []PatternTemplate
       MethodPatterns   []PatternTemplate
       TestPatterns     []string
   }
   ```

3. **Create multi-language test fixtures** and add to CI.

### 5.3 Medium-term (Phase 5)

1. **Semantic indexing via tree-sitter AST**:
   - Extract SymbolType, Name, Visibility, IsTest from AST
   - Index as searchable metadata
   - Enable semantic queries

2. **Polyglot monorepo support**:
   - Detect language per file/directory
   - Apply language-specific patterns per file
   - Unified ranking across languages

### 5.4 Long-term (Research)

1. **Learn patterns from codebase structure**:
   - Identify which paths contain implementations vs wrappers via heuristics
   - Discover test file conventions from file naming patterns
   - Adapt to non-standard project structures

2. **Benchmark on public codebases**:
   - kubernetes/kubernetes (Go)
   - django/django (Python)
   - facebook/react (JavaScript)
   - rust-lang/rust (Rust)
   - Compare search quality across ecosystems

---

## Part 6: The Uncomfortable Questions

### Q: Are we building a product or a science project?

If the goal is a product that works for our team, the current approach is fine. If the goal is a tool others can use, we need generalization.

### Q: Is 100% Tier 1 on our codebase meaningful?

No. It proves we can tune to our tests. It doesn't prove the tool is good.

### Q: Should we have researched before implementing?

Yes. A literature review of code search tools (Sourcegraph, GitHub Code Search, OpenGrok, Zoekt) would have revealed:
- Language-aware indexing is standard
- AST-based symbol extraction is the norm
- Path heuristics are configurable, not hardcoded

### Q: Is this salvageable?

Yes, but it requires:
1. Acknowledging the problem
2. Deciding if generalization is worth the effort
3. Refactoring incrementally with multi-language tests

---

## Part 7: The Verdict

**We have been solving the wrong problem.**

The problem was not "make `Search function` return `engine.go`."

The problem was "build a code search tool that understands code across languages."

We optimized for the symptom (our failing queries) not the cause (lack of language-aware search).

The "Just Do It Now" philosophy is powerful for shipping, but without periodic reflection, it leads to local maxima—solutions that work here but nowhere else.

---

## Appendix: Code Locations

| File | Line | Issue |
|------|------|-------|
| `internal/search/decomposer.go` | 187-207 | Go-specific syntax patterns |
| `internal/search/decomposer.go` | 211-222 | Hardcoded domain knowledge |
| `internal/search/options.go` | 296-305 | Go path conventions |
| `internal/search/options.go` | 218-243 | Partial test file coverage |
| `.aman-pm/validation/spec.md` | 30-56 | Project-specific expectations |

---

## Appendix: What Other Tools Do

| Tool | Language Support | How |
|------|------------------|-----|
| **Sourcegraph** | 40+ languages | Language-specific indexers, LSIF |
| **GitHub Code Search** | Many | Syntax-aware tokenization per language |
| **OpenGrok** | 60+ languages | Ctags + language analyzers |
| **Zoekt** | Many | Trigram indexing (language-agnostic) |
| **AmanMCP** | Go (+ partial JS/Python) | Hardcoded patterns |

---

## Conclusion

This is not a failure. This is a learning.

The question now is: do we want AmanMCP to be a Go-specific tool, or a generalizable code search solution?

The answer determines the next phase of work.

---

*This analysis was requested by the user and represents an honest assessment of the codebase state. No action has been taken—decisions rest with the project maintainers.*
