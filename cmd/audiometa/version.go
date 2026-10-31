package main

import (
	"fmt"
	"io"

	"github.com/simonhull/audiometa"
)

type versionDTO struct {
	Version   string `json:"version"`
	GitCommit string `json:"git_commit"`
	BuildTime string `json:"build_time"`
	GoVersion string `json:"go_version"`
}

// runVersion implements "audiometa version".
func runVersion(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("version", "[--json]", stderr)
	asJSON := fs.Bool("json", false, "emit version information as JSON")
	if code, ok := parse(fs, args); !ok {
		return code
	}
	vi := audiometa.GetVersionInfo()
	out := &printer{w: stdout}
	if *asJSON {
		if err := writeJSONLine(out, versionDTO(vi)); err != nil {
			fmt.Fprintf(stderr, "audiometa version: %v\n", err)
			return exitFail
		}
		return exitOK
	}
	out.printf("audiometa %s (commit %s, built %s, %s)\n", vi.Version, vi.GitCommit, vi.BuildTime, vi.GoVersion)
	if out.err != nil {
		return exitFail
	}
	return exitOK
}
