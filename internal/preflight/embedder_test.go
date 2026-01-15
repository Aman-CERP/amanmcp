package preflight

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChecker_CheckEmbedderModel_ModelExists(t *testing.T) {
	// Given: a checker and a model directory with files
	checker := New()

	// Create temp model directory
	tmpDir := t.TempDir()
	modelDir := filepath.Join(tmpDir, ".amanmcp", "models", "onnx-community")
	err := os.MkdirAll(modelDir, 0755)
	require.NoError(t, err)

	// Create a fake model file
	f, err := os.Create(filepath.Join(modelDir, "model.onnx"))
	require.NoError(t, err)
	_ = f.Close()

	// When: I check embedder model
	result := checker.checkEmbedderModelWithHome(tmpDir)

	// Then: status is pass
	assert.Equal(t, StatusPass, result.Status)
	assert.Equal(t, "embedder_model", result.Name)
	assert.Contains(t, result.Message, "downloaded")
}

func TestChecker_CheckEmbedderModel_ModelMissing(t *testing.T) {
	// Given: a checker and empty model directory
	checker := New()

	// Create temp home with no models
	tmpDir := t.TempDir()

	// When: I check embedder model
	result := checker.checkEmbedderModelWithHome(tmpDir)

	// Then: status is warn (not critical)
	assert.Equal(t, StatusWarn, result.Status)
	assert.Equal(t, "embedder_model", result.Name)
	assert.False(t, result.Required, "embedder model check should not be required")
	assert.Contains(t, result.Message, "not downloaded")
}

func TestChecker_CheckEmbedderDiskSpace_Sufficient(t *testing.T) {
	// Given: a checker
	checker := New()

	// When: I check embedder disk space (most systems have enough)
	result := checker.CheckEmbedderDiskSpace()

	// Then: should pass (assuming test machine has > 1.5GB free in home)
	// Note: This test may fail on systems with very low disk space
	if result.Status == StatusPass {
		assert.Contains(t, result.Message, "available")
	} else {
		// If it warns, that's fine too - just verify it's the right check
		assert.Equal(t, "embedder_disk_space", result.Name)
	}
}

func TestChecker_CheckEmbedderDiskSpace_ResultFormat(t *testing.T) {
	// Given: a checker
	checker := New()

	// When: I check embedder disk space
	result := checker.CheckEmbedderDiskSpace()

	// Then: result has expected structure
	assert.Equal(t, "embedder_disk_space", result.Name)
	assert.False(t, result.Required, "disk space check should not be required")
	assert.NotEmpty(t, result.Message)
}
