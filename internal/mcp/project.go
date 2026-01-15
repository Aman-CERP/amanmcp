package mcp

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ProjectDetector detects project metadata from common files.
type ProjectDetector struct {
	rootPath string
	logger   *slog.Logger
}

// NewProjectDetector creates a new project detector.
func NewProjectDetector(rootPath string, logger *slog.Logger) *ProjectDetector {
	if logger == nil {
		logger = slog.Default()
	}
	return &ProjectDetector{
		rootPath: rootPath,
		logger:   logger,
	}
}

// Detect returns project information detected from the project directory.
// Detection order: go.mod -> package.json -> pyproject.toml -> directory name.
func (d *ProjectDetector) Detect() *ProjectInfo {
	info := &ProjectInfo{
		RootPath: d.rootPath,
		Name:     filepath.Base(d.rootPath), // Default to directory name
		Type:     "unknown",
	}

	// Try go.mod first
	if name := d.detectGoMod(); name != "" {
		info.Name = name
		info.Type = "go"
		return info
	}

	// Try package.json
	if name := d.detectPackageJSON(); name != "" {
		info.Name = name
		info.Type = "node"
		return info
	}

	// Try pyproject.toml
	if name := d.detectPyproject(); name != "" {
		info.Name = name
		info.Type = "python"
		return info
	}

	return info
}

// detectGoMod parses go.mod and extracts the module name.
func (d *ProjectDetector) detectGoMod() string {
	goModPath := filepath.Join(d.rootPath, "go.mod")
	file, err := os.Open(goModPath)
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()

	// Parse first line: "module <path>"
	scanner := bufio.NewScanner(file)
	moduleRegex := regexp.MustCompile(`^module\s+(.+)$`)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if matches := moduleRegex.FindStringSubmatch(line); len(matches) > 1 {
			modulePath := matches[1]
			// Extract last segment of module path
			return filepath.Base(modulePath)
		}
	}

	return ""
}

// detectPackageJSON parses package.json and extracts the name.
func (d *ProjectDetector) detectPackageJSON() string {
	pkgPath := filepath.Join(d.rootPath, "package.json")
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return ""
	}

	var pkg struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return ""
	}

	name := pkg.Name
	if name == "" {
		return ""
	}

	// Handle scoped packages (@org/name -> name)
	if strings.HasPrefix(name, "@") {
		parts := strings.Split(name, "/")
		if len(parts) > 1 {
			name = parts[len(parts)-1]
		}
	}

	return name
}

// detectPyproject parses pyproject.toml and extracts the project name.
func (d *ProjectDetector) detectPyproject() string {
	pyPath := filepath.Join(d.rootPath, "pyproject.toml")
	file, err := os.Open(pyPath)
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()

	// Simple TOML parsing for name field
	// Looking for: name = "project-name" under [project] section
	scanner := bufio.NewScanner(file)
	nameRegex := regexp.MustCompile(`^\s*name\s*=\s*["']([^"']+)["']`)
	inProjectSection := false

	for scanner.Scan() {
		line := scanner.Text()

		// Check for section headers
		if strings.HasPrefix(strings.TrimSpace(line), "[") {
			inProjectSection = strings.TrimSpace(line) == "[project]"
			continue
		}

		// Look for name in [project] section
		if inProjectSection {
			if matches := nameRegex.FindStringSubmatch(line); len(matches) > 1 {
				return matches[1]
			}
		}
	}

	return ""
}
