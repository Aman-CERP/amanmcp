// Package version provides build and version information for AmanMCP.
package version

import (
	"fmt"
	"runtime"
)

// Version is the current version of AmanMCP.
// Set via ldflags at build time, or defaults to dev.
// GoReleaser sets: -X github.com/Aman-CERP/amanmcp/pkg/version.Version={{.Version}}
// Makefile sets: -X github.com/Aman-CERP/amanmcp/pkg/version.Version=$(VERSION) from VERSION file
var Version = "dev"

// Build information set via ldflags at build time.
// GoReleaser sets these via ldflags.
var (
	// Commit is the git commit hash.
	// GoReleaser sets: -X github.com/Aman-CERP/amanmcp/pkg/version.Commit={{.ShortCommit}}
	Commit = "unknown"

	// Date is the build date in RFC3339 format.
	// GoReleaser sets: -X github.com/Aman-CERP/amanmcp/pkg/version.Date={{.Date}}
	Date = "unknown"

	// GoVersion is the Go version used to build the binary (set at runtime).
	GoVersion = runtime.Version()
)

// BuildInfo is structured version information for JSON output.
type BuildInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Date      string `json:"date"`
	GoVersion string `json:"go_version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

// String returns a formatted version string with all build info.
func String() string {
	return fmt.Sprintf("amanmcp %s (commit: %s, built: %s, go: %s)",
		Version, Commit, Date, GoVersion)
}

// Short returns just the version string.
func Short() string {
	return Version
}

// GetInfo returns structured version information.
func GetInfo() BuildInfo {
	return BuildInfo{
		Version:   Version,
		Commit:    Commit,
		Date:      Date,
		GoVersion: GoVersion,
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}
}

// Info returns a formatted version string suitable for display.
// Deprecated: Use String() for full info or Short() for version only.
func Info() string {
	return fmt.Sprintf("amanmcp version %s", Version)
}

// Full returns complete version and build information.
// Deprecated: Use String() instead.
func Full() string {
	return fmt.Sprintf(
		"amanmcp version %s\n  git commit: %s\n  build time: %s\n  go version: %s\n  platform: %s/%s",
		Version,
		Commit,
		Date,
		runtime.Version(),
		runtime.GOOS,
		runtime.GOARCH,
	)
}
