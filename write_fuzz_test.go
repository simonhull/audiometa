package audiometa

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/simonhull/audiometa/internal/registry"
	"github.com/simonhull/audiometa/internal/types"
)

// fuzzWritablePaths are the corpus files that seed FuzzWriteRoundTrip: one per
// writable format and layout. It mirrors the writable rows of the round-trip
// corpus (that table lives in the external test package).
var fuzzWritablePaths = []string{
	"testdata/sample.mp3",
	"testdata/sample_v24.mp3",
	"testdata/sample.flac",
	"testdata/sample.m4a",
	"testdata/sample_faststart.m4b",
	"testdata/bench/book.mp3",
	"testdata/bench/book.flac",
	"testdata/bench/book.m4b",
}

// fuzzPNG is a 1x1 PNG for the artwork operations.
func fuzzPNG() []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, 1, 1))); err != nil {
		panic(err) // encoding a fixed in-memory image cannot fail
	}
	return buf.Bytes()
}

// opReader consumes the fuzzer's op bytes.
type opReader struct{ b []byte }

func (r *opReader) next() (byte, bool) {
	if len(r.b) == 0 {
		return 0, false
	}
	c := r.b[0]
	r.b = r.b[1:]
	return c, true
}

// str reads a length byte (modulo 32) and that many bytes as a string.
func (r *opReader) str() string {
	n, _ := r.next()
	k := min(int(n%32), len(r.b))
	s := string(r.b[:k])
	r.b = r.b[k:]
	return s
}

// applyFuzzOps interprets ops as edits of f: each op byte selects, modulo 6,
// a raw key edit, Title, Artists, Year, AddArtwork or RemoveArtwork.
func applyFuzzOps(f *File, ops []byte, png []byte) {
	keys := []string{"ZZTEST"}
	for _, b := range types.FieldBindings(f.Format) {
		keys = append(keys, b.Keys...)
	}
	r := &opReader{b: ops}
	for {
		op, ok := r.next()
		if !ok {
			return
		}
		switch op % 6 {
		case 0:
			idx, _ := r.next()
			f.Tags.Set(keys[int(idx)%len(keys)], r.str())
		case 1:
			f.Tags.Title = r.str()
		case 2:
			n, _ := r.next()
			artists := make([]string, 0, 3)
			for range int(n%3) + 1 {
				artists = append(artists, r.str())
			}
			f.Tags.Artists = artists
		case 3:
			y, _ := r.next()
			f.Tags.Year = 1900 + int(y)
		case 4:
			t, _ := r.next()
			_ = f.AddArtwork(Artwork{Data: png, Type: ArtworkType(t % 21)}) // rejection is fine
		case 5:
			t, _ := r.next()
			f.RemoveArtwork(ArtworkType(t % 21))
		}
	}
}

// FuzzWriteRoundTrip applies random edits to random inputs through the public
// write path of every registered writer. A successful write must re-open, keep
// the audio payload bit-identical and be deterministic; a failed one must be
// an ordinary error, never a recovered panic.
//
// The per-format fuzz targets (FuzzWriteFLAC in internal/flac) drive one
// writer directly with a fixed edit set; this one covers the dispatch, edit
// building and every format with variable edits.
func FuzzWriteRoundTrip(f *testing.F) {
	for _, path := range fuzzWritablePaths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		f.Add(data, []byte{})
		f.Add(data, []byte{0, 1, 'x', 2, 'y'})
	}
	f.Add([]byte("ID3\x04\x00\x00\x00\x00\x00\x00"), []byte{0})
	f.Add([]byte("fLaC"), []byte{0})

	img := fuzzPNG()
	f.Fuzz(func(t *testing.T, data, ops []byte) {
		if len(data) > 1<<20 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		p, err := openReader(ctx, bytes.NewReader(data), int64(len(data)), "fuzz")
		if err != nil {
			return
		}
		file := &File{File: *p.file, reader: bytes.NewReader(data), parser: p.parser}
		tw := registry.GetWriter(file.Format)
		if tw == nil {
			return
		}
		// Write refuses a source whose audio cannot be digested, before it
		// writes anything, so such inputs are outside the invariants below.
		before, err := tw.AudioDigest(ctx, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return
		}
		applyFuzzOps(file, ops, img)

		var out bytes.Buffer
		if _, err := file.WriteToContext(ctx, &out); err != nil {
			var ce *types.CorruptedFileError
			if errors.As(err, &ce) && strings.HasPrefix(ce.Reason, "parser panic") {
				t.Fatalf("writer panicked: %v", err)
			}
			return
		}

		if _, err := openReader(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), "fuzz"); err != nil {
			t.Fatalf("output does not re-open: %v", err)
		}
		after, err := tw.AudioDigest(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("audio digest of output: %v, equal %v", err, bytes.Equal(before, after))
		}
		var again bytes.Buffer
		if _, err := file.WriteToContext(ctx, &again); err != nil {
			t.Fatalf("second write: %v", err)
		}
		if !bytes.Equal(again.Bytes(), out.Bytes()) {
			t.Fatal("second write is not byte-identical")
		}
	})
}
