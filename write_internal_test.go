package audiometa

// Tests in this file swap the package-level lookupWriter, so none of them may
// call t.Parallel.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/simonhull/audiometa/internal/registry"
	"github.com/simonhull/audiometa/internal/types"
	"github.com/simonhull/audiometa/internal/writeutil"
)

const sampleFLAC = "testdata/sample.flac"

// fakeTrailer is appended by fakeWriter, so its output still parses as the
// source format and is recognizably different from the source.
var fakeTrailer = []byte("FAKE")

// fakeWriter is a TagWriter that records the EditSet it got and writes the
// source bytes followed by fakeTrailer.
type fakeWriter struct {
	mu    sync.Mutex
	calls int
	got   *types.EditSet

	digestFn func(size int64) []byte // nil: a fixed digest, whatever the input
	onWrite  func()                  // side effect while WriteFile runs
	panics   bool
}

func (w *fakeWriter) WriteFile(ctx context.Context, out io.Writer, src io.ReaderAt, size int64, edits *types.EditSet) (int64, error) {
	w.mu.Lock()
	w.calls++
	w.got = edits
	w.mu.Unlock()
	if w.panics {
		panic("fake writer exploded")
	}
	if w.onWrite != nil {
		w.onWrite()
	}
	n, err := writeutil.CopyRange(ctx, out, src, 0, size)
	if err != nil {
		return n, err
	}
	m, err := out.Write(fakeTrailer)
	return n + int64(m), err
}

func (w *fakeWriter) AudioDigest(_ context.Context, _ io.ReaderAt, size int64) ([]byte, error) {
	if w.digestFn != nil {
		return w.digestFn(size), nil
	}
	sum := sha256.Sum256([]byte("fixed audio digest"))
	return sum[:], nil
}

func (w *fakeWriter) callCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.calls
}

// inPlaceFake is a fakeWriter that can also patch in place.
type inPlaceFake struct {
	fakeWriter
	off   int64
	patch []byte
	fits  bool
}

func (w *inPlaceFake) InPlacePatch(context.Context, io.ReaderAt, int64, *types.EditSet) (int64, []byte, bool, error) {
	return w.off, w.patch, w.fits, nil
}

// useWriter makes lookupWriter return tw for every format until the test ends.
func useWriter(t *testing.T, tw registry.TagWriter) {
	t.Helper()
	prev := lookupWriter
	lookupWriter = func(Format) registry.TagWriter { return tw }
	t.Cleanup(func() { lookupWriter = prev })
}

// copySample copies a testdata file into a temp dir and returns its path and bytes.
func copySample(t *testing.T, src string) (string, []byte) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	return writeTemp(t, filepath.Base(src), data), data
}

func openSample(t *testing.T, path string) *File {
	t.Helper()
	f, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func assertFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s: %d bytes, want %d identical bytes", path, len(got), len(want))
	}
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(dir, ".*.audiometa-*.tmp"))
	if len(matches) > 0 {
		t.Errorf("temp files left: %v", matches)
	}
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestWriteTo_NoWriterIsUnsupported(t *testing.T) {
	f := openSample(t, sampleFLAC)
	n, err := f.WriteTo(io.Discard)
	if !errors.Is(err, ErrUnsupportedWrite) || n != 0 {
		t.Errorf("WriteTo = %d, %v; want 0, ErrUnsupportedWrite", n, err)
	}

	path, data := copySample(t, sampleFLAC)
	err = Write(path, func(f *File) error { f.Tags.Title = "x"; return nil })
	if !errors.Is(err, ErrUnsupportedWrite) {
		t.Errorf("Write err = %v, want ErrUnsupportedWrite", err)
	}
	assertFileBytes(t, path, data)
}

func TestWriteTo_NoEditsCopiesVerbatim(t *testing.T) {
	fw := &fakeWriter{}
	useWriter(t, fw)
	f := openSample(t, sampleFLAC)
	src, err := os.ReadFile(sampleFLAC)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	n, err := f.WriteTo(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(len(src)) || !bytes.Equal(buf.Bytes(), src) {
		t.Errorf("no-edit WriteTo is not a verbatim copy (%d bytes)", n)
	}
	if fw.callCount() != 0 {
		t.Errorf("writer called %d times for an empty edit set", fw.callCount())
	}
}

func TestWriteTo_EditSets(t *testing.T) {
	tests := []struct {
		name       string
		edit       func(f *File)
		wantKeys   []types.KeyEdit
		wantFields []types.FieldEdit
	}{
		{"typed field", func(f *File) { f.Tags.Title = "New" },
			nil, []types.FieldEdit{{Field: types.FieldTitle, Values: []string{"New"}}}},
		{"raw key", func(f *File) { f.Tags.Set("TITLE", "New") },
			[]types.KeyEdit{{Key: "TITLE", Values: []string{"New"}}}, nil},
		{"delete key", func(f *File) { f.Tags.Delete("TITLE") },
			[]types.KeyEdit{{Key: "TITLE", Values: nil}}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fw := &fakeWriter{}
			useWriter(t, fw)
			f := openSample(t, sampleFLAC)
			tc.edit(f)
			if _, err := f.WriteTo(io.Discard); err != nil {
				t.Fatal(err)
			}
			if !slices.EqualFunc(fw.got.Keys, tc.wantKeys, func(a, b types.KeyEdit) bool {
				return a.Key == b.Key && slices.Equal(a.Values, b.Values)
			}) {
				t.Errorf("Keys = %+v, want %+v", fw.got.Keys, tc.wantKeys)
			}
			if !slices.EqualFunc(fw.got.Fields, tc.wantFields, func(a, b types.FieldEdit) bool {
				return a.Field == b.Field && slices.Equal(a.Values, b.Values)
			}) {
				t.Errorf("Fields = %+v, want %+v", fw.got.Fields, tc.wantFields)
			}
			if fw.got.Orig == nil || fw.got.Orig.Title != "Test Title" || fw.got.Final != &f.Tags {
				t.Errorf("EditSet context: Orig %+v, Final %p (want %p)", fw.got.Orig, fw.got.Final, &f.Tags)
			}
		})
	}
}

func TestWriteTo_LeavesFileUnchanged(t *testing.T) {
	useWriter(t, &fakeWriter{})
	f := openSample(t, sampleFLAC)
	f.Tags.Title = "New"
	if err := f.AddArtwork(Artwork{Data: tinyPNG(t), Type: ArtworkFrontCover}); err != nil {
		t.Fatal(err)
	}
	var first, second bytes.Buffer
	if _, err := f.WriteTo(&first); err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteTo(&second); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Error("two WriteTo calls wrote different bytes")
	}
	if f.Tags.Title != "New" || len(f.pendingArtwork()) != 1 {
		t.Error("WriteTo changed the File's pending edits")
	}
}

func TestWriteTo_ConflictingEdits(t *testing.T) {
	useWriter(t, &fakeWriter{})
	f := openSample(t, sampleFLAC)
	f.Tags.Set("TITLE", "A")
	f.Tags.Title = "B"
	if _, err := f.WriteTo(io.Discard); !errors.Is(err, ErrConflictingEdits) {
		t.Errorf("err = %v, want ErrConflictingEdits", err)
	}
}

func TestWriteTo_ChapterEditUnsupported(t *testing.T) {
	useWriter(t, &fakeWriter{})
	f := openSample(t, sampleFLAC)
	f.Chapters = append(f.Chapters, Chapter{Title: "Extra", Index: len(f.Chapters)})
	if _, err := f.WriteTo(io.Discard); !errors.Is(err, ErrUnsupportedWrite) {
		t.Errorf("err = %v, want ErrUnsupportedWrite", err)
	}
}

func TestWriteTo_AudioEditsIgnored(t *testing.T) {
	fw := &fakeWriter{}
	useWriter(t, fw)
	f := openSample(t, sampleFLAC)
	f.Audio.Bitrate = 1
	if _, err := f.WriteTo(io.Discard); err != nil {
		t.Fatal(err)
	}
	if fw.callCount() != 0 {
		t.Error("an Audio change produced edits")
	}
}

func TestWriteTo_Canceled(t *testing.T) {
	useWriter(t, &fakeWriter{})
	f := openSample(t, sampleFLAC)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.WriteToContext(ctx, io.Discard); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestWriteTo_WriterPanic(t *testing.T) {
	useWriter(t, &fakeWriter{panics: true})
	f := openSample(t, sampleFLAC)
	f.Tags.Title = "x"
	_, err := f.WriteTo(io.Discard)
	if _, ok := errors.AsType[*CorruptedFileError](err); !ok {
		t.Errorf("err = %v, want *CorruptedFileError", err)
	}
}

func TestArtworkEdits(t *testing.T) {
	fw := &fakeWriter{}
	useWriter(t, fw)
	f := openSample(t, sampleFLAC)
	pngData := tinyPNG(t)

	invalid := []Artwork{
		{Type: ArtworkFrontCover},
		{Data: pngData, Type: ArtworkPublisherLogotype + 1},
		{Data: pngData, Type: ArtworkOther - 1},
		{Data: pngData, Description: "\xff"},
		{Data: []byte("not an image")},
	}
	for _, a := range invalid {
		if err := f.AddArtwork(a); !errors.Is(err, ErrInvalidArtwork) {
			t.Errorf("AddArtwork(%v) err = %v, want ErrInvalidArtwork", a, err)
		}
		if err := f.ReplaceArtwork(a); !errors.Is(err, ErrInvalidArtwork) {
			t.Errorf("ReplaceArtwork(%v) err = %v, want ErrInvalidArtwork", a, err)
		}
	}
	if ops := f.pendingArtwork(); len(ops) != 0 {
		t.Fatalf("rejected artwork recorded %d ops", len(ops))
	}

	buf := slices.Clone(pngData)
	if err := f.AddArtwork(Artwork{Data: buf, Type: ArtworkBackCover}); err != nil {
		t.Fatal(err)
	}
	buf[0] = 0 // caller reuses its buffer
	if err := f.ReplaceArtwork(Artwork{Data: pngData, Type: ArtworkFrontCover, MIMEType: "image/x-custom"}); err != nil {
		t.Fatal(err)
	}
	f.RemoveArtwork(ArtworkLeaflet)
	f.RemoveAllArtwork()

	if _, err := f.WriteTo(io.Discard); err != nil {
		t.Fatal(err)
	}
	ops := fw.got.Artwork
	kinds := make([]types.ArtworkOpKind, len(ops))
	for i, op := range ops {
		kinds[i] = op.Kind
	}
	want := []types.ArtworkOpKind{types.ArtworkOpAdd, types.ArtworkOpRemove, types.ArtworkOpAdd, types.ArtworkOpRemove, types.ArtworkOpRemoveAll}
	if !slices.Equal(kinds, want) {
		t.Fatalf("op kinds = %v, want %v", kinds, want)
	}
	added := ops[0].Art
	if added.MIMEType != "image/png" || added.Width != 1 || added.Height != 1 {
		t.Errorf("added artwork = %s %dx%d, want image/png 1x1", added.MIMEType, added.Width, added.Height)
	}
	if !bytes.Equal(added.Data, pngData) {
		t.Error("AddArtwork did not copy Data")
	}
	if ops[1].Type != ArtworkFrontCover || ops[2].Art.MIMEType != "image/x-custom" {
		t.Errorf("ReplaceArtwork ops = %+v, %+v", ops[1].Type, ops[2].Art.MIMEType)
	}
	if ops[3].Type != ArtworkLeaflet {
		t.Errorf("RemoveArtwork type = %v", ops[3].Type)
	}

	art, err := f.ExtractArtwork()
	if err != nil {
		t.Fatal(err)
	}
	stored, err := openSample(t, sampleFLAC).ExtractArtwork()
	if err != nil {
		t.Fatal(err)
	}
	if len(art) != len(stored) || (len(art) > 0 && !bytes.Equal(art[0].Data, stored[0].Data)) {
		t.Errorf("ExtractArtwork reports %d images, the file stores %d; pending edits must not show", len(art), len(stored))
	}
}

func TestArtworkEdits_Concurrent(t *testing.T) {
	f := openSample(t, sampleFLAC)
	pngData := tinyPNG(t)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			_ = f.AddArtwork(Artwork{Data: pngData})
			f.RemoveArtwork(ArtworkIcon)
			_ = f.pendingArtwork()
		})
	}
	wg.Wait()
	if got := len(f.pendingArtwork()); got != 16 {
		t.Errorf("pending ops = %d, want 16", got)
	}
}

func TestWrite_ReplacesFile(t *testing.T) {
	useWriter(t, &fakeWriter{})
	path, data := copySample(t, sampleFLAC)
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	err := Write(path, func(f *File) error {
		f.Tags.Title = "New"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	assertFileBytes(t, path, append(slices.Clone(data), fakeTrailer...))
	assertNoTempFiles(t, filepath.Dir(path))
	if st, err := os.Stat(path); err != nil || st.Mode().Perm() != 0o640 {
		t.Errorf("mode after Write = %v, %v; want 0640", st.Mode().Perm(), err)
	}
}

func TestWrite_EditErrorReturnedUnchanged(t *testing.T) {
	fw := &fakeWriter{}
	useWriter(t, fw)
	path, data := copySample(t, sampleFLAC)
	boom := errors.New("boom")
	err := Write(path, func(f *File) error {
		f.Tags.Title = "New"
		return boom
	})
	if err != boom { //nolint:errorlint // Write must return the callback's error unchanged
		t.Errorf("err = %v, want the callback's error itself", err)
	}
	assertFileBytes(t, path, data)
	if fw.callCount() != 0 {
		t.Error("writer called after the edit callback failed")
	}
}

func TestWrite_NoEditsLeavesFileAlone(t *testing.T) {
	useWriter(t, &fakeWriter{})
	path, data := copySample(t, sampleFLAC)
	mtime := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, func(*File) error { return nil }); err != nil {
		t.Fatal(err)
	}
	assertFileBytes(t, path, data)
	if st, _ := os.Stat(path); !st.ModTime().Equal(mtime) {
		t.Errorf("mtime = %v, want unchanged %v", st.ModTime(), mtime)
	}
}

func TestWrite_PreserveModTime(t *testing.T) {
	useWriter(t, &fakeWriter{})
	path, _ := copySample(t, sampleFLAC)
	mtime := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	edit := func(f *File) error { f.Tags.Title = "New"; return nil }
	if err := Write(path, edit, WithPreserveModTime()); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(path); !st.ModTime().Equal(mtime) {
		t.Errorf("mtime = %v, want preserved %v", st.ModTime(), mtime)
	}
}

func TestWrite_VerifyFailure(t *testing.T) {
	path, data := copySample(t, sampleFLAC)
	srcSize := int64(len(data))
	useWriter(t, &fakeWriter{digestFn: func(size int64) []byte {
		return []byte{byte(size - srcSize)} // the written file has a different "audio" digest
	}})
	err := Write(path, func(f *File) error { f.Tags.Title = "New"; return nil })
	if !errors.Is(err, ErrVerifyFailed) {
		t.Errorf("err = %v, want ErrVerifyFailed", err)
	}
	assertFileBytes(t, path, data)
	assertNoTempFiles(t, filepath.Dir(path))
}

func TestWrite_ConcurrentModification(t *testing.T) {
	path, data := copySample(t, sampleFLAC)
	fw := &fakeWriter{onWrite: func() {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			panic(err)
		}
		_, _ = f.Write([]byte{0})
		_ = f.Close()
	}}
	useWriter(t, fw)
	err := Write(path, func(f *File) error { f.Tags.Title = "New"; return nil })
	if !errors.Is(err, ErrConcurrentModification) {
		t.Errorf("err = %v, want ErrConcurrentModification", err)
	}
	assertFileBytes(t, path, append(slices.Clone(data), 0))
	assertNoTempFiles(t, filepath.Dir(path))
}

func TestWrite_FollowsSymlink(t *testing.T) {
	useWriter(t, &fakeWriter{})
	path, data := copySample(t, sampleFLAC)
	link := filepath.Join(filepath.Dir(path), "link.flac")
	if err := os.Symlink(path, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := Write(link, func(f *File) error { f.Tags.Title = "New"; return nil }); err != nil {
		t.Fatal(err)
	}
	assertFileBytes(t, path, append(slices.Clone(data), fakeTrailer...))
	if st, err := os.Lstat(link); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Error("the symlink was replaced")
	}
}

func TestWrite_InPlace(t *testing.T) {
	edit := func(f *File) error { f.Tags.Title = "New"; return nil }

	t.Run("patch applied", func(t *testing.T) {
		path, data := copySample(t, sampleFLAC)
		fw := &inPlaceFake{off: 0, patch: []byte("fLaC"), fits: true}
		useWriter(t, fw)
		if err := Write(path, edit, WithInPlace()); err != nil {
			t.Fatal(err)
		}
		assertFileBytes(t, path, data)
		if fw.callCount() != 0 {
			t.Error("WriteFile called although the patch fit")
		}
	})
	t.Run("does not fit falls back", func(t *testing.T) {
		path, data := copySample(t, sampleFLAC)
		fw := &inPlaceFake{fits: false}
		useWriter(t, fw)
		if err := Write(path, edit, WithInPlace()); err != nil {
			t.Fatal(err)
		}
		assertFileBytes(t, path, append(slices.Clone(data), fakeTrailer...))
	})
	t.Run("verify failure restores", func(t *testing.T) {
		path, data := copySample(t, sampleFLAC)
		fw := &inPlaceFake{off: 0, patch: []byte("XXXX"), fits: true}
		useWriter(t, fw)
		err := Write(path, edit, WithInPlace())
		if !errors.Is(err, ErrVerifyFailed) {
			t.Errorf("err = %v, want ErrVerifyFailed", err)
		}
		assertFileBytes(t, path, data)
	})
	t.Run("not requested", func(t *testing.T) {
		path, data := copySample(t, sampleFLAC)
		fw := &inPlaceFake{off: 0, patch: []byte("fLaC"), fits: true}
		useWriter(t, fw)
		if err := Write(path, edit); err != nil {
			t.Fatal(err)
		}
		assertFileBytes(t, path, append(slices.Clone(data), fakeTrailer...))
	})
}

// TestSetSync_MatchesParser checks that the field bindings mirror the
// parsers: re-setting any raw key of a parsed corpus file to its own values
// leaves every typed field as the parser produced it.
func TestSetSync_MatchesParser(t *testing.T) {
	paths, err := filepath.Glob("testdata/*.*")
	if err != nil {
		t.Fatal(err)
	}
	bench, err := filepath.Glob("testdata/bench/book.*")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range append(paths, bench...) {
		if filepath.Ext(path) == ".golden" {
			continue
		}
		t.Run(filepath.Base(path), func(t *testing.T) {
			f := openSample(t, path)
			for key, values := range f.Tags.All() {
				tags := f.Tags.Clone()
				tags.Set(key, values...)
				if !tags.Equal(&f.Tags) {
					t.Errorf("Set(%q, %q) changed typed fields:\n got  %+v\n want %+v", key, values, *tags, f.Tags)
				}
			}
		})
	}
}
