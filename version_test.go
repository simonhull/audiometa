package audiometa

import "testing"

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
