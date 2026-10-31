package audiometa

import (
	"runtime"
	"runtime/debug"
)

// version can be overridden at build time via
//
//	-ldflags "-X github.com/simonhull/audiometa.version=v1.2.3"
//
// When unset, the module version from build info is used: tagged installs
// report their real tag, working-tree builds report "(devel)".
var version = ""

// GetVersion returns the library's version string.
//
// The value is the ldflags-injected version if present, otherwise the module
// version recorded in the binary's build info, otherwise "unknown".
func GetVersion() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" {
		return bi.Main.Version
	}
	return "unknown"
}

// VersionInfo contains detailed version information.
type VersionInfo struct {
	// Version is the semantic version (e.g., "v0.10.0")
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
		Version:   GetVersion(),
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
