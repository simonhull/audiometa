package flac

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/simonhull/audiometa/internal/registry"
	"github.com/simonhull/audiometa/internal/types"
)

func TestWriter_Registered(t *testing.T) {
	if _, ok := registry.GetWriter(types.FormatFLAC).(*writer); !ok {
		t.Error("no FLAC writer registered")
	}
	for _, f := range []types.Format{types.FormatOgg, types.FormatOpus} {
		if registry.GetWriter(f) != nil {
			t.Errorf("a writer is registered for %s", f)
		}
	}
}

// richFLAC has one block of every kind the writer keeps verbatim.
func richFLAC() []byte {
	cue := make([]byte, 396)
	for i := range cue {
		cue[i] = byte(i * 7)
	}
	return append(buildFLAC(
		flacBlock(false, blockTypeStreamInfo, streamInfoPayload(44100, 44100)),
		flacBlock(false, blockTypeSeekTable, []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x10, 0}),
		flacBlock(false, blockTypeApplication, []byte("TEST\x01\x02\x03\x04")),
		flacBlock(false, blockTypeCueSheet, cue),
		flacBlock(false, blockTypeVorbisComment, vorbisCommentPayload("title=Old", "ARTIST=A")),
		flacBlock(true, blockTypePadding, make([]byte, 100)),
	), audioTail...)
}

func TestFBlock_VerbatimRoundTrip(t *testing.T) {
	src := richFLAC()
	m := metaOf(t, src)
	if m.audioStart != int64(len(src)-len(audioTail)) || m.oldMetaLen != m.audioStart-4 {
		t.Fatalf("audioStart %d, oldMetaLen %d", m.audioStart, m.oldMetaLen)
	}
	got, err := m.serialize()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, src[4:m.audioStart]) {
		t.Error("serialize differs from the source metadata")
	}
}

func TestFBlock_CorpusVerbatim(t *testing.T) {
	for _, name := range []string{"sample.flac", "sample_upper.flac"} {
		src := corpusFile(t, name)
		m := metaOf(t, src)
		got, err := m.serialize()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, src[4:m.audioStart]) {
			t.Errorf("%s: serialize differs from the source metadata", name)
		}
	}
}

// grow appends a comment of n bytes (growing the block by 4+n) to m's
// VORBIS_COMMENT block.
func grow(t *testing.T, m *fmeta, n int) {
	t.Helper()
	i := slices.IndexFunc(m.blocks, func(b fblock) bool { return b.typ == blockTypeVorbisComment })
	v, err := parseVC(m.blocks[i].payload)
	if err != nil {
		t.Fatal(err)
	}
	v.comments = append(v.comments, []byte("X="+strings.Repeat("x", n-2)))
	m.blocks[i].payload = v.bytes()
}

func TestFBlock_PaddingAbsorbsGrowth(t *testing.T) {
	m := metaOf(t, simpleFLAC(100, "TITLE=T"))
	grow(t, m, 50)
	out, err := m.serialize()
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(out)) != m.oldMetaLen {
		t.Errorf("metadata is %d bytes, want %d (audio must not move)", len(out), m.oldMetaLen)
	}
	hs := headersOf(t, out)
	if last := hs[len(hs)-1]; last.typ != blockTypePadding || last.n != 100-54 {
		t.Errorf("trailing block %+v, want PADDING of %d", last, 100-54)
	}
}

func TestFBlock_PaddingExhausted(t *testing.T) {
	m := metaOf(t, simpleFLAC(10, "TITLE=T"))
	grow(t, m, 100)
	out, err := m.serialize()
	if err != nil {
		t.Fatal(err)
	}
	hs := headersOf(t, out)
	if last := hs[len(hs)-1]; last.typ != blockTypePadding || last.n != defaultPadding {
		t.Errorf("trailing block %+v, want PADDING of %d", last, defaultPadding)
	}
}

func TestFBlock_SmallGapGrows(t *testing.T) {
	// Growth leaves 1-3 bytes: too few for a PADDING header, so the
	// metadata gets fresh padding instead.
	for _, gap := range []int{1, 2, 3} {
		m := metaOf(t, simpleFLAC(100, "TITLE=T"))
		grow(t, m, 100-gap)
		out, err := m.serialize()
		if err != nil {
			t.Fatal(err)
		}
		hs := headersOf(t, out)
		if last := hs[len(hs)-1]; last.typ != blockTypePadding || last.n != defaultPadding {
			t.Errorf("gap %d: trailing block %+v, want PADDING of %d", gap, last, defaultPadding)
		}
	}
}

func TestFBlock_ExactFitNoPadding(t *testing.T) {
	m := metaOf(t, simpleFLAC(100, "TITLE=T"))
	grow(t, m, 100) // 4 + 100 = the whole PADDING block
	out, err := m.serialize()
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(out)) != m.oldMetaLen {
		t.Errorf("metadata is %d bytes, want %d", len(out), m.oldMetaLen)
	}
	hs := headersOf(t, out)
	if got := []uint8{hs[0].typ, hs[1].typ}; len(hs) != 2 || !slices.Equal(got, []uint8{blockTypeStreamInfo, blockTypeVorbisComment}) {
		t.Fatalf("blocks %+v, want STREAMINFO, VORBIS_COMMENT", hs)
	}
	if !hs[1].last {
		t.Error("the last real block lacks the is-last bit")
	}
}

func TestFBlock_IsLastOnlyOnFinal(t *testing.T) {
	src := richFLAC()
	// Move PADDING (the source's last block) to second place: serialize must
	// set the is-last bit on whatever ends up last, and only there.
	m := metaOf(t, src)
	m.blocks = append([]fblock{m.blocks[0], m.blocks[len(m.blocks)-1]}, m.blocks[1:len(m.blocks)-1]...)
	grow(t, m, 300)
	out, err := m.serialize()
	if err != nil {
		t.Fatal(err)
	}
	hs := headersOf(t, out)
	for i, h := range hs {
		if h.last != (i == len(hs)-1) {
			t.Errorf("block %d (type %d): is-last %v", i, h.typ, h.last)
		}
	}
}

func TestFBlock_Corrupted(t *testing.T) {
	si := flacBlock(false, blockTypeStreamInfo, streamInfoPayload(44100, 44100))
	tests := map[string][]byte{
		"STREAMINFO not first": buildFLAC(flacBlock(false, blockTypeVorbisComment, vorbisCommentPayload()), flacBlock(true, blockTypeStreamInfo, streamInfoPayload(44100, 44100))),
		"truncated chain":      append(buildFLAC(si, flacBlock(false, blockTypePadding, make([]byte, 8))), 0x01, 0x00),
		"no last block":        buildFLAC(si, flacBlock(false, blockTypePadding, make([]byte, 8))),
		"block past EOF":       buildFLAC(si, flacBlock(true, blockTypePadding, make([]byte, 8)))[:50],
		"no magic":             append([]byte("ID3\x04"), si...),
	}
	for name, src := range tests {
		_, err := readFMeta(context.Background(), srOf(src), int64(len(src)))
		if _, ok := errors.AsType[*types.CorruptedFileError](err); !ok {
			t.Errorf("%s: err = %v, want CorruptedFileError", name, err)
		}
		if _, err := tryWrite(src, editsFor(t, simpleFLAC(8), nil)); err == nil {
			t.Errorf("%s: WriteFile succeeded", name)
		}
	}
}

func TestFBlock_TooLarge(t *testing.T) {
	if err := checkBlockLen(0xFFFFFF); err != nil {
		t.Errorf("0xFFFFFF: %v", err)
	}
	if err := checkBlockLen(0x1000000); !errors.Is(err, types.ErrTagTooLarge) {
		t.Errorf("0x1000000: err = %v, want ErrTagTooLarge", err)
	}
}

// pictureFLAC has a front and a back cover, a VORBIS_COMMENT and padding.
func pictureFLAC() []byte {
	return append(buildFLAC(
		flacBlock(false, blockTypeStreamInfo, streamInfoPayload(44100, 44100)),
		flacBlock(false, blockTypePicture, picturePayload(3, "image/png", []byte("front"))),
		flacBlock(false, blockTypePicture, picturePayload(4, "image/jpeg", []byte("back"))),
		flacBlock(false, blockTypeVorbisComment, vorbisCommentPayload("TITLE=T")),
		flacBlock(true, blockTypePadding, make([]byte, 512)),
	), audioTail...)
}

func TestPicture_ReplaceFront(t *testing.T) {
	src := pictureFLAC()
	art := types.Artwork{MIMEType: "image/png", Description: "Cover ✓", Data: []byte("new front"), Type: types.ArtworkFrontCover, Width: 600, Height: 400}
	out := writeBytes(t, src, editsFor(t, src, nil,
		types.ArtworkOp{Kind: types.ArtworkOpRemove, Type: types.ArtworkFrontCover},
		types.ArtworkOp{Kind: types.ArtworkOpAdd, Art: art}))

	got := extractTestArtwork(t, out)
	if len(got) != 2 || got[0].Type != types.ArtworkBackCover || got[1].Type != types.ArtworkFrontCover {
		t.Fatalf("artwork %v, want back then front", got)
	}
	if a := got[1]; !bytes.Equal(a.Data, art.Data) || a.MIMEType != art.MIMEType || a.Description != art.Description || a.Width != 600 || a.Height != 400 {
		t.Errorf("new front = %+v", a)
	}
	before := blocksOfType(metaOf(t, src).blocks, blockTypePicture)[1].payload
	if after := blocksOfType(metaOf(t, out).blocks, blockTypePicture)[0].payload; !bytes.Equal(before, after) {
		t.Error("back cover payload changed")
	}
	if f := parseBytes(t, out); f.Tags.Title != "T" {
		t.Errorf("Title = %q", f.Tags.Title)
	}
}

func TestPicture_RemoveByType(t *testing.T) {
	src := pictureFLAC()
	out := writeBytes(t, src, editsFor(t, src, nil, types.ArtworkOp{Kind: types.ArtworkOpRemove, Type: types.ArtworkBackCover}))
	if got := extractTestArtwork(t, out); len(got) != 1 || got[0].Type != types.ArtworkFrontCover {
		t.Errorf("artwork %v, want the front cover only", got)
	}
}

func TestPicture_RemoveAll(t *testing.T) {
	src := pictureFLAC()
	out := writeBytes(t, src, editsFor(t, src, nil, types.ArtworkOp{Kind: types.ArtworkOpRemoveAll}))
	if got := extractTestArtwork(t, out); len(got) != 0 {
		t.Errorf("%d pictures left", len(got))
	}
	if int64(len(out)) != int64(len(src)) {
		t.Errorf("output is %d bytes, want %d (padding absorbs the shrink)", len(out), len(src))
	}
}

func TestPicture_AddPlacement(t *testing.T) {
	src := richFLAC()
	art := types.Artwork{MIMEType: "image/png", Data: []byte("img"), Type: types.ArtworkFrontCover}
	out := writeBytes(t, src, editsFor(t, src, nil, types.ArtworkOp{Kind: types.ArtworkOpAdd, Art: art}))
	want := []uint8{blockTypeStreamInfo, blockTypeSeekTable, blockTypeApplication, blockTypeCueSheet, blockTypeVorbisComment, blockTypePicture, blockTypePadding}
	if got := blockTypes(metaOf(t, out).blocks); !slices.Equal(got, want) {
		t.Errorf("blocks %v, want %v", got, want)
	}

	// With pictures present, a new one goes after the last of them.
	src = pictureFLAC()
	out = writeBytes(t, src, editsFor(t, src, nil, types.ArtworkOp{Kind: types.ArtworkOpAdd, Art: art}))
	want = []uint8{blockTypeStreamInfo, blockTypePicture, blockTypePicture, blockTypePicture, blockTypeVorbisComment, blockTypePadding}
	if got := blockTypes(metaOf(t, out).blocks); !slices.Equal(got, want) {
		t.Errorf("blocks %v, want %v", got, want)
	}
	if got := extractTestArtwork(t, out); len(got) != 3 || !bytes.Equal(got[2].Data, art.Data) {
		t.Errorf("artwork %v, want the new picture last", got)
	}
}

func TestPicture_InvalidRefused(t *testing.T) {
	src := pictureFLAC()
	for name, art := range map[string]types.Artwork{
		"non-ASCII MIME": {MIMEType: "image/pngé", Data: []byte("x"), Type: types.ArtworkFrontCover},
		"bad type":       {MIMEType: "image/png", Data: []byte("x"), Type: types.ArtworkPublisherLogotype + 1},
		"bad desc":       {MIMEType: "image/png", Description: "\xff", Data: []byte("x"), Type: types.ArtworkFrontCover},
	} {
		_, err := tryWrite(src, editsFor(t, src, nil, types.ArtworkOp{Kind: types.ArtworkOpAdd, Art: art}))
		if !errors.Is(err, types.ErrUnsupportedWrite) {
			t.Errorf("%s: err = %v, want ErrUnsupportedWrite", name, err)
		}
	}
}

func TestVC_CreatedWhenMissing(t *testing.T) {
	for name, tc := range map[string]struct {
		src  []byte
		want []uint8
	}{
		"after STREAMINFO": {
			src: append(buildFLAC(
				flacBlock(false, blockTypeStreamInfo, streamInfoPayload(44100, 44100)),
				flacBlock(false, blockTypeApplication, []byte("TEST")),
				flacBlock(true, blockTypePadding, make([]byte, 64)),
			), audioTail...),
			want: []uint8{blockTypeStreamInfo, blockTypeVorbisComment, blockTypeApplication, blockTypePadding},
		},
		"after SEEKTABLE": {
			src: append(buildFLAC(
				flacBlock(false, blockTypeStreamInfo, streamInfoPayload(44100, 44100)),
				flacBlock(true, blockTypeSeekTable, make([]byte, 18)),
			), audioTail...),
			want: []uint8{blockTypeStreamInfo, blockTypeSeekTable, blockTypeVorbisComment, blockTypePadding},
		},
	} {
		out := writeBytes(t, tc.src, editsFor(t, tc.src, func(tg *types.Tags) { tg.Title = "New" }))
		if got := blockTypes(metaOf(t, out).blocks); !slices.Equal(got, tc.want) {
			t.Errorf("%s: blocks %v, want %v", name, got, tc.want)
		}
		v := commentsOf(t, out)
		if string(v.vendor) != "audiometa" || !slices.Equal(strs(v.comments), []string{"TITLE=New"}) {
			t.Errorf("%s: vendor %q, comments %q", name, v.vendor, strs(v.comments))
		}
		if f := parseBytes(t, out); f.Tags.Title != "New" {
			t.Errorf("%s: Title = %q", name, f.Tags.Title)
		}
	}
}

func TestVC_MultipleBlocksRefused(t *testing.T) {
	src := append(buildFLAC(
		flacBlock(false, blockTypeStreamInfo, streamInfoPayload(44100, 44100)),
		flacBlock(false, blockTypeVorbisComment, vorbisCommentPayload("TITLE=A")),
		flacBlock(false, blockTypeVorbisComment, vorbisCommentPayload("ALBUM=B")),
		flacBlock(true, blockTypePadding, make([]byte, 64)),
	), audioTail...)
	if _, err := tryWrite(src, editsFor(t, src, func(tg *types.Tags) { tg.Title = "X" })); !errors.Is(err, types.ErrUnsupportedWrite) {
		t.Errorf("tag edit: err = %v, want ErrUnsupportedWrite", err)
	}

	art := types.Artwork{MIMEType: "image/png", Data: []byte("img"), Type: types.ArtworkFrontCover}
	out := writeBytes(t, src, editsFor(t, src, nil, types.ArtworkOp{Kind: types.ArtworkOpAdd, Art: art}))
	before, after := blocksOfType(metaOf(t, src).blocks, blockTypeVorbisComment), blocksOfType(metaOf(t, out).blocks, blockTypeVorbisComment)
	if len(after) != 2 || !bytes.Equal(before[0].payload, after[0].payload) || !bytes.Equal(before[1].payload, after[1].payload) {
		t.Error("VORBIS_COMMENT blocks changed on an artwork-only edit")
	}
}

func TestVC_MalformedBlockCorrupted(t *testing.T) {
	bad := vorbisCommentPayload("TITLE=A")
	bad[len(bad)-8] = 0xFF // comment length runs past the block
	src := append(buildFLAC(
		flacBlock(false, blockTypeStreamInfo, streamInfoPayload(44100, 44100)),
		flacBlock(true, blockTypeVorbisComment, bad),
	), audioTail...)
	edits := &types.EditSet{Keys: []types.KeyEdit{{Key: "TITLE", Values: []string{"X"}}}, Orig: &types.Tags{}, Final: &types.Tags{}}
	_, err := tryWrite(src, edits)
	if _, ok := errors.AsType[*types.CorruptedFileError](err); !ok {
		t.Errorf("err = %v, want CorruptedFileError", err)
	}
}

func TestWriter_AudioVerbatim(t *testing.T) {
	src := simpleFLAC(8, "TITLE=T")
	out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Title = strings.Repeat("long ", 100) }))
	m := metaOf(t, out)
	if m.audioStart == int64(len(src)-len(audioTail)) {
		t.Fatal("audio did not move; the test needs a growing edit")
	}
	if !bytes.Equal(out[m.audioStart:], audioTail) {
		t.Error("audio changed")
	}
}

func TestWriter_Deterministic(t *testing.T) {
	src := pictureFLAC()
	edits := editsFor(t, src, func(tg *types.Tags) {
		tg.Title = "X"
		tg.Narrator = "N"
		tg.Set("ZZZ", "1", "2")
	}, types.ArtworkOp{Kind: types.ArtworkOpAdd, Art: types.Artwork{MIMEType: "image/png", Data: []byte("img"), Type: types.ArtworkFrontCover}})
	if a, b := writeBytes(t, src, edits), writeBytes(t, src, edits); !bytes.Equal(a, b) {
		t.Error("two writes differ")
	}
}

func TestWriter_InPlaceEqualsFullRewrite(t *testing.T) {
	src := pictureFLAC()
	edits := editsFor(t, src, func(tg *types.Tags) { tg.Title = "Another title" }, types.ArtworkOp{Kind: types.ArtworkOpRemoveAll})
	full := writeBytes(t, src, edits)

	off, patch, ok, err := (&writer{}).InPlacePatch(context.Background(), bytes.NewReader(src), int64(len(src)), edits)
	if err != nil || !ok {
		t.Fatalf("InPlacePatch: ok %v, err %v", ok, err)
	}
	if off != 4 {
		t.Errorf("off = %d, want 4", off)
	}
	patched := slices.Clone(src)
	copy(patched[off:], patch)
	if !bytes.Equal(patched, full) {
		t.Error("in-place result differs from WriteFile")
	}
}

func TestWriter_InPlaceNotOK(t *testing.T) {
	src := simpleFLAC(8, "TITLE=T")
	edits := editsFor(t, src, func(tg *types.Tags) { tg.Title = "A title that does not fit in the padding" })
	_, _, ok, err := (&writer{}).InPlacePatch(context.Background(), bytes.NewReader(src), int64(len(src)), edits)
	if ok || err != nil {
		t.Errorf("ok %v, err %v; want false, nil", ok, err)
	}
}

func TestWriter_AudioDigest(t *testing.T) {
	for name, tc := range map[string]struct {
		data  []byte
		start int64
	}{
		"sample.flac":       {corpusFile(t, "sample.flac"), 8618},
		"sample_upper.flac": {nil, -1},
		"synthetic":         {richFLAC(), int64(len(richFLAC()) - len(audioTail))},
	} {
		if tc.data == nil {
			tc.data = corpusFile(t, name)
			tc.start = metaOf(t, tc.data).audioStart
		}
		got, err := (&writer{}).AudioDigest(context.Background(), bytes.NewReader(tc.data), int64(len(tc.data)))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if want := sha256.Sum256(tc.data[tc.start:]); !bytes.Equal(got, want[:]) {
			t.Errorf("%s: digest mismatch", name)
		}
	}
}

// corpusFile reads a file from the repository's testdata directory.
func corpusFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestWriter_Corpus(t *testing.T) {
	for _, name := range []string{"sample.flac", "sample_upper.flac"} {
		src := corpusFile(t, name)
		if out := writeBytes(t, src, editsFor(t, src, nil)); !bytes.Equal(out, src) {
			t.Errorf("%s: rewrite with no edits is not byte-identical", name)
		}

		out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Title = "Edited" }))
		before, after := parseBytes(t, src), parseBytes(t, out)
		if after.Tags.Title != "Edited" || after.Tags.Artist != before.Tags.Artist || after.Tags.Album != before.Tags.Album {
			t.Errorf("%s: Title %q, Artist %q, Album %q", name, after.Tags.Title, after.Tags.Artist, after.Tags.Album)
		}
		if a, b := extractTestArtwork(t, src), extractTestArtwork(t, out); len(a) != len(b) || (len(a) > 0 && !bytes.Equal(a[0].Data, b[0].Data)) {
			t.Errorf("%s: artwork changed", name)
		}
		m, n := metaOf(t, src), metaOf(t, out)
		for _, typ := range []uint8{blockTypeStreamInfo, blockTypeSeekTable, blockTypePicture} {
			x, y := blocksOfType(m.blocks, typ), blocksOfType(n.blocks, typ)
			if !slices.EqualFunc(x, y, func(a, b fblock) bool { return bytes.Equal(a.payload, b.payload) }) {
				t.Errorf("%s: blocks of type %d changed", name, typ)
			}
		}
		if string(commentsOf(t, out).vendor) != string(commentsOf(t, src).vendor) {
			t.Errorf("%s: vendor changed", name)
		}
		if n.audioStart != m.audioStart {
			t.Errorf("%s: audio moved from %d to %d", name, m.audioStart, n.audioStart)
		}
	}
}

func TestWriter_DeleteEveryKey(t *testing.T) {
	src := corpusFile(t, "sample_upper.flac")
	edits := editsFor(t, src, func(tg *types.Tags) {
		var keys []string
		for k := range tg.All() {
			keys = append(keys, k)
		}
		tg.Delete(keys...)
	})
	out := writeBytes(t, src, edits)
	v := commentsOf(t, out)
	if len(v.comments) != 0 || string(v.vendor) != "reference libFLAC 1.5.0 20250211" {
		t.Errorf("vendor %q, comments %q; want the vendor only", v.vendor, strs(v.comments))
	}
	if f := parseBytes(t, out); len(maps.Collect(f.Tags.All())) != 0 {
		t.Errorf("raw tags left: %v", maps.Collect(f.Tags.All()))
	}
}

func FuzzWriteFLAC(f *testing.F) {
	f.Add(richFLAC())
	f.Add(pictureFLAC())
	f.Add(simpleFLAC(8, "TITLE=T"))
	edits := &types.EditSet{
		Keys:    []types.KeyEdit{{Key: "TITLE", Values: []string{"Fuzz"}}},
		Fields:  []types.FieldEdit{{Field: types.FieldYear, Values: []string{"1999"}}},
		Artwork: []types.ArtworkOp{{Kind: types.ArtworkOpRemove, Type: types.ArtworkFrontCover}, {Kind: types.ArtworkOpAdd, Art: types.Artwork{MIMEType: "image/png", Data: []byte("img"), Type: types.ArtworkFrontCover}}},
		Orig:    &types.Tags{},
		Final:   &types.Tags{},
	}
	w := &writer{}
	f.Fuzz(func(t *testing.T, data []byte) {
		out, err := tryWrite(data, edits)
		if err != nil {
			return
		}
		// A successful write is a well-formed FLAC with the same audio.
		ctx := context.Background()
		a, errA := w.AudioDigest(ctx, bytes.NewReader(data), int64(len(data)))
		b, errB := w.AudioDigest(ctx, bytes.NewReader(out), int64(len(out)))
		if errA != nil || errB != nil || !bytes.Equal(a, b) {
			t.Fatalf("audio digest: %v, %v, equal %v", errA, errB, bytes.Equal(a, b))
		}
		if v, ok := commentValue(t, out, "TITLE"); !ok || v != "Fuzz" {
			t.Fatalf("TITLE = %q, %v", v, ok)
		}
	})
}

// commentValue returns the first value of key in data's only VORBIS_COMMENT block.
func commentValue(t *testing.T, data []byte, key string) (string, bool) {
	t.Helper()
	return commentsOf(t, data).first(key)
}
