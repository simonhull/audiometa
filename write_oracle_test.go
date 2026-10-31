package audiometa_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/simonhull/audiometa"
)

// oracleTool returns the path of name, or skips the test. When
// AUDIOMETA_REQUIRE_ORACLES=1 (set in CI) a missing tool fails instead.
func oracleTool(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		if os.Getenv("AUDIOMETA_REQUIRE_ORACLES") == "1" {
			t.Fatalf("oracle %s is required but not on PATH: %v", name, err)
		}
		t.Skipf("oracle %s not on PATH", name)
	}
	return path
}

// runOracle runs tool with args under a 60 s timeout and returns its stdout.
// A non-zero exit or any stderr output fails the test.
func runOracle(t *testing.T, tool string, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, tool, args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil || stderr.Len() > 0 {
		t.Fatalf("%s %s: %v\nstderr: %s", filepath.Base(tool), strings.Join(args, " "), err, stderr.String())
	}
	return stdout.Bytes()
}

// probeResult is the part of ffprobe's JSON the oracle compares.
type probeResult struct {
	Streams []struct {
		Disposition struct {
			AttachedPic int `json:"attached_pic"`
		} `json:"disposition"`
	} `json:"streams"`
	Chapters []json.RawMessage `json:"chapters"`
	Format   struct {
		Tags map[string]string `json:"tags"`
	} `json:"format"`
}

// counts returns the number of ffprobe streams that are not cover images and
// the number that are (ffprobe lists embedded artwork as a video stream).
func (p probeResult) counts() (media, pictures int) {
	for _, s := range p.Streams {
		if s.Disposition.AttachedPic == 1 {
			pictures++
		} else {
			media++
		}
	}
	return media, pictures
}

func ffprobe(t *testing.T, tool, path string) probeResult {
	t.Helper()
	out := runOracle(t, tool, "-v", "error", "-show_format", "-show_streams", "-show_chapters", "-of", "json", path)
	var res probeResult
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("decode ffprobe output for %s: %v", path, err)
	}
	return res
}

// TestWriteOracle hands edited files to ffprobe, ffmpeg and flac, which share
// no code with audiometa: the container must parse cleanly, keep its streams
// and chapters, and decode end to end.
func TestWriteOracle(t *testing.T) {
	ffprobeTool := oracleTool(t, "ffprobe")
	ffmpegTool := oracleTool(t, "ffmpeg")
	flacTool := oracleTool(t, "flac")

	const title = "Oracle title ✓"
	edits := []struct {
		name  string
		apply func(f *audiometa.File) error
		title bool // the edit sets Title
		cover bool // the edit leaves exactly one cover image
	}{
		{"title", func(f *audiometa.File) error { f.Tags.Title = title; return nil }, true, false},
		{"long comment", func(f *audiometa.File) error { f.Tags.Comment = longComment; return nil }, false, false},
		{"replace artwork", func(f *audiometa.File) error {
			return f.ReplaceArtwork(audiometa.Artwork{Data: onePixelPNG(), Type: audiometa.ArtworkFrontCover})
		}, false, true},
	}

	for _, tc := range roundTripCorpus {
		if !tc.writable {
			continue
		}
		t.Run(filepath.Base(tc.path), func(t *testing.T) {
			want := ffprobe(t, ffprobeTool, tc.path)
			for _, e := range edits {
				t.Run(e.name, func(t *testing.T) {
					f := openFile(t, tc.path)
					if err := e.apply(f); errors.Is(err, audiometa.ErrUnsupportedWrite) {
						t.Skipf("%s unsupported for %s", e.name, f.Format)
					} else if err != nil {
						t.Fatal(err)
					}
					var buf bytes.Buffer
					if _, err := f.WriteTo(&buf); errors.Is(err, audiometa.ErrUnsupportedWrite) {
						t.Skipf("%s unsupported for %s", e.name, f.Format)
					} else if err != nil {
						t.Fatalf("WriteTo: %v", err)
					}
					out := filepath.Join(t.TempDir(), filepath.Base(tc.path))
					if err := os.WriteFile(out, buf.Bytes(), 0o600); err != nil {
						t.Fatal(err)
					}

					got := ffprobe(t, ffprobeTool, out)
					media, pictures := got.counts()
					wantMedia, wantPictures := want.counts()
					if media != wantMedia {
						t.Errorf("%d media streams, want %d", media, wantMedia)
					}
					if e.cover {
						// ReplaceArtwork swaps only the front cover; other
						// pictures stay.
						wantPictures = 1
						for _, a := range extract(t, openFile(t, tc.path)) {
							if a.Type != audiometa.ArtworkFrontCover {
								wantPictures++
							}
						}
					}
					if pictures != wantPictures {
						t.Errorf("%d cover streams, want %d", pictures, wantPictures)
					}
					if len(got.Chapters) != len(want.Chapters) {
						t.Errorf("%d chapters, want %d", len(got.Chapters), len(want.Chapters))
					}
					if e.title {
						found := false
						for k, v := range got.Format.Tags {
							if strings.EqualFold(k, "title") {
								found = true
								if v != title {
									t.Errorf("ffprobe title = %q, want %q", v, title)
								}
							}
						}
						if !found {
							t.Errorf("ffprobe reports no title tag: %v", got.Format.Tags)
						}
					}

					runOracle(t, ffmpegTool, "-v", "error", "-i", out, "-f", "null", "-")
					if f.Format == audiometa.FormatFLAC {
						runOracle(t, flacTool, "-t", "-s", out)
					}
				})
			}
		})
	}
}
