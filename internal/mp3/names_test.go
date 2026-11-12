package mp3

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

const (
	picardAlbumID = "MusicBrainz Album Id"
	upperAlbumID  = "MUSICBRAINZ ALBUM ID"
	mbOwner       = "http://musicbrainz.org"
	mbUFIDKey     = "UFID:http://musicbrainz.org"
	recordingID   = "8f3471b5-7e6a-48da-86a9-c1c07a0f47ae"
)

// ufidPayload builds a UFID payload: [owner][NUL][identifier].
func ufidPayload(owner, id string) []byte {
	return slices.Concat([]byte(owner), []byte{0}, []byte(id))
}

// txxxDescriptions lists the descriptions of data's TXXX frames, in order.
func txxxDescriptions(t *testing.T, data []byte) []string {
	t.Helper()
	wt := framesOf(t, data)
	out := make([]string, 0, len(framesWithID(wt.frames, frameIDTXXX)))
	for _, f := range framesWithID(wt.frames, frameIDTXXX) {
		d, _ := wt.txxxDescription(f)
		out = append(out, d)
	}
	return out
}

// newTXXXEncoding is the encoding the writer picks for ASCII text in version.
func newTXXXEncoding(version byte) byte {
	if version == 4 {
		return encUTF8
	}
	return encLatin1
}

func TestTXXX_ReadKeepsDescriptionSpelling(t *testing.T) {
	t.Parallel()
	for _, v := range []byte{3, 4} {
		f := parseBytes(t, file(id3Tag(v, id3Frame(v, frameIDTXXX, txxxPayload(0, picardAlbumID, "id")))))
		if got := f.Tags.Get(upperAlbumID); !slices.Equal(got, []string{"id"}) {
			t.Errorf("v2.%d: Get(%q) = %q", v, upperAlbumID, got)
		}
		if got := f.Tags.Name(upperAlbumID); got != picardAlbumID {
			t.Errorf("v2.%d: Name = %q, want %q", v, got, picardAlbumID)
		}
	}
}

func TestApply_TXXXSpelling(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		existing string // the description the file has, if any
		set      func(*types.Tags)
		want     string
	}{
		{"new frame takes the caller's spelling", "", func(tg *types.Tags) { tg.SetNamed(picardAlbumID, "new") }, picardAlbumID},
		{"new frame set by key is upper-case", "", func(tg *types.Tags) { tg.Set(upperAlbumID, "new") }, upperAlbumID},
		{"upper-case frame stays upper-case", upperAlbumID, func(tg *types.Tags) { tg.SetNamed(picardAlbumID, "new") }, upperAlbumID},
		{"Picard frame keeps its spelling", picardAlbumID, func(tg *types.Tags) { tg.Set(upperAlbumID, "new") }, picardAlbumID},
		{"Picard frame keeps its spelling by name", picardAlbumID, func(tg *types.Tags) { tg.SetNamed(picardAlbumID, "new") }, picardAlbumID},
	}
	for _, tt := range tests {
		for _, v := range []byte{3, 4} {
			frames := [][]byte{id3Frame(v, "TIT2", latin1Text("T"))}
			if tt.existing != "" {
				frames = append(frames, id3Frame(v, frameIDTXXX, txxxPayload(0, tt.existing, "old")))
			}
			src := file(id3Tag(v, frames...))
			out := writeBytes(t, src, editsFor(t, src, tt.set))
			txxx := framesWithID(framesOf(t, out).frames, frameIDTXXX)
			want := txxxPayload(newTXXXEncoding(v), tt.want, "new")
			if len(txxx) != 1 || !bytes.Equal(txxx[0].payload, want) {
				t.Errorf("%s, v2.%d: TXXX frames %+v, want one %q", tt.name, v, txxx, want)
			}
			if got := parseBytes(t, out).Tags.Name(upperAlbumID); got != tt.want {
				t.Errorf("%s, v2.%d: Name after write = %q, want %q", tt.name, v, got, tt.want)
			}
		}
	}
}

// A typed-field write names a new MusicBrainz TXXX frame as Picard does; a
// raw-key write keeps the upper-case key unless the caller named it, and an
// existing frame always keeps its own spelling.
func TestApply_TXXXTypedFieldSpelling(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		existing string
		set      func(*types.Tags)
		want     string
	}{
		{"album ID", "", func(tg *types.Tags) { tg.MusicBrainzAlbumID = "new" }, picardAlbumID},
		{"artist ID", "", func(tg *types.Tags) { tg.MusicBrainzArtistID = "new" }, "MusicBrainz Artist Id"},
		{"existing upper-case frame", upperAlbumID, func(tg *types.Tags) { tg.MusicBrainzAlbumID = "new" }, upperAlbumID},
		{"existing Picard frame", picardAlbumID, func(tg *types.Tags) { tg.MusicBrainzAlbumID = "new" }, picardAlbumID},
		{"existing lower-case frame", "musicbrainz album id", func(tg *types.Tags) { tg.MusicBrainzAlbumID = "new" }, "musicbrainz album id"},
		{"barcode", "", func(tg *types.Tags) { tg.Barcode = "new" }, "BARCODE"},
		{"catalog number", "", func(tg *types.Tags) { tg.CatalogNumber = "new" }, "CATALOGNUMBER"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			for _, v := range []byte{3, 4} {
				frames := [][]byte{id3Frame(v, "TIT2", latin1Text("T"))}
				if tt.existing != "" {
					frames = append(frames, id3Frame(v, frameIDTXXX, txxxPayload(0, tt.existing, "old")))
				}
				src := file(id3Tag(v, frames...))
				out := writeBytes(t, src, editsFor(t, src, tt.set))
				if got := txxxDescriptions(t, out); !slices.Equal(got, []string{tt.want}) {
					t.Errorf("v2.%d: TXXX descriptions %q, want [%q]", v, got, tt.want)
				}
			}
		})
	}
}

func TestApply_TypedUFID(t *testing.T) {
	t.Parallel()
	for _, v := range []byte{3, 4} {
		src := file(id3Tag(v, id3Frame(v, "TIT2", latin1Text("T"))))
		out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.MusicBrainzTrackID = recordingID }))
		ufid := framesWithID(framesOf(t, out).frames, frameIDUFID)
		if len(ufid) != 1 || !bytes.Equal(ufid[0].payload, ufidPayload(mbOwner, recordingID)) {
			t.Errorf("v2.%d: UFID frames %+v", v, ufid)
		}
		if got := parseBytes(t, out).Tags.MusicBrainzTrackID; got != recordingID {
			t.Errorf("v2.%d: MusicBrainzTrackID = %q", v, got)
		}

		// Replace in place, then clear.
		src = out
		out = writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.MusicBrainzTrackID = "other" }))
		if got := parseBytes(t, out).Tags.Get(mbUFIDKey); !slices.Equal(got, []string{"other"}) {
			t.Errorf("v2.%d: replaced UFID = %q", v, got)
		}
		out = writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.MusicBrainzTrackID = "" }))
		if ufid := framesWithID(framesOf(t, out).frames, frameIDUFID); len(ufid) != 0 {
			t.Errorf("v2.%d: cleared, UFID frames %+v", v, ufid)
		}
		if got := txxxDescriptions(t, out); len(got) != 0 {
			t.Errorf("v2.%d: wrote TXXX frames %q", v, got)
		}

		// Refused, never truncated.
		long := strings.Repeat("a", maxUFIDIdentifier+1)
		if _, err := tryWrite(src, editsFor(t, src, func(tg *types.Tags) { tg.MusicBrainzTrackID = long })); !errors.Is(err, types.ErrUnsupportedWrite) {
			t.Errorf("v2.%d: %d-byte identifier: err = %v, want ErrUnsupportedWrite", v, len(long), err)
		}
	}
}

func TestApply_OriginalDate(t *testing.T) {
	t.Parallel()
	text := func(version byte, id, v string) []byte { return id3Frame(version, id, latin1Text(v)) }
	set := func(v string) func(*types.Tags) { return func(tg *types.Tags) { tg.OriginalDate = v } }
	tests := []struct {
		name       string
		tag        []byte
		mutate     func(*types.Tags)
		tdor, tory []string
		err        bool
	}{
		{"v2.4 writes the timestamp", id3Tag(4, text(4, "TIT2", "T")), set("2001-05-03"), []string{"2001-05-03"}, nil, false},
		{"v2.4 drops TORY", id3Tag(4, text(4, "TIT2", "T"), text(4, "TORY", "1999")), set("2001-05-03"), []string{"2001-05-03"}, nil, false},
		{"v2.4 keeps a present TDOR", id3Tag(4, text(4, "TDOR", "1999")), set("2001"), []string{"2001"}, nil, false},
		{"v2.4 clears both", id3Tag(4, text(4, "TDOR", "1999"), text(4, "TORY", "1998")), set(""), nil, nil, false},
		{"v2.3 writes the year", id3Tag(3, text(3, "TIT2", "T")), set("2001-05-03"), nil, []string{"2001"}, false},
		{"v2.3 replaces TORY", id3Tag(3, text(3, "TORY", "1999")), set("2002"), nil, []string{"2002"}, false},
		{"v2.3 clears", id3Tag(3, text(3, "TORY", "1999")), set(""), nil, nil, false},
		{"v2.3 refuses a value with no year", id3Tag(3, text(3, "TORY", "1999")), set("May 2001"), nil, nil, true},
		{"v2.3 refuses a short year", id3Tag(3, text(3, "TIT2", "T")), set("99"), nil, nil, true},
		{"v2.2 source writes TDOR", id3Tag(2, id3v22Frame("TOR", latin1Text("1999"))), set("2001-05-03"), []string{"2001-05-03"}, nil, false},
		{
			"v2.2 source: raw TORY edit becomes TDOR", id3Tag(2, id3v22Frame("TOR", latin1Text("1999"))),
			func(tg *types.Tags) { tg.Set("TORY", "2000") },
			[]string{"2000"},
			nil, false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src := file(tt.tag)
			out, err := tryWrite(src, editsFor(t, src, tt.mutate))
			if tt.err {
				if !errors.Is(err, types.ErrUnsupportedWrite) {
					t.Fatalf("err = %v, want ErrUnsupportedWrite", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			tags := parseBytes(t, out).Tags
			if got := tags.Get("TDOR"); !slices.Equal(got, tt.tdor) {
				t.Errorf("TDOR = %q, want %q", got, tt.tdor)
			}
			if got := tags.Get("TORY"); !slices.Equal(got, tt.tory) {
				t.Errorf("TORY = %q, want %q", got, tt.tory)
			}
			want := ""
			if len(tt.tdor) > 0 {
				want = tt.tdor[0]
			} else if len(tt.tory) > 0 {
				want = tt.tory[0]
			}
			if tags.OriginalDate != want {
				t.Errorf("OriginalDate = %q, want %q", tags.OriginalDate, want)
			}
		})
	}
}

// Frames whose descriptions differ only in case share one raw key. Reading
// gives every value and the first frame's spelling; writing the key makes them
// one frame, named and placed as the first was; an edit to another key leaves
// them alone.
func TestApply_TXXXCaseDuplicates(t *testing.T) {
	t.Parallel()
	for _, v := range []byte{3, 4} {
		for _, order := range [][2]string{{upperAlbumID, picardAlbumID}, {picardAlbumID, upperAlbumID}} {
			src := file(id3Tag(v,
				id3Frame(v, frameIDTXXX, txxxPayload(0, order[0], "a")),
				id3Frame(v, "TIT2", latin1Text("T")),
				id3Frame(v, frameIDTXXX, txxxPayload(0, order[1], "b")),
			))
			tags := parseBytes(t, src).Tags
			if got := tags.Get(upperAlbumID); !slices.Equal(got, []string{"a", "b"}) {
				t.Errorf("v2.%d %q: Get = %q, want [a b]", v, order, got)
			}
			if got := tags.Name(upperAlbumID); got != order[0] {
				t.Errorf("v2.%d %q: Name = %q, want the first", v, order, got)
			}

			out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Set("TIT2", "U") }))
			if got := txxxDescriptions(t, out); !slices.Equal(got, order[:]) {
				t.Errorf("v2.%d %q: an edit elsewhere changed the frames to %q", v, order, got)
			}

			out = writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.SetNamed(picardAlbumID, "c") }))
			after := framesOf(t, out).frames
			if !slices.Equal(frameIDs(after), []string{frameIDTXXX, "TIT2"}) {
				t.Errorf("v2.%d %q: frames %v, want the merged TXXX where the first was", v, order, frameIDs(after))
			}
			if want := txxxPayload(newTXXXEncoding(v), order[0], "c"); !bytes.Equal(after[0].payload, want) {
				t.Errorf("v2.%d %q: TXXX %q, want %q", v, order, after[0].payload, want)
			}
		}
	}
}

func TestUFID_Read(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		tag  []byte
		want []string
	}{
		{"v2.3", id3Tag(3, id3Frame(3, frameIDUFID, ufidPayload(mbOwner, recordingID))), []string{recordingID}},
		{"v2.4", id3Tag(4, id3Frame(4, frameIDUFID, ufidPayload(mbOwner, recordingID))), []string{recordingID}},
		{"v2.2", id3Tag(2, id3v22Frame("UFI", ufidPayload(mbOwner, recordingID))), []string{recordingID}},
		{"binary identifier", id3Tag(4, id3Frame(4, frameIDUFID, ufidPayload(mbOwner, "\xff\x00\x01"))), nil},
		{"no owner", id3Tag(4, id3Frame(4, frameIDUFID, ufidPayload("", recordingID))), nil},
		{"no identifier", id3Tag(4, id3Frame(4, frameIDUFID, ufidPayload(mbOwner, ""))), nil},
	}
	for _, tt := range tests {
		f := parseBytes(t, file(tt.tag))
		if got := f.Tags.Get(mbUFIDKey); !slices.Equal(got, tt.want) {
			t.Errorf("%s: Get(%q) = %q, want %q", tt.name, mbUFIDKey, got, tt.want)
		}
	}
}

func TestApply_UFID(t *testing.T) {
	t.Parallel()
	const other = "http://example.org"
	for _, v := range []byte{3, 4} {
		// New: appended, read back under its key.
		src := file(id3Tag(v, id3Frame(v, "TIT2", latin1Text("T"))))
		out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Set(mbUFIDKey, recordingID) }))
		ufid := framesWithID(framesOf(t, out).frames, frameIDUFID)
		if len(ufid) != 1 || !bytes.Equal(ufid[0].payload, ufidPayload(mbOwner, recordingID)) {
			t.Errorf("v2.%d new: UFID frames %+v", v, ufid)
		}
		if got := parseBytes(t, out).Tags.Get(mbUFIDKey); !slices.Equal(got, []string{recordingID}) {
			t.Errorf("v2.%d new: read back %q", v, got)
		}
		if got := txxxDescriptions(t, out); len(got) != 0 {
			t.Errorf("v2.%d new: wrote TXXX frames %q", v, got)
		}

		// Replace: in place, other owners untouched.
		src = file(id3Tag(v,
			id3Frame(v, frameIDUFID, ufidPayload(other, "x")),
			id3Frame(v, frameIDUFID, ufidPayload(mbOwner, "old")),
			id3Frame(v, "TIT2", latin1Text("T")),
		))
		out = writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Set(mbUFIDKey, recordingID) }))
		after := framesOf(t, out).frames
		if !slices.Equal(frameIDs(after), []string{frameIDUFID, frameIDUFID, "TIT2"}) ||
			!bytes.Equal(after[0].payload, ufidPayload(other, "x")) ||
			!bytes.Equal(after[1].payload, ufidPayload(mbOwner, recordingID)) {
			t.Errorf("v2.%d replace: frames %+v", v, after)
		}

		// Delete: only that owner's frame goes.
		out = writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Delete(mbUFIDKey) }))
		after = framesOf(t, out).frames
		if !slices.Equal(frameIDs(after), []string{frameIDUFID, "TIT2"}) || !bytes.Equal(after[0].payload, ufidPayload(other, "x")) {
			t.Errorf("v2.%d delete: frames %+v", v, after)
		}
	}
}

func TestApply_UFIDKeepsBinaryIdentifiers(t *testing.T) {
	t.Parallel()
	binaryID := ufidPayload(mbOwner, "\xff\x00\x01")
	src := file(id3Tag(4, id3Frame(4, frameIDUFID, binaryID), id3Frame(4, "TIT2", latin1Text("T"))))
	out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Set("TIT2", "U") }))
	if ufid := framesWithID(framesOf(t, out).frames, frameIDUFID); len(ufid) != 1 || !bytes.Equal(ufid[0].payload, binaryID) {
		t.Errorf("UFID frames %+v, want the binary one kept", ufid)
	}
}

func TestApply_UFIDRefused(t *testing.T) {
	t.Parallel()
	src := file(id3Tag(4, id3Frame(4, "TIT2", latin1Text("T"))))
	tests := map[string]func(*types.Tags){
		"two identifiers":   func(tg *types.Tags) { tg.Set(mbUFIDKey, "a", "b") },
		"long identifier":   func(tg *types.Tags) { tg.Set(mbUFIDKey, strings.Repeat("a", maxUFIDIdentifier+1)) },
		"empty identifier":  func(tg *types.Tags) { tg.Set(mbUFIDKey, "") },
		"empty owner":       func(tg *types.Tags) { tg.Set("UFID:", recordingID) },
		"non-ASCII owner":   func(tg *types.Tags) { tg.Set("UFID:http://exämple.org", recordingID) },
		"bare UFID as TXXX": func(tg *types.Tags) { tg.Set("UFID", recordingID) },
	}
	for name, set := range tests {
		if _, err := tryWrite(src, editsFor(t, src, set)); !errors.Is(err, types.ErrUnsupportedWrite) {
			t.Errorf("%s: err = %v, want ErrUnsupportedWrite", name, err)
		}
	}
	if _, err := tryWrite(src, editsFor(t, src, func(tg *types.Tags) {
		tg.Set(mbUFIDKey, strings.Repeat("a", maxUFIDIdentifier))
	})); err != nil {
		t.Errorf("64-byte identifier: %v", err)
	}
}
