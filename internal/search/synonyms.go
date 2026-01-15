package search

// Code Synonym Dictionary for Query Expansion (QI-1 Lite)
//
// This dictionary maps natural language terms to their code equivalents
// based on research from:
// - CodeSearchNet dataset (github.com/github/CodeSearchNet)
// - Cross-language syntax analysis (rigaux.org/language-study)
// - Neural Query Expansion research (ml4code.github.io)
// - GitHub's Semantic Code Search (github.blog/ai-and-ml)
//
// Key insight from CodeSearchNet: "Standard information retrieval methods
// do not work well in code search as there is often little shared vocabulary
// between search terms and results (e.g., a method called deserialize_JSON_obj_from_stream
// may be a correct result for the query 'read JSON data')"
//
// Sources:
// - https://arxiv.org/html/2408.11058v1 (LLM Agents for Code Search)
// - https://rigaux.org/language-study/syntax-across-languages.html
// - https://github.com/github/CodeSearchNet
// - https://opensourceconnections.com/blog/2021/10/19/fundamentals-of-query-rewriting-part-1-introduction-to-query-expansion/

// CodeSynonyms maps natural language terms to code vocabulary equivalents.
// Multiple entries are OR'd together to expand query coverage.
//
// Design principles:
// 1. Map user vocabulary → code vocabulary (not vice versa)
// 2. Include cross-language keyword variants (func, def, function, fn)
// 3. Include common abbreviations (req, resp, ctx, cfg)
// 4. Include case variants for Go (camelCase, PascalCase)
var CodeSynonyms = map[string][]string{
	// ==========================================================================
	// Function/Method Terms (Cross-Language Keywords)
	// Source: rigaux.org/language-study/syntax-across-languages.html
	// QI-1: Prioritize most common code terms first (maxExpansions=3)
	// ==========================================================================
	"function": {"func", "method", "fn", "def", "Function", "Func"},
	"method":   {"func", "fn", "def", "function", "Method", "Func"},
	"func":     {"function", "method", "def", "fn"},
	"def":      {"func", "function", "method"},
	"fn":       {"func", "function", "method", "def"},
	"lambda":   {"anonymous", "closure", "arrow", "=>", "->"},

	// ==========================================================================
	// Type/Class Terms (OOP Concepts)
	// Source: CodeSearchNet vocabulary analysis
	// ==========================================================================
	"class":     {"type", "struct", "interface", "Class", "Type"},
	"type":      {"class", "struct", "interface", "Type"},
	"struct":    {"class", "type", "structure", "Struct"},
	"interface": {"protocol", "trait", "Interface", "contract"},
	"object":    {"instance", "obj", "struct", "Object"},
	"instance":  {"object", "obj", "new"},

	// ==========================================================================
	// Error Handling Terms
	// Source: Go idioms, Java exceptions, Python exceptions
	// ==========================================================================
	"error":     {"err", "Err", "Error", "exception", "fail", "failure"},
	"err":       {"error", "Error", "Err"},
	"exception": {"error", "err", "panic", "Exception"},
	"handle":    {"handler", "Handler", "handle", "catch", "process"},
	"handler":   {"handle", "Handle", "Handler", "callback"},
	"retry":     {"Retry", "retry", "attempt", "backoff", "Backoff"},
	"backoff":   {"Backoff", "retry", "delay", "exponential", "sleep"},
	"panic":     {"Panic", "fatal", "crash", "abort"},
	"recover":   {"Recover", "catch", "handle", "rescue"},

	// ==========================================================================
	// HTTP/Network Terms
	// Source: Common abbreviations across web frameworks
	// ==========================================================================
	"request":  {"req", "Req", "Request", "http", "HTTP"},
	"req":      {"request", "Request", "http"},
	"response": {"resp", "Resp", "Response", "reply"},
	"resp":     {"response", "Response", "reply"},
	"http":     {"HTTP", "request", "response", "web", "api"},
	"api":      {"API", "endpoint", "handler", "route"},
	"endpoint": {"handler", "route", "api", "path"},
	"server":   {"Server", "serve", "listener", "daemon"},
	"client":   {"Client", "conn", "connection"},

	// ==========================================================================
	// Context/Configuration Terms
	// Source: Go context package, config patterns
	// ==========================================================================
	"context": {"ctx", "Ctx", "Context"},
	"ctx":     {"context", "Context"},
	"config":  {"cfg", "Cfg", "Config", "configuration", "settings", "options"},
	"cfg":     {"config", "Config", "configuration"},
	"options": {"opts", "Opts", "Options", "config", "settings"},
	"opts":    {"options", "Options", "config"},
	"settings": {"config", "options", "preferences", "Settings"},

	// ==========================================================================
	// Database/Storage Terms
	// Source: Repository pattern, ORM terminology
	// ==========================================================================
	"database": {"db", "DB", "Database", "store", "storage"},
	"db":       {"database", "Database", "store"},
	"store":    {"Store", "storage", "database", "repository", "db"},
	"storage":  {"store", "Store", "database", "persist"},
	"repository": {"repo", "Repo", "Repository", "store"},
	"repo":       {"repository", "Repository", "store"},
	"query":      {"Query", "search", "find", "select"},
	"insert":     {"Insert", "add", "create", "save"},
	"update":     {"Update", "modify", "edit", "change"},
	"delete":     {"Delete", "remove", "drop", "destroy"},

	// ==========================================================================
	// Search/Index Terms (Domain-Specific for AmanMCP)
	// QI-1: Added specific mappings for dogfood queries
	// ==========================================================================
	"search":    {"Search", "find", "query", "lookup", "retrieve", "Engine"},
	"find":      {"Find", "search", "get", "lookup", "query"},
	"index":     {"Index", "indexer", "indexing", "catalog", "Coordinator"},
	"embed":     {"Embed", "embedding", "embedder", "vector", "Embedder"},
	"embedding": {"Embedding", "embed", "vector", "Embedder"},
	"embedder":  {"Embedder", "embed", "embedding", "Ollama", "vector"},
	"ollama":    {"Ollama", "embedder", "Embedder", "embed", "OllamaEmbedder"},
	"vector":    {"Vector", "embedding", "dense", "semantic"},
	"chunk":     {"Chunk", "segment", "block", "piece"},
	"token":     {"Token", "tokenize", "tokenizer", "word"},
	"parse":     {"Parse", "parser", "Parser", "parsing"},
	"ast":       {"AST", "tree", "syntax", "abstract"},

	// ==========================================================================
	// Common Actions/Verbs
	// Source: CRUD operations, lifecycle methods
	// ==========================================================================
	"create": {"Create", "new", "make", "init", "initialize"},
	"new":    {"New", "create", "make", "init"},
	"init":   {"Init", "initialize", "Initialize", "setup", "new"},
	"get":    {"Get", "fetch", "retrieve", "read", "load"},
	"set":    {"Set", "put", "assign", "write", "store"},
	"read":   {"Read", "get", "load", "fetch"},
	"write":  {"Write", "save", "store", "put"},
	"load":   {"Load", "read", "get", "fetch", "parse"},
	"save":   {"Save", "write", "store", "persist"},
	"close":  {"Close", "shutdown", "stop", "cleanup"},
	"start":  {"Start", "begin", "run", "launch", "init"},
	"stop":   {"Stop", "halt", "end", "close", "shutdown"},
	"run":    {"Run", "execute", "start", "process"},

	// ==========================================================================
	// Testing Terms
	// Source: Testing framework conventions
	// ==========================================================================
	"test":   {"Test", "testing", "spec", "check", "verify"},
	"mock":   {"Mock", "fake", "stub", "spy"},
	"assert": {"Assert", "expect", "require", "check"},
	"bench":  {"Bench", "benchmark", "Benchmark", "perf"},

	// ==========================================================================
	// Concurrency Terms
	// Source: Go concurrency, async patterns
	// ==========================================================================
	"async":     {"Async", "goroutine", "concurrent", "parallel"},
	"goroutine": {"Goroutine", "async", "concurrent", "go"},
	"channel":   {"Channel", "chan", "Chan", "pipe"},
	"chan":      {"channel", "Channel", "pipe"},
	"mutex":     {"Mutex", "lock", "Lock", "sync"},
	"lock":      {"Lock", "mutex", "Mutex", "sync"},
	"wait":      {"Wait", "block", "await", "sync"},
	"sync":      {"Sync", "synchronize", "wait", "concurrent"},

	// ==========================================================================
	// File/IO Terms
	// ==========================================================================
	"file":      {"File", "path", "filesystem", "io"},
	"path":      {"Path", "file", "filepath", "directory"},
	"directory": {"dir", "Dir", "Directory", "folder", "path"},
	"dir":       {"directory", "Directory", "folder"},
	"io":        {"IO", "input", "output", "stream"},
	"reader":    {"Reader", "read", "input", "stream"},
	"writer":    {"Writer", "write", "output", "stream"},

	// ==========================================================================
	// Logging/Debug Terms
	// ==========================================================================
	"log":   {"Log", "logger", "Logger", "logging", "slog"},
	"debug": {"Debug", "trace", "verbose", "log"},
	"info":  {"Info", "log", "message"},
	"warn":  {"Warn", "warning", "Warning", "alert"},
	"fatal": {"Fatal", "panic", "critical", "error"},

	// ==========================================================================
	// Natural Language → Code Mappings
	// Source: CodeSearchNet vocabulary gap analysis
	// ==========================================================================
	"implementation": {"impl", "Impl", "implement"},
	"where":          {"location", "file", "path", "find"},
	"how":            {"implementation", "code", "logic"},
	"what":           {"definition", "type", "struct"},
	"created":        {"create", "new", "init", "make"},
	"defined":        {"definition", "declare", "type"},
	"called":         {"call", "invoke", "execute"},
	"returns":        {"return", "output", "result"},
	"parameter":      {"param", "arg", "argument", "input"},
	"argument":       {"arg", "param", "parameter", "input"},
}

// GetSynonyms returns all synonyms for a given term.
// Returns an empty slice if no synonyms exist.
func GetSynonyms(term string) []string {
	if synonyms, ok := CodeSynonyms[term]; ok {
		return synonyms
	}
	// Try lowercase
	if synonyms, ok := CodeSynonyms[toLower(term)]; ok {
		return synonyms
	}
	return nil
}

// toLower is a simple lowercase helper to avoid importing strings.
func toLower(s string) string {
	b := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}
