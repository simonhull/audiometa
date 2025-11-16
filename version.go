package audiometa

import "runtime"

// Version is the semantic version of the audiometa library.
const Version = "0.1.0"

// GetVersion returns the current version string.
func GetVersion() string {
	return Version
}

// VersionInfo contains detailed version information.
type VersionInfo struct {
	// Version is the semantic version (e.g., "0.1.0")
	Version string
	// GitCommit is the git commit hash (set via ldflags at build time)
	GitCommit string
	// BuildTime is the build timestamp (set via ldflags at build time)
	BuildTime string
	// GoVersion is the Go version used to build
	GoVersion string
}

// GetVersionInfo returns detailed version information
//
// GitCommit, BuildTime, and GoVersion are populated at build time via -ldflags.
// If not set, they will show as "unknown".
//
// Example build command:
//
//	go build -ldflags="-X github.com/simonhull/audiometa.gitCommit=$(git rev-parse HEAD) \
//	  -X github.com/simonhull/audiometa.buildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
//	  -X github.com/simonhull/audiometa.goVersion=$(go version | awk '{print $3}')"
func GetVersionInfo() VersionInfo {
	goVer := goVersion
	if goVer == "unknown" {
		// Fallback to runtime if not set via ldflags
		goVer = runtime.Version()
	}

	return VersionInfo{
		Version:   Version,
		GitCommit: gitCommit,
		BuildTime: buildTime,
		GoVersion: goVer,
	}
}

// Variables populated at build time via -ldflags.
var (
	gitCommit = "unknown"
	buildTime = "unknown"
	goVersion = "unknown"
)
