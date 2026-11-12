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

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/registry"
	"github.com/simonhull/audiometa/internal/testbudget"
	"github.com/simonhull/audiometa/internal/trailer"
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
	"testdata/sample.ogg",
	"testdata/sample_retagged.ogg",
	"testdata/sample.opus",
	"testdata/sample_retagged.opus",
	"testdata/bench/book.opus",
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
	// A raw TORY edit on an ID3v2.2 tag lands in TDOR (plan 077).
	f.Add([]byte("ID3\x0200\x00\x00\x00\x000"), []byte("0X00"))
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
		// MP3 writes also update an existing ID3v1 trailer, so no edit can be
		// shadowed by a stale trailer.
		if c, ok := tw.(registry.ID3v1Configurer); ok {
			tw = c.WithID3v1(registry.ID3v1Update)
		}

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
		testbudget.Check(t, budget, len(data), "WriteFile", func() {
			_, err = writeSafely(ctx, tw, &out, file.reader, file.src.size, file.src.path, edits)
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
		if _, err := writeSafely(ctx, tw, &again, file.reader, file.src.size, file.src.path, edits); err != nil {
			t.Fatalf("second write: %v", err)
		}
		if !bytes.Equal(again.Bytes(), out.Bytes()) {
			t.Fatal("second write is not byte-identical")
		}
		checkLanded(t, file.Format, data, &p.file.Tags, &q.file.Tags, edits)
		checkID3v1Trailer(t, file.Format, data, out.Bytes(), &file.Tags)
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
		if got := final.Get(landedKey(format, data, e.Key)); !slices.Equal(got, e.Values) {
			t.Errorf("%v: key %q = %q, want %q", format, e.Key, got, e.Values)
		}
	}
	for _, e := range edits.Fields {
		if got := typedValues(final, e.Field); !slices.Equal(got, e.Values) {
			t.Errorf("%v: field %v = %q, want %q", format, e.Field, got, e.Values)
		}
	}

	touched := map[string]bool{}
	if len(edits.Artwork) > 0 {
		touched["METADATA_BLOCK_PICTURE"] = true // Ogg Vorbis and Opus store pictures as a comment
	}
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
// year frame to TYER and the original year frame to TORY, which are v2.3-only,
// and keeps the other 3-character text frames under their ID3v2.2 IDs unless
// it has an ID3v2.3 equivalent (TRC, TCR, TPB read as TSRC, TCOP, TPUB).
var v22Renames = map[string]string{
	"TYER": "TDRC", "TORY": "TDOR",
	"TBP": "TBPM", "TCP": "TCMP", "TDY": "TDLY", "TEN": "TENC",
	"TFT": "TFLT", "TKE": "TKEY", "TLA": "TLAN", "TLE": "TLEN", "TMT": "TMED",
	"TOA": "TOPE", "TOF": "TOFN", "TOL": "TOLY", "TOT": "TOAL",
	"TP3": "TPE3", "TP4": "TPE4", "TS2": "TSO2",
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

// checkID3v1Trailer fails t when an MP3 that ended in an ID3v1 trailer no
// longer ends in one that holds final's title, artist, album, comment, year
// and track number, within the ID3v1 field widths. The expected values come
// from this independent encoding of the format's rules, not from the writer.
func checkID3v1Trailer(t *testing.T, format Format, data, out []byte, final *Tags) {
	t.Helper()
	if format != FormatMP3 || !endsInID3v1(t, data) {
		return
	}
	if !endsInID3v1(t, out) {
		t.Fatal("the output lost the ID3v1 trailer")
	}
	tr := out[len(out)-128:]
	text := func(name string, from, to int, want string) {
		t.Helper()
		if got, exp := id3v1Field(tr[from:to]), id3v1Expected(want, to-from); got != exp {
			t.Errorf("ID3v1 %s = %q, want %q (from %q)", name, got, exp, want)
		}
	}
	text("title", 3, 33, final.Title)
	text("artist", 33, 63, final.Artist)
	text("album", 63, 93, final.Album)
	commentEnd := 127
	if final.TrackNumber >= 1 && final.TrackNumber <= 255 {
		commentEnd = 125
		if tr[125] != 0 || int(tr[126]) != final.TrackNumber {
			t.Errorf("ID3v1 track bytes = %d, %d, want 0, %d", tr[125], tr[126], final.TrackNumber)
		}
	}
	text("comment", 97, commentEnd, final.Comment)
	wantYear := make([]byte, 4)
	if final.Year >= 1900 && final.Year <= 2100 {
		wantYear = []byte(strconv.Itoa(final.Year))
	}
	if !bytes.Equal(tr[93:97], wantYear) {
		t.Errorf("ID3v1 year = %q, want %q", tr[93:97], wantYear)
	}
}

// endsInID3v1 reports whether data, an MP3 file, ends in an ID3v1 trailer that
// follows its ID3v2 tag.
func endsInID3v1(t *testing.T, data []byte) bool {
	t.Helper()
	var start int64
	if len(data) >= 10 && string(data[:3]) == "ID3" {
		start = 10 + int64(data[6])<<21 | int64(data[7])<<14 | int64(data[8])<<7 | int64(data[9])
		if data[3] == 4 && data[5]&0x10 != 0 {
			start += 10
		}
	}
	size := int64(len(data))
	if size-start < 128 {
		return false
	}
	ok, err := trailer.EndsWithID3v1(binutil.NewSafeReader(bytes.NewReader(data), size, ""), start, size)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

// id3v1Field decodes an ID3v1 text field: cut at the first NUL, read as
// ISO-8859-1, surrounding white space removed.
func id3v1Field(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	runes := make([]rune, len(b))
	for i, c := range b {
		runes[i] = rune(c)
	}
	return strings.TrimSpace(string(runes))
}

// id3v1Expected returns what an ID3v1 field of width bytes reads back as for
// s: ISO-8859-1 with '?' for other characters, cut to width and at a NUL.
func id3v1Expected(s string, width int) string {
	var b []byte
	for _, r := range s {
		if r == 0 || len(b) == width {
			break
		}
		if r > 0xFF {
			r = '?'
		}
		b = append(b, byte(r))
	}
	return id3v1Field(b)
}
