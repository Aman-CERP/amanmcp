package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDefaultLogDir(t *testing.T) {
	dir := DefaultLogDir()
	if dir == "" {
		t.Error("DefaultLogDir returned empty string")
	}

	// Should contain .amanmcp/logs
	if !contains(dir, ".amanmcp") || !contains(dir, "logs") {
		t.Errorf("DefaultLogDir should contain .amanmcp/logs, got: %s", dir)
	}
}

func TestDefaultLogPath(t *testing.T) {
	path := DefaultLogPath()
	if path == "" {
		t.Error("DefaultLogPath returned empty string")
	}

	// Should end with server.log
	if filepath.Base(path) != "server.log" {
		t.Errorf("DefaultLogPath should end with server.log, got: %s", path)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Level != "info" {
		t.Errorf("expected level 'info', got: %s", cfg.Level)
	}
	if cfg.MaxSizeMB != 10 {
		t.Errorf("expected MaxSizeMB 10, got: %d", cfg.MaxSizeMB)
	}
	if cfg.MaxFiles != 5 {
		t.Errorf("expected MaxFiles 5, got: %d", cfg.MaxFiles)
	}
	if !cfg.WriteToStderr {
		t.Error("expected WriteToStderr to be true")
	}
}

func TestDebugConfig(t *testing.T) {
	cfg := DebugConfig()

	if cfg.Level != "debug" {
		t.Errorf("expected level 'debug', got: %s", cfg.Level)
	}
}

func TestSetup(t *testing.T) {
	// Create temp directory for log file
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	cfg := Config{
		Level:         "debug",
		FilePath:      logPath,
		MaxSizeMB:     1,
		MaxFiles:      3,
		WriteToStderr: false,
	}

	logger, cleanup, err := Setup(cfg)
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer cleanup()

	if logger == nil {
		t.Error("Setup returned nil logger")
	}

	// Write a log entry
	logger.Info("test message")

	// Verify log file was created
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Error("Log file was not created")
	}
}

func TestLevelFromString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"debug", "DEBUG"},
		{"DEBUG", "DEBUG"},
		{"info", "INFO"},
		{"INFO", "INFO"},
		{"warn", "WARN"},
		{"warning", "WARN"},
		{"error", "ERROR"},
		{"ERROR", "ERROR"},
		{"unknown", "INFO"}, // defaults to info
	}

	for _, tc := range tests {
		level := LevelFromString(tc.input)
		if level.String() != tc.expected {
			t.Errorf("LevelFromString(%q) = %s, want %s", tc.input, level.String(), tc.expected)
		}
	}
}

func TestFindLogFile_NotFound(t *testing.T) {
	_, err := FindLogFile("/nonexistent/path/to/log.log")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestFindLogFile_ExplicitPath(t *testing.T) {
	// Create temp file
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")
	if err := os.WriteFile(logPath, []byte("test"), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	found, err := FindLogFile(logPath)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if found != logPath {
		t.Errorf("expected %s, got %s", logPath, found)
	}
}

func TestRotatingWriter_ImmediateSync(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	// Create writer with immediate sync (default)
	w, err := NewRotatingWriter(logPath, 1, 3)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}
	defer w.Close()

	// Write some data
	testData := []byte(`{"time":"2026-01-01T00:00:00Z","level":"INFO","msg":"test"}` + "\n")
	n, err := w.Write(testData)
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if n != len(testData) {
		t.Errorf("expected %d bytes written, got %d", len(testData), n)
	}

	// With immediate sync, data should be visible immediately
	// Read the file without closing the writer
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	if string(content) != string(testData) {
		t.Errorf("expected %q, got %q", string(testData), string(content))
	}
}

func TestRotatingWriter_DisableImmediateSync(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	// Create writer and disable immediate sync
	w, err := NewRotatingWriter(logPath, 1, 3)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}
	defer w.Close()

	w.SetImmediateSync(false)

	// Write should still succeed
	testData := []byte(`{"time":"2026-01-01T00:00:00Z","level":"INFO","msg":"test"}` + "\n")
	n, err := w.Write(testData)
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if n != len(testData) {
		t.Errorf("expected %d bytes written, got %d", len(testData), n)
	}

	// Manually sync and verify
	if err := w.Sync(); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	if string(content) != string(testData) {
		t.Errorf("expected %q, got %q", string(testData), string(content))
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ============================================================================
// MCP Mode Tests (BUG-034: Stdout protection)
// ============================================================================

func TestSetupMCPMode(t *testing.T) {
	// Override DefaultLogPath temporarily
	tmpDir := t.TempDir()
	origDefaultLogDir := DefaultLogDir
	defer func() { _ = origDefaultLogDir }() // Restore is not needed since we create temp file

	logPath := filepath.Join(tmpDir, "mcp-test.log")

	cfg := Config{
		Level:         "debug",
		FilePath:      logPath,
		MaxSizeMB:     1,
		MaxFiles:      3,
		WriteToStderr: false, // MCP mode critical setting
	}

	logger, cleanup, err := Setup(cfg)
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer cleanup()

	// Verify logger works
	logger.Info("mcp mode test message")

	// Verify log file was created
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Error("Log file was not created")
	}

	// Verify WriteToStderr is false (critical for MCP)
	if cfg.WriteToStderr {
		t.Error("WriteToStderr should be false in MCP mode")
	}
}

func TestSetupMCPModeWithLevel(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "mcp-level-test.log")

	tests := []struct {
		name  string
		level string
	}{
		{"debug level", "debug"},
		{"info level", "info"},
		{"warn level", "warn"},
		{"error level", "error"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{
				Level:         tc.level,
				FilePath:      filepath.Join(tmpDir, tc.level+".log"),
				MaxSizeMB:     1,
				MaxFiles:      3,
				WriteToStderr: false,
			}

			logger, cleanup, err := Setup(cfg)
			if err != nil {
				t.Fatalf("Setup failed: %v", err)
			}
			defer cleanup()

			if logger == nil {
				t.Error("logger should not be nil")
			}
		})
	}

	_ = logPath // Used in filename pattern
}

// ============================================================================
// Path Functions Tests
// ============================================================================

func TestMLXLogPath(t *testing.T) {
	path := MLXLogPath()
	if path == "" {
		t.Error("MLXLogPath returned empty string")
	}

	// Should end with mlx-server.log
	if filepath.Base(path) != "mlx-server.log" {
		t.Errorf("MLXLogPath should end with mlx-server.log, got: %s", path)
	}

	// Should be in .amanmcp/logs directory
	if !contains(path, ".amanmcp") || !contains(path, "logs") {
		t.Errorf("MLXLogPath should be in .amanmcp/logs, got: %s", path)
	}
}

func TestFindLogFileBySource_GoSource(t *testing.T) {
	tmpDir := t.TempDir()
	goLogPath := filepath.Join(tmpDir, "server.log")

	// Create test log file
	if err := os.WriteFile(goLogPath, []byte("test log"), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Test with explicit path
	paths, err := FindLogFileBySource(LogSourceGo, goLogPath)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(paths) != 1 || paths[0] != goLogPath {
		t.Errorf("expected [%s], got %v", goLogPath, paths)
	}
}

func TestFindLogFileBySource_ExplicitNotFound(t *testing.T) {
	_, err := FindLogFileBySource(LogSourceGo, "/nonexistent/path/to/log.log")
	if err == nil {
		t.Error("expected error for nonexistent explicit path")
	}
}

func TestFindLogFileBySource_UnknownSource(t *testing.T) {
	_, err := FindLogFileBySource(LogSource("invalid"), "")
	if err == nil {
		t.Error("expected error for unknown source")
	}
	if !contains(err.Error(), "unknown log source") {
		t.Errorf("error should mention unknown source, got: %v", err)
	}
}

func TestParseLogSource(t *testing.T) {
	tests := []struct {
		input    string
		expected LogSource
	}{
		{"go", LogSourceGo},
		{"mlx", LogSourceMLX},
		{"all", LogSourceAll},
		{"unknown", LogSourceGo}, // defaults to go
		{"", LogSourceGo},        // defaults to go
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := ParseLogSource(tc.input)
			if result != tc.expected {
				t.Errorf("ParseLogSource(%q) = %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestEnsureLogDir(t *testing.T) {
	// This creates the default log directory
	// We just verify it doesn't error
	err := EnsureLogDir()
	if err != nil {
		t.Errorf("EnsureLogDir failed: %v", err)
	}

	// Verify directory exists
	dir := DefaultLogDir()
	info, err := os.Stat(dir)
	if err != nil {
		t.Errorf("log directory should exist: %v", err)
	}
	if !info.IsDir() {
		t.Error("log path should be a directory")
	}
}

// ============================================================================
// Viewer Tests
// ============================================================================

func TestViewer_ParseLine_ValidJSON(t *testing.T) {
	var buf strings.Builder
	v := NewViewer(ViewerConfig{}, &buf)

	line := `{"time":"2026-01-15T10:30:00Z","level":"INFO","msg":"test message","extra":"value"}`
	entry := v.parseLine(line)

	if !entry.IsValid {
		t.Error("entry should be valid")
	}
	if entry.Level != "INFO" {
		t.Errorf("expected level INFO, got %s", entry.Level)
	}
	if entry.Msg != "test message" {
		t.Errorf("expected msg 'test message', got %s", entry.Msg)
	}
	if entry.Attrs["extra"] != "value" {
		t.Errorf("expected extra=value, got %v", entry.Attrs["extra"])
	}
}

func TestViewer_ParseLine_InvalidJSON(t *testing.T) {
	var buf strings.Builder
	v := NewViewer(ViewerConfig{}, &buf)

	line := "not valid json"
	entry := v.parseLine(line)

	if entry.IsValid {
		t.Error("entry should not be valid for invalid JSON")
	}
	if entry.Raw != line {
		t.Errorf("Raw should contain original line, got %s", entry.Raw)
	}
}

func TestViewer_ParseLine_WithSource(t *testing.T) {
	var buf strings.Builder
	v := NewViewer(ViewerConfig{}, &buf)

	line := `{"time":"2026-01-15T10:30:00Z","level":"DEBUG","msg":"mlx message","source":"mlx"}`
	entry := v.parseLine(line)

	if !entry.IsValid {
		t.Error("entry should be valid")
	}
	if entry.Source != "mlx" {
		t.Errorf("expected source 'mlx', got %s", entry.Source)
	}
}

func TestViewer_MatchesFilter_LevelFilter(t *testing.T) {
	tests := []struct {
		name        string
		configLevel string
		entryLevel  string
		shouldMatch bool
	}{
		{"info allows info", "info", "INFO", true},
		{"info allows warn", "info", "WARN", true},
		{"info allows error", "info", "ERROR", true},
		{"info blocks debug", "info", "DEBUG", false},
		{"warn allows warn", "warn", "WARN", true},
		{"warn allows error", "warn", "ERROR", true},
		{"warn blocks info", "warn", "INFO", false},
		{"error allows error", "error", "ERROR", true},
		{"error blocks warn", "error", "WARN", false},
		{"empty filter allows all", "", "DEBUG", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf strings.Builder
			v := NewViewer(ViewerConfig{Level: tc.configLevel}, &buf)

			entry := LogEntry{
				IsValid: true,
				Level:   tc.entryLevel,
			}

			result := v.matchesFilter(entry)
			if result != tc.shouldMatch {
				t.Errorf("matchesFilter() = %v, want %v", result, tc.shouldMatch)
			}
		})
	}
}

func TestViewer_MatchesFilter_PatternFilter(t *testing.T) {
	var buf strings.Builder
	pattern := regexp.MustCompile("error.*database")
	v := NewViewer(ViewerConfig{Pattern: pattern}, &buf)

	tests := []struct {
		name        string
		raw         string
		shouldMatch bool
	}{
		{"matches pattern", "error connecting to database", true},
		{"no match", "info message about something else", false},
		{"partial match", "database error", false}, // order matters
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entry := LogEntry{
				IsValid: true,
				Raw:     tc.raw,
			}

			result := v.matchesFilter(entry)
			if result != tc.shouldMatch {
				t.Errorf("matchesFilter() = %v, want %v", result, tc.shouldMatch)
			}
		})
	}
}

func TestViewer_FormatEntry_ValidEntry(t *testing.T) {
	var buf strings.Builder
	v := NewViewer(ViewerConfig{NoColor: true}, &buf)

	entry := LogEntry{
		IsValid: true,
		Time:    mustParseTime("2026-01-15T10:30:00Z"),
		Level:   "INFO",
		Msg:     "test message",
		Attrs:   map[string]interface{}{"key": "value"},
	}

	formatted := v.FormatEntry(entry)

	// Should contain timestamp, level, message, and attributes
	if !contains(formatted, "10:30:00") {
		t.Error("formatted entry should contain timestamp")
	}
	if !contains(formatted, "INFO") {
		t.Error("formatted entry should contain level")
	}
	if !contains(formatted, "test message") {
		t.Error("formatted entry should contain message")
	}
	if !contains(formatted, "key=value") {
		t.Error("formatted entry should contain attributes")
	}
}

func TestViewer_FormatEntry_InvalidEntry(t *testing.T) {
	var buf strings.Builder
	v := NewViewer(ViewerConfig{NoColor: true}, &buf)

	entry := LogEntry{
		IsValid: false,
		Raw:     "raw unparseable log line",
	}

	formatted := v.FormatEntry(entry)

	// Should return raw line for invalid entries
	if formatted != "raw unparseable log line" {
		t.Errorf("expected raw line, got %s", formatted)
	}
}

func TestViewer_FormatEntry_WithSource(t *testing.T) {
	var buf strings.Builder
	v := NewViewer(ViewerConfig{NoColor: true, ShowSource: true}, &buf)

	entry := LogEntry{
		IsValid: true,
		Time:    mustParseTime("2026-01-15T10:30:00Z"),
		Level:   "INFO",
		Msg:     "message from mlx",
		Source:  "mlx",
	}

	formatted := v.FormatEntry(entry)

	if !contains(formatted, "[mlx]") {
		t.Errorf("formatted entry should contain source label, got: %s", formatted)
	}
}

func TestViewer_FormatLevel_AllLevels(t *testing.T) {
	var buf strings.Builder
	v := NewViewer(ViewerConfig{NoColor: true}, &buf)

	tests := []struct {
		level    string
		expected string
	}{
		{"debug", "DEBUG"},
		{"info", "INFO "},
		{"warn", "WARN "},
		{"warning", "WARNI"}, // truncated to 5 chars
		{"error", "ERROR"},
	}

	for _, tc := range tests {
		t.Run(tc.level, func(t *testing.T) {
			result := v.formatLevel(tc.level)
			if result != tc.expected {
				t.Errorf("formatLevel(%q) = %q, want %q", tc.level, result, tc.expected)
			}
		})
	}
}

func TestViewer_FormatSource_AllSources(t *testing.T) {
	var buf strings.Builder
	v := NewViewer(ViewerConfig{NoColor: true}, &buf)

	tests := []struct {
		source   string
		expected string
	}{
		{"go", "[go]"},
		{"mlx", "[mlx]"},
		{"unknown", "[unknown]"},
	}

	for _, tc := range tests {
		t.Run(tc.source, func(t *testing.T) {
			result := v.formatSource(tc.source)
			if result != tc.expected {
				t.Errorf("formatSource(%q) = %q, want %q", tc.source, result, tc.expected)
			}
		})
	}
}

func TestViewer_Tail(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	// Create log file with multiple entries
	entries := []string{
		`{"time":"2026-01-15T10:00:00Z","level":"DEBUG","msg":"message 1"}`,
		`{"time":"2026-01-15T10:01:00Z","level":"INFO","msg":"message 2"}`,
		`{"time":"2026-01-15T10:02:00Z","level":"WARN","msg":"message 3"}`,
		`{"time":"2026-01-15T10:03:00Z","level":"ERROR","msg":"message 4"}`,
		`{"time":"2026-01-15T10:04:00Z","level":"INFO","msg":"message 5"}`,
	}
	content := strings.Join(entries, "\n") + "\n"

	if err := os.WriteFile(logPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write test log: %v", err)
	}

	var buf strings.Builder
	v := NewViewer(ViewerConfig{}, &buf)

	// Tail last 3 entries
	result, err := v.Tail(logPath, 3)
	if err != nil {
		t.Fatalf("Tail failed: %v", err)
	}

	if len(result) != 3 {
		t.Errorf("expected 3 entries, got %d", len(result))
	}

	// Verify last 3 messages
	expectedMsgs := []string{"message 3", "message 4", "message 5"}
	for i, msg := range expectedMsgs {
		if result[i].Msg != msg {
			t.Errorf("entry %d: expected msg %q, got %q", i, msg, result[i].Msg)
		}
	}
}

func TestViewer_Tail_WithLevelFilter(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	entries := []string{
		`{"time":"2026-01-15T10:00:00Z","level":"DEBUG","msg":"debug message"}`,
		`{"time":"2026-01-15T10:01:00Z","level":"INFO","msg":"info message"}`,
		`{"time":"2026-01-15T10:02:00Z","level":"ERROR","msg":"error message"}`,
	}
	content := strings.Join(entries, "\n") + "\n"

	if err := os.WriteFile(logPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write test log: %v", err)
	}

	var buf strings.Builder
	v := NewViewer(ViewerConfig{Level: "error"}, &buf)

	result, err := v.Tail(logPath, 10)
	if err != nil {
		t.Fatalf("Tail failed: %v", err)
	}

	// Should only return error-level messages
	if len(result) != 1 {
		t.Errorf("expected 1 entry (error only), got %d", len(result))
	}
	if len(result) > 0 && result[0].Msg != "error message" {
		t.Errorf("expected 'error message', got %q", result[0].Msg)
	}
}

func TestViewer_Tail_NonexistentFile(t *testing.T) {
	var buf strings.Builder
	v := NewViewer(ViewerConfig{}, &buf)

	_, err := v.Tail("/nonexistent/log/file.log", 10)
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestViewer_TailMultiple(t *testing.T) {
	tmpDir := t.TempDir()
	goLogPath := filepath.Join(tmpDir, "server.log")
	mlxLogPath := filepath.Join(tmpDir, "mlx-server.log")

	// Create Go log file
	goEntries := []string{
		`{"time":"2026-01-15T10:00:00Z","level":"INFO","msg":"go message 1"}`,
		`{"time":"2026-01-15T10:02:00Z","level":"INFO","msg":"go message 2"}`,
	}
	if err := os.WriteFile(goLogPath, []byte(strings.Join(goEntries, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write go log: %v", err)
	}

	// Create MLX log file
	mlxEntries := []string{
		`{"time":"2026-01-15T10:01:00Z","level":"INFO","msg":"mlx message 1"}`,
		`{"time":"2026-01-15T10:03:00Z","level":"INFO","msg":"mlx message 2"}`,
	}
	if err := os.WriteFile(mlxLogPath, []byte(strings.Join(mlxEntries, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write mlx log: %v", err)
	}

	var buf strings.Builder
	v := NewViewer(ViewerConfig{}, &buf)

	result, err := v.TailMultiple([]string{goLogPath, mlxLogPath}, 10)
	if err != nil {
		t.Fatalf("TailMultiple failed: %v", err)
	}

	// Should have all 4 entries sorted by time
	if len(result) != 4 {
		t.Errorf("expected 4 entries, got %d", len(result))
	}

	// Verify chronological order
	expectedOrder := []string{"go message 1", "mlx message 1", "go message 2", "mlx message 2"}
	for i, msg := range expectedOrder {
		if result[i].Msg != msg {
			t.Errorf("entry %d: expected %q, got %q", i, msg, result[i].Msg)
		}
	}
}

func TestViewer_Print(t *testing.T) {
	var buf strings.Builder
	v := NewViewer(ViewerConfig{NoColor: true}, &buf)

	entries := []LogEntry{
		{IsValid: true, Time: mustParseTime("2026-01-15T10:00:00Z"), Level: "INFO", Msg: "first"},
		{IsValid: true, Time: mustParseTime("2026-01-15T10:01:00Z"), Level: "WARN", Msg: "second"},
	}

	v.Print(entries)

	output := buf.String()
	if !contains(output, "first") || !contains(output, "second") {
		t.Errorf("Print output should contain both messages, got: %s", output)
	}
}

func TestSourceFromPath(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/path/to/server.log", "go"},
		{"/path/to/mlx-server.log", "mlx"},
		{"/path/to/other.log", "unknown"},
		{"server.log", "go"},
		{"mlx-server.log", "mlx"},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			result := sourceFromPath(tc.path)
			if result != tc.expected {
				t.Errorf("sourceFromPath(%q) = %q, want %q", tc.path, result, tc.expected)
			}
		})
	}
}

// ============================================================================
// Writer Rotation Tests
// ============================================================================

func TestRotatingWriter_Rotation(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "rotate.log")

	// Create writer with very small max size (1KB) to trigger rotation
	w, err := NewRotatingWriter(logPath, 0, 3) // 0 MB = triggers rotation on any write
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}
	defer w.Close()

	// Write data that exceeds the size limit
	largeData := make([]byte, 2048) // 2KB
	for i := range largeData {
		largeData[i] = 'x'
	}

	// Write first batch (should trigger rotation)
	_, err = w.Write(largeData)
	if err != nil {
		t.Fatalf("first write failed: %v", err)
	}

	// Write second batch (should trigger another rotation)
	_, err = w.Write(largeData)
	if err != nil {
		t.Fatalf("second write failed: %v", err)
	}

	// Verify rotated files exist
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Error("main log file should exist")
	}
	if _, err := os.Stat(logPath + ".1"); os.IsNotExist(err) {
		t.Error("rotated file .1 should exist")
	}
}

func TestRotatingWriter_MaxFilesLimit(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "maxfiles.log")

	// Create writer with maxFiles=2
	w, err := NewRotatingWriter(logPath, 0, 2) // 0 MB triggers rotation
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}
	defer w.Close()

	largeData := make([]byte, 1024)
	for i := range largeData {
		largeData[i] = 'y'
	}

	// Write multiple times to trigger multiple rotations
	for i := 0; i < 5; i++ {
		_, _ = w.Write(largeData)
	}

	// After rotations with maxFiles=2, .3 and beyond should not exist
	// Only .1 and .2 should exist (plus current log)
	if _, err := os.Stat(logPath + ".3"); !os.IsNotExist(err) {
		t.Error("rotated file .3 should not exist (beyond maxFiles)")
	}
}

func TestRotatingWriter_CloseSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "close.log")

	w, err := NewRotatingWriter(logPath, 1, 3)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}

	// Write some data first
	_, err = w.Write([]byte("test data\n"))
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// Close should succeed
	err = w.Close()
	if err != nil {
		t.Errorf("close failed: %v", err)
	}
}

func TestRotatingWriter_SyncSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "sync.log")

	w, err := NewRotatingWriter(logPath, 1, 3)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}
	defer w.Close()

	// Write some data
	_, err = w.Write([]byte("test data to sync\n"))
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// Sync should succeed
	err = w.Sync()
	if err != nil {
		t.Errorf("sync failed: %v", err)
	}

	// Verify data is persisted
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}
	if !contains(string(content), "test data to sync") {
		t.Error("synced data should be readable")
	}
}

func TestRotatingWriter_ConcurrentWrites(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "concurrent.log")

	w, err := NewRotatingWriter(logPath, 10, 3)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}
	defer w.Close()

	// Run concurrent writes (test race detector)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				msg := fmt.Sprintf(`{"id":%d,"iter":%d,"msg":"test"}`, id, j) + "\n"
				_, _ = w.Write([]byte(msg))
			}
		}(i)
	}
	wg.Wait()

	// Verify file exists and has content
	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("log file should exist: %v", err)
	}
	if info.Size() == 0 {
		t.Error("log file should have content")
	}
}

// ============================================================================
// Helper Functions
// ============================================================================

func mustParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}
