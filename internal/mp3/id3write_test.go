package mp3

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"slices"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

// audioTail is what follows the tag in synthetic files: MPEG audio.
var audioTail = bytes.Repeat(mpegFrame, 64)

// file joins a tag and audioTail.
func file(tag []byte) []byte { return append(slices.Clone(tag), audioTail...) }

// sameFrames reports whether a and b hold the same frames (ID and payload) in order.
func sameFrames(a, b []wframe) bool {
	return slices.EqualFunc(a, b, func(x, y wframe) bool { return x.id == y.id && bytes.Equal(x.payload, y.payload) })
}

// without returns the frames whose ID is not in ids.
func without(frames []wframe, ids ...string) []wframe {
	return slices.DeleteFunc(slices.Clone(frames), func(f wframe) bool { return slices.Contains(ids, f.id) })
}

// framesWithID returns the frames with ID id.
func framesWithID(frames []wframe, id string) []wframe {
	return slices.DeleteFunc(slices.Clone(frames), func(f wframe) bool { return f.id != id })
}

func TestWTag_VerbatimRoundTrip(t *testing.T) {
	for _, v := range []byte{3, 4} {
		ctoc := append([]byte("toc\x00\x03\x01"), "ch0\x00"...)
		tag := id3TagFlags(v, 0, 64,
			id3Frame(v, "TIT2", latin1Text("Title")),
			id3Frame(v, "TXXX", txxxPayload(0, "Narrator", "Someone")),
			id3Frame(v, "XYZW", []byte{1, 2, 3, 4}),
			id3Frame(v, "PRIV", []byte("owner\x00secret")),
			id3Frame(v, "CHAP", chapFrameBody("ch0", 0, 1000, id3Frame(v, "TIT2", latin1Text("One")))),
			id3Frame(v, "CTOC", ctoc),
			id3Frame(v, "APIC", apicPayload("image/jpeg", 3, "", []byte{0xFF, 0xD8, 0xFF, 0xE0})),
		)
		wt := framesOf(t, file(tag))
		got, err := wt.serialize()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, tag) {
			t.Errorf("v2.%d: serialize differs from the source tag", v)
		}
		if want := []string{"TIT2", "TXXX", "XYZW", "PRIV", "CHAP", "CTOC", "APIC"}; !slices.Equal(frameIDs(wt.frames), want) {
			t.Errorf("v2.%d: frames %v, want %v", v, frameIDs(wt.frames), want)
		}
	}
}

func TestWTag_V23TagUnsync(t *testing.T) {
	// The tag body is unsynchronised: the title byte FF is stored as FF 00.
	// v2.3 frame sizes count the decoded bytes.
	frame := id3Frame(3, "TIT2", []byte{0, 'a', 0xFF, 'x'})
	stored := append(slices.Clone(frame[:10]), 0, 'a', 0xFF, 0x00, 'x')
	src := file(id3TagFlags(3, flagUnsync, 0, stored))

	wt := framesOf(t, src)
	tag, err := wt.serialize()
	if err != nil {
		t.Fatal(err)
	}
	if tag[5] != 0 {
		t.Errorf("header flags = %#x, want 0", tag[5])
	}
	if !bytes.Equal(wt.frames[0].payload, []byte{0, 'a', 0xFF, 'x'}) {
		t.Errorf("payload = %x, want de-unsynchronised", wt.frames[0].payload)
	}
	out := writeBytes(t, src, editsFor(t, src, nil, types.ArtworkOp{Kind: types.ArtworkOpRemoveAll}))
	if got, want := parseBytes(t, out).Tags.Title, parseBytes(t, src).Tags.Title; got != want || want != "aÿx" {
		t.Errorf("Title = %q, source %q, want %q", got, want, "aÿx")
	}
}

func TestWTag_V24FrameUnsync(t *testing.T) {
	src := file(id3Tag(4, id3FrameFlags(4, "TIT2", v24FrameUnsync, []byte{0, 'a', 0xFF, 0x00, 'x'})))
	tag, err := framesOf(t, src).serialize()
	if err != nil {
		t.Fatal(err)
	}
	want := id3Tag(4, id3Frame(4, "TIT2", []byte{0, 'a', 0xFF, 'x'}))
	if !bytes.Equal(tag[10:len(want)], want[10:]) {
		t.Errorf("frame = %x, want %x", tag[10:], want[10:])
	}
}

func TestWTag_V24CompressedPreserved(t *testing.T) {
	frame := id3FrameFlags(4, "XYZW", v24FrameCompression|v24FrameDataLength, []byte{0, 0, 0, 10, 0x78, 0x9C, 1, 2, 3})
	src := file(id3Tag(4, frame))
	tag, err := framesOf(t, src).serialize()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(tag[10:10+len(frame)], frame) {
		t.Errorf("compressed frame = %x, want %x", tag[10:10+len(frame)], frame)
	}
}

func TestWTag_V22ConvertsToV24(t *testing.T) {
	src := file(v22Tag())
	wt := framesOf(t, src)
	if want := []string{"TIT2", "TPE1", "TALB", "TRCK", "MVNM", "COMM", "APIC"}; !slices.Equal(frameIDs(wt.frames), want) {
		t.Errorf("frames %v, want %v", frameIDs(wt.frames), want)
	}
	tag, err := wt.serialize()
	if err != nil {
		t.Fatal(err)
	}
	if tag[3] != 4 {
		t.Errorf("version byte = %d, want 4", tag[3])
	}
	out := append(tag, src[wt.oldTotal:]...)
	before, after := parseBytes(t, src).Tags, parseBytes(t, out).Tags
	if after.Title != before.Title || after.Artist != before.Artist || after.Album != before.Album {
		t.Errorf("tags %q/%q/%q, want %q/%q/%q", after.Title, after.Artist, after.Album, before.Title, before.Artist, before.Album)
	}
	artBefore := mustArtwork(t, src)
	artAfter := mustArtwork(t, out)
	if len(artAfter) != 1 || len(artBefore) != 1 || !bytes.Equal(artAfter[0].Data, artBefore[0].Data) {
		t.Errorf("artwork %v, want %v", artAfter, artBefore)
	}
}

func mustArtwork(t *testing.T, data []byte) []types.Artwork {
	t.Helper()
	art, err := extractArtwork(context.Background(), bytes.NewReader(data), int64(len(data)), "t.mp3")
	if err != nil {
		t.Fatal(err)
	}
	return art
}

func TestWTag_V22UnconvertibleRefused(t *testing.T) {
	src := file(v22Tag(id3v22Frame("TDA", latin1Text("0101"))))
	_, err := readWTag(context.Background(), srOf(src), int64(len(src)))
	if !errors.Is(err, types.ErrUnsupportedWrite) {
		t.Errorf("err = %v, want ErrUnsupportedWrite", err)
	}
}

func TestWTag_UnsupportedVersionRefused(t *testing.T) {
	for _, v := range []byte{0, 1, 5} {
		src := file(id3Tag(v, id3Frame(4, "TIT2", latin1Text("x"))))
		if _, err := tryWrite(src, &types.EditSet{Orig: &types.Tags{}, Final: &types.Tags{}}); !errors.Is(err, types.ErrUnsupportedWrite) {
			t.Errorf("v2.%d: err = %v, want ErrUnsupportedWrite", v, err)
		}
	}
	compressed := file(id3TagFlags(2, flagV22Compression, 0, id3v22Frame("TT2", latin1Text("x"))))
	if _, err := tryWrite(compressed, &types.EditSet{Orig: &types.Tags{}, Final: &types.Tags{}}); !errors.Is(err, types.ErrUnsupportedWrite) {
		t.Errorf("compressed v2.2: err = %v, want ErrUnsupportedWrite", err)
	}
}

func TestWTag_FrameOverrunsTag(t *testing.T) {
	frame := id3Frame(3, "TIT2", latin1Text("Title"))
	frame[7] = 100 // declare 100 bytes in a tag that holds far fewer
	src := file(id3Tag(3, frame))
	_, err := readWTag(context.Background(), srOf(src), int64(len(src)))
	if _, ok := errors.AsType[*types.CorruptedFileError](err); !ok {
		t.Errorf("err = %v, want *CorruptedFileError", err)
	}
}

func TestWTag_Untagged(t *testing.T) {
	wt := framesOf(t, audioTail)
	if wt.oldTotal != 0 || wt.outVersion != 4 || len(wt.frames) != 0 {
		t.Errorf("oldTotal %d, outVersion %d, %d frames", wt.oldTotal, wt.outVersion, len(wt.frames))
	}
	if tag, err := wt.serialize(); err != nil || tag != nil {
		t.Errorf("serialize of an empty untagged list = %x, %v; want no tag", tag, err)
	}
}

func TestCheckTagSize(t *testing.T) {
	if err := checkTagSize(0x0FFFFFFF); err != nil {
		t.Errorf("0x0FFFFFFF: %v", err)
	}
	if err := checkTagSize(0x10000000); !errors.Is(err, types.ErrTagTooLarge) {
		t.Errorf("0x10000000: err = %v, want ErrTagTooLarge", err)
	}
	if err := checkFrameSize(0x10000000, 4); !errors.Is(err, types.ErrTagTooLarge) {
		t.Errorf("v2.4 frame 0x10000000: err = %v, want ErrTagTooLarge", err)
	}
	if err := checkFrameSize(0x10000000, 3); err != nil {
		t.Errorf("v2.3 frame 0x10000000: %v", err)
	}
}

func TestWTag_Padding(t *testing.T) {
	src := file(id3TagFlags(4, 0, 100, id3Frame(4, "TIT2", latin1Text("A long title"))))
	oldTotal := len(src) - len(audioTail)

	shrink := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Title = "T" }))
	if got := len(shrink) - len(audioTail); got != oldTotal {
		t.Errorf("shrunk tag is %d bytes, want %d", got, oldTotal)
	}

	grow := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Comment = string(bytes.Repeat([]byte("c"), 500)) }))
	wt := framesOf(t, grow)
	if padding := int(wt.oldTotal) - 10 - serializedFramesLen(t, wt); padding < minGrowPadding {
		t.Errorf("grown tag has %d bytes of padding, want >= %d", padding, minGrowPadding)
	}

	untagged := writeBytes(t, audioTail, editsFor(t, audioTail, func(tg *types.Tags) { tg.Title = "T" }))
	wt = framesOf(t, untagged)
	if padding := int(wt.oldTotal) - 10 - serializedFramesLen(t, wt); padding != minGrowPadding {
		t.Errorf("new tag has %d bytes of padding, want %d", padding, minGrowPadding)
	}
	if !bytes.Equal(untagged[wt.oldTotal:], audioTail) {
		t.Error("audio changed after adding a tag")
	}
}

// serializedFramesLen returns the encoded length of wt's frames.
func serializedFramesLen(t *testing.T, wt *wtag) int {
	t.Helper()
	var b []byte
	for _, f := range wt.frames {
		var err error
		if b, err = wt.appendFrame(b, f); err != nil {
			t.Fatal(err)
		}
	}
	return len(b)
}

func TestApply_TextKeyEdit(t *testing.T) {
	tests := []struct {
		value   string
		version byte
		wantEnc byte
	}{
		{"Nouveau ✓", 4, encUTF8},
		{"Nouveau ✓", 3, encUTF16},
		{"Cafe", 3, encLatin1},
		{"Café", 3, encLatin1},
	}
	for _, tt := range tests {
		v := tt.version
		src := file(id3Tag(v,
			id3Frame(v, "TALB", latin1Text("Album")),
			id3Frame(v, "TIT2", latin1Text("Old")),
			id3Frame(v, "XYZW", []byte{9, 9}),
		))
		out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Set("TIT2", tt.value) }))
		before, after := framesOf(t, src).frames, framesOf(t, out).frames
		tit2 := framesWithID(after, "TIT2")
		if len(tit2) != 1 || tit2[0].payload[0] != tt.wantEnc {
			t.Errorf("v2.%d %q: TIT2 frames %v, want one with encoding %d", v, tt.value, tit2, tt.wantEnc)
		}
		if !slices.Equal(frameIDs(after), []string{"TALB", "TIT2", "XYZW"}) {
			t.Errorf("v2.%d: order %v", v, frameIDs(after))
		}
		if !sameFrames(without(before, "TIT2"), without(after, "TIT2")) {
			t.Errorf("v2.%d: frames other than TIT2 changed", v)
		}
		if got := parseBytes(t, out).Tags.Title; got != tt.value {
			t.Errorf("v2.%d: Title = %q, want %q", v, got, tt.value)
		}
	}
}

func TestApply_TXXXPreservesDescriptionCase(t *testing.T) {
	src := file(id3Tag(3, id3Frame(3, "TXXX", txxxPayload(0, "Narrator", "Old"))))
	out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Set("NARRATOR", "X") }))
	txxx := framesWithID(framesOf(t, out).frames, "TXXX")
	if len(txxx) != 1 || !bytes.Equal(txxx[0].payload, txxxPayload(0, "Narrator", "X")) {
		t.Errorf("TXXX frames %+v, want one Narrator=X", txxx)
	}
}

func TestApply_FieldEdits(t *testing.T) {
	tests := []struct {
		name    string
		version byte
		frames  [][]byte
		mutate  func(*types.Tags)
		key     string // raw key to check, or "" for Comment
		want    string
		wantErr error
	}{
		{"title", 4, nil, func(tg *types.Tags) { tg.Title = "T" }, "TIT2", "T", nil},
		{"narrator", 4, nil, func(tg *types.Tags) { tg.Narrator = "N" }, "NARRATOR", "N", nil},
		{"year keeps date", 4, [][]byte{id3Frame(4, "TDRC", latin1Text("2021-05-03"))}, func(tg *types.Tags) { tg.Year = 1999 }, "TDRC", "1999-05-03", nil},
		{"year v2.3", 3, nil, func(tg *types.Tags) { tg.Year = 1999 }, "TYER", "1999", nil},
		{"year v2.4", 4, nil, func(tg *types.Tags) { tg.Year = 1999 }, "TDRC", "1999", nil},
		{"track", 3, nil, func(tg *types.Tags) { tg.TrackNumber, tg.TrackTotal = 3, 12 }, "TRCK", "3/12", nil},
		{"disc", 4, nil, func(tg *types.Tags) { tg.DiscNumber = 2 }, "TPOS", "2", nil},
		{"comment", 3, nil, func(tg *types.Tags) { tg.Comment = "Hello" }, "", "Hello", nil},
		{"date", 4, nil, func(tg *types.Tags) { tg.Date = "2020-01-01" }, "", "", types.ErrUnsupportedWrite},
		{"lyrics", 4, nil, func(tg *types.Tags) { tg.Lyrics = "la" }, "", "", types.ErrUnsupportedWrite},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frames := append([][]byte{id3Frame(tt.version, "TSSE", latin1Text("enc"))}, tt.frames...)
			src := file(id3Tag(tt.version, frames...))
			out, err := tryWrite(src, editsFor(t, src, tt.mutate))
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			tags := parseBytes(t, out).Tags
			got := tags.Comment
			if tt.key != "" {
				got = tags.GetFirst(tt.key)
			}
			if got != tt.want {
				t.Errorf("%s = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestApply_CommentKeepsLanguageAndClears(t *testing.T) {
	src := file(id3Tag(3,
		id3Frame(3, "COMM", commPayload("fra", "", "Bonjour")),
		id3Frame(3, "COMM", commPayload("eng", "iTunNORM", " 0000")),
	))
	out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Comment = "Salut" }))
	comm := framesWithID(framesOf(t, out).frames, "COMM")
	if len(comm) != 2 || !bytes.Equal(comm[0].payload, commPayload("fra", "", "Salut")) {
		t.Errorf("COMM frames %+v", comm)
	}
	cleared := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Comment = "" }))
	comm = framesWithID(framesOf(t, cleared).frames, "COMM")
	if len(comm) != 1 || !bytes.Equal(comm[0].payload, commPayload("eng", "iTunNORM", " 0000")) {
		t.Errorf("after clearing, COMM frames %+v, want only the iTunNORM one", comm)
	}
	if c := parseBytes(t, cleared).Tags.Comment; c != "" {
		t.Errorf("Comment = %q after clearing", c)
	}
}

func TestApply_SourceKeyOnly(t *testing.T) {
	src := file(id3Tag(4,
		id3Frame(4, "GRP1", latin1Text("Old")),
		id3Frame(4, "TIT1", latin1Text("Content")),
	))
	before := framesOf(t, src).frames
	out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Grouping = "G" }))
	after := framesOf(t, out).frames
	if got := parseBytes(t, out).Tags.GetFirst("GRP1"); got != "G" {
		t.Errorf("GRP1 = %q, want G", got)
	}
	if !sameFrames(framesWithID(before, "TIT1"), framesWithID(after, "TIT1")) {
		t.Error("TIT1 changed")
	}

	cleared := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Grouping = "" }))
	if ids := frameIDs(framesOf(t, cleared).frames); len(ids) != 0 {
		t.Errorf("frames %v left after clearing Grouping", ids)
	}
}

func TestApply_NoCrossFieldWrites(t *testing.T) {
	v1 := id3v1Trailer("Book 3", "Someone", "", "2020", "", 0)
	src := append(file(id3Tag(4,
		id3Frame(4, "TCOM", latin1Text("C")),
		id3Frame(4, "GRP1", latin1Text("Saga #5")),
		id3Frame(4, "TIT2", latin1Text("Book 3")),
	)), v1...)
	before := framesOf(t, src).frames

	tests := []struct {
		name    string
		mutate  func(*types.Tags)
		changed string // the only frame ID allowed to change
	}{
		{"narrator", func(tg *types.Tags) { tg.Narrator = "N" }, "TXXX"},
		{"series", func(tg *types.Tags) { tg.Series = "S" }, "TXXX"},
		{"title", func(tg *types.Tags) { tg.Title = "T" }, "TIT2"},
	}
	for _, tt := range tests {
		out := writeBytes(t, src, editsFor(t, src, tt.mutate))
		after := framesOf(t, out).frames
		if !sameFrames(without(before, tt.changed), without(after, tt.changed)) {
			t.Errorf("%s: frames other than %s changed: %v", tt.name, tt.changed, frameIDs(after))
		}
		if !bytes.Equal(out[len(out)-id3v1Size:], v1) {
			t.Errorf("%s: ID3v1 trailer changed", tt.name)
		}
	}
	tags := parseBytes(t, writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Narrator = "N" }))).Tags
	if tags.GetFirst("NARRATOR") != "N" || !slices.Equal(tags.Composers, []string{"C"}) {
		t.Errorf("NARRATOR %q, Composers %q", tags.GetFirst("NARRATOR"), tags.Composers)
	}
}

func TestApply_MultiValue(t *testing.T) {
	for _, v := range []byte{3, 4} {
		src := file(id3Tag(v, id3Frame(v, "TPE1", latin1Text("X"))))
		out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) {
			tg.Artists = []string{"A", "B"}
			tg.Artist = "A"
		}))
		if got := parseBytes(t, out).Tags.Artists; !slices.Equal(got, []string{"A", "B"}) {
			t.Errorf("v2.%d: Artists = %q", v, got)
		}
	}
}

func TestApply_VersionSpecificKeyRefused(t *testing.T) {
	src := file(id3Tag(3, id3Frame(3, "TIT2", latin1Text("x"))))
	if _, err := tryWrite(src, editsFor(t, src, func(tg *types.Tags) { tg.Set("TDRC", "2020") })); !errors.Is(err, types.ErrUnsupportedWrite) {
		t.Errorf("TDRC on v2.3: err = %v, want ErrUnsupportedWrite", err)
	}
	src = file(id3Tag(4, id3Frame(4, "TIT2", latin1Text("x"))))
	if _, err := tryWrite(src, editsFor(t, src, func(tg *types.Tags) { tg.Set("TYER", "2020") })); !errors.Is(err, types.ErrUnsupportedWrite) {
		t.Errorf("TYER on v2.4: err = %v, want ErrUnsupportedWrite", err)
	}
	// Deleting one is always allowed.
	src = file(id3Tag(4, id3Frame(4, "TYER", latin1Text("2020"))))
	out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Delete("TYER") }))
	if ids := frameIDs(framesOf(t, out).frames); len(ids) != 0 {
		t.Errorf("frames %v, want TYER deleted", ids)
	}
}

func TestApply_CompressedTXXXRefused(t *testing.T) {
	src := file(id3Tag(4,
		id3FrameFlags(4, "TXXX", v24FrameCompression|v24FrameDataLength, []byte{0, 0, 0, 9, 0x78, 0x9C, 1}),
		id3Frame(4, "TIT2", latin1Text("x")),
	))
	if _, err := tryWrite(src, editsFor(t, src, func(tg *types.Tags) { tg.Set("NARRATOR", "x") })); !errors.Is(err, types.ErrUnsupportedWrite) {
		t.Errorf("err = %v, want ErrUnsupportedWrite", err)
	}
	// A text-frame edit does not need to read the TXXX and leaves it alone.
	out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Title = "y" }))
	if !sameFrames(framesWithID(framesOf(t, src).frames, "TXXX"), framesWithID(framesOf(t, out).frames, "TXXX")) {
		t.Error("compressed TXXX changed")
	}
}

// testPNG returns a 2x1 PNG.
func testPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, 2, 1))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestApply_Artwork(t *testing.T) {
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0, 0, 0}
	src := file(id3Tag(3,
		id3Frame(3, "TIT2", latin1Text("x")),
		id3Frame(3, "APIC", apicPayload("image/jpeg", 3, "front", jpeg)),
		id3Frame(3, "APIC", apicPayload("image/jpeg", 4, "back", jpeg)),
		id3Frame(3, "TALB", latin1Text("y")),
	))
	pngData := testPNG(t)
	newArt := func(typ types.ArtworkType) types.Artwork {
		return types.Artwork{MIMEType: "image/png", Data: pngData, Type: typ, Description: "Ünïcode ✓"}
	}
	backFrame := framesWithID(framesOf(t, src).frames, "APIC")[1:]

	tests := []struct {
		name  string
		ops   []types.ArtworkOp
		types []types.ArtworkType
	}{
		{"replace front", []types.ArtworkOp{
			{Kind: types.ArtworkOpRemove, Type: types.ArtworkFrontCover},
			{Kind: types.ArtworkOpAdd, Art: newArt(types.ArtworkFrontCover)},
		}, []types.ArtworkType{types.ArtworkBackCover, types.ArtworkFrontCover}},
		{"remove back", []types.ArtworkOp{{Kind: types.ArtworkOpRemove, Type: types.ArtworkBackCover}}, []types.ArtworkType{types.ArtworkFrontCover}},
		{"remove all", []types.ArtworkOp{{Kind: types.ArtworkOpRemoveAll}}, nil},
		{"add", []types.ArtworkOp{{Kind: types.ArtworkOpAdd, Art: newArt(types.ArtworkArtist)}}, []types.ArtworkType{types.ArtworkFrontCover, types.ArtworkBackCover, types.ArtworkArtist}},
	}
	for _, tt := range tests {
		out := writeBytes(t, src, editsFor(t, src, nil, tt.ops...))
		art := mustArtwork(t, out)
		var got []types.ArtworkType
		for _, a := range art {
			got = append(got, a.Type)
		}
		if !slices.Equal(got, tt.types) {
			t.Errorf("%s: artwork types %v, want %v", tt.name, got, tt.types)
		}
		for _, a := range art {
			if a.MIMEType == "image/png" && (!bytes.Equal(a.Data, pngData) || a.Description != "Ünïcode ✓") {
				t.Errorf("%s: new picture %v, desc %q", tt.name, a, a.Description)
			}
		}
		if tt.name == "replace front" && !sameFrames(framesWithID(framesOf(t, out).frames, "APIC")[:1], backFrame) {
			t.Error("replace front: back cover frame changed")
		}
		if ids := frameIDs(without(framesOf(t, out).frames, "APIC")); !slices.Equal(ids, []string{"TIT2", "TALB"}) {
			t.Errorf("%s: other frames %v", tt.name, ids)
		}
	}
}

func TestApply_DRM(t *testing.T) {
	tests := map[string][]byte{
		"v2.4 encrypted": file(id3Tag(4, id3FrameFlags(4, "TIT2", v24FrameEncryption, []byte{1, 0, 0}))),
		"v2.3 encrypted": file(id3Tag(3, id3FrameFlags(3, "XYZW", v23FrameEncryption, []byte{1, 0, 0}))),
		"AENC":           file(id3Tag(3, id3Frame(3, "AENC", []byte("owner\x00\x00\x00\x00\x00")))),
		"v2.2 CRA":       file(v22Tag(id3v22Frame("CRA", []byte("owner\x00\x00\x00\x00\x00")))),
	}
	for name, src := range tests {
		if _, err := tryWrite(src, &types.EditSet{Orig: &types.Tags{}, Final: &types.Tags{}}); !errors.Is(err, types.ErrDRMProtected) {
			t.Errorf("%s: err = %v, want ErrDRMProtected", name, err)
		}
	}
}

func TestApply_InvalidUTF8Refused(t *testing.T) {
	src := file(id3Tag(4, id3Frame(4, "TIT2", latin1Text("x"))))
	for _, v := range []string{"bad \xff", "nul\x00inside"} {
		edits := editsFor(t, src, nil)
		edits.Keys = []types.KeyEdit{{Key: "TIT2", Values: []string{v}}}
		if _, err := tryWrite(src, edits); !errors.Is(err, types.ErrUnsupportedWrite) {
			t.Errorf("%q: err = %v, want ErrUnsupportedWrite", v, err)
		}
	}
}

func TestApply_MediaMarkersRefused(t *testing.T) {
	src := file(id3Tag(4, id3Frame(4, "TIT2", latin1Text("x"))))
	for _, mutate := range []func(*types.Tags){
		func(tg *types.Tags) { tg.Set("OVERDRIVE MEDIAMARKERS", "x") },
		func(tg *types.Tags) { tg.Set("OVERDRIVE MEDIAMARKERS"); tg.Delete("OVERDRIVE MEDIAMARKERS") },
	} {
		edits := editsFor(t, src, mutate)
		if len(edits.Keys) == 0 {
			edits.Keys = []types.KeyEdit{{Key: "OVERDRIVE MEDIAMARKERS"}} // a deletion
		}
		if _, err := tryWrite(src, edits); !errors.Is(err, types.ErrUnsupportedWrite) {
			t.Errorf("err = %v, want ErrUnsupportedWrite", err)
		}
	}
}

// Maintainer addendum: any key can be deleted; chapters are never touched.
func TestApply_DeleteKeys(t *testing.T) {
	for _, v := range []byte{3, 4} {
		chap := id3Frame(v, "CHAP", chapFrameBody("ch0", 0, 1000, id3Frame(v, "TIT2", latin1Text("One"))))
		ctoc := id3Frame(v, "CTOC", append([]byte("toc\x00\x03\x01"), "ch0\x00"...))
		apic := id3Frame(v, "APIC", apicPayload("image/jpeg", 3, "", []byte{0xFF, 0xD8, 0xFF, 0xE0}))
		src := file(id3Tag(v,
			id3Frame(v, "TIT2", latin1Text("Title")),
			id3Frame(v, "TPE1", latin1Text("Artist")),
			id3Frame(v, "TXXX", txxxPayload(0, "My Custom", "c")),
			chap, ctoc, apic,
		))
		before := framesOf(t, src).frames

		// A typed key.
		out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Delete("TIT2") }))
		if !sameFrames(without(before, "TIT2"), framesOf(t, out).frames) {
			t.Errorf("v2.%d delete TIT2: frames %v", v, frameIDs(framesOf(t, out).frames))
		}
		if tags := parseBytes(t, out).Tags; tags.Title != "" || tags.Artist != "Artist" {
			t.Errorf("v2.%d delete TIT2: Title %q, Artist %q", v, tags.Title, tags.Artist)
		}

		// A key with no typed field.
		out = writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Delete("MY CUSTOM") }))
		if !sameFrames(without(before, "TXXX"), framesOf(t, out).frames) {
			t.Errorf("v2.%d delete TXXX: frames %v", v, frameIDs(framesOf(t, out).frames))
		}

		// Every key: the tag keeps only the non-key frames.
		out = writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) {
			for k := range tg.All() {
				tg.Delete(k)
			}
		}))
		after := framesOf(t, out).frames
		if !sameFrames(after, without(before, "TIT2", "TPE1", "TXXX")) {
			t.Errorf("v2.%d delete all: frames %v", v, frameIDs(after))
		}
		parsed := parseBytes(t, out)
		for k, vals := range parsed.Tags.All() {
			t.Errorf("v2.%d delete all: key %q = %q left", v, k, vals)
		}
		if !slices.Equal(parsed.Chapters, parseBytes(t, src).Chapters) {
			t.Errorf("v2.%d delete all: chapters changed", v)
		}
	}
}

func TestApply_V22KeysNormalized(t *testing.T) {
	src := file(v22Tag(id3v22Frame("TYE", latin1Text("2020")), id3v22Frame("TEN", latin1Text("enc"))))
	out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) {
		tg.Year = 1999
		tg.Delete("TEN")
	}))
	wt := framesOf(t, out)
	if wt.inVersion != 4 || len(framesWithID(wt.frames, "TENC")) != 0 {
		t.Errorf("version %d, frames %v", wt.inVersion, frameIDs(wt.frames))
	}
	if got := parseBytes(t, out).Tags.GetFirst("TDRC"); got != "1999" {
		t.Errorf("TDRC = %q, want 1999", got)
	}
}
