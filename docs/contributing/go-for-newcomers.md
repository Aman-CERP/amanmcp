# Go for Newcomers

A quick primer on Go for developers who want to contribute to AmanMCP but haven't used Go before.

**Reading time:** 15 minutes
**Audience:** Developers with experience in other languages (Python, JavaScript, Java, etc.)

---

## Why This Guide?

AmanMCP is written in Go. If you're coming from Python, JavaScript, or another language, Go has some unique characteristics that might surprise you. This guide covers what you need to know to read and contribute to AmanMCP code.

---

## The Essentials

### Go is compiled and statically typed

```go
// Types are declared after the variable name
var count int = 42
name := "AmanMCP"  // Type inferred (string)

// Functions have typed parameters and return values
func Add(a int, b int) int {
    return a + b
}
```

### Multiple return values

Go functions commonly return both a result and an error:

```go
func ReadFile(path string) ([]byte, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, err  // Return error to caller
    }
    return data, nil  // nil error means success
}

// Calling code
data, err := ReadFile("config.yaml")
if err != nil {
    log.Fatal(err)  // Handle error
}
// Use data...
```

**This pattern is everywhere in Go and AmanMCP.**

### Error handling is explicit

No exceptions. Errors are values that you check:

```go
// BAD: Ignoring errors
data, _ := ReadFile("config.yaml")  // _ means "ignore"

// GOOD: Handle errors
data, err := ReadFile("config.yaml")
if err != nil {
    return fmt.Errorf("failed to read config: %w", err)
}
```

AmanMCP uses wrapped errors with context:

```go
return fmt.Errorf("failed to open database: %w", err)
```

---

## Go Syntax Quick Reference

### Variables and constants

```go
// Variables
var x int           // Declare
x = 10              // Assign
y := 20             // Declare and assign (short syntax)

// Constants
const MaxRetries = 3
```

### Functions

```go
// Basic function
func greet(name string) string {
    return "Hello, " + name
}

// Multiple return values
func divide(a, b int) (int, error) {
    if b == 0 {
        return 0, errors.New("division by zero")
    }
    return a / b, nil
}

// Named return values
func getUser() (user User, err error) {
    // ...
    return  // Returns user and err
}
```

### Structs (like classes)

```go
// Define a struct
type User struct {
    ID    int
    Name  string
    Email string
}

// Create an instance
u := User{
    ID:    1,
    Name:  "Alice",
    Email: "alice@example.com",
}

// Access fields
fmt.Println(u.Name)  // "Alice"
```

### Methods (functions on structs)

```go
type Store struct {
    db *sql.DB
}

// Method with receiver
func (s *Store) GetUser(id int) (*User, error) {
    // s is like "self" or "this"
    row := s.db.QueryRow("SELECT * FROM users WHERE id = ?", id)
    // ...
}

// Calling the method
store := &Store{db: db}
user, err := store.GetUser(42)
```

### Interfaces

Interfaces are implicit - a type implements an interface by having the right methods:

```go
// Interface definition
type Reader interface {
    Read(p []byte) (n int, err error)
}

// Any type with a Read method implements Reader
type MyFile struct {}

func (f *MyFile) Read(p []byte) (n int, err error) {
    // Implementation
}

// MyFile automatically implements Reader
var r Reader = &MyFile{}
```

### Slices and maps

```go
// Slice (dynamic array)
numbers := []int{1, 2, 3}
numbers = append(numbers, 4)  // [1, 2, 3, 4]

// Map (dictionary/hash)
ages := map[string]int{
    "Alice": 30,
    "Bob":   25,
}
ages["Charlie"] = 35

// Check if key exists
age, exists := ages["Dave"]
if !exists {
    fmt.Println("Dave not found")
}
```

### Control flow

```go
// If statements
if x > 10 {
    // ...
} else if x > 5 {
    // ...
} else {
    // ...
}

// If with initialization
if err := doSomething(); err != nil {
    return err
}

// For loop (only loop type in Go)
for i := 0; i < 10; i++ {
    // ...
}

// Range over slice
for index, value := range numbers {
    fmt.Println(index, value)
}

// While loop (for without conditions)
for {
    // Infinite loop, use break to exit
}

// Switch
switch day {
case "Monday":
    // ...
case "Tuesday", "Wednesday":
    // ...
default:
    // ...
}
```

---

## Go-Specific Patterns in AmanMCP

### Package organization

```text
amanmcp/
├── cmd/amanmcp/        # Entry point
│   └── main.go
├── internal/           # Private packages
│   ├── config/
│   ├── search/
│   └── mcp/
└── pkg/                # Public packages
    └── version/
```

- `internal/` packages can only be imported within AmanMCP
- `pkg/` packages can be imported by other projects

### Defer for cleanup

`defer` schedules a function call to run when the surrounding function returns:

```go
func ReadConfig(path string) (*Config, error) {
    file, err := os.Open(path)
    if err != nil {
        return nil, err
    }
    defer file.Close()  // Will run when function returns

    // ... read file ...
    return config, nil
}  // file.Close() called here
```

**Important in AmanMCP:** Always `defer` Close() calls for tree-sitter objects:

```go
parser := treesitter.NewParser()
defer parser.Close()

tree := parser.Parse(sourceCode)
defer tree.Close()
```

### Context for cancellation

`context.Context` passes cancellation signals and deadlines:

```go
func Search(ctx context.Context, query string) ([]Result, error) {
    // Check if cancelled
    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    default:
    }

    // Do work...
}

// Usage
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

results, err := Search(ctx, "authentication")
```

### Goroutines and channels

Concurrency is built into Go:

```go
// Start a goroutine (lightweight thread)
go func() {
    result := expensiveOperation()
    fmt.Println(result)
}()

// Channels for communication
results := make(chan int)

go func() {
    results <- 42  // Send to channel
}()

value := <-results  // Receive from channel
```

---

## Reading AmanMCP Code

### Common patterns you'll see

#### Error wrapping

```go
if err != nil {
    return nil, fmt.Errorf("failed to parse config: %w", err)
}
```

The `%w` verb wraps the error, preserving the original for inspection.

#### Options pattern

```go
type SearchOptions struct {
    MaxResults int
    Threshold  float64
}

func Search(query string, opts SearchOptions) ([]Result, error) {
    if opts.MaxResults == 0 {
        opts.MaxResults = 10  // Default
    }
    // ...
}
```

#### Interface satisfaction check

```go
// Compile-time check that Store implements Searcher
var _ Searcher = (*Store)(nil)
```

---

## Building and Testing

### Build commands

```bash
# Build
go build ./cmd/amanmcp

# Run tests
go test ./...

# Run tests with race detector
go test -race ./...

# Format code
go fmt ./...

# Lint (if golangci-lint installed)
golangci-lint run
```

### Using Make (AmanMCP uses Makefile)

```bash
make build       # Compile
make test        # Run tests
make lint        # Run linter
make ci-check    # Full CI check
```

---

## Common Gotchas

### nil vs empty

```go
var s []int     // nil slice
s = []int{}     // empty slice (not nil)

// Both have len 0, but nil check differs
if s == nil {
    // Only true for nil slice
}
```

### Pointers vs values

```go
// Value receiver - gets a copy
func (u User) GetName() string {
    return u.Name
}

// Pointer receiver - can modify original
func (u *User) SetName(name string) {
    u.Name = name
}
```

In AmanMCP, methods that modify state use pointer receivers.

### Variable shadowing

```go
x := 10
if true {
    x := 20  // New variable, shadows outer x
    fmt.Println(x)  // 20
}
fmt.Println(x)  // 10 (outer x unchanged)
```

---

## Resources

### Official Documentation

- [A Tour of Go](https://go.dev/tour/) - Interactive tutorial
- [Effective Go](https://go.dev/doc/effective_go) - Best practices
- [Go by Example](https://gobyexample.com/) - Practical examples

### AmanMCP-Specific

- [Code Conventions](code-conventions.md) - AmanMCP coding standards
- [Testing Guide](testing-guide.md) - How to write tests
- [TDD Rationale](tdd-rationale.md) - Why we use TDD

---

## Quick Reference Card

```text
┌─────────────────────────────────────────────────────────────┐
│  Go Quick Reference for AmanMCP Contributors                │
├─────────────────────────────────────────────────────────────┤
│  Variables        │  x := 10                                │
│  Functions        │  func name(a int) int { return a }      │
│  Multiple return  │  func f() (int, error) { }              │
│  Error check      │  if err != nil { return err }           │
│  Struct           │  type X struct { Field int }            │
│  Method           │  func (x *X) Method() { }               │
│  Interface        │  type I interface { Method() }          │
│  Slice            │  []int{1, 2, 3}                         │
│  Map              │  map[string]int{"a": 1}                 │
│  Defer            │  defer cleanup()                        │
│  Goroutine        │  go func() { }()                        │
│  Channel          │  ch := make(chan int)                   │
├─────────────────────────────────────────────────────────────┤
│  Build            │  make build                             │
│  Test             │  make test                              │
│  Lint             │  make lint                              │
│  CI Check         │  make ci-check                          │
└─────────────────────────────────────────────────────────────┘
```

---

## Next Steps

1. Read [Code Conventions](code-conventions.md) for AmanMCP-specific patterns
2. Try fixing a `good-first-issue` to practice
3. Ask questions in PR discussions - we're happy to help!
