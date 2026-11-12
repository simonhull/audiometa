package audiometa

// Tests in this file swap the package-level lookupWriter, so none of them may
// call t.Parallel.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
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

// PreservedDigest mirrors AudioDigest, so digestFn drives verification too.
func (w *fakeWriter) PreservedDigest(ctx context.Context, src io.ReaderAt, size int64) ([]byte, error) {
	return w.AudioDigest(ctx, src, size)
}

func (w *fakeWriter) callCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.calls
}

// inPlaceFake is a fakeWriter that can also patch in place.
type inPlaceFake struct {
	fakeWriter
	off     int64
	patch   []byte
	fits    bool
	err     error  // returned by InPlacePatch
	onPatch func() // side effect while InPlacePatch runs

	// preserved, when set, replaces the fixed PreservedDigest, so a test
	// can make verification depend on the bytes on disk.
	preserved func(ctx context.Context, src io.ReaderAt, size int64) ([]byte, error)
}

func (w *inPlaceFake) InPlacePatch(context.Context, io.ReaderAt, int64, *types.EditSet) (*registry.Patch, error) {
	if w.onPatch != nil {
		w.onPatch()
	}
	if w.err != nil || !w.fits {
		return nil, w.err
	}
	return &registry.Patch{Off: w.off, Data: w.patch}, nil
}

func (w *inPlaceFake) PreservedDigest(ctx context.Context, src io.ReaderAt, size int64) ([]byte, error) {
	if w.preserved != nil {
		return w.preserved(ctx, src, size)
	}
	return w.fakeWriter.PreservedDigest(ctx, src, size)
}

// digestOutside hashes every byte outside [off, off+n), as an honest
// in-place writer's PreservedDigest does.
func digestOutside(off int64, n int) func(context.Context, io.ReaderAt, int64) ([]byte, error) {
	return func(ctx context.Context, src io.ReaderAt, size int64) ([]byte, error) {
		h := sha256.New()
		end := off + int64(n)
		if _, err := writeutil.CopyRange(ctx, h, src, 0, off); err != nil {
			return nil, err
		}
		if _, err := writeutil.CopyRange(ctx, h, src, end, size-end); err != nil {
			return nil, err
		}
		return h.Sum(nil), nil
	}
}

// digestAll hashes the whole file, so any patch that still parses fails
// verification on the digest.
func digestAll(ctx context.Context, src io.ReaderAt, size int64) ([]byte, error) {
	h := sha256.New()
	if _, err := writeutil.CopyRange(ctx, h, src, 0, size); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// titlePatch returns where sample.flac stores "Test Title" and a same-length
// replacement that still parses.
func titlePatch(t *testing.T, data []byte) (int64, []byte) {
	t.Helper()
	i := bytes.Index(data, []byte("Test Title"))
	if i < 0 {
		t.Fatal(`sample has no "Test Title"`)
	}
	return int64(i), []byte("Best Title")
}

// patched returns data with patch written at off.
func patched(data []byte, off int64, patch []byte) []byte {
	out := slices.Clone(data)
	copy(out[off:], patch)
	return out
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
	const sampleOgg = "testdata/sample.ogg"
	prev := lookupWriter
	lookupWriter = func(Format) registry.TagWriter { return nil } // a format without a writer
	t.Cleanup(func() { lookupWriter = prev })
	f := openSample(t, sampleOgg)
	n, err := f.WriteTo(io.Discard)
	if !errors.Is(err, ErrUnsupportedWrite) || n != 0 {
		t.Errorf("WriteTo = %d, %v; want 0, ErrUnsupportedWrite", n, err)
	}

	path, data := copySample(t, sampleOgg)
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

// panicAfterWriter writes 10 bytes and then panics.
type panicAfterWriter struct{ fakeWriter }

func (w *panicAfterWriter) WriteFile(_ context.Context, out io.Writer, _ io.ReaderAt, _ int64, _ *types.EditSet) (int64, error) {
	if _, err := out.Write(make([]byte, 10)); err != nil {
		return 0, err
	}
	panic("late boom")
}

func TestWriteTo_PanicReportsBytesWritten(t *testing.T) {
	useWriter(t, &panicAfterWriter{})
	f := openSample(t, sampleFLAC)
	f.Tags.Title = "x"
	n, err := f.WriteTo(io.Discard)
	if _, ok := errors.AsType[*CorruptedFileError](err); !ok {
		t.Errorf("err = %v, want *CorruptedFileError", err)
	}
	if n != 10 {
		t.Errorf("n = %d, want the 10 bytes that reached the writer", n)
	}
}

func TestSafely(t *testing.T) {
	t.Parallel()

	v, err := safely("p", func() (int, error) { panic("boom") })
	cerr, ok := errors.AsType[*CorruptedFileError](err)
	if !ok || !strings.HasPrefix(cerr.Reason, "parser panic: boom") || v != 0 {
		t.Errorf("safely(panic) = %d, %v; want 0 and a CorruptedFileError for the panic", v, err)
	}

	sentinel := errors.New("sentinel")
	v, err = safely("p", func() (int, error) { return 7, sentinel })
	if v != 7 || !errors.Is(err, sentinel) {
		t.Errorf("safely(return) = %d, %v; want 7, sentinel", v, err)
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
	if runtime.GOOS != "windows" { // Windows maps only the read-only bit
		if st, err := os.Stat(path); err != nil || st.Mode().Perm() != 0o640 {
			t.Errorf("mode after Write = %v, %v; want 0640", st.Mode().Perm(), err)
		}
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

// appendByte appends one zero byte to path, as another writer would.
func appendByte(t *testing.T, path string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Error(err)
		return
	}
	defer f.Close()
	if _, err := f.Write([]byte{0}); err != nil {
		t.Error(err)
	}
}

func TestWrite_ConcurrentModification(t *testing.T) {
	path, data := copySample(t, sampleFLAC)
	fw := &fakeWriter{onWrite: func() { appendByte(t, path) }}
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
		off, patch := titlePatch(t, data)
		fw := &inPlaceFake{off: off, patch: patch, fits: true, preserved: digestOutside(off, len(patch))}
		useWriter(t, fw)
		if err := Write(path, edit, WithInPlace()); err != nil {
			t.Fatal(err)
		}
		assertFileBytes(t, path, patched(data, off, patch))
		if fw.callCount() != 0 {
			t.Error("WriteFile called although the patch fit")
		}
		if got := openSample(t, path).Tags.Title; got != "Best Title" {
			t.Errorf("Title = %q, want the patched title", got)
		}
	})
	t.Run("preserve mod time", func(t *testing.T) {
		path, data := copySample(t, sampleFLAC)
		mtime := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
		off, patch := titlePatch(t, data)
		useWriter(t, &inPlaceFake{off: off, patch: patch, fits: true, preserved: digestOutside(off, len(patch))})
		if err := Write(path, edit, WithInPlace(), WithPreserveModTime()); err != nil {
			t.Fatal(err)
		}
		assertFileBytes(t, path, patched(data, off, patch))
		if st, _ := os.Stat(path); !st.ModTime().Equal(mtime) {
			t.Errorf("mtime = %v, want preserved %v", st.ModTime(), mtime)
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
	t.Run("unparseable result restores", func(t *testing.T) {
		path, data := copySample(t, sampleFLAC)
		mtime := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
		useWriter(t, &inPlaceFake{off: 0, patch: []byte("XXXX"), fits: true})
		if err := Write(path, edit, WithInPlace()); !errors.Is(err, ErrVerifyFailed) {
			t.Errorf("err = %v, want ErrVerifyFailed", err)
		}
		assertFileBytes(t, path, data)
		if st, _ := os.Stat(path); !st.ModTime().Equal(mtime) {
			t.Errorf("mtime after restore = %v, want the original %v", st.ModTime(), mtime)
		}
	})
	t.Run("digest mismatch restores", func(t *testing.T) {
		path, data := copySample(t, sampleFLAC)
		off, patch := titlePatch(t, data)
		mtime := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
		useWriter(t, &inPlaceFake{off: off, patch: patch, fits: true, preserved: digestAll})
		err := Write(path, edit, WithInPlace())
		if !errors.Is(err, ErrVerifyFailed) {
			t.Errorf("err = %v, want ErrVerifyFailed", err)
		}
		assertFileBytes(t, path, data)
		if st, _ := os.Stat(path); !st.ModTime().Equal(mtime) {
			t.Errorf("mtime after restore = %v, want the original %v", st.ModTime(), mtime)
		}
	})
	t.Run("patch outside file", func(t *testing.T) {
		path, data := copySample(t, sampleFLAC)
		for _, off := range []int64{-1, int64(len(data)) - 2} {
			fw := &inPlaceFake{off: off, patch: []byte("ABCD"), fits: true}
			useWriter(t, fw)
			if err := Write(path, edit, WithInPlace()); err == nil || !strings.Contains(err.Error(), "outside file") {
				t.Errorf("off %d: err = %v, want an outside-file error", off, err)
			}
			assertFileBytes(t, path, data)
			if fw.callCount() != 0 {
				t.Errorf("off %d: fell back to WriteFile after a bad patch", off)
			}
		}
	})
	t.Run("patch error", func(t *testing.T) {
		path, data := copySample(t, sampleFLAC)
		boom := errors.New("boom")
		fw := &inPlaceFake{err: boom}
		useWriter(t, fw)
		if err := Write(path, edit, WithInPlace()); !errors.Is(err, boom) {
			t.Errorf("err = %v, want boom", err)
		}
		assertFileBytes(t, path, data)
		if fw.callCount() != 0 {
			t.Error("fell back to WriteFile after InPlacePatch failed")
		}
	})
	t.Run("concurrent modification", func(t *testing.T) {
		path, data := copySample(t, sampleFLAC)
		off, patch := titlePatch(t, data)
		useWriter(t, &inPlaceFake{off: off, patch: patch, fits: true, onPatch: func() { appendByte(t, path) }})
		if err := Write(path, edit, WithInPlace()); !errors.Is(err, ErrConcurrentModification) {
			t.Errorf("err = %v, want ErrConcurrentModification", err)
		}
		assertFileBytes(t, path, append(slices.Clone(data), 0))
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

// rangedFake is an inPlaceFake that also reports preserved ranges and counts
// its digest calls.
type rangedFake struct {
	inPlaceFake
	ranges      [][]registry.Range // returned by successive PreservedRanges calls; the last repeats
	rangeCalls  int
	digestCalls int
}

func (w *rangedFake) PreservedRanges(context.Context, io.ReaderAt, int64) ([]registry.Range, error) {
	i := min(w.rangeCalls, len(w.ranges)-1)
	w.rangeCalls++
	return w.ranges[i], nil
}

func (w *rangedFake) PreservedDigest(ctx context.Context, src io.ReaderAt, size int64) ([]byte, error) {
	w.digestCalls++
	return w.inPlaceFake.PreservedDigest(ctx, src, size)
}

// TestWrite_InPlaceRanges covers verifying an in-place patch
// by its preserved ranges instead of by digests.
func TestWrite_InPlaceRanges(t *testing.T) {
	edit := func(f *File) error { f.Tags.Title = "New"; return nil }
	mtime := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	outside := func(off int64, n, size int) []registry.Range {
		return []registry.Range{{Off: 0, End: off}, {Off: off + int64(n), End: int64(size)}}
	}

	t.Run("honest patch needs no digest", func(t *testing.T) {
		path, data := copySample(t, sampleFLAC)
		off, patch := titlePatch(t, data)
		fw := &rangedFake{
			off: off, patch: patch, fits: true,
			ranges: [][]registry.Range{outside(off, len(patch), len(data))},
		}
		useWriter(t, fw)
		if err := Write(path, edit, WithInPlace()); err != nil {
			t.Fatal(err)
		}
		assertFileBytes(t, path, patched(data, off, patch))
		if fw.digestCalls != 0 || fw.callCount() != 0 {
			t.Errorf("PreservedDigest calls = %d, WriteFile calls = %d, want none", fw.digestCalls, fw.callCount())
		}
		if fw.rangeCalls != 2 {
			t.Errorf("PreservedRanges calls = %d, want 2 (source and patched file)", fw.rangeCalls)
		}
	})
	t.Run("overlapping patch is refused before any write", func(t *testing.T) {
		path, data := copySample(t, sampleFLAC)
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
		off, patch := titlePatch(t, data)
		fw := &rangedFake{
			off: off, patch: patch, fits: true,
			ranges: [][]registry.Range{{{Off: off + 2, End: int64(len(data))}}},
		}
		useWriter(t, fw)
		if err := Write(path, edit, WithInPlace()); !errors.Is(err, ErrVerifyFailed) {
			t.Errorf("err = %v, want ErrVerifyFailed", err)
		}
		assertFileBytes(t, path, data)
		if st, _ := os.Stat(path); !st.ModTime().Equal(mtime) {
			t.Errorf("mtime = %v, want untouched %v: something was written", st.ModTime(), mtime)
		}
		if fw.rangeCalls != 1 {
			t.Errorf("PreservedRanges calls = %d, want 1 (the source only)", fw.rangeCalls)
		}
	})
	t.Run("ranges that move after the patch restore", func(t *testing.T) {
		path, data := copySample(t, sampleFLAC)
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
		off, patch := titlePatch(t, data)
		fw := &rangedFake{
			off: off, patch: patch, fits: true,
			ranges: [][]registry.Range{
				outside(off, len(patch), len(data)),
				outside(off, len(patch)+1, len(data)),
			},
		}
		useWriter(t, fw)
		if err := Write(path, edit, WithInPlace()); !errors.Is(err, ErrVerifyFailed) {
			t.Errorf("err = %v, want ErrVerifyFailed", err)
		}
		assertFileBytes(t, path, data)
		if st, _ := os.Stat(path); !st.ModTime().Equal(mtime) {
			t.Errorf("mtime after restore = %v, want the original %v", st.ModTime(), mtime)
		}
	})
	t.Run("unparseable result restores", func(t *testing.T) {
		path, data := copySample(t, sampleFLAC)
		fw := &rangedFake{
			off: 0, patch: []byte("XXXX"), fits: true,
			ranges: [][]registry.Range{{{Off: 4, End: int64(len(data))}}},
		}
		useWriter(t, fw)
		if err := Write(path, edit, WithInPlace()); !errors.Is(err, ErrVerifyFailed) {
			t.Errorf("err = %v, want ErrVerifyFailed", err)
		}
		assertFileBytes(t, path, data)
	})
	t.Run("atomic fallback still digests", func(t *testing.T) {
		path, data := copySample(t, sampleFLAC)
		fw := &rangedFake{fits: false, ranges: [][]registry.Range{{}}}
		useWriter(t, fw)
		if err := Write(path, edit, WithInPlace()); err != nil {
			t.Fatal(err)
		}
		assertFileBytes(t, path, append(slices.Clone(data), fakeTrailer...))
		if fw.digestCalls != 2 {
			t.Errorf("PreservedDigest calls = %d, want 2 (source and written file)", fw.digestCalls)
		}
	})
	t.Run("writer without ranges keeps the digests", func(t *testing.T) {
		path, data := copySample(t, sampleFLAC)
		off, patch := titlePatch(t, data)
		calls := 0
		honest := digestOutside(off, len(patch))
		useWriter(t, &inPlaceFake{off: off, patch: patch, fits: true,
			preserved: func(ctx context.Context, src io.ReaderAt, size int64) ([]byte, error) {
				calls++
				return honest(ctx, src, size)
			}})
		if err := Write(path, edit, WithInPlace()); err != nil {
			t.Fatal(err)
		}
		assertFileBytes(t, path, patched(data, off, patch))
		if calls != 2 {
			t.Errorf("PreservedDigest calls = %d, want 2 (source and patched file)", calls)
		}
	})
}

func TestWrite_Canceled(t *testing.T) {
	edit := func(f *File) error { f.Tags.Title = "New"; return nil }

	t.Run("before start", func(t *testing.T) {
		path, data := copySample(t, sampleFLAC)
		fw := &fakeWriter{}
		useWriter(t, fw)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if err := WriteContext(ctx, path, edit); !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
		assertFileBytes(t, path, data)
		if fw.callCount() != 0 {
			t.Error("writer called after cancellation")
		}
	})
	t.Run("during atomic write", func(t *testing.T) {
		path, data := copySample(t, sampleFLAC)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		useWriter(t, &fakeWriter{onWrite: cancel})
		if err := WriteContext(ctx, path, edit); !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
		assertFileBytes(t, path, data)
		assertNoTempFiles(t, filepath.Dir(path))
	})
	t.Run("during in-place verify", func(t *testing.T) {
		path, data := copySample(t, sampleFLAC)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		mtime := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
		off, patch := titlePatch(t, data)
		calls := 0
		honest := digestOutside(off, len(patch))
		useWriter(t, &inPlaceFake{off: off, patch: patch, fits: true,
			preserved: func(ctx context.Context, src io.ReaderAt, size int64) ([]byte, error) {
				if calls++; calls == 2 { // the first call digests the source, the second verifies
					cancel()
					return nil, ctx.Err()
				}
				return honest(ctx, src, size)
			}})
		err := WriteContext(ctx, path, edit, WithInPlace())
		if !errors.Is(err, context.Canceled) || !errors.Is(err, ErrVerifyFailed) {
			t.Errorf("err = %v, want context.Canceled and ErrVerifyFailed", err)
		}
		assertFileBytes(t, path, data)
		if st, _ := os.Stat(path); !st.ModTime().Equal(mtime) {
			t.Errorf("mtime after restore = %v, want the original %v", st.ModTime(), mtime)
		}
	})
}

// TestWrite_InPlaceReadOnlyFallsBack: a read-only file in a writable directory
// is rewritten atomically, as without WithInPlace, and keeps its mode.
func TestWrite_InPlaceReadOnlyFallsBack(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs Unix permissions and a non-root user")
	}
	path, data := copySample(t, sampleFLAC)
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	off, patch := titlePatch(t, data)
	// No preserved digest: the fixed one lets the atomic fallback (source +
	// "FAKE") verify.
	fw := &inPlaceFake{off: off, patch: patch, fits: true}
	useWriter(t, fw)
	if err := Write(path, func(f *File) error { f.Tags.Title = "New"; return nil }, WithInPlace()); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if fw.callCount() != 1 {
		t.Errorf("WriteFile calls = %d, want 1 (atomic fallback)", fw.callCount())
	}
	assertFileBytes(t, path, append(slices.Clone(data), fakeTrailer...))
	if st, err := os.Stat(path); err != nil || st.Mode().Perm() != 0o444 {
		t.Errorf("mode = %v, %v; want 0444", st.Mode().Perm(), err)
	}
}

// TestWrite_ConcurrentReplace replaces the file with a same-size copy that
// keeps the original mtime, as another program's atomic save might.
func TestWrite_ConcurrentReplace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot rename over a file that Write holds open")
	}
	path, data := copySample(t, sampleFLAC)
	st0, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	other := slices.Clone(data)
	other[len(other)-1] ^= 0xFF
	useWriter(t, &fakeWriter{onWrite: func() {
		tmp := path + ".other"
		if err := os.WriteFile(tmp, other, 0o600); err != nil {
			t.Error(err)
		}
		if err := os.Chtimes(tmp, st0.ModTime(), st0.ModTime()); err != nil {
			t.Error(err)
		}
		if err := os.Rename(tmp, path); err != nil {
			t.Error(err)
		}
	}})
	if err := Write(path, func(f *File) error { f.Tags.Title = "New"; return nil }); !errors.Is(err, ErrConcurrentModification) {
		t.Errorf("err = %v, want ErrConcurrentModification", err)
	}
	assertFileBytes(t, path, other)
	assertNoTempFiles(t, filepath.Dir(path))
}

// TestWrite_ConcurrentSameSizeRewrite rewrites the file in place with the same
// size and puts the mtime back, as another in-place writer with
// WithPreserveModTime does. Only the inode change time reveals it.
func TestWrite_ConcurrentSameSizeRewrite(t *testing.T) {
	st, err := os.Stat(sampleFLAC)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := changeTime(st); !ok {
		t.Skip("no inode change time on this platform")
	}
	for _, inPlace := range []bool{false, true} {
		t.Run(fmt.Sprintf("inPlace=%v", inPlace), func(t *testing.T) {
			path, data := copySample(t, sampleFLAC)
			st0, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			other := slices.Clone(data)
			other[len(other)-1] ^= 0xFF
			rewrite := func() {
				time.Sleep(20 * time.Millisecond) // ctime has clock-tick granularity
				if err := os.WriteFile(path, other, 0o600); err != nil {
					t.Error(err)
				}
				if err := os.Chtimes(path, st0.ModTime(), st0.ModTime()); err != nil {
					t.Error(err)
				}
			}
			var opts []WriteOption
			if inPlace {
				off, patch := titlePatch(t, data)
				useWriter(t, &inPlaceFake{off: off, patch: patch, fits: true, onPatch: rewrite})
				opts = append(opts, WithInPlace())
			} else {
				useWriter(t, &fakeWriter{onWrite: rewrite})
			}
			if err := Write(path, func(f *File) error { f.Tags.Title = "New"; return nil }, opts...); !errors.Is(err, ErrConcurrentModification) {
				t.Errorf("err = %v, want ErrConcurrentModification", err)
			}
			assertFileBytes(t, path, other)
		})
	}
}

// TestWrite_ConcurrentDelete removes the file while Write runs.
func TestWrite_ConcurrentDelete(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot remove a file that Write holds open")
	}
	path, _ := copySample(t, sampleFLAC)
	useWriter(t, &fakeWriter{onWrite: func() {
		if err := os.Remove(path); err != nil {
			t.Error(err)
		}
	}})
	err := Write(path, func(f *File) error { f.Tags.Title = "New"; return nil })
	if !errors.Is(err, ErrConcurrentModification) || !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err = %v, want ErrConcurrentModification wrapping fs.ErrNotExist", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("file recreated: %v", err)
	}
	assertNoTempFiles(t, filepath.Dir(path))
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
