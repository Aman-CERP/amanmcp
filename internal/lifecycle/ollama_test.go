package lifecycle

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Test helpers

// mockServer creates a test server with configurable responses
func mockServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(handler)
}

// newTestManager creates a manager for testing with custom host
func newTestManager(t *testing.T, host string) *OllamaManager {
	t.Helper()
	m := NewOllamaManagerWithHost(host)
	return m
}

// ============================================================================
// IsInstalled Tests
// ============================================================================

func TestIsInstalled_OllamaCLI(t *testing.T) {
	m := NewOllamaManager()

	// Override lookPath to simulate ollama in PATH
	m.lookPath = func(file string) (string, error) {
		if file == "ollama" {
			return "/usr/local/bin/ollama", nil
		}
		return "", exec.ErrNotFound
	}

	installed, path, err := m.IsInstalled()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !installed {
		t.Error("expected installed to be true")
	}
	if path != "/usr/local/bin/ollama" {
		t.Errorf("expected path /usr/local/bin/ollama, got %s", path)
	}
}

func TestIsInstalled_NotFound(t *testing.T) {
	m := NewOllamaManager()

	// Override to simulate nothing found
	m.lookPath = func(file string) (string, error) {
		return "", exec.ErrNotFound
	}
	m.fileExists = func(path string) bool {
		return false
	}

	installed, path, err := m.IsInstalled()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if installed {
		t.Error("expected installed to be false")
	}
	if path != "" {
		t.Errorf("expected empty path, got %s", path)
	}
}

// ============================================================================
// IsRunning Tests
// ============================================================================

func TestIsRunning_OllamaUp(t *testing.T) {
	server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"models":[]}`))
		}
	})
	defer server.Close()

	m := newTestManager(t, server.URL)
	running, err := m.IsRunning()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !running {
		t.Error("expected running to be true")
	}
}

func TestIsRunning_OllamaDown(t *testing.T) {
	// Use invalid host to simulate connection refused
	m := newTestManager(t, "http://localhost:1")
	running, err := m.IsRunning()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if running {
		t.Error("expected running to be false")
	}
}

// ============================================================================
// HasModel Tests
// ============================================================================

func TestHasModel_Found(t *testing.T) {
	server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			resp := map[string]interface{}{
				"models": []map[string]interface{}{
					{"name": "qwen3-embedding:0.6b"},
					{"name": "embeddinggemma:latest"},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		}
	})
	defer server.Close()

	m := newTestManager(t, server.URL)
	ctx := context.Background()

	// Exact match
	hasModel, err := m.HasModel(ctx, "qwen3-embedding:0.6b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasModel {
		t.Error("expected hasModel to be true for exact match")
	}

	// Base name match
	hasModel, err = m.HasModel(ctx, "embeddinggemma")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasModel {
		t.Error("expected hasModel to be true for base name match")
	}
}

func TestHasModel_NotFound(t *testing.T) {
	server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			resp := map[string]interface{}{
				"models": []map[string]interface{}{
					{"name": "llama2:7b"},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		}
	})
	defer server.Close()

	m := newTestManager(t, server.URL)
	ctx := context.Background()

	hasModel, err := m.HasModel(ctx, "qwen3-embedding:0.6b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasModel {
		t.Error("expected hasModel to be false")
	}
}

// ============================================================================
// ListModels Tests
// ============================================================================

func TestListModels_Success(t *testing.T) {
	server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			resp := map[string]interface{}{
				"models": []map[string]interface{}{
					{"name": "model1"},
					{"name": "model2"},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		}
	})
	defer server.Close()

	m := newTestManager(t, server.URL)
	models, err := m.ListModels(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 2 {
		t.Errorf("expected 2 models, got %d", len(models))
	}
	if models[0] != "model1" || models[1] != "model2" {
		t.Errorf("unexpected models: %v", models)
	}
}

// ============================================================================
// Status Tests
// ============================================================================

func TestStatus_FullStatus(t *testing.T) {
	server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			resp := map[string]interface{}{
				"models": []map[string]interface{}{
					{"name": "qwen3-embedding:0.6b"},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		}
	})
	defer server.Close()

	m := newTestManager(t, server.URL)
	m.lookPath = func(file string) (string, error) {
		return "/usr/local/bin/ollama", nil
	}

	status, err := m.Status(context.Background(), "qwen3-embedding:0.6b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !status.Installed {
		t.Error("expected Installed to be true")
	}
	if !status.Running {
		t.Error("expected Running to be true")
	}
	if !status.HasModel {
		t.Error("expected HasModel to be true")
	}
	if status.TargetModel != "qwen3-embedding:0.6b" {
		t.Errorf("expected TargetModel qwen3-embedding:0.6b, got %s", status.TargetModel)
	}
}

// ============================================================================
// WaitForReady Tests
// ============================================================================

func TestWaitForReady_AlreadyRunning(t *testing.T) {
	server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"models":[]}`))
	})
	defer server.Close()

	m := newTestManager(t, server.URL)
	ctx := context.Background()

	err := m.WaitForReady(ctx, time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWaitForReady_BecomesReady(t *testing.T) {
	callCount := 0
	server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount < 3 {
			// First 2 calls fail
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		// 3rd call succeeds
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"models":[]}`))
	})
	defer server.Close()

	m := newTestManager(t, server.URL)
	ctx := context.Background()

	err := m.WaitForReady(ctx, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount < 3 {
		t.Errorf("expected at least 3 calls, got %d", callCount)
	}
}

func TestWaitForReady_Timeout(t *testing.T) {
	server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Always fail
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	defer server.Close()

	m := newTestManager(t, server.URL)
	ctx := context.Background()

	err := m.WaitForReady(ctx, 500*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("expected timeout error, got: %v", err)
	}
}

// ============================================================================
// PullModel Tests
// ============================================================================

func TestPullModel_AlreadyExists(t *testing.T) {
	server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			resp := map[string]interface{}{
				"models": []map[string]interface{}{
					{"name": "qwen3-embedding:0.6b"},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		}
	})
	defer server.Close()

	m := newTestManager(t, server.URL)
	ctx := context.Background()

	err := m.PullModel(ctx, "qwen3-embedding:0.6b", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPullModel_Success(t *testing.T) {
	server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			// Model not found
			resp := map[string]interface{}{"models": []map[string]interface{}{}}
			_ = json.NewEncoder(w).Encode(resp)
		case "/api/pull":
			// Simulate streaming response
			w.WriteHeader(http.StatusOK)
			flusher, ok := w.(http.Flusher)
			if ok {
				_, _ = w.Write([]byte(`{"status":"pulling"}`))
				flusher.Flush()
				_, _ = w.Write([]byte("\n"))
				_, _ = w.Write([]byte(`{"status":"downloading","total":1000,"completed":500}`))
				flusher.Flush()
				_, _ = w.Write([]byte("\n"))
				_, _ = w.Write([]byte(`{"status":"success","total":1000,"completed":1000}`))
			}
		}
	})
	defer server.Close()

	m := newTestManager(t, server.URL)
	ctx := context.Background()

	progressCalled := false
	err := m.PullModel(ctx, "test-model", func(p PullProgress) {
		progressCalled = true
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !progressCalled {
		t.Error("expected progress callback to be called")
	}
}

// ============================================================================
// EnsureReady Tests
// ============================================================================

func TestEnsureReady_AlreadyReady(t *testing.T) {
	server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			resp := map[string]interface{}{
				"models": []map[string]interface{}{
					{"name": "qwen3-embedding:0.6b"},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		}
	})
	defer server.Close()

	m := newTestManager(t, server.URL)
	m.lookPath = func(file string) (string, error) {
		return "/usr/local/bin/ollama", nil
	}

	ctx := context.Background()
	opts := DefaultEnsureOpts()
	// Silence output for tests
	opts.Stdout = &strings.Builder{}
	opts.Stderr = &strings.Builder{}

	err := m.EnsureReady(ctx, "qwen3-embedding:0.6b", opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureReady_NotInstalled(t *testing.T) {
	m := NewOllamaManager()
	m.lookPath = func(file string) (string, error) {
		return "", exec.ErrNotFound
	}
	m.fileExists = func(path string) bool {
		return false
	}

	ctx := context.Background()
	opts := DefaultEnsureOpts()

	err := m.EnsureReady(ctx, "qwen3-embedding:0.6b", opts)
	if err == nil {
		t.Fatal("expected error for not installed")
	}

	var notInstalled *NotInstalledError
	if _, ok := err.(*NotInstalledError); !ok {
		t.Errorf("expected NotInstalledError, got %T: %v", err, notInstalled)
	}
}

func TestEnsureReady_NotRunning_NoAutoStart(t *testing.T) {
	// Create a server that returns 503 to simulate Ollama not ready
	server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	defer server.Close()

	m := newTestManager(t, server.URL)
	m.lookPath = func(file string) (string, error) {
		return "/usr/local/bin/ollama", nil
	}

	ctx := context.Background()
	opts := DefaultEnsureOpts()
	opts.AutoStart = false
	opts.Stdout = &strings.Builder{}
	opts.Stderr = &strings.Builder{}

	err := m.EnsureReady(ctx, "qwen3-embedding:0.6b", opts)
	if err == nil {
		t.Fatal("expected error for not running")
	}

	if _, ok := err.(*NotRunningError); !ok {
		t.Errorf("expected NotRunningError, got %T: %v", err, err)
	}
}

// ============================================================================
// Error Type Tests
// ============================================================================

func TestNotInstalledError(t *testing.T) {
	err := &NotInstalledError{}
	if err.Error() != "ollama is not installed" {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

func TestNotRunningError(t *testing.T) {
	err := &NotRunningError{}
	if err.Error() != "ollama is not running" {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

func TestModelNotFoundError(t *testing.T) {
	err := &ModelNotFoundError{Model: "test-model"}
	expected := "model test-model not found"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

// ============================================================================
// InstallInstructions Tests
// ============================================================================

func TestInstallInstructions(t *testing.T) {
	instructions := InstallInstructions()
	if instructions == "" {
		t.Error("expected non-empty instructions")
	}
	if !strings.Contains(instructions, "ollama.com") {
		t.Error("expected instructions to contain ollama.com")
	}
}

// ============================================================================
// IsRemoteHost Tests
// ============================================================================

func TestIsRemoteHost(t *testing.T) {
	tests := []struct {
		host   string
		remote bool
	}{
		{"http://localhost:11434", false},
		{"http://127.0.0.1:11434", false},
		{"http://ollama.example.com:11434", true},
		{"http://192.168.1.100:11434", true},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			m := NewOllamaManagerWithHost(tt.host)
			if m.IsRemoteHost() != tt.remote {
				t.Errorf("IsRemoteHost() = %v, want %v", m.IsRemoteHost(), tt.remote)
			}
		})
	}
}

// ============================================================================
// Host Tests
// ============================================================================

func TestHost_Default(t *testing.T) {
	m := NewOllamaManager()
	if m.Host() != DefaultHost {
		t.Errorf("expected %s, got %s", DefaultHost, m.Host())
	}
}

func TestHost_Custom(t *testing.T) {
	m := NewOllamaManagerWithHost("http://custom:1234")
	if m.Host() != "http://custom:1234" {
		t.Errorf("expected http://custom:1234, got %s", m.Host())
	}
}
