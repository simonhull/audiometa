package audiometa_test

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"github.com/simonhull/audiometa"
)

// Fixtures in testdata/ are produced by internal/testcorpus/generate.sh.

func TestOpen_Corpus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		path       string
		wantFormat audiometa.Format
		wantCodec  string
	}{
		{path: "testdata/sample.mp3", wantFormat: audiometa.FormatMP3},
		{path: "testdata/sample_v24.mp3", wantFormat: audiometa.FormatMP3},
		{path: "testdata/sample.flac", wantFormat: audiometa.FormatFLAC},
		{path: "testdata/sample_upper.flac", wantFormat: audiometa.FormatFLAC},
		{path: "testdata/sample.m4a", wantFormat: audiometa.FormatM4A},
		{path: "testdata/sample.m4b", wantFormat: audiometa.FormatM4A},
		{path: "testdata/sample_faststart.m4a", wantFormat: audiometa.FormatM4A},
		{path: "testdata/sample_faststart.m4b", wantFormat: audiometa.FormatM4A},
		{path: "testdata/sample.ogg", wantFormat: audiometa.FormatOgg},
		{path: "testdata/sample.opus", wantFormat: audiometa.FormatOpus},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			f, err := audiometa.Open(tc.path)
			if err != nil {
				t.Fatalf("Open(%s): %v", tc.path, err)
			}
			defer f.Close()

			if f.Format != tc.wantFormat {
				t.Errorf("Format = %v, want %v", f.Format, tc.wantFormat)
			}
			if f.Size <= 0 {
				t.Errorf("Size = %d, want > 0", f.Size)
			}
			if d := f.Audio.Duration; d < 1900*time.Millisecond || d > 2200*time.Millisecond {
				t.Errorf("Duration = %v, want about 2s", d)
			}
			if f.Audio.SampleRate == 0 {
				t.Errorf("SampleRate = 0, want non-zero")
			}

			t.Run("tags", func(t *testing.T) {
				if f.Tags.Title != "Test Title" {
					t.Errorf("Title = %q, want %q", f.Tags.Title, "Test Title")
				}
				if f.Tags.Artist != "Test Artist" {
					t.Errorf("Artist = %q, want %q", f.Tags.Artist, "Test Artist")
				}
				if f.Tags.Album != "Test Album" {
					t.Errorf("Album = %q, want %q", f.Tags.Album, "Test Album")
				}
			})
		})
	}
}

func TestOpen_Corpus_Chapters(t *testing.T) {
	t.Parallel()
	wantTitles := []string{"Chapter One", "Chapter Two"}
	cases := []struct {
		path string
	}{
		{path: "testdata/sample.mp3"},
		{path: "testdata/sample.m4b"},
		{path: "testdata/sample_faststart.m4b"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			f, err := audiometa.Open(tc.path)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer f.Close()
			if len(f.Chapters) != len(wantTitles) {
				t.Fatalf("got %d chapters, want %d: %+v", len(f.Chapters), len(wantTitles), f.Chapters)
			}
			for i, ch := range f.Chapters {
				if ch.Title != wantTitles[i] {
					t.Errorf("chapter %d title = %q, want %q", i, ch.Title, wantTitles[i])
				}
				if ch.Index != i+1 {
					t.Errorf("chapter %d index = %d, want %d", i, ch.Index, i+1)
				}
				wantStart := time.Duration(i) * time.Second
				if ch.StartTime != wantStart || ch.EndTime != wantStart+time.Second {
					t.Errorf("chapter %d span = %v-%v, want %v-%v", i, ch.StartTime, ch.EndTime, wantStart, wantStart+time.Second)
				}
			}
		})
	}
}

// Chapters are reported for the formats that carry them; chapter-less
// fixtures must report none.
func TestOpen_Corpus_NoChapters(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"testdata/sample.m4a", "testdata/sample_upper.flac"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			f, err := audiometa.Open(path)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer f.Close()
			if len(f.Chapters) != 0 {
				t.Errorf("got %d chapters, want 0", len(f.Chapters))
			}
		})
	}
}

func TestExtractArtwork_Corpus(t *testing.T) {
	t.Parallel()
	// JPEG start-of-image marker.
	jpegMagic := []byte{0xFF, 0xD8, 0xFF}
	for _, path := range []string{"testdata/sample.mp3", "testdata/sample.flac", "testdata/sample.m4b"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			f, err := audiometa.Open(path)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer f.Close()

			first, err := f.ExtractArtwork()
			if err != nil {
				t.Fatalf("ExtractArtwork: %v", err)
			}
			if len(first) != 1 {
				t.Fatalf("got %d artworks, want 1", len(first))
			}
			if !bytes.HasPrefix(first[0].Data, jpegMagic) {
				t.Errorf("artwork data does not start with JPEG magic: % x", first[0].Data[:min(4, len(first[0].Data))])
			}

			// Second call must be served from the cache: same backing array.
			second, err := f.ExtractArtwork()
			if err != nil {
				t.Fatalf("second ExtractArtwork: %v", err)
			}
			if len(second) != len(first) || &second[0] != &first[0] {
				t.Error("second ExtractArtwork did not return the cached slice")
			}
		})
	}
}

func TestExtractArtwork_NoArtwork(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"testdata/sample.m4a", "testdata/sample_upper.flac", "testdata/sample.ogg", "testdata/sample.opus"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			f, err := audiometa.Open(path)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer f.Close()
			art, err := f.ExtractArtwork()
			if err != nil {
				t.Fatalf("ExtractArtwork: %v", err)
			}
			if len(art) != 0 {
				t.Errorf("got %d artworks, want 0", len(art))
			}
		})
	}
}

// TestOpen_Corpus_MP4Strict opens every MP4-family fixture under strict
// parsing: a well-formed file must not draw a warning.
func TestOpen_Corpus_MP4Strict(t *testing.T) {
	t.Parallel()
	var paths []string
	for _, pattern := range []string{"testdata/*.m4a", "testdata/*.m4b", "testdata/*.mp4"} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, matches...)
	}
	if len(paths) == 0 {
		t.Fatal("no MP4-family fixtures found")
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			f, err := audiometa.Open(path, audiometa.WithStrictParsing())
			if err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestOpen_Corpus_LosslessMP4(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"testdata/sample_alac.m4a", "testdata/sample_flac.mp4"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			f, err := audiometa.Open(path, audiometa.WithStrictParsing())
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			a := f.Audio
			if a.SampleRate != 96000 || a.BitDepth != 24 || a.Channels != 2 || !a.Lossless || a.Container != "MP4" || !a.IsHighRes() {
				t.Errorf("audio = %+v, want 96 kHz, 24-bit, stereo, lossless, MP4", a)
			}
			if f.Tags.Title != "Test Title" {
				t.Errorf("Title = %q", f.Tags.Title)
			}
		})
	}
}
