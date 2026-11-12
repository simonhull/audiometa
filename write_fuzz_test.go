package audiometa

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/simonhull/audiometa/internal/registry"
	"github.com/simonhull/audiometa/internal/testbudget"
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
	"testdata/sample.wav",
	"testdata/sample_id3.wav",
	"testdata/sample_rf64.wav",
	"testdata/sample.aiff",
	"testdata/bench/book.mp3",
	"testdata/bench/book.flac",
	"testdata/bench/book.m4b",
	"testdata/sample_mono_vbr.mp3",
	"testdata/sample_mpeg2.mp3",
	"testdata/sample_upper.flac",
	"testdata/sample.m4b",
	"testdata/sample_faststart.m4a",
	"testdata/sample.aifc",
	"testdata/sample_ext.wav",
	"testdata/sample_float.wav",
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
// the audio payload bit-identical, be deterministic and land the edits: every
// edited key and field reads back, and every raw key no edit or field binding
// touches keeps its values. An in-place patch must equal the full rewrite. A
// failed write must be an ordinary error, never a recovered panic, and no
// operation may allocate more than its budget.
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
		f.Add(data, []byte{1, 3, 'a', 'b', 'c', 3, 50, 0, 7, 4, 'q', 'r', 's', 't'})
	}
	f.Add([]byte("ID3\x04\x00\x00\x00\x00\x00\x00"), []byte{0})
	f.Add([]byte("fLaC"), []byte{0})
	// A raw TYER edit on an empty ID3v2.2 tag, which is rewritten as v2.4 (plan 085).
	f.Add([]byte("ID3\x0200\x00\x00\x00\x00"), []byte("0000"))
	// An empty text chunk last in the file (plan 058).
	f.Add([]byte("FORM0000AIFFCOMM\x00\x00\x00\x12000000000000000000"), []byte("00"))

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
		file := &File{File: *p.file, src: p.source(), reader: bytes.NewReader(data), parser: p.parser}
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

		edits, err := file.buildEdits(ctx)
		if err != nil {
			failOnPanic(t, err)
			return
		}
		if edits.Empty() {
			return
		}

		budget := testbudget.For(testbudget.Write, file.Format)
		var out bytes.Buffer
		testbudget.Check(t, budget, len(data), "WriteToContext", func() {
			_, err = file.WriteToContext(ctx, &out)
		})
		if err != nil {
			failOnPanic(t, err)
			return
		}

		checkInPlace(ctx, t, tw, file, data, edits, out.Bytes())

		q, err := openReader(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), "fuzz")
		if err != nil {
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
		checkLanded(t, file.Format, data, &p.file.Tags, &q.file.Tags, edits)
	})
}

// failOnPanic fails t when err reports a recovered parser or writer panic.
func failOnPanic(t *testing.T, err error) {
	t.Helper()
	var ce *types.CorruptedFileError
	if errors.As(err, &ce) && strings.HasPrefix(ce.Reason, "parser panic") {
		t.Fatalf("writer panicked: %v", err)
	}
}

// checkInPlace fails t when tw can patch data in place and the patched bytes
// differ from out, the full rewrite of the same edits.
func checkInPlace(ctx context.Context, t *testing.T, tw registry.TagWriter, file *File, data []byte, edits *types.EditSet, out []byte) {
	t.Helper()
	ip, ok := tw.(registry.InPlaceWriter)
	if !ok {
		return
	}
	var (
		p   *registry.Patch
		err error
	)
	testbudget.Check(t, testbudget.For(testbudget.Write, file.Format), len(data), "InPlacePatch", func() {
		p, err = safely("fuzz", func() (*registry.Patch, error) {
			return ip.InPlacePatch(ctx, bytes.NewReader(data), int64(len(data)), edits)
		})
	})
	if err != nil {
		t.Fatalf("%v: InPlacePatch failed after WriteToContext succeeded: %v", file.Format, err)
	}
	if p == nil {
		return
	}
	if p.Off < 0 || p.Off+int64(len(p.Data)) > int64(len(data)) {
		t.Fatalf("%v: InPlacePatch at %d of %d bytes does not fit in %d bytes", file.Format, p.Off, len(p.Data), len(data))
	}
	if pr, ok := tw.(registry.PreservedRanger); ok {
		preserved, err := safely("fuzz", func() ([]registry.Range, error) {
			return pr.PreservedRanges(ctx, bytes.NewReader(data), int64(len(data)))
		})
		if err != nil {
			t.Fatalf("%v: PreservedRanges failed after WriteToContext succeeded: %v", file.Format, err)
		}
		patch := registry.Range{Off: p.Off, End: p.Off + int64(len(p.Data))}
		for _, r := range preserved {
			if patch.Overlaps(r) {
				t.Fatalf("%v: in-place patch %v overlaps preserved bytes %v", file.Format, patch, r)
			}
		}
	}
	patched := slices.Clone(data)
	copy(patched[p.Off:], p.Data)
	if !bytes.Equal(patched, out) {
		t.Fatalf("%v: patching in place differs from the full rewrite", file.Format)
	}
}

// checkLanded fails t when the written file does not read back the edits: each
// key edit and field edit must read back exactly, and every raw key that no
// edit touches and no field binds must keep its values. Edits whose values a
// format cannot hold exactly (see representable) skip the check.
func checkLanded(t *testing.T, format Format, data []byte, orig, final *Tags, edits *types.EditSet) {
	t.Helper()
	for _, e := range edits.Keys {
		if !representable(e.Values) {
			return
		}
	}
	for _, e := range edits.Fields {
		if !representable(e.Values) {
			return
		}
	}
	for _, e := range edits.Keys {
		if reason := landedExempt(format, data, 0, e.Values); reason != "" {
			continue
		}
		if got := final.Get(landedKey(format, data, e.Key)); !slices.Equal(got, e.Values) {
			t.Errorf("%v: key %q = %q, want %q", format, e.Key, got, e.Values)
		}
	}
	for _, e := range edits.Fields {
		if reason := landedExempt(format, data, e.Field, e.Values); reason != "" {
			continue
		}
		if got := typedValues(final, e.Field); !slices.Equal(got, e.Values) {
			t.Errorf("%v: field %v = %q, want %q", format, e.Field, got, e.Values)
		}
	}

	touched := map[string]bool{}
	for _, e := range edits.Keys {
		touched[e.Key] = true
		touched[landedKey(format, data, e.Key)] = true
	}
	for _, b := range types.FieldBindings(format) {
		for _, k := range b.Keys {
			touched[k] = true
		}
	}
	for key, want := range orig.All() {
		if touched[key] || touched[landedKey(format, data, key)] {
			continue
		}
		if got := final.Get(key); !slices.Equal(got, want) {
			t.Errorf("%v: untouched key %q = %q, want %q", format, key, got, want)
		}
	}
}

// representable reports whether every value survives a write and read in
// every format: non-empty, valid UTF-8, free of control characters and without
// leading or trailing white space. Others are lossy by design (a NUL ends a
// native text value, a tag reader trims white space, and so on).
func representable(values []string) bool {
	for _, v := range values {
		if v == "" || !utf8.ValidString(v) || strings.TrimSpace(v) != v {
			return false
		}
		if strings.IndexFunc(v, unicode.IsControl) >= 0 {
			return false
		}
	}
	return true
}

// typedValues returns field f of t encoded as types.fieldValues does: nil when
// empty or zero, one value for strings and ints, a copy for slices.
func typedValues(t *Tags, f types.Field) []string {
	switch v := reflect.ValueOf(t).Elem().FieldByName(f.String()).Interface().(type) {
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	case int:
		if v == 0 {
			return nil
		}
		return []string{strconv.Itoa(v)}
	case []string:
		if len(v) == 0 {
			return nil
		}
		return slices.Clone(v)
	default:
		return nil
	}
}

// v22Renames maps the raw keys an ID3v2.2 tag exposes to the ID3v2.4 frame
// that holds the value once the tag is rewritten. The reader normalizes the
// year frame to TYER and keeps the other 3-character text frames under their
// ID3v2.2 IDs.
var v22Renames = map[string]string{
	"TYER": "TDRC",
	"TBP":  "TBPM", "TCP": "TCMP", "TCR": "TCOP", "TDY": "TDLY", "TEN": "TENC",
	"TFT": "TFLT", "TKE": "TKEY", "TLA": "TLAN", "TLE": "TLEN", "TMT": "TMED",
	"TOA": "TOPE", "TOF": "TOFN", "TOL": "TOLY", "TOR": "TDOR", "TOT": "TOAL",
	"TP3": "TPE3", "TP4": "TPE4", "TPB": "TPUB", "TRC": "TSRC", "TS2": "TSO2",
	"TSA": "TSOA", "TSC": "TSOC", "TSP": "TSOP", "TSS": "TSSE", "TST": "TSOT",
	"TXT": "TEXT",
}

// landedKey returns the raw key an edit of key reads back under. A v2.2 tag is
// rewritten as v2.4; (*wtag).normalizeKey moves edits of v2.3-only keys to
// their v2.4 frames. Keep this table in sync with it.
func landedKey(format Format, data []byte, key string) string {
	if hasID3v22Tag(format, data) {
		if renamed, ok := v22Renames[key]; ok {
			return renamed
		}
	}
	return key
}

// hasID3v22Tag reports whether the ID3v2 tag Write edits in data, at the start
// of an MP3 or in the ID3 chunk of a WAV or AIFF file, is version 2.2.
func hasID3v22Tag(format Format, data []byte) bool {
	if format == FormatMP3 {
		return bytes.HasPrefix(data, []byte("ID3\x02"))
	}
	return (format == FormatWAV || format == FormatAIFF) && id3ChunkIsV22(data)
}

// id3ChunkIsV22 walks the chunks of a RIFF, RIFX, RF64 or FORM file and reports
// whether its "id3 " or "ID3 " chunk starts an ID3v2.2 tag.
func id3ChunkIsV22(data []byte) bool {
	if len(data) < 12 {
		return false
	}
	order := binary.ByteOrder(binary.LittleEndian)
	if m := string(data[:4]); m == "RIFX" || m == "FORM" {
		order = binary.BigEndian
	}
	for off := 12; off+8 <= len(data); {
		id, size := string(data[off:off+4]), int(order.Uint32(data[off+4:off+8]))
		if size < 0 || size > len(data)-off-8 {
			return false // truncated, or an RF64 placeholder size
		}
		body := data[off+8 : off+8+size]
		if strings.EqualFold(id, "id3 ") {
			return bytes.HasPrefix(body, []byte("ID3\x02"))
		}
		off += 8 + size + size&1
	}
	return false
}

// landedExempt names why an edit of field (0 for a raw key) to values may
// legitimately not read back, or returns "". Each case is a debt with an owner.
func landedExempt(format Format, data []byte, field types.Field, values []string) string {
	switch {
	case len(values) == 0 && format == FormatMP3 && hasID3v1(data):
		// Known finding 3 (plan 058): clearing a field on an MP3 with an ID3v1
		// trailer reads back the ID3v1 value, which Write does not update
		// (documented in the README; plan 079 owns it).
		return "cleared field shadowed by the ID3v1 trailer"
	}
	return ""
}

// hasID3v1 reports whether data ends in an ID3v1 tag.
func hasID3v1(data []byte) bool {
	return len(data) >= 128 && string(data[len(data)-128:][:3]) == "TAG"
}
