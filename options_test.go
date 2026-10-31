package audiometa_test

import (
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
	for _, path := range []string{"testdata/sample.flac", "testdata/sample.mp3", "testdata/sample.m4b"} {
		t.Run(path, func(t *testing.T) {
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
	// Strict is evaluated at Open; preload + oversize must fail it.
	f, err := audiometa.Open("testdata/sample.flac", audiometa.WithArtworkPreload(), audiometa.WithMaxArtworkSize(1), audiometa.WithStrictParsing())
	if err == nil {
		_ = f.Close()
		t.Fatal("expected strict parsing error")
	}
}
