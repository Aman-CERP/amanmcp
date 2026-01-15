package version

import (
	"encoding/json"
	"regexp"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersion_IsNotEmpty(t *testing.T) {
	// Given: the version package is imported

	// When: accessing Version

	// Then: it should not be empty
	assert.NotEmpty(t, Version, "Version should not be empty")
}

func TestVersion_FollowsSemverOrDev(t *testing.T) {
	// Given: the version package is imported

	// When: accessing Version

	// Then: it should follow semver format (X.Y.Z or X.Y.Z-suffix) or be "dev" for development builds
	// BUG-006 fix: default is now "dev", ldflags inject actual version at build time
	if Version == "dev" {
		t.Log("Version is 'dev' (development build without ldflags)")
		return // Valid for development builds
	}
	semverRegex := regexp.MustCompile(`^\d+\.\d+\.\d+(-[a-zA-Z0-9.]+)?$`)
	require.True(t, semverRegex.MatchString(Version), "Version should follow semver format, got: %s", Version)
}

func TestBuildInfo_AllFieldsExist(t *testing.T) {
	// Given: the version package is imported

	// When: accessing build info fields

	// Then: they should be defined (may be empty at build time)
	// These are set via ldflags at build time
	assert.NotNil(t, &Commit)
	assert.NotNil(t, &Date)
}

func TestString_ReturnsFormattedString(t *testing.T) {
	// Given: the version package is imported

	// When: calling String()

	// Then: it should return a formatted version string with all info
	str := String()
	assert.Contains(t, str, Version, "String should contain version")
	assert.Contains(t, str, "amanmcp", "String should contain program name")
	assert.Contains(t, str, "commit", "String should contain commit info")
	assert.Contains(t, str, "go", "String should contain Go version")
}

func TestShort_ReturnsVersion(t *testing.T) {
	// Given: the version package is imported

	// When: calling Short()

	// Then: it should return just the version string
	short := Short()
	assert.Equal(t, Version, short, "Short() should return Version")
}

func TestGetInfo_ReturnsInfo(t *testing.T) {
	// Given: the version package is imported

	// When: calling GetInfo()

	// Then: it should return an Info struct with all fields
	info := GetInfo()

	assert.Equal(t, Version, info.Version, "Info.Version should match Version")
	assert.Equal(t, Commit, info.Commit, "Info.Commit should match Commit")
	assert.Equal(t, Date, info.Date, "Info.Date should match Date")
	assert.Equal(t, runtime.Version(), info.GoVersion, "Info.GoVersion should match runtime.Version()")
	assert.Equal(t, runtime.GOOS, info.OS, "Info.OS should match runtime.GOOS")
	assert.Equal(t, runtime.GOARCH, info.Arch, "Info.Arch should match runtime.GOARCH")
}

func TestGetInfo_IsJSONSerializable(t *testing.T) {
	// Given: the version package is imported

	// When: serializing GetInfo() to JSON

	// Then: it should produce valid JSON with expected fields
	info := GetInfo()
	data, err := json.Marshal(info)
	require.NoError(t, err, "GetInfo() should be JSON serializable")

	// Parse back and verify
	var parsed map[string]string
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err, "JSON should be parseable")

	assert.Contains(t, parsed, "version", "JSON should contain version field")
	assert.Contains(t, parsed, "commit", "JSON should contain commit field")
	assert.Contains(t, parsed, "date", "JSON should contain date field")
	assert.Contains(t, parsed, "go_version", "JSON should contain go_version field")
	assert.Contains(t, parsed, "os", "JSON should contain os field")
	assert.Contains(t, parsed, "arch", "JSON should contain arch field")
}

// Compatibility tests for existing code
func TestInfo_ReturnsFormattedString(t *testing.T) {
	// Given: the version package is imported

	// When: calling Info()

	// Then: it should return a formatted version string (backward compat)
	info := Info()
	assert.Contains(t, info, Version, "Info should contain version")
	assert.Contains(t, info, "amanmcp", "Info should contain program name")
}
