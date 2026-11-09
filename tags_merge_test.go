package audiometa_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/simonhull/audiometa"
)

// TestTagsMerge_ThenWrite merges raw keys into an opened file's tags and
// writes them: the merge must not make the write conflict, and t's values win.
func TestTagsMerge_ThenWrite(t *testing.T) {
	for path, titleKey := range map[string]string{
		"testdata/sample.flac": "TITLE",
		"testdata/sample.mp3":  "TIT2",
		"testdata/sample.m4a":  "\xa9nam",
	} {
		t.Run(path, func(t *testing.T) {
			f, err := audiometa.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			before := f.Tags.Title
			other := &audiometa.Tags{}
			other.Set(titleKey, "Other title")
			other.Set("ZZMERGED", "x")
			f.Tags.Merge(other)

			var out bytes.Buffer
			if _, err := f.WriteTo(&out); err != nil {
				t.Fatalf("WriteTo after Merge: %v", err)
			}
			// The root package has no public open-from-bytes; go through a temp file.
			written := filepath.Join(t.TempDir(), filepath.Base(path))
			if err := os.WriteFile(written, out.Bytes(), 0o600); err != nil {
				t.Fatal(err)
			}
			g, err := audiometa.Open(written)
			if err != nil {
				t.Fatalf("re-open: %v", err)
			}
			defer g.Close()
			if g.Tags.Title != before {
				t.Errorf("Title = %q, want %q (t's value must be kept)", g.Tags.Title, before)
			}
			if got := g.Tags.GetFirst("ZZMERGED"); got != "x" {
				t.Errorf("ZZMERGED = %q, want x", got)
			}
		})
	}
}
