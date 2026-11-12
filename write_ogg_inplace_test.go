package audiometa_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simonhull/audiometa"
)

// TestWriteInPlaceOgg checks that WithInPlace patches an Ogg Vorbis or Opus
// file when the edit keeps the comment header's length, and falls back to the
// atomic rewrite otherwise.
func TestWriteInPlaceOgg(t *testing.T) {
	for _, src := range []string{"testdata/sample.ogg", "testdata/sample.opus", "testdata/bench/book.opus"} {
		t.Run(filepath.Base(src), func(t *testing.T) {
			title := tagsOf(t, src).Title
			path := filepath.Join(t.TempDir(), filepath.Base(src))
			if err := os.WriteFile(path, readTestdata(t, src), 0o600); err != nil {
				t.Fatal(err)
			}
			digest := func() [32]byte { return oggAudioDigest(t, readTestdata(t, path)) }
			want := digest()

			// Stat through an open handle: on Windows only a handle stat carries
			// the file identity os.SameFile compares; a path stat loads it lazily.
			stat := func() os.FileInfo {
				h, err := os.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				defer h.Close()
				st, err := h.Stat()
				if err != nil {
					t.Fatal(err)
				}
				return st
			}
			before := stat()
			same := strings.Repeat("Q", len(title))
			if err := audiometa.Write(path, func(f *audiometa.File) error { f.Tags.Title = same; return nil }, audiometa.WithInPlace()); err != nil {
				t.Fatal(err)
			}
			after := stat()
			if !os.SameFile(before, after) || after.Size() != before.Size() {
				t.Error("an equal-length edit was not patched in place")
			}
			if got := tagsOf(t, path).Title; got != same {
				t.Errorf("title = %q, want %q", got, same)
			}
			checkOggPages(t, readTestdata(t, path))

			if err := audiometa.Write(path, func(f *audiometa.File) error { f.Tags.Title = same + " and more"; return nil }, audiometa.WithInPlace()); err != nil {
				t.Fatal(err)
			}
			if got := tagsOf(t, path).Title; got != same+" and more" {
				t.Errorf("title = %q after the longer edit", got)
			}
			checkOggPages(t, readTestdata(t, path))
			if digest() != want {
				t.Error("audio changed")
			}
		})
	}
}
