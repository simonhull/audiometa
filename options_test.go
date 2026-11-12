package audiometa_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simonhull/audiometa"
)

func hasWarning(f *audiometa.File, sub string) bool {
	for _, w := range f.Warnings {
		if w.Stage == audiometa.StageArtwork && strings.Contains(w.Message, sub) {
			return true
		}
	}
	return false
}

func TestWithMaxArtworkSize(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"testdata/sample.flac", "testdata/sample.mp3", "testdata/sample.m4b"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			f, err := audiometa.Open(path, audiometa.WithMaxArtworkSize(1))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			art, err := f.ExtractArtwork()
			if err != nil {
				t.Fatal(err)
			}
			if len(art) != 0 {
				t.Errorf("got %d images, want oversized image skipped", len(art))
			}
			if !hasWarning(f, "exceeds max size 1") {
				t.Errorf("missing size warning: %v", f.Warnings)
			}
		})
	}
}

func TestWithMaxArtworkSizeAllowsFit(t *testing.T) {
	t.Parallel()
	f, err := audiometa.Open("testdata/sample.flac", audiometa.WithMaxArtworkSize(1<<20))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	art, err := f.ExtractArtwork()
	if err != nil || len(art) != 1 {
		t.Fatalf("art=%d err=%v", len(art), err)
	}
	if hasWarning(f, "exceeds") {
		t.Error("unexpected size warning")
	}
}

func TestMaxArtworkSizeDefaultNoLimit(t *testing.T) {
	t.Parallel()
	f, err := audiometa.Open("testdata/sample.flac")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	art, _ := f.ExtractArtwork()
	if len(art) != 1 || hasWarning(f, "exceeds") {
		t.Errorf("art=%d warnings=%v", len(art), f.Warnings)
	}
}

func TestMaxArtworkSizeIgnoreWarnings(t *testing.T) {
	t.Parallel()
	f, err := audiometa.Open("testdata/sample.flac", audiometa.WithMaxArtworkSize(1), audiometa.WithIgnoreWarnings())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	art, _ := f.ExtractArtwork()
	if len(art) != 0 || len(f.Warnings) != 0 {
		t.Errorf("art=%d warnings=%v", len(art), f.Warnings)
	}
}

func TestArtworkPreloadAndCaching(t *testing.T) {
	t.Parallel()
	f, err := audiometa.Open("testdata/sample.flac", audiometa.WithArtworkPreload(), audiometa.WithMaxArtworkSize(1))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	n := len(f.Warnings)
	for range 2 {
		if art, err := f.ExtractArtwork(); err != nil || len(art) != 0 {
			t.Fatalf("art=%d err=%v", len(art), err)
		}
	}
	if len(f.Warnings) != n || n == 0 {
		t.Errorf("warnings %d -> %d; preload should warn once and cache", n, len(f.Warnings))
	}
}

func TestMaxArtworkSizeWithStrictParsing(t *testing.T) {
	t.Parallel()
	// Strict is evaluated at Open; preload + oversize must fail it.
	f, err := audiometa.Open("testdata/sample.flac", audiometa.WithArtworkPreload(), audiometa.WithMaxArtworkSize(1), audiometa.WithStrictParsing())
	if err == nil {
		_ = f.Close()
		t.Fatal("expected strict parsing error")
	}
}

// TestWarningPolicyOrder pins how strict and ignore combine: strict wins whichever
// order the options come in.
func TestWarningPolicyOrder(t *testing.T) {
	t.Parallel()

	oneWarning := []byte{'f', 'L', 'a', 'C', 0x80, 0, 0, 0} // a STREAMINFO block of length 0
	path := filepath.Join(t.TempDir(), "w.flac")
	if err := os.WriteFile(path, oneWarning, 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name         string
		opts         []audiometa.Option
		wantFail     bool
		wantWarnings bool
	}{
		{"neither", nil, false, true},
		{"strict", []audiometa.Option{audiometa.WithStrictParsing()}, true, false},
		{"ignore", []audiometa.Option{audiometa.WithIgnoreWarnings()}, false, false},
		{"strict then ignore", []audiometa.Option{audiometa.WithStrictParsing(), audiometa.WithIgnoreWarnings()}, true, false},
		{"ignore then strict", []audiometa.Option{audiometa.WithIgnoreWarnings(), audiometa.WithStrictParsing()}, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f, err := audiometa.Open(path, tt.opts...)
			if (err != nil) != tt.wantFail {
				t.Fatalf("Open err = %v, want failure %v", err, tt.wantFail)
			}
			if err != nil {
				return
			}
			defer f.Close()
			if got := len(f.Warnings) > 0; got != tt.wantWarnings {
				t.Errorf("warnings = %v, want present %v", f.Warnings, tt.wantWarnings)
			}
		})
	}
}
