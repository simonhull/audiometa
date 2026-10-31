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

// unknownVersion is reported when no version can be determined.
const unknownVersion = "unknown"

// modulePath is this library's module path, used to find it in build info.
const modulePath = "github.com/simonhull/audiometa"

// Version is the library version at program start.
//
// Deprecated: use GetVersion. Version was a hand-maintained constant that fell
// out of date; it is now derived from build info and kept only so existing
// callers keep compiling.
var Version = GetVersion()

// GetVersion returns the library's version string.
//
// The value is the ldflags-injected version if present, otherwise the module
// version recorded in the binary's build info, otherwise "unknown".
func GetVersion() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		return moduleVersion(bi)
	}
	return unknownVersion
}

// moduleVersion finds this library's version in bi. If the library is the main
// module its version is used; otherwise it is looked up in the dependencies,
// preferring a replacement's version when one is set. It returns "unknown"
// when the module cannot be found.
func moduleVersion(bi *debug.BuildInfo) string {
	if bi == nil {
		return unknownVersion
	}
	if bi.Main.Path == modulePath && bi.Main.Version != "" {
		return bi.Main.Version
	}
	for _, dep := range bi.Deps {
		if dep == nil || dep.Path != modulePath {
			continue
		}
		if dep.Replace != nil && dep.Replace.Version != "" {
			return dep.Replace.Version
		}
		if dep.Version != "" {
			return dep.Version
		}
	}
	return unknownVersion
}

// VersionInfo contains detailed version information.
type VersionInfo struct {
	// Version is the library version, as reported by GetVersion (e.g. "v0.10.0").
	Version string
	// GitCommit is the git commit hash, set via -ldflags at build time.
	GitCommit string
	// BuildTime is the build timestamp, set via -ldflags at build time.
	BuildTime string
	// GoVersion is the Go version used to build.
	GoVersion string
}

// GetVersionInfo returns detailed version information.
//
// Version comes from [GetVersion]. GitCommit and BuildTime are set at build
// time via -ldflags and are "unknown" otherwise. GoVersion is set the same
// way and falls back to [runtime.Version].
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
