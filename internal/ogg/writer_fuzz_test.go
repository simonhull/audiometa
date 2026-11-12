package ogg

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simonhull/audiometa/internal/registry"
	"github.com/simonhull/audiometa/internal/testbudget"
	"github.com/simonhull/audiometa/internal/types"
)

// FuzzWriteOgg runs the registered writer over arbitrary bytes with an edit
// that keeps the header page count and one that adds a page. A write that
// succeeds must read back, keep the audio and the preserved bytes, land the
// edit and be deterministic, and an in-place patch must give the same bytes; one
// that fails must not have written anything.
func FuzzWriteOgg(f *testing.F) {
	for _, name := range []string{"sample.ogg", "sample.opus", "sample_retagged.ogg", "sample_retagged.opus", "bench/book.opus"} {
		if data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name)); err == nil {
			f.Add(data)
		}
	}
	f.Add(syntheticOpus([]string{"TITLE=x"}, 3))
	f.Add(syntheticOpus([]string{"TITLE=x", "BIG=" + strings.Repeat("y", 70_000)}, 3))
	good, err := os.ReadFile(filepath.Join("..", "..", "testdata", "sample.opus"))
	if err == nil {
		f.Add(append(bytes.Clone(good), "trailing tag"...))
		f.Add(good[:len(good)-7])
	}

	small := keyEdit("TITLE", "Fuzz")
	big := keyEdit("BIG", strings.Repeat("z", 70_000))
	f.Fuzz(func(t *testing.T, data []byte) {
		ctx := context.Background()
		file, err := (&parser{}).Parse(ctx, bytes.NewReader(data), int64(len(data)), "fuzz")
		if err != nil {
			return
		}
		w, ok := registry.GetWriter(file.Format).(*writer)
		if !ok {
			return
		}
		for _, edits := range []*types.EditSet{small, big} {
			var out bytes.Buffer
			var werr error
			testbudget.Check(t, testbudget.For(testbudget.Write, file.Format), len(data), "WriteFile", func() {
				_, werr = w.WriteFile(ctx, &out, bytes.NewReader(data), int64(len(data)), edits)
			})
			if werr != nil {
				if out.Len() != 0 {
					t.Fatalf("a failed write put %d bytes out: %v", out.Len(), werr)
				}
				continue
			}
			checkFuzzOutput(t, w, data, out.Bytes(), edits)
		}
	})
}

// checkFuzzOutput fails t when out, the successful rewrite of data with
// edits, breaks a promise of the writer.
func checkFuzzOutput(t *testing.T, w *writer, data, out []byte, edits *types.EditSet) {
	t.Helper()
	ctx := context.Background()
	got, err := (&parser{}).Parse(ctx, bytes.NewReader(out), int64(len(out)), "fuzz")
	if err != nil {
		t.Fatalf("output does not re-open: %v", err)
	}
	key := edits.Keys[0]
	if v := got.Tags.Get(strings.ToUpper(key.Key)); len(v) == 0 || v[len(v)-1] != key.Values[0] {
		t.Fatalf("edit %s did not land: %q", key.Key, v)
	}
	for what, f := range map[string]func(context.Context, io.ReaderAt, int64) ([]byte, error){
		"AudioDigest": w.AudioDigest, "PreservedDigest": w.PreservedDigest,
	} {
		a, errA := f(ctx, bytes.NewReader(data), int64(len(data)))
		b, errB := f(ctx, bytes.NewReader(out), int64(len(out)))
		if errA != nil || errB != nil || !bytes.Equal(a, b) {
			t.Fatalf("%s: %v, %v, equal %v", what, errA, errB, bytes.Equal(a, b))
		}
	}
	if p, err := w.InPlacePatch(ctx, bytes.NewReader(data), int64(len(data)), edits); err != nil {
		t.Fatalf("InPlacePatch failed after WriteFile succeeded: %v", err)
	} else if p != nil {
		if p.Off < 0 || p.Off+int64(len(p.Data)) > int64(len(data)) {
			t.Fatalf("patch of %d bytes at %d does not fit in %d bytes", len(p.Data), p.Off, len(data))
		}
		patched := bytes.Clone(data)
		copy(patched[p.Off:], p.Data)
		if !bytes.Equal(patched, out) {
			t.Fatal("patching in place differs from the full rewrite")
		}
	}
	var again bytes.Buffer
	if _, err := w.WriteFile(ctx, &again, bytes.NewReader(data), int64(len(data)), edits); err != nil || !bytes.Equal(again.Bytes(), out) {
		t.Fatalf("second write: %v, identical %v", err, bytes.Equal(again.Bytes(), out))
	}
}
