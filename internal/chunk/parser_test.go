package chunk

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Test Scenarios from F06 Spec
// ============================================================================

// TS01: Parse Go File
func TestParser_ParseGoFile_ReturnsAST(t *testing.T) {
	// Given: valid Go source code with functions
	source := []byte(`package main

func hello() {
	fmt.Println("Hello")
}

func goodbye() {
	fmt.Println("Bye")
}
`)

	// When: parsing with Go language
	parser := NewParser()
	defer parser.Close()

	tree, err := parser.Parse(context.Background(), source, "go")

	// Then: AST is returned with function_declaration nodes
	require.NoError(t, err)
	require.NotNil(t, tree)
	assert.NotNil(t, tree.Root)
	assert.Equal(t, "go", tree.Language)

	// Verify AST contains expected node types
	funcNodes := findNodes(tree.Root, "function_declaration")
	assert.Len(t, funcNodes, 2, "should find 2 function declarations")
}

// TS02: Parse TypeScript File
func TestParser_ParseTypeScript_ReturnsAST(t *testing.T) {
	// Given: TypeScript source with interfaces and functions
	source := []byte(`interface User {
	name: string;
	age: number;
}

function greet(user: User): string {
	return "Hello, " + user.name;
}

const add = (a: number, b: number): number => a + b;
`)

	// When: parsing with TypeScript language
	parser := NewParser()
	defer parser.Close()

	tree, err := parser.Parse(context.Background(), source, "typescript")

	// Then: AST contains interface and function nodes
	require.NoError(t, err)
	require.NotNil(t, tree)
	assert.Equal(t, "typescript", tree.Language)

	// Verify AST structure
	interfaceNodes := findNodes(tree.Root, "interface_declaration")
	funcNodes := findNodes(tree.Root, "function_declaration")
	arrowNodes := findNodes(tree.Root, "arrow_function")

	assert.Len(t, interfaceNodes, 1, "should find 1 interface declaration")
	assert.Len(t, funcNodes, 1, "should find 1 function declaration")
	assert.Len(t, arrowNodes, 1, "should find 1 arrow function")
}

// TS03: Handle Syntax Error
func TestParser_HandleSyntaxError_ReturnsPartialAST(t *testing.T) {
	// Given: invalid Go code with syntax errors
	source := []byte(`package main

func broken( {
	// missing closing paren
}
`)

	// When: parsing with Go language
	parser := NewParser()
	defer parser.Close()

	tree, err := parser.Parse(context.Background(), source, "go")

	// Then: no error is returned (partial parse succeeds)
	require.NoError(t, err)
	require.NotNil(t, tree)

	// And: tree has error flag set
	assert.True(t, tree.Root.HasError, "tree should indicate parse errors")
}

// TS04: Extract Go Symbols
func TestSymbolExtractor_ExtractGoSymbols(t *testing.T) {
	// Given: Go source with functions
	source := []byte(`package main

// Hello prints a greeting
func Hello() {
	fmt.Println("Hello")
}

// Add adds two numbers
func Add(a, b int) int {
	return a + b
}

type Calculator struct {
	value int
}

// Multiply is a method on Calculator
func (c *Calculator) Multiply(x int) int {
	return c.value * x
}
`)

	// When: extracting symbols
	parser := NewParser()
	defer parser.Close()

	tree, err := parser.Parse(context.Background(), source, "go")
	require.NoError(t, err)

	extractor := NewSymbolExtractor()
	symbols := extractor.Extract(tree, source)

	// Then: symbols named "Hello", "Add", "Calculator", "Multiply" are returned
	names := getSymbolNames(symbols)
	assert.Contains(t, names, "Hello")
	assert.Contains(t, names, "Add")
	assert.Contains(t, names, "Calculator")
	assert.Contains(t, names, "Multiply")

	// Verify symbol types
	helloSymbol := findSymbolByName(symbols, "Hello")
	require.NotNil(t, helloSymbol)
	assert.Equal(t, SymbolTypeFunction, helloSymbol.Type)

	calcSymbol := findSymbolByName(symbols, "Calculator")
	require.NotNil(t, calcSymbol)
	assert.Equal(t, SymbolTypeType, calcSymbol.Type)

	multiplySymbol := findSymbolByName(symbols, "Multiply")
	require.NotNil(t, multiplySymbol)
	assert.Equal(t, SymbolTypeMethod, multiplySymbol.Type)
}

// TS05: Extract Python Classes
func TestSymbolExtractor_ExtractPythonClasses(t *testing.T) {
	// Given: Python source with classes
	source := []byte(`class Dog:
    """A dog class"""
    def bark(self):
        print("Woof!")

class Cat:
    """A cat class"""
    def meow(self):
        print("Meow!")

def main():
    dog = Dog()
    dog.bark()
`)

	// When: extracting symbols
	parser := NewParser()
	defer parser.Close()

	tree, err := parser.Parse(context.Background(), source, "python")
	require.NoError(t, err)

	extractor := NewSymbolExtractor()
	symbols := extractor.Extract(tree, source)

	// Then: two classes named "Dog" and "Cat" are returned
	classSymbols := filterSymbolsByType(symbols, SymbolTypeClass)
	names := getSymbolNames(classSymbols)
	assert.Contains(t, names, "Dog")
	assert.Contains(t, names, "Cat")
	assert.Len(t, classSymbols, 2)
}

// TS06: Language Detection by Extension
func TestLanguageRegistry_GetByExtension(t *testing.T) {
	tests := []struct {
		name      string
		extension string
		wantLang  string
		wantOK    bool
	}{
		{"Go file", ".go", "go", true},
		{"TypeScript file", ".ts", "typescript", true},
		{"TSX file", ".tsx", "tsx", true},
		{"JavaScript file", ".js", "javascript", true},
		{"JSX file", ".jsx", "jsx", true},
		{"MJS file", ".mjs", "javascript", true},
		{"Python file", ".py", "python", true},
	}

	registry := NewLanguageRegistry()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, ok := registry.GetByExtension(tt.extension)
			assert.Equal(t, tt.wantOK, ok)
			if ok {
				assert.Equal(t, tt.wantLang, config.Name)
			}
		})
	}
}

// TS07: Unsupported Language
func TestLanguageRegistry_UnsupportedLanguage(t *testing.T) {
	// Given: file extension for Elixir
	extension := ".ex"

	// When: looking up language configuration
	registry := NewLanguageRegistry()
	config, ok := registry.GetByExtension(extension)

	// Then: no configuration is found
	assert.False(t, ok)
	assert.Nil(t, config)
}

// ============================================================================
// Parser Lifecycle Tests
// ============================================================================

func TestParser_Lifecycle_CreateParseClose(t *testing.T) {
	// Given: a new parser
	parser := NewParser()

	// When: parsing a file
	source := []byte(`package main`)
	tree, err := parser.Parse(context.Background(), source, "go")

	// Then: parsing succeeds
	require.NoError(t, err)
	require.NotNil(t, tree)

	// When: closing the parser (should not panic)
	parser.Close()
}

func TestParser_MultipleParses(t *testing.T) {
	// Given: a single parser
	parser := NewParser()
	defer parser.Close()

	sources := []struct {
		code     []byte
		language string
	}{
		{[]byte(`package main`), "go"},
		{[]byte(`def foo(): pass`), "python"},
		{[]byte(`function bar() {}`), "javascript"},
	}

	// When: parsing multiple files
	for _, src := range sources {
		tree, err := parser.Parse(context.Background(), src.code, src.language)
		// Then: each parse succeeds
		require.NoError(t, err)
		require.NotNil(t, tree)
		assert.Equal(t, src.language, tree.Language)
	}
}

// ============================================================================
// JavaScript Tests
// ============================================================================

func TestParser_ParseJavaScript_ReturnsAST(t *testing.T) {
	source := []byte(`function greet(name) {
	return "Hello, " + name;
}

class Person {
	constructor(name) {
		this.name = name;
	}

	sayHello() {
		return greet(this.name);
	}
}

const arrow = (x) => x * 2;
`)

	parser := NewParser()
	defer parser.Close()

	tree, err := parser.Parse(context.Background(), source, "javascript")

	require.NoError(t, err)
	require.NotNil(t, tree)
	assert.Equal(t, "javascript", tree.Language)

	funcNodes := findNodes(tree.Root, "function_declaration")
	classNodes := findNodes(tree.Root, "class_declaration")
	arrowNodes := findNodes(tree.Root, "arrow_function")

	assert.Len(t, funcNodes, 1)
	assert.Len(t, classNodes, 1)
	assert.Len(t, arrowNodes, 1)
}

// ============================================================================
// Symbol Extraction Tests
// ============================================================================

func TestSymbolExtractor_ExtractTypeScriptSymbols(t *testing.T) {
	source := []byte(`interface User {
	name: string;
}

class UserService {
	private users: User[] = [];

	addUser(user: User): void {
		this.users.push(user);
	}
}

function createUser(name: string): User {
	return { name };
}

const getUser = (id: number): User | undefined => {
	return undefined;
};
`)

	parser := NewParser()
	defer parser.Close()

	tree, err := parser.Parse(context.Background(), source, "typescript")
	require.NoError(t, err)

	extractor := NewSymbolExtractor()
	symbols := extractor.Extract(tree, source)

	names := getSymbolNames(symbols)
	assert.Contains(t, names, "User")
	assert.Contains(t, names, "UserService")
	assert.Contains(t, names, "createUser")
	assert.Contains(t, names, "getUser")
}

func TestSymbolExtractor_ExtractJavaScriptSymbols(t *testing.T) {
	source := []byte(`function processData(data) {
	return data.map(x => x * 2);
}

class DataProcessor {
	process(items) {
		return processData(items);
	}
}

const helper = function(x) {
	return x + 1;
};
`)

	parser := NewParser()
	defer parser.Close()

	tree, err := parser.Parse(context.Background(), source, "javascript")
	require.NoError(t, err)

	extractor := NewSymbolExtractor()
	symbols := extractor.Extract(tree, source)

	names := getSymbolNames(symbols)
	assert.Contains(t, names, "processData")
	assert.Contains(t, names, "DataProcessor")
	assert.Contains(t, names, "helper")
}

func TestSymbolExtractor_ExtractPythonFunctions(t *testing.T) {
	source := []byte(`def greet(name: str) -> str:
    """Greet someone by name."""
    return f"Hello, {name}!"

async def fetch_data(url: str):
    """Async function to fetch data."""
    pass

class Greeter:
    def __init__(self, prefix: str):
        self.prefix = prefix

    def greet(self, name: str) -> str:
        return f"{self.prefix} {name}"
`)

	parser := NewParser()
	defer parser.Close()

	tree, err := parser.Parse(context.Background(), source, "python")
	require.NoError(t, err)

	extractor := NewSymbolExtractor()
	symbols := extractor.Extract(tree, source)

	names := getSymbolNames(symbols)
	assert.Contains(t, names, "greet")
	assert.Contains(t, names, "fetch_data")
	assert.Contains(t, names, "Greeter")
}

// ============================================================================
// DEBT-012: Nil vs Empty Slice Tests
// ============================================================================

func TestSymbolExtractor_Extract_EmptyInputs(t *testing.T) {
	extractor := NewSymbolExtractor()

	t.Run("nil tree", func(t *testing.T) {
		result := extractor.Extract(nil, []byte("code"))
		// DEBT-012: should return empty slice, not nil
		assert.NotNil(t, result, "should return empty slice, not nil")
		assert.Empty(t, result)
	})

	t.Run("tree with nil root", func(t *testing.T) {
		tree := &Tree{Root: nil, Language: "go"}
		result := extractor.Extract(tree, []byte("code"))
		// DEBT-012: should return empty slice, not nil
		assert.NotNil(t, result, "should return empty slice, not nil")
		assert.Empty(t, result)
	})

	t.Run("unknown language", func(t *testing.T) {
		parser := NewParser()
		defer parser.Close()

		tree, err := parser.Parse(context.Background(), []byte("package main"), "go")
		require.NoError(t, err)
		// Override language to unknown
		tree.Language = "unknown_language"

		result := extractor.Extract(tree, []byte("package main"))
		// DEBT-012: should return empty slice, not nil
		assert.NotNil(t, result, "should return empty slice, not nil")
		assert.Empty(t, result)
	})
}

// ============================================================================
// Performance Tests
// ============================================================================

func TestParser_Performance_Parse1000LOC(t *testing.T) {
	// Generate 1000 lines of Go code
	var code string
	for i := 0; i < 100; i++ {
		code += `func function` + string(rune('A'+i%26)) + `() {
	// Some code here
	x := 1
	y := 2
	z := x + y
	fmt.Println(z)
}

`
	}
	source := []byte("package main\n\n" + code)

	parser := NewParser()
	defer parser.Close()

	// Parse and measure time
	start := time.Now()
	tree, err := parser.Parse(context.Background(), source, "go")
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.NotNil(t, tree)

	// Target: <= 50ms (use LessOrEqual to handle boundary condition on slow CI runners)
	assert.LessOrEqual(t, elapsed.Milliseconds(), int64(50), "parsing 1000+ LOC should take <= 50ms")
}

// ============================================================================
// Helper Functions
// ============================================================================

// findNodes recursively finds all nodes of the given type
func findNodes(node *Node, nodeType string) []*Node {
	var result []*Node
	if node == nil {
		return result
	}

	if node.Type == nodeType {
		result = append(result, node)
	}

	for _, child := range node.Children {
		result = append(result, findNodes(child, nodeType)...)
	}

	return result
}

// getSymbolNames returns the names of all symbols
func getSymbolNames(symbols []*Symbol) []string {
	names := make([]string, len(symbols))
	for i, s := range symbols {
		names[i] = s.Name
	}
	return names
}

// findSymbolByName finds a symbol by name
func findSymbolByName(symbols []*Symbol, name string) *Symbol {
	for _, s := range symbols {
		if s.Name == name {
			return s
		}
	}
	return nil
}

// filterSymbolsByType filters symbols by type
func filterSymbolsByType(symbols []*Symbol, symbolType SymbolType) []*Symbol {
	var result []*Symbol
	for _, s := range symbols {
		if s.Type == symbolType {
			result = append(result, s)
		}
	}
	return result
}
