//go:build ignore

// Package main generates synthetic test corpus for benchmarking.
// Usage: go run scripts/generate-test-corpus.go -files 1000 -output testdata/bench
package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
)

var (
	numFiles  = flag.Int("files", 1000, "Number of files to generate")
	outputDir = flag.String("output", "testdata/bench", "Output directory")
	seed      = flag.Int64("seed", 42, "Random seed for reproducibility")
)

// Language templates for realistic code generation
var goTemplate = `package %s

import (
	"context"
	"fmt"
	"time"
)

// %s provides %s functionality.
type %s struct {
	id        string
	name      string
	createdAt time.Time
	config    map[string]any
}

// New%s creates a new %s instance.
func New%s(id, name string) *%s {
	return &%s{
		id:        id,
		name:      name,
		createdAt: time.Now(),
		config:    make(map[string]any),
	}
}

// %s performs the main operation.
func (s *%s) %s(ctx context.Context, input string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	result := fmt.Sprintf("processed: %%s by %%s", input, s.name)
	return result, nil
}

// Get%s retrieves %s data.
func (s *%s) Get%s() string {
	return s.%s
}

// Set%s updates %s data.
func (s *%s) Set%s(value string) {
	s.%s = value
}
`

var tsTemplate = `import { useState, useEffect, useCallback } from 'react';

interface %sProps {
  id: string;
  name: string;
  onUpdate?: (data: %sData) => void;
}

interface %sData {
  value: string;
  timestamp: number;
  metadata: Record<string, unknown>;
}

/**
 * %s component for %s functionality.
 */
export function %s({ id, name, onUpdate }: %sProps): JSX.Element {
  const [data, setData] = useState<%sData | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const fetch%s = useCallback(async () => {
    setLoading(true);
    try {
      const response = await fetch('/api/%s/' + id);
      const result = await response.json();
      setData(result);
      onUpdate?.(result);
    } catch (e) {
      setError(e instanceof Error ? e : new Error('Unknown error'));
    } finally {
      setLoading(false);
    }
  }, [id, onUpdate]);

  useEffect(() => {
    fetch%s();
  }, [fetch%s]);

  if (loading) return <div>Loading %s...</div>;
  if (error) return <div>Error: {error.message}</div>;
  if (!data) return <div>No data</div>;

  return (
    <div className="%s-container">
      <h2>{name}</h2>
      <p>ID: {id}</p>
      <pre>{JSON.stringify(data, null, 2)}</pre>
    </div>
  );
}

export default %s;
`

var pyTemplate = `"""
%s module for %s functionality.
"""
from dataclasses import dataclass, field
from datetime import datetime
from typing import Optional, Dict, Any, List
import logging

logger = logging.getLogger(__name__)


@dataclass
class %sConfig:
    """Configuration for %s."""

    name: str
    enabled: bool = True
    timeout: int = 30
    options: Dict[str, Any] = field(default_factory=dict)


@dataclass
class %sResult:
    """Result from %s operation."""

    success: bool
    data: Optional[Any] = None
    error: Optional[str] = None
    timestamp: datetime = field(default_factory=datetime.now)


class %s:
    """
    %s provides %s capabilities.

    Example:
        >>> handler = %s(config)
        >>> result = handler.process(data)
    """

    def __init__(self, config: %sConfig):
        self.config = config
        self._cache: Dict[str, Any] = {}
        self._initialized = False
        logger.info(f"Initializing %s with config: {config.name}")

    def initialize(self) -> None:
        """Initialize the %s handler."""
        if self._initialized:
            return
        self._initialized = True
        logger.debug("%s initialized successfully")

    def process(self, data: Dict[str, Any]) -> %sResult:
        """Process the input data."""
        if not self._initialized:
            self.initialize()

        try:
            # Process the data
            result = self._transform(data)
            return %sResult(success=True, data=result)
        except Exception as e:
            logger.error(f"%s processing failed: {e}")
            return %sResult(success=False, error=str(e))

    def _transform(self, data: Dict[str, Any]) -> Any:
        """Transform input data."""
        return {**data, "processed_by": self.config.name}

    def get_stats(self) -> Dict[str, int]:
        """Get processing statistics."""
        return {
            "cache_size": len(self._cache),
            "initialized": int(self._initialized),
        }
`

var mdTemplate = `# %s

## Overview

%s provides comprehensive %s functionality for modern applications.

## Features

- **Fast Processing**: Optimized for performance
- **Type Safety**: Full TypeScript support
- **Extensible**: Plugin architecture
- **Well Documented**: Comprehensive API docs

## Installation

` + "```bash" + `
npm install %s
# or
go get github.com/example/%s
` + "```" + `

## Quick Start

` + "```go" + `
package main

import "%s"

func main() {
    client := %s.New()
    result, err := client.Process(data)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(result)
}
` + "```" + `

## Configuration

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| timeout | int | 30 | Request timeout in seconds |
| retries | int | 3 | Number of retry attempts |
| debug | bool | false | Enable debug logging |

## API Reference

### %s.New(options)

Creates a new %s instance.

**Parameters:**
- ` + "`options`" + ` - Configuration options

**Returns:** %s instance

### %s.Process(data)

Processes the input data.

**Parameters:**
- ` + "`data`" + ` - Input data to process

**Returns:** Processed result

## Error Handling

` + "```go" + `
result, err := client.Process(data)
if err != nil {
    switch e := err.(type) {
    case *%s.ValidationError:
        // Handle validation error
    case *%s.TimeoutError:
        // Handle timeout
    default:
        // Handle other errors
    }
}
` + "```" + `

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

MIT License - see [LICENSE](LICENSE) for details.
`

// Word pools for generating realistic names
var (
	nouns = []string{
		"Handler", "Manager", "Service", "Controller", "Processor",
		"Engine", "Client", "Server", "Worker", "Factory",
		"Builder", "Parser", "Validator", "Formatter", "Converter",
		"Cache", "Store", "Queue", "Pool", "Buffer",
		"Router", "Dispatcher", "Scheduler", "Monitor", "Logger",
		"Auth", "User", "Session", "Token", "Config",
		"Data", "Event", "Message", "Request", "Response",
	}
	adjectives = []string{
		"async", "sync", "fast", "smart", "simple",
		"advanced", "basic", "custom", "default", "dynamic",
		"global", "local", "main", "core", "base",
		"internal", "external", "public", "private", "shared",
	}
	verbs = []string{
		"Process", "Handle", "Execute", "Run", "Start",
		"Stop", "Create", "Delete", "Update", "Read",
		"Parse", "Format", "Validate", "Convert", "Transform",
		"Send", "Receive", "Fetch", "Store", "Cache",
	}
	domains = []string{
		"authentication", "authorization", "caching", "logging", "monitoring",
		"messaging", "scheduling", "routing", "parsing", "validation",
		"serialization", "compression", "encryption", "hashing", "indexing",
		"searching", "filtering", "sorting", "pagination", "batching",
	}
)

func main() {
	flag.Parse()
	rand.Seed(*seed)

	if err := os.MkdirAll(*outputDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating output directory: %v\n", err)
		os.Exit(1)
	}

	// Create subdirectories
	subdirs := []string{"go", "typescript", "python", "docs"}
	for _, subdir := range subdirs {
		if err := os.MkdirAll(filepath.Join(*outputDir, subdir), 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Error creating subdirectory %s: %v\n", subdir, err)
			os.Exit(1)
		}
	}

	fmt.Printf("Generating %d files in %s...\n", *numFiles, *outputDir)

	// Distribute files across languages
	goFiles := *numFiles * 40 / 100      // 40% Go
	tsFiles := *numFiles * 30 / 100      // 30% TypeScript
	pyFiles := *numFiles * 20 / 100      // 20% Python
	mdFiles := *numFiles - goFiles - tsFiles - pyFiles // ~10% Markdown

	generated := 0

	// Generate Go files
	for i := 0; i < goFiles; i++ {
		if err := generateGoFile(i); err != nil {
			fmt.Fprintf(os.Stderr, "Error generating Go file %d: %v\n", i, err)
		}
		generated++
	}

	// Generate TypeScript files
	for i := 0; i < tsFiles; i++ {
		if err := generateTSFile(i); err != nil {
			fmt.Fprintf(os.Stderr, "Error generating TS file %d: %v\n", i, err)
		}
		generated++
	}

	// Generate Python files
	for i := 0; i < pyFiles; i++ {
		if err := generatePyFile(i); err != nil {
			fmt.Fprintf(os.Stderr, "Error generating Python file %d: %v\n", i, err)
		}
		generated++
	}

	// Generate Markdown files
	for i := 0; i < mdFiles; i++ {
		if err := generateMDFile(i); err != nil {
			fmt.Fprintf(os.Stderr, "Error generating MD file %d: %v\n", i, err)
		}
		generated++
	}

	fmt.Printf("Generated %d files successfully.\n", generated)
}

func randomWord(pool []string) string {
	return pool[rand.Intn(len(pool))]
}

func generateGoFile(index int) error {
	noun := randomWord(nouns)
	adj := randomWord(adjectives)
	verb := randomWord(verbs)
	domain := randomWord(domains)

	pkgName := fmt.Sprintf("pkg%d", index)
	typeName := noun
	methodName := verb

	content := fmt.Sprintf(goTemplate,
		pkgName,
		typeName, domain, typeName,
		typeName, adj, typeName, typeName, typeName,
		methodName, typeName, methodName,
		"Name", domain, typeName, "Name", "name",
		"Name", domain, typeName, "Name", "name",
	)

	filename := filepath.Join(*outputDir, "go", fmt.Sprintf("%s_%s_%d.go", adj, noun, index))
	return os.WriteFile(filename, []byte(content), 0644)
}

func generateTSFile(index int) error {
	noun := randomWord(nouns)
	domain := randomWord(domains)

	content := fmt.Sprintf(tsTemplate,
		noun, noun, noun,
		noun, domain, noun, noun, noun,
		noun, noun,
		noun, noun, noun,
		noun, noun,
	)

	filename := filepath.Join(*outputDir, "typescript", fmt.Sprintf("%s%d.tsx", noun, index))
	return os.WriteFile(filename, []byte(content), 0644)
}

func generatePyFile(index int) error {
	noun := randomWord(nouns)
	domain := randomWord(domains)

	content := fmt.Sprintf(pyTemplate,
		noun, domain,
		noun, noun,
		noun, noun,
		noun, noun, domain, noun, noun,
		noun, noun, noun,
		noun, noun,
		noun, noun, noun,
	)

	filename := filepath.Join(*outputDir, "python", fmt.Sprintf("%s_%d.py", noun, index))
	return os.WriteFile(filename, []byte(content), 0644)
}

func generateMDFile(index int) error {
	noun := randomWord(nouns)
	domain := randomWord(domains)

	content := fmt.Sprintf(mdTemplate,
		noun,
		noun, domain,
		noun, noun, noun, noun,
		noun, noun, noun,
		noun, noun, noun,
		noun, noun,
	)

	filename := filepath.Join(*outputDir, "docs", fmt.Sprintf("%s_%d.md", noun, index))
	return os.WriteFile(filename, []byte(content), 0644)
}
