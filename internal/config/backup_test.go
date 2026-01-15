package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBackupUserConfig(t *testing.T) {
	// Create temp directory for test
	tmpDir := t.TempDir()

	// Override config path for testing
	origXDG := os.Getenv("XDG_CONFIG_HOME")
	os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer os.Setenv("XDG_CONFIG_HOME", origXDG)

	configDir := filepath.Join(tmpDir, "amanmcp")
	configPath := filepath.Join(configDir, "config.yaml")

	t.Run("no config exists", func(t *testing.T) {
		backupPath, err := BackupUserConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if backupPath != "" {
			t.Errorf("expected empty backup path for non-existent config, got %s", backupPath)
		}
	})

	t.Run("backup existing config", func(t *testing.T) {
		// Create config directory and file
		if err := os.MkdirAll(configDir, 0755); err != nil {
			t.Fatalf("failed to create config dir: %v", err)
		}
		testContent := "version: 1\nembeddings:\n  provider: ollama\n"
		if err := os.WriteFile(configPath, []byte(testContent), 0644); err != nil {
			t.Fatalf("failed to write test config: %v", err)
		}

		backupPath, err := BackupUserConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if backupPath == "" {
			t.Fatal("expected non-empty backup path")
		}

		// Verify backup exists and has correct content
		backupContent, err := os.ReadFile(backupPath)
		if err != nil {
			t.Fatalf("failed to read backup: %v", err)
		}
		if string(backupContent) != testContent {
			t.Errorf("backup content mismatch:\ngot: %s\nwant: %s", backupContent, testContent)
		}

		// Verify backup filename format
		if !filepath.IsAbs(backupPath) {
			t.Errorf("backup path should be absolute: %s", backupPath)
		}
	})
}

func TestListUserConfigBackups(t *testing.T) {
	tmpDir := t.TempDir()

	origXDG := os.Getenv("XDG_CONFIG_HOME")
	os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer os.Setenv("XDG_CONFIG_HOME", origXDG)

	configDir := filepath.Join(tmpDir, "amanmcp")
	configPath := filepath.Join(configDir, "config.yaml")

	// Create config directory
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	t.Run("no backups exist", func(t *testing.T) {
		backups, err := ListUserConfigBackups()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(backups) != 0 {
			t.Errorf("expected 0 backups, got %d", len(backups))
		}
	})

	t.Run("list multiple backups", func(t *testing.T) {
		// Create some backup files with different timestamps
		timestamps := []string{"20260101-100000", "20260101-110000", "20260101-120000"}
		for _, ts := range timestamps {
			backupName := filepath.Join(configDir, "config.yaml.bak."+ts)
			if err := os.WriteFile(backupName, []byte("test"), 0644); err != nil {
				t.Fatalf("failed to create backup: %v", err)
			}
			// Small delay to ensure different mod times
			time.Sleep(10 * time.Millisecond)
		}

		backups, err := ListUserConfigBackups()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(backups) != 3 {
			t.Errorf("expected 3 backups, got %d", len(backups))
		}

		// Verify sorted by mod time (newest first)
		for i := 1; i < len(backups); i++ {
			info1, _ := os.Stat(backups[i-1])
			info2, _ := os.Stat(backups[i])
			if info1.ModTime().Before(info2.ModTime()) {
				t.Errorf("backups not sorted correctly: %s before %s", backups[i-1], backups[i])
			}
		}
	})

	t.Run("cleanup old backups", func(t *testing.T) {
		// Create config file
		if err := os.WriteFile(configPath, []byte("test config"), 0644); err != nil {
			t.Fatalf("failed to write config: %v", err)
		}

		// Create 4 more backups (should trigger cleanup)
		for i := 0; i < 4; i++ {
			_, err := BackupUserConfig()
			if err != nil {
				t.Fatalf("failed to create backup: %v", err)
			}
			time.Sleep(10 * time.Millisecond)
		}

		// Should have at most MaxBackups
		backups, err := ListUserConfigBackups()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(backups) > MaxBackups {
			t.Errorf("expected at most %d backups, got %d", MaxBackups, len(backups))
		}
	})
}

func TestMergeNewDefaults(t *testing.T) {
	t.Run("adds missing search config fields", func(t *testing.T) {
		// FEAT-UNIX2: Simulates upgrade from pre-v0.8.2 config without search weights
		cfg := &Config{
			Version: 1,
			Search: SearchConfig{
				ChunkSize:  1500,
				MaxResults: 20,
				// BM25Weight, SemanticWeight, RRFConstant are 0 (not set)
			},
		}

		added := cfg.MergeNewDefaults()

		// Should add search config fields with defaults
		if cfg.Search.BM25Weight != 0.65 {
			t.Errorf("BM25Weight should be 0.65, got %f", cfg.Search.BM25Weight)
		}
		if cfg.Search.SemanticWeight != 0.35 {
			t.Errorf("SemanticWeight should be 0.35, got %f", cfg.Search.SemanticWeight)
		}
		if cfg.Search.RRFConstant != 60 {
			t.Errorf("RRFConstant should be 60, got %d", cfg.Search.RRFConstant)
		}

		// Should report the fields
		hasBM25 := false
		hasSemantic := false
		hasRRF := false
		for _, field := range added {
			if field == "search.bm25_weight" {
				hasBM25 = true
			}
			if field == "search.semantic_weight" {
				hasSemantic = true
			}
			if field == "search.rrf_constant" {
				hasRRF = true
			}
		}
		if !hasBM25 {
			t.Error("should report bm25_weight as added")
		}
		if !hasSemantic {
			t.Error("should report semantic_weight as added")
		}
		if !hasRRF {
			t.Error("should report rrf_constant as added")
		}
	})

	t.Run("adds missing thermal fields", func(t *testing.T) {
		cfg := &Config{
			Version: 1,
			Embeddings: EmbeddingsConfig{
				Provider: "ollama",
				Model:    "test-model",
				// TimeoutProgression and RetryTimeoutMultiplier are 0
			},
		}

		added := cfg.MergeNewDefaults()

		// Should add thermal fields
		if cfg.Embeddings.TimeoutProgression == 0 {
			t.Error("TimeoutProgression should be set to default")
		}
		if cfg.Embeddings.RetryTimeoutMultiplier == 0 {
			t.Error("RetryTimeoutMultiplier should be set to default")
		}

		// Should report the fields
		hasTimeout := false
		hasRetry := false
		for _, field := range added {
			if field == "embeddings.timeout_progression" {
				hasTimeout = true
			}
			if field == "embeddings.retry_timeout_multiplier" {
				hasRetry = true
			}
		}
		if !hasTimeout {
			t.Error("should report timeout_progression as added")
		}
		if !hasRetry {
			t.Error("should report retry_timeout_multiplier as added")
		}
	})

	t.Run("preserves existing values", func(t *testing.T) {
		cfg := &Config{
			Version: 1,
			Search: SearchConfig{
				BM25Weight:     0.4, // Custom value
				SemanticWeight: 0.6, // Custom value
				RRFConstant:    80,  // Custom value
			},
			Embeddings: EmbeddingsConfig{
				Provider:               "ollama",
				Model:                  "custom-model",
				TimeoutProgression:     2.5, // Custom value
				RetryTimeoutMultiplier: 1.8, // Custom value
			},
			Performance: PerformanceConfig{
				SQLiteCacheMB: 128, // Custom value
			},
		}

		added := cfg.MergeNewDefaults()

		// Should NOT change existing search values
		if cfg.Search.BM25Weight != 0.4 {
			t.Errorf("BM25Weight changed from 0.4 to %f", cfg.Search.BM25Weight)
		}
		if cfg.Search.SemanticWeight != 0.6 {
			t.Errorf("SemanticWeight changed from 0.6 to %f", cfg.Search.SemanticWeight)
		}
		if cfg.Search.RRFConstant != 80 {
			t.Errorf("RRFConstant changed from 80 to %d", cfg.Search.RRFConstant)
		}
		// Should NOT change existing embeddings values
		if cfg.Embeddings.TimeoutProgression != 2.5 {
			t.Errorf("TimeoutProgression changed from 2.5 to %f", cfg.Embeddings.TimeoutProgression)
		}
		if cfg.Embeddings.RetryTimeoutMultiplier != 1.8 {
			t.Errorf("RetryTimeoutMultiplier changed from 1.8 to %f", cfg.Embeddings.RetryTimeoutMultiplier)
		}
		if cfg.Performance.SQLiteCacheMB != 128 {
			t.Errorf("SQLiteCacheMB changed from 128 to %d", cfg.Performance.SQLiteCacheMB)
		}

		// Should NOT report them as added
		for _, field := range added {
			if field == "search.bm25_weight" ||
				field == "search.semantic_weight" ||
				field == "search.rrf_constant" ||
				field == "embeddings.timeout_progression" ||
				field == "embeddings.retry_timeout_multiplier" ||
				field == "performance.sqlite_cache_mb" {
				t.Errorf("should not report %s as added (was already set)", field)
			}
		}
	})

	t.Run("returns empty for complete config", func(t *testing.T) {
		// Create a complete config
		cfg := NewConfig()

		added := cfg.MergeNewDefaults()

		if len(added) != 0 {
			t.Errorf("expected 0 added fields for complete config, got %v", added)
		}
	})
}

func TestWriteYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg := &Config{
		Version: 1,
		Embeddings: EmbeddingsConfig{
			Provider: "ollama",
			Model:    "test-model",
		},
	}

	if err := cfg.WriteYAML(configPath); err != nil {
		t.Fatalf("failed to write YAML: %v", err)
	}

	// Verify file exists and is readable
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if len(data) == 0 {
		t.Error("written file is empty")
	}

	// Verify it contains expected content
	content := string(data)
	if !contains(content, "provider: ollama") {
		t.Error("written file should contain provider: ollama")
	}
	if !contains(content, "model: test-model") {
		t.Error("written file should contain model: test-model")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
