package logging

import (
	"fmt"
	"os"
	"path/filepath"
)

// DefaultLogDir returns the default log directory (~/.amanmcp/logs/).
// Falls back to temp directory if home directory is unavailable.
func DefaultLogDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), ".amanmcp", "logs")
	}
	return filepath.Join(home, ".amanmcp", "logs")
}

// DefaultLogPath returns the default server log path.
func DefaultLogPath() string {
	return filepath.Join(DefaultLogDir(), "server.log")
}

// MLXLogPath returns the MLX server log path.
func MLXLogPath() string {
	return filepath.Join(DefaultLogDir(), "mlx-server.log")
}

// LogSource represents the source of logs to view.
type LogSource string

const (
	// LogSourceGo is the Go server logs (default).
	LogSourceGo LogSource = "go"
	// LogSourceMLX is the MLX Python server logs.
	LogSourceMLX LogSource = "mlx"
	// LogSourceAll combines all log sources.
	LogSourceAll LogSource = "all"
)

// FindLogFile attempts to find the log file for viewing.
// Priority:
// 1. Explicit path (if provided)
// 2. ~/.amanmcp/logs/server.log (global)
//
// Returns an error if no log file is found.
func FindLogFile(explicit string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err == nil {
			return explicit, nil
		}
		return "", fmt.Errorf("log file not found: %s", explicit)
	}

	// Try global path
	globalPath := DefaultLogPath()
	if _, err := os.Stat(globalPath); err == nil {
		return globalPath, nil
	}

	return "", fmt.Errorf("no log file found. Server may not have run with --debug yet.\nExpected at: %s", globalPath)
}

// FindLogFileBySource finds log files based on the source type.
// Returns a list of log file paths that exist.
func FindLogFileBySource(source LogSource, explicit string) ([]string, error) {
	// Explicit path takes precedence
	if explicit != "" {
		if _, err := os.Stat(explicit); err == nil {
			return []string{explicit}, nil
		}
		return nil, fmt.Errorf("log file not found: %s", explicit)
	}

	var paths []string
	var checked []string

	switch source {
	case LogSourceGo:
		goPath := DefaultLogPath()
		checked = append(checked, goPath)
		if _, err := os.Stat(goPath); err == nil {
			paths = append(paths, goPath)
		}

	case LogSourceMLX:
		mlxPath := MLXLogPath()
		checked = append(checked, mlxPath)
		if _, err := os.Stat(mlxPath); err == nil {
			paths = append(paths, mlxPath)
		}

	case LogSourceAll:
		goPath := DefaultLogPath()
		mlxPath := MLXLogPath()
		checked = append(checked, goPath, mlxPath)

		if _, err := os.Stat(goPath); err == nil {
			paths = append(paths, goPath)
		}
		if _, err := os.Stat(mlxPath); err == nil {
			paths = append(paths, mlxPath)
		}

	default:
		return nil, fmt.Errorf("unknown log source: %s (use: go, mlx, all)", source)
	}

	if len(paths) == 0 {
		hint := getLogHint(source)
		return nil, fmt.Errorf("no log files found for source '%s'.\nChecked: %v\n\n%s", source, checked, hint)
	}

	return paths, nil
}

// ParseLogSource parses a string into a LogSource.
func ParseLogSource(s string) LogSource {
	switch s {
	case "mlx":
		return LogSourceMLX
	case "all":
		return LogSourceAll
	default:
		return LogSourceGo
	}
}

// EnsureLogDir creates the log directory if it doesn't exist.
func EnsureLogDir() error {
	dir := DefaultLogDir()
	return os.MkdirAll(dir, 0o755)
}

// getLogHint returns a helpful message on how to generate logs for the given source.
func getLogHint(source LogSource) string {
	switch source {
	case LogSourceGo:
		return "To generate Go server logs:\n  amanmcp --debug serve"
	case LogSourceMLX:
		return "To generate MLX server logs:\n  cd mlx-server && python server.py"
	case LogSourceAll:
		return "To generate logs:\n  Go:  amanmcp --debug serve\n  MLX: cd mlx-server && python server.py"
	default:
		return ""
	}
}
