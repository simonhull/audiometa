package audiometa_test

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"image"
	"image/png"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/simonhull/audiometa"
	"github.com/simonhull/audiometa/internal/types"
)

// roundTripCase is one corpus file of the write round-trip harness.
type roundTripCase struct {
	path     string
	writable bool // flipped to true by the plan that adds the format's writer
}

var roundTripCorpus = []roundTripCase{
	{"testdata/sample.mp3", true},
	{"testdata/sample_mono_vbr.mp3", true},
	{"testdata/sample_mpeg2.mp3", true},
	{"testdata/sample_v24.mp3", true}, // ID3v2.4 with an ID3v1 trailer
	{"testdata/sample.flac", true},
	{"testdata/sample_upper.flac", true},
	{"testdata/sample.m4a", true},
	{"testdata/sample.m4b", true},
	{"testdata/sample_faststart.m4a", true},
	{"testdata/sample_faststart.m4b", true},
	{"testdata/bench/book.mp3", true}, // 50 chapters, rich tags
	{"testdata/bench/book.flac", true},
	{"testdata/bench/book.m4b", true},
	{"testdata/bench/book.opus", false}, // deferred format: stays false
	{"testdata/sample.ogg", false},      // deferred format: stays false
	{"testdata/sample.opus", false},     // deferred format: stays false
}

// tagEdit is one format-agnostic edit; unsupported lists formats where it must
// return ErrUnsupportedWrite. A non-empty formats or paths restricts the edit
// to those formats or corpus files.
type tagEdit struct {
	name        string
	fields      []string // Go names of the Tags fields the edit sets or clears
	apply       func(f *audiometa.File) error
	check       func(t *testing.T, f *audiometa.File) // asserts on the re-opened file
	unsupported []audiometa.Format
	formats     []audiometa.Format
	paths       []string
}

// appliesTo reports whether e runs on the corpus file path of format.
func (e tagEdit) appliesTo(path string, format audiometa.Format) bool {
	return (len(e.formats) == 0 || slices.Contains(e.formats, format)) &&
		(len(e.paths) == 0 || slices.Contains(e.paths, path))
}

var longComment = strings.Repeat("0123456789abcdef", 256) // 4096 characters

var roundTripEdits = []tagEdit{
	{
		name:    "set REPLAYGAIN_TRACK_GAIN",
		apply:   func(f *audiometa.File) error { f.Tags.Set("REPLAYGAIN_TRACK_GAIN", "-6.00 dB"); return nil },
		formats: []audiometa.Format{audiometa.FormatMP3, audiometa.FormatFLAC, audiometa.FormatM4A, audiometa.FormatM4B},
		check: func(t *testing.T, f *audiometa.File) {
			if rg := f.Audio.ReplayGain; rg == nil || rg.TrackGain != -6.0 {
				t.Errorf("ReplayGain = %+v, want TrackGain -6", rg)
			}
		},
	},
	{
		name:   "set title",
		fields: []string{"Title"},
		apply:  func(f *audiometa.File) error { f.Tags.Title = "Nouveau titre ✓ 日本語"; return nil },
		check: func(t *testing.T, f *audiometa.File) {
			if f.Tags.Title != "Nouveau titre ✓ 日本語" {
				t.Errorf("Title = %q", f.Tags.Title)
			}
		},
	},
	{
		name:   "clear album",
		fields: []string{"Album"},
		apply:  func(f *audiometa.File) error { f.Tags.Album = ""; return nil },
		check: func(t *testing.T, f *audiometa.File) {
			if f.Tags.Album != "" {
				t.Errorf("Album = %q, want cleared", f.Tags.Album)
			}
		},
	},
	{
		name:   "set artists",
		fields: []string{"Artist", "Artists"},
		apply: func(f *audiometa.File) error {
			f.Tags.Artists = []string{"A", "B"}
			f.Tags.Artist = "A"
			return nil
		},
		check: func(t *testing.T, f *audiometa.File) {
			if f.Tags.Artist != "A" || !slices.Equal(f.Tags.Artists, []string{"A", "B"}) {
				t.Errorf("Artist = %q, Artists = %q", f.Tags.Artist, f.Tags.Artists)
			}
		},
	},
	{
		name:   "long comment",
		fields: []string{"Comment"},
		apply:  func(f *audiometa.File) error { f.Tags.Comment = longComment; return nil },
		check: func(t *testing.T, f *audiometa.File) {
			if f.Tags.Comment != longComment {
				t.Errorf("Comment has %d characters, want %d", len(f.Tags.Comment), len(longComment))
			}
		},
	},
	{
		name:   "set year",
		fields: []string{"Year"},
		apply:  func(f *audiometa.File) error { f.Tags.Year = 1999; return nil },
		check: func(t *testing.T, f *audiometa.File) {
			if f.Tags.Year != 1999 {
				t.Errorf("Year = %d, want 1999", f.Tags.Year)
			}
		},
	},
}

// roundTripMP3Edits are edits of MP3-native keys.
var roundTripMP3Edits = []tagEdit{
	{
		name:    "set NARRATOR",
		fields:  []string{"Narrator"},
		apply:   func(f *audiometa.File) error { f.Tags.Set("NARRATOR", "Reader"); return nil },
		formats: []audiometa.Format{audiometa.FormatMP3},
		check: func(t *testing.T, f *audiometa.File) {
			if f.Tags.Narrator != "Reader" {
				t.Errorf("Narrator = %q, want Reader", f.Tags.Narrator)
			}
		},
	},
	{
		// Only on a file without an ID3v1 trailer, which would supply a fallback title.
		name:    "delete TIT2",
		fields:  []string{"Title"},
		apply:   func(f *audiometa.File) error { f.Tags.Delete("TIT2"); return nil },
		formats: []audiometa.Format{audiometa.FormatMP3},
		paths:   []string{"testdata/sample.mp3"},
		check: func(t *testing.T, f *audiometa.File) {
			if f.Tags.Title != "" {
				t.Errorf("Title = %q, want empty", f.Tags.Title)
			}
		},
	},
}

// roundTripFLACEdits are edits of Vorbis comment keys and fields only the
// FLAC harness checks.
var roundTripFLACEdits = []tagEdit{
	{
		name:    "set lyrics",
		fields:  []string{"Lyrics"},
		apply:   func(f *audiometa.File) error { f.Tags.Lyrics = "la la"; return nil },
		formats: []audiometa.Format{audiometa.FormatFLAC},
		check: func(t *testing.T, f *audiometa.File) {
			if f.Tags.Lyrics != "la la" {
				t.Errorf("Lyrics = %q, want la la", f.Tags.Lyrics)
			}
		},
	},
}

// roundTripM4AEdits are edits of M4A-native keys and typed-only fields.
var roundTripM4AEdits = []tagEdit{
	{
		name:    "set NARRATOR freeform",
		fields:  []string{"Narrator"},
		apply:   func(f *audiometa.File) error { f.Tags.Set("NARRATOR", "Reader"); return nil },
		formats: []audiometa.Format{audiometa.FormatM4A, audiometa.FormatM4B},
		check: func(t *testing.T, f *audiometa.File) {
			if f.Tags.Narrator != "Reader" {
				t.Errorf("Narrator = %q, want Reader", f.Tags.Narrator)
			}
		},
	},
	{
		name:    "set track 3/12",
		fields:  []string{"TrackNumber", "TrackTotal"},
		apply:   func(f *audiometa.File) error { f.Tags.TrackNumber, f.Tags.TrackTotal = 3, 12; return nil },
		formats: []audiometa.Format{audiometa.FormatM4A, audiometa.FormatM4B},
		check: func(t *testing.T, f *audiometa.File) {
			if f.Tags.TrackNumber != 3 || f.Tags.TrackTotal != 12 {
				t.Errorf("track = %d/%d, want 3/12", f.Tags.TrackNumber, f.Tags.TrackTotal)
			}
		},
	},
}

var roundTripArtworkEdits = []tagEdit{
	{
		name: "replace front cover",
		apply: func(f *audiometa.File) error {
			return f.ReplaceArtwork(audiometa.Artwork{Data: onePixelPNG(), Type: audiometa.ArtworkFrontCover})
		},
		check: func(t *testing.T, f *audiometa.File) {
			art := extract(t, f)
			fronts := 0
			for _, a := range art {
				if a.Type == audiometa.ArtworkFrontCover {
					fronts++
					if !bytes.Equal(a.Data, onePixelPNG()) {
						t.Error("front cover is not the new image")
					}
				}
			}
			if fronts != 1 {
				t.Errorf("%d front covers, want 1", fronts)
			}
		},
	},
	{
		name:  "remove all artwork",
		apply: func(f *audiometa.File) error { f.RemoveAllArtwork(); return nil },
		check: func(t *testing.T, f *audiometa.File) {
			if art := extract(t, f); len(art) != 0 {
				t.Errorf("%d images left, want 0", len(art))
			}
		},
	},
	{
		name: "add back cover",
		apply: func(f *audiometa.File) error {
			return f.AddArtwork(audiometa.Artwork{Data: onePixelPNG(), Type: audiometa.ArtworkBackCover})
		},
		check: func(t *testing.T, f *audiometa.File) {
			art := extract(t, f)
			if len(art) == 0 || art[len(art)-1].Type != audiometa.ArtworkBackCover || !bytes.Equal(art[len(art)-1].Data, onePixelPNG()) {
				t.Error("back cover not appended")
			}
		},
		unsupported: []audiometa.Format{audiometa.FormatM4A, audiometa.FormatM4B},
	},
}

// onePixelPNG returns a 1x1 grayscale PNG.
func onePixelPNG() []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, 1, 1))); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func extract(t *testing.T, f *audiometa.File) []audiometa.Artwork {
	t.Helper()
	art, err := f.ExtractArtwork()
	if err != nil {
		t.Fatal(err)
	}
	return art
}

func openFile(t *testing.T, path string) *audiometa.File {
	t.Helper()
	f, err := audiometa.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestWriteRoundTrip_Corpus(t *testing.T) {
	for _, tc := range roundTripCorpus {
		t.Run(filepath.Base(tc.path), func(t *testing.T) {
			if !tc.writable {
				f := openFile(t, tc.path)
				if _, err := f.WriteTo(io.Discard); !errors.Is(err, audiometa.ErrUnsupportedWrite) {
					t.Errorf("WriteTo err = %v, want ErrUnsupportedWrite", err)
				}
				return
			}
			src := readTestdata(t, tc.path)

			var buf bytes.Buffer
			if _, err := openFile(t, tc.path).WriteTo(&buf); err != nil {
				t.Fatalf("no-edit WriteTo: %v", err)
			}
			if !bytes.Equal(buf.Bytes(), src) {
				t.Error("no-edit WriteTo is not byte-identical to the source")
			}

			for _, e := range slices.Concat(roundTripEdits, roundTripMP3Edits, roundTripFLACEdits, roundTripM4AEdits, roundTripArtworkEdits) {
				t.Run(e.name, func(t *testing.T) { runRoundTrip(t, tc.path, src, e) })
			}
			t.Run("delete keys", func(t *testing.T) { runDeleteRoundTrip(t, tc.path, src) })
		})
	}
}

// runRoundTrip applies e to a fresh File, writes it and checks the re-opened result.
func runRoundTrip(t *testing.T, path string, src []byte, e tagEdit) {
	t.Helper()
	orig := openFile(t, path)
	if !e.appliesTo(path, orig.Format) {
		t.Skipf("%s does not apply to %s", e.name, path)
	}
	f := openFile(t, path)
	if err := e.apply(f); err != nil {
		t.Fatalf("apply: %v", err)
	}
	var out bytes.Buffer
	_, err := f.WriteTo(&out)
	if slices.Contains(e.unsupported, f.Format) {
		if !errors.Is(err, audiometa.ErrUnsupportedWrite) {
			t.Errorf("err = %v, want ErrUnsupportedWrite", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	reopened := writeAndReopen(t, path, out.Bytes())
	e.check(t, reopened)
	assertPreserved(t, orig, f, reopened, src, out.Bytes(), e.fields)

	var again bytes.Buffer
	if _, err := f.WriteTo(&again); err != nil {
		t.Fatalf("second WriteTo: %v", err)
	}
	if !bytes.Equal(again.Bytes(), out.Bytes()) {
		t.Error("second WriteTo of the same File is not byte-identical")
	}
}

// customKeys is a raw key with no typed field, per format family.
var customKeys = map[audiometa.Format]string{
	audiometa.FormatMP3:  "AUDIOMETA_TEST", // TXXX description
	audiometa.FormatFLAC: "AUDIOMETA_TEST", // Vorbis comment
	audiometa.FormatM4A:  "AUDIOMETA_TEST", // iTunes freeform atom
	audiometa.FormatM4B:  "AUDIOMETA_TEST",
}

// titleKeys is the raw key Title is read from, per format family.
var titleKeys = map[audiometa.Format]string{
	audiometa.FormatMP3:  "TIT2",
	audiometa.FormatFLAC: "TITLE",
	audiometa.FormatM4A:  "\xA9nam",
	audiometa.FormatM4B:  "\xA9nam",
}

// runDeleteRoundTrip injects a key with no typed field, then deletes it and the
// title key with Tags.Delete, and checks that only those two keys go away.
func runDeleteRoundTrip(t *testing.T, path string, src []byte) {
	t.Helper()
	f := openFile(t, path)
	custom, title := customKeys[f.Format], titleKeys[f.Format]

	f.Tags.Set(custom, "injected")
	var injected bytes.Buffer
	if _, err := f.WriteTo(&injected); err != nil {
		t.Fatalf("inject %q: %v", custom, err)
	}
	withCustom := writeAndReopen(t, path, injected.Bytes())
	if withCustom.Tags.GetFirst(custom) != "injected" {
		t.Fatalf("injected key %q did not round-trip", custom)
	}

	edited := openFile(t, withCustom.Path)
	edited.Tags.Delete(title, custom)
	var out bytes.Buffer
	if _, err := edited.WriteTo(&out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	reopened := writeAndReopen(t, path, out.Bytes())
	for _, k := range []string{title, custom} {
		if v := reopened.Tags.Get(k); v != nil {
			t.Errorf("deleted key %q still present: %q", k, v)
		}
	}
	assertPreserved(t, withCustom, edited, reopened, src, out.Bytes(), []string{"Title"})
}

// writeAndReopen writes data under path's basename in a temp dir and opens it.
func writeAndReopen(t *testing.T, path string, data []byte) *audiometa.File {
	t.Helper()
	out := filepath.Join(t.TempDir(), filepath.Base(path))
	if err := os.WriteFile(out, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return openFile(t, out)
}

// assertPreserved checks that writing edited (derived from orig) to written,
// re-opened as reopened, changed nothing but the edited fields: audio,
// chapters, every other raw key, every other typed field and, for M4A, the
// top-level atom layout.
//
// "Other" excludes only keys and fields that share a native key with an
// edited field (Year and Date share DATE in Vorbis; Artist and Artists share
// TPE1), because writing one necessarily rewrites the other. No field is
// excluded on any other ground: a change elsewhere would mean the writer
// inferred one field from another.
func assertPreserved(t *testing.T, orig, edited, reopened *audiometa.File, src, written []byte, fields []string) {
	t.Helper()
	format := orig.Format
	if audioDigest(t, format, src) != audioDigest(t, format, written) {
		t.Error("audio payload changed")
	}
	if !slices.Equal(reopened.Chapters, orig.Chapters) {
		t.Errorf("chapters changed:\n got  %+v\n want %+v", reopened.Chapters, orig.Chapters)
	}
	if format == audiometa.FormatM4A || format == audiometa.FormatM4B {
		if got, want := m4aTopLevelTypes(t, written), m4aTopLevelTypes(t, src); !slices.Equal(got, want) {
			t.Errorf("top-level atoms = %v, want %v", got, want)
		}
	}

	changedFields, changedKeys := editScope(format, fields)
	for k, v := range edited.Tags.All() { // keys edited through Set/Delete
		if !slices.Equal(v, orig.Tags.Get(k)) {
			changedKeys[k] = true
		}
	}
	for k := range orig.Tags.All() {
		if edited.Tags.Get(k) == nil {
			changedKeys[k] = true
		}
	}
	for _, k := range slices.Sorted(maps.Keys(rawKeys(orig, reopened))) {
		if !changedKeys[k] && !slices.Equal(orig.Tags.Get(k), reopened.Tags.Get(k)) {
			t.Errorf("untouched raw key %q changed: %q -> %q", k, orig.Tags.Get(k), reopened.Tags.Get(k))
		}
	}

	ov, rv := reflect.ValueOf(orig.Tags), reflect.ValueOf(reopened.Tags)
	typ := reflect.TypeFor[audiometa.Tags]()
	for i := range typ.NumField() {
		sf := typ.Field(i)
		if !sf.IsExported() || changedFields[sf.Name] {
			continue
		}
		if !reflect.DeepEqual(ov.Field(i).Interface(), rv.Field(i).Interface()) {
			t.Errorf("field %s changed: %v -> %v (no field may be inferred from another)", sf.Name, ov.Field(i).Interface(), rv.Field(i).Interface())
		}
	}
}

// editScope returns the typed fields allowed to change when fields are edited
// in format (the fields plus those sharing a bound key with them) and the raw
// keys bound to any of them.
func editScope(format audiometa.Format, fields []string) (map[string]bool, map[string]bool) {
	changedFields, changedKeys := map[string]bool{}, map[string]bool{}
	for _, name := range fields {
		changedFields[name] = true
	}
	bindings := types.FieldBindings(format)
	for _, b := range bindings {
		if changedFields[b.Field.String()] {
			for _, k := range b.Keys {
				changedKeys[k] = true
			}
		}
	}
	for _, b := range bindings {
		if slices.ContainsFunc(b.Keys, func(k string) bool { return changedKeys[k] }) {
			changedFields[b.Field.String()] = true
		}
	}
	return changedFields, changedKeys
}

// rawKeys returns the union of both files' raw keys.
func rawKeys(a, b *audiometa.File) map[string]bool {
	keys := map[string]bool{}
	for k := range a.Tags.All() {
		keys[k] = true
	}
	for k := range b.Tags.All() {
		keys[k] = true
	}
	return keys
}

// mp3RoundTripPaths lists the writable MP3 corpus files.
func mp3RoundTripPaths(t *testing.T) []string {
	t.Helper()
	var paths []string
	for _, tc := range roundTripCorpus {
		if tc.writable && strings.HasSuffix(tc.path, ".mp3") {
			paths = append(paths, tc.path)
		}
	}
	if len(paths) == 0 {
		t.Fatal("no writable MP3 corpus files")
	}
	return paths
}

// writeEdited applies edit to a fresh File for path and returns WriteTo's output.
func writeEdited(t *testing.T, path string, edit func(f *audiometa.File)) []byte {
	t.Helper()
	f := openFile(t, path)
	edit(f)
	var out bytes.Buffer
	if _, err := f.WriteTo(&out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return out.Bytes()
}

func TestWriteRoundTrip_MP3VersionPreserved(t *testing.T) {
	for path, want := range map[string]byte{"testdata/sample.mp3": 3, "testdata/sample_v24.mp3": 4} {
		out := writeEdited(t, path, func(f *audiometa.File) { f.Tags.Title = "New" })
		if string(out[:3]) != "ID3" || out[3] != want {
			t.Errorf("%s: output header %q, want ID3v2.%d", path, out[:4], want)
		}
	}
}

func TestWriteRoundTrip_MP3ID3v1Preserved(t *testing.T) {
	const path = "testdata/sample_v24.mp3"
	src := readTestdata(t, path)
	if string(src[len(src)-128:len(src)-125]) != "TAG" {
		t.Fatal("sample_v24.mp3 has no ID3v1 trailer")
	}
	out := writeEdited(t, path, func(f *audiometa.File) { f.Tags.Title = "New" })
	if !bytes.Equal(out[len(out)-128:], src[len(src)-128:]) {
		t.Error("ID3v1 trailer changed")
	}
}

// Deleting every raw key leaves a valid file with no raw tags, the same audio
// and the same chapters. The ID3v2 tag is kept (with the non-key frames such
// as APIC and CHAP, and padding).
func TestWriteRoundTrip_MP3DeleteAllKeys(t *testing.T) {
	for _, path := range mp3RoundTripPaths(t) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			src := readTestdata(t, path)
			orig := openFile(t, path)
			out := writeEdited(t, path, func(f *audiometa.File) {
				f.Tags.Delete(slices.Collect(maps.Keys(maps.Collect(f.Tags.All())))...)
			})
			if string(out[:3]) != "ID3" {
				t.Error("ID3v2 tag dropped")
			}
			reopened := writeAndReopen(t, path, out)
			for k, v := range reopened.Tags.All() {
				t.Errorf("raw key %q = %q survived", k, v)
			}
			if audioDigest(t, audiometa.FormatMP3, src) != audioDigest(t, audiometa.FormatMP3, out) {
				t.Error("audio payload changed")
			}
			if !slices.Equal(reopened.Chapters, orig.Chapters) {
				t.Errorf("chapters changed: %+v", reopened.Chapters)
			}
		})
	}
}

// flacAudioStart walks the metadata blocks of a FLAC file to the first audio byte.
func flacAudioStart(t *testing.T, data []byte) int {
	t.Helper()
	off := 4
	for {
		if off+4 > len(data) {
			t.Fatalf("flac: metadata block header at %d past EOF", off)
		}
		last := data[off]&0x80 != 0
		off += 4 + (int(data[off+1])<<16 | int(data[off+2])<<8 | int(data[off+3]))
		if last {
			return off
		}
	}
}

// A small edit fits in sample.flac's PADDING block, so the audio stays put.
func TestWriteRoundTrip_FLACKeepsAudioOffset(t *testing.T) {
	const path = "testdata/sample.flac"
	if got := flacAudioStart(t, readTestdata(t, path)); got != 8618 {
		t.Fatalf("source audio starts at %d, want 8618", got)
	}
	out := writeEdited(t, path, func(f *audiometa.File) { f.Tags.Title = "New" })
	if got := flacAudioStart(t, out); got != 8618 {
		t.Errorf("audio starts at %d, want 8618", got)
	}
}

// Deleting every raw key leaves a VORBIS_COMMENT block with only the vendor
// string: re-open gives no raw tags, and the audio and chapters are unchanged.
func TestWriteRoundTrip_FLACDeleteAllKeys(t *testing.T) {
	for _, path := range []string{"testdata/sample.flac", "testdata/sample_upper.flac"} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			src := readTestdata(t, path)
			orig := openFile(t, path)
			out := writeEdited(t, path, func(f *audiometa.File) {
				f.Tags.Delete(slices.Collect(maps.Keys(maps.Collect(f.Tags.All())))...)
			})
			reopened := writeAndReopen(t, path, out)
			for k, v := range reopened.Tags.All() {
				t.Errorf("raw key %q = %q survived", k, v)
			}
			if audioDigest(t, audiometa.FormatFLAC, src) != audioDigest(t, audiometa.FormatFLAC, out) {
				t.Error("audio payload changed")
			}
			if !slices.Equal(reopened.Chapters, orig.Chapters) {
				t.Errorf("chapters changed: %+v", reopened.Chapters)
			}
		})
	}
}

// TestWriteRoundTrip_M4AFaststartGrowth grows the metadata of a faststart
// M4B (moov before mdat), which moves the audio and shifts every chunk
// offset, and checks that the audio, chapters and atom layout survive. With
// ffmpeg on PATH it also decodes the result and compares the decoded PCM.
func TestWriteRoundTrip_M4AFaststartGrowth(t *testing.T) {
	const path = "testdata/sample_faststart.m4b"
	src := readTestdata(t, path)
	orig := openFile(t, path)
	out := writeEdited(t, path, func(f *audiometa.File) { f.Tags.Comment = longComment })
	if len(out) <= len(src) {
		t.Fatalf("output is %d bytes, source %d: the edit did not grow the file", len(out), len(src))
	}
	reopened := writeAndReopen(t, path, out)
	if reopened.Tags.Comment != longComment {
		t.Errorf("Comment has %d characters, want %d", len(reopened.Tags.Comment), len(longComment))
	}
	if audioDigest(t, orig.Format, src) != audioDigest(t, orig.Format, out) {
		t.Error("audio payload changed")
	}
	if len(orig.Chapters) != 2 || !slices.Equal(reopened.Chapters, orig.Chapters) {
		t.Errorf("chapters = %+v, want %+v", reopened.Chapters, orig.Chapters)
	}
	if got, want := m4aTopLevelTypes(t, out), m4aTopLevelTypes(t, src); !slices.Equal(got, want) {
		t.Errorf("top-level atoms = %v, want %v", got, want)
	}

	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not on PATH")
	}
	if pcm(t, reopened.Path) != pcm(t, path) {
		t.Error("decoded audio differs from the source")
	}
}

// pcm decodes path with ffmpeg to 16-bit PCM and returns its SHA-256. Any
// decoder message fails the test.
func pcm(t *testing.T, path string) [32]byte {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("ffmpeg", "-v", "error", "-i", path, "-map", "0:a", "-f", "s16le", "-")
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil || stderr.Len() > 0 {
		t.Fatalf("ffmpeg decode of %s: %v %s", path, err, stderr.String())
	}
	if stdout.Len() == 0 {
		t.Fatalf("ffmpeg decoded no audio from %s", path)
	}
	return sha256.Sum256(stdout.Bytes())
}

// TestWriteRoundTrip_M4AInPlace shortens the title of a faststart M4B with
// Write and WithInPlace: the new moov fits in the old one plus its free
// atom, so the file keeps its size and the audio does not move.
func TestWriteRoundTrip_M4AInPlace(t *testing.T) {
	src := readTestdata(t, "testdata/sample_faststart.m4b")
	path := filepath.Join(t.TempDir(), "book.m4b")
	if err := os.WriteFile(path, src, 0o600); err != nil {
		t.Fatal(err)
	}
	err := audiometa.Write(path, func(f *audiometa.File) error { f.Tags.Title = "Short"; return nil }, audiometa.WithInPlace())
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	out := readTestdata(t, path)
	if len(out) != len(src) {
		t.Errorf("in-place write changed the size from %d to %d", len(src), len(out))
	}
	if got := openFile(t, path).Tags.Title; got != "Short" {
		t.Errorf("Title = %q", got)
	}
	if audioDigest(t, audiometa.FormatM4A, src) != audioDigest(t, audiometa.FormatM4A, out) {
		t.Error("audio payload changed")
	}
}

// Deleting every raw key of an M4A/M4B leaves a valid file whose ilst holds
// only covr (artwork is not a raw key), with no raw tags, the same audio and
// the same chapters.
func TestWriteRoundTrip_M4ADeleteAllKeys(t *testing.T) {
	for _, tc := range roundTripCorpus {
		if !tc.writable || !strings.Contains(tc.path, ".m4") {
			continue
		}
		t.Run(filepath.Base(tc.path), func(t *testing.T) {
			src := readTestdata(t, tc.path)
			orig := openFile(t, tc.path)
			out := writeEdited(t, tc.path, func(f *audiometa.File) {
				f.Tags.Delete(slices.Collect(maps.Keys(maps.Collect(f.Tags.All())))...)
			})
			if !bytes.Contains(out, []byte("ilst")) {
				t.Error("ilst dropped") // it keeps covr, which is artwork, not a raw key
			}
			reopened := writeAndReopen(t, tc.path, out)
			for k, v := range reopened.Tags.All() {
				t.Errorf("raw key %q = %q survived", k, v)
			}
			if audioDigest(t, orig.Format, src) != audioDigest(t, orig.Format, out) {
				t.Error("audio payload changed")
			}
			if !slices.Equal(reopened.Chapters, orig.Chapters) {
				t.Errorf("chapters changed: %+v", reopened.Chapters)
			}
		})
	}
}

// precedenceValue returns the test value for the i-th candidate key of a
// field: a year for Year, a number for int fields, else a distinct string.
func precedenceValue(field string, i int) string {
	switch field {
	case "Year":
		return strconv.Itoa(2000 + i)
	case "TrackTotal", "DiscTotal", "SeriesPart":
		return strconv.Itoa(10 + i)
	default:
		return "v" + strconv.Itoa(i)
	}
}

// fieldString reads the typed field name of tags as a string ("" when zero).
func fieldString(tags *audiometa.Tags, name string) string {
	v := reflect.ValueOf(tags).Elem().FieldByName(name)
	if v.Kind() == reflect.Int {
		if v.Int() == 0 {
			return ""
		}
		return strconv.FormatInt(v.Int(), 10)
	}
	return v.String()
}

// TestReadPrecedence_MatchesBindings writes every candidate key of each
// multi-key field in REVERSE precedence order (lowest first, so it precedes
// the higher keys in the file) and checks the parsed field equals the highest
// precedence key's value, whatever the tag order.
func TestReadPrecedence_MatchesBindings(t *testing.T) {
	for _, path := range []string{
		"testdata/sample.flac", "testdata/sample_upper.flac",
		"testdata/sample.mp3", "testdata/sample_v24.mp3",
		"testdata/sample.m4a", "testdata/sample.m4b",
	} {
		format := openFile(t, path).Format
		for _, b := range types.FieldBindings(format) {
			if len(b.Keys) < 2 {
				continue
			}
			field := b.Field.String()
			t.Run(filepath.Base(path)+"/"+field, func(t *testing.T) {
				file := writeTempCopy(t, path)
				// Clear every candidate key first so the file starts clean.
				if err := audiometa.Write(file, func(f *audiometa.File) error {
					f.Tags.Delete(b.Keys...)
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				top := -1 // index of the highest-precedence key the writer accepted
				written := 0
				for i, key := range slices.Backward(b.Keys) {
					err := audiometa.Write(file, func(f *audiometa.File) error {
						f.Tags.Set(key, precedenceValue(field, i))
						return nil
					})
					if errors.Is(err, audiometa.ErrUnsupportedWrite) {
						continue // e.g. TDRC on an ID3v2.3 tag
					}
					if err != nil {
						t.Fatalf("write %q: %v", key, err)
					}
					top = i
					written++
				}
				if written < 2 {
					t.Skipf("the writer accepts only %d candidate key(s) for %s", written, field)
				}
				got := openFile(t, file)
				if want := precedenceValue(field, top); fieldString(&got.Tags, field) != want {
					t.Errorf("%s = %q, want %q (from %q)", field, fieldString(&got.Tags, field), want, b.Keys[top])
				}
			})
		}
	}
}

// TestWrite_FLACTotalTracksFirst round-trips a FLAC whose lower-precedence
// TOTALTRACKS precedes TRACKTOTAL: setting TrackTotal must read back.
func TestWrite_FLACTotalTracksFirst(t *testing.T) {
	file := writeTempCopy(t, "testdata/sample.flac")
	for _, step := range [][2]string{{"TOTALTRACKS", "5"}, {"TRACKTOTAL", "7"}} {
		if err := audiometa.Write(file, func(f *audiometa.File) error {
			f.Tags.Set(step[0], step[1])
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if got := openFile(t, file).Tags.TrackTotal; got != 7 {
		t.Fatalf("TrackTotal = %d, want 7 (TRACKTOTAL outranks TOTALTRACKS)", got)
	}
	if err := audiometa.Write(file, func(f *audiometa.File) error {
		f.Tags.TrackTotal = 12
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := openFile(t, file).Tags.TrackTotal; got != 12 {
		t.Errorf("TrackTotal after Set = %d, want 12", got)
	}
}

// writeTempCopy copies a corpus file into a temp dir and returns the copy's path.
func writeTempCopy(t *testing.T, src string) string {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), filepath.Base(src))
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return dst
}
