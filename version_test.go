package audiometa

import (
	"runtime/debug"
	"testing"
)

func TestGetVersion(t *testing.T) {
	v := GetVersion()
	if v == "" {
		t.Fatal("GetVersion() returned an empty string")
	}
	if v == "0.1.0" {
		t.Errorf("GetVersion() = %q, the stale hardcoded value", v)
	}
}

func TestGetVersionLdflagsOverride(t *testing.T) {
	old := version
	t.Cleanup(func() { version = old })
	version = "v9.9.9"

	if got := GetVersion(); got != "v9.9.9" {
		t.Errorf("GetVersion() = %q, want ldflags override v9.9.9", got)
	}
}

func TestGetVersionInfoMatchesGetVersion(t *testing.T) {
	if got, want := GetVersionInfo().Version, GetVersion(); got != want {
		t.Errorf("GetVersionInfo().Version = %q, want %q", got, want)
	}
}

func TestModuleVersion(t *testing.T) {
	tests := []struct {
		name string
		bi   *debug.BuildInfo
		want string
	}{
		{
			name: "main module",
			bi:   &debug.BuildInfo{Main: debug.Module{Path: modulePath, Version: "v0.10.0"}},
			want: "v0.10.0",
		},
		{
			name: "consuming app is main, library is a dependency",
			bi: &debug.BuildInfo{
				Main: debug.Module{Path: "example.com/app", Version: "v5.0.0"},
				Deps: []*debug.Module{
					{Path: "example.com/other", Version: "v1.0.0"},
					{Path: modulePath, Version: "v0.9.0"},
				},
			},
			want: "v0.9.0",
		},
		{
			name: "replaced dependency uses replacement version",
			bi: &debug.BuildInfo{
				Main: debug.Module{Path: "example.com/app", Version: "v5.0.0"},
				Deps: []*debug.Module{{
					Path: modulePath, Version: "v0.9.0",
					Replace: &debug.Module{Path: "example.com/fork", Version: "v0.9.1-fork"},
				}},
			},
			want: "v0.9.1-fork",
		},
		{
			name: "replaced with local path falls back to dependency version",
			bi: &debug.BuildInfo{
				Main: debug.Module{Path: "example.com/app", Version: "v5.0.0"},
				Deps: []*debug.Module{{
					Path: modulePath, Version: "v0.9.0",
					Replace: &debug.Module{Path: "../audiometa"},
				}},
			},
			want: "v0.9.0",
		},
		{
			name: "not found does not leak the app version",
			bi: &debug.BuildInfo{
				Main: debug.Module{Path: "example.com/app", Version: "v5.0.0"},
				Deps: []*debug.Module{{Path: "example.com/other", Version: "v1.0.0"}},
			},
			want: "unknown",
		},
		{name: "nil build info", bi: nil, want: "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := moduleVersion(tt.bi); got != tt.want {
				t.Errorf("moduleVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}
