package m4a

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/simonhull/audiometa/internal/registry"
	"github.com/simonhull/audiometa/internal/types"
)

func TestWriter_Registered(t *testing.T) {
	for _, f := range []types.Format{types.FormatM4A, types.FormatM4B} {
		if _, ok := registry.GetWriter(f).(*writer); !ok {
			t.Errorf("no writer registered for %s", f)
		}
	}
}

// layoutOf plans a write of src with mutate applied.
func layoutOf(t *testing.T, src []byte, mutate func(*types.Tags)) *layout {
	t.Helper()
	l, err := planWrite(context.Background(), srOf(src), int64(len(src)), editsFor(t, src, mutate))
	if err != nil {
		t.Fatalf("planWrite: %v", err)
	}
	return l
}

// titled builds a file whose only item is a 20-character title.
func titled(faststart bool) []byte {
	return buildMP4(faststart, ilstText("\xA9nam", strings.Repeat("t", 20)))
}

// growTitle returns a mutation that makes the 20-character title n bytes longer.
func growTitle(n int) func(*types.Tags) {
	return func(tg *types.Tags) { tg.Title = strings.Repeat("T", 20+n) }
}

// assertAudioAtOffsets checks that every chunk offset of out points at the
// original mdat payload.
func assertAudioAtOffsets(t *testing.T, out []byte) {
	t.Helper()
	for _, o := range chunkOffsets(t, out) {
		if !bytes.Equal(payloadAt(t, out, o), mdatPayload()) {
			t.Errorf("chunk offset %d does not point at the audio payload", o)
		}
	}
}

func TestLayout_FitsInFree(t *testing.T) {
	src := titled(true)
	out := writeTags(t, src, growTitle(-3))
	if l := layoutOf(t, src, growTitle(-3)); l.delta != 0 || l.free != 11 {
		t.Errorf("delta = %d, free = %d; want 0 and 11", l.delta, l.free)
	}
	if len(out) != len(src) {
		t.Errorf("length %d, want %d", len(out), len(src))
	}
	if topAtomOf(t, out, atomMdat) != topAtomOf(t, src, atomMdat) {
		t.Error("mdat moved")
	}
	if f := topAtomOf(t, out, atomFree); f.end-f.start != 11 {
		t.Errorf("free is %d bytes, want 11", f.end-f.start)
	}
	if !slices.Equal(chunkOffsets(t, out), chunkOffsets(t, src)) {
		t.Error("chunk offsets changed")
	}
	assertAudioAtOffsets(t, out)
}

func TestLayout_ExactFit(t *testing.T) {
	src := titled(true)
	out := writeTags(t, src, growTitle(8))
	if l := layoutOf(t, src, growTitle(8)); l.delta != 0 || l.free != 0 {
		t.Errorf("delta = %d, free = %d; want 0 and 0", l.delta, l.free)
	}
	if got := topTypes(t, out); !slices.Equal(got, []string{"ftyp", atomMoov, atomMdat}) {
		t.Errorf("top level = %v", got)
	}
	if len(out) != len(src) {
		t.Errorf("length %d, want %d", len(out), len(src))
	}
	assertAudioAtOffsets(t, out)
}

func TestLayout_GrowShiftsOffsets(t *testing.T) {
	src := buildMP4With(mp4Opts{faststart: true, items: [][]byte{ilstText("\xA9nam", strings.Repeat("t", 20))}, extra: []uint64{0}})
	// The second chunk offset (0) lies before moov and must not move.
	before := chunkOffsets(t, src)
	l := layoutOf(t, src, growTitle(100))
	region := int64(topAtomOf(t, src, atomMdat).start - topAtomOf(t, src, atomMoov).start)
	if want := int64(len(l.moov)) + growFree - region; l.delta != want || l.delta <= 0 {
		t.Fatalf("delta = %d, want %d", l.delta, want)
	}
	out := writeTags(t, src, growTitle(100))
	after := chunkOffsets(t, out)
	if after[0] != before[0]+uint64(l.delta) {
		t.Errorf("offset %d -> %d, want +%d", before[0], after[0], l.delta)
	}
	if after[1] != 0 {
		t.Errorf("offset before moov moved: %d", after[1])
	}
	if !bytes.Equal(payloadAt(t, out, after[0]), mdatPayload()) {
		t.Error("shifted offset does not point at the audio payload")
	}
	if f := topAtomOf(t, out, atomFree); f.end-f.start != growFree {
		t.Errorf("free is %d bytes, want %d", f.end-f.start, growFree)
	}
	if int64(len(out)-len(src)) != l.delta {
		t.Errorf("file grew by %d, want delta %d", len(out)-len(src), l.delta)
	}
}

func TestLayout_Remainder1to7Grows(t *testing.T) {
	src := titled(true)
	l := layoutOf(t, src, growTitle(5)) // 8 bytes of free, 5 used: 3 left
	if l.delta != growFree-3 || l.free != growFree {
		t.Errorf("delta = %d, free = %d; want %d and %d", l.delta, l.free, growFree-3, growFree)
	}
	assertAudioAtOffsets(t, writeTags(t, src, growTitle(5)))
}

func TestLayout_MoovLast(t *testing.T) {
	src := titled(false)
	out := writeTags(t, src, growTitle(100))
	if got := topTypes(t, out); !slices.Equal(got, []string{"ftyp", atomMdat, atomMoov}) {
		t.Errorf("top level = %v", got)
	}
	if !slices.Equal(chunkOffsets(t, out), chunkOffsets(t, src)) {
		t.Error("chunk offsets changed")
	}
	if len(out) != len(src)+100 {
		t.Errorf("length %d, want %d", len(out), len(src)+100)
	}
	assertAudioAtOffsets(t, out)
}

func TestLayout_MoovLastShrinkPads(t *testing.T) {
	src := titled(false)
	out := writeTags(t, src, growTitle(-10))
	if got := topTypes(t, out); !slices.Equal(got, []string{"ftyp", atomMdat, atomMoov, atomFree}) || len(out) != len(src) {
		t.Errorf("top level = %v, length %d", got, len(out))
	}
}

func TestShift_Co64(t *testing.T) {
	src := buildMP4With(mp4Opts{faststart: true, co64: true, items: [][]byte{ilstText("\xA9nam", "t")}})
	before := chunkOffsets(t, src)
	l := layoutOf(t, src, growTitle(100))
	out := writeTags(t, src, growTitle(100))
	after := chunkOffsets(t, out)
	if l.delta <= 0 || after[0] != before[0]+uint64(l.delta) {
		t.Errorf("co64 offset %d -> %d, delta %d", before[0], after[0], l.delta)
	}
	assertAudioAtOffsets(t, out)
}

func TestShift_StcoOverflowRefused(t *testing.T) {
	src := buildMP4With(mp4Opts{faststart: true, items: [][]byte{ilstText("\xA9nam", "t")}, extra: []uint64{0xFFFFFF00}})
	if _, err := tryTags(t, src, growTitle(100)); !errors.Is(err, types.ErrUnsupportedWrite) {
		t.Errorf("err = %v, want ErrUnsupportedWrite", err)
	}
	// The same offset in co64 shifts fine.
	src = buildMP4With(mp4Opts{faststart: true, co64: true, items: [][]byte{ilstText("\xA9nam", "t")}, extra: []uint64{0xFFFFFF00}})
	out := writeTags(t, src, growTitle(100))
	if got := chunkOffsets(t, out)[1]; got <= 0xFFFFFFFF {
		t.Errorf("co64 offset = %#x, want past 32 bits", got)
	}
}

func TestShift_OffsetIntoMoovRefused(t *testing.T) {
	build := func(o uint64) []byte {
		return buildMP4With(mp4Opts{faststart: true, items: [][]byte{ilstText("\xA9nam", strings.Repeat("t", 20))}, extra: []uint64{o}})
	}
	layout := build(0) // chunk offset values do not change the layout
	moovStart := uint64(topAtomOf(t, layout, atomMoov).start)
	for _, o := range []uint64{moovStart, moovStart + 10, uint64(topAtomOf(t, layout, atomFree).start)} {
		src := build(o)
		if _, err := tryTags(t, src, growTitle(100)); !errors.Is(err, types.ErrUnsupportedWrite) {
			t.Errorf("offset %d: err = %v, want ErrUnsupportedWrite", o, err)
		}
	}
}

func TestStructure_DRMRefused(t *testing.T) {
	for _, entry := range []string{"drms", "enca", "aavd", "drmi"} {
		src := buildMP4With(mp4Opts{entries: []string{"mp4a", entry}, items: [][]byte{ilstText("\xA9nam", "t")}})
		if _, err := tryTags(t, src, growTitle(1)); !errors.Is(err, types.ErrDRMProtected) {
			t.Errorf("%s: err = %v, want ErrDRMProtected", entry, err)
		}
	}
}

func TestStructure_FragmentedRefused(t *testing.T) {
	for name, o := range map[string]mp4Opts{
		"moof": {topExtra: atom("moof", atom("mfhd", make([]byte, 8)))},
		"mvex": {moovExtra: atom("mvex", atom("trex", make([]byte, 24)))},
	} {
		o.items = [][]byte{ilstText("\xA9nam", "t")}
		src := buildMP4With(o)
		if _, err := tryTags(t, src, growTitle(1)); !errors.Is(err, types.ErrUnsupportedWrite) {
			t.Errorf("%s: err = %v, want ErrUnsupportedWrite", name, err)
		}
	}
}

func TestStructure_MoovCount(t *testing.T) {
	src := titled(false)
	m := topAtomOf(t, src, atomMoov)
	for name, data := range map[string][]byte{
		"no moov":  src[:m.start],
		"two moov": append(slices.Clone(src), src[m.start:m.end]...),
	} {
		if _, err := tryWrite(data, editsFor(t, src, growTitle(1))); !isCorrupted(err) {
			t.Errorf("%s: err = %v, want CorruptedFileError", name, err)
		}
	}
}

func TestTopLevel_SizeZeroMdat(t *testing.T) {
	src := titled(true)
	mdat := topAtomOf(t, src, atomMdat)
	binary.BigEndian.PutUint32(src[mdat.start:], 0) // extends to EOF
	out := writeTags(t, src, growTitle(100))
	if !bytes.Equal(digestOf(t, out), digestOf(t, src)) {
		t.Error("audio digest changed")
	}
	for _, o := range chunkOffsets(t, out) {
		if !bytes.Equal(out[o:o+256], mdatPayload()) {
			t.Error("shifted offset does not point at the payload")
		}
	}
}

// isCorrupted reports whether err is a CorruptedFileError.
func isCorrupted(err error) bool {
	_, ok := errors.AsType[*types.CorruptedFileError](err)
	return ok
}

// digestOf returns the writer's audio digest of data.
func digestOf(t *testing.T, data []byte) []byte {
	t.Helper()
	d, err := (&writer{}).AudioDigest(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestWriter_InPlaceEqualsFullRewrite(t *testing.T) {
	for _, faststart := range []bool{true, false} {
		src := titled(faststart)
		edits := editsFor(t, src, growTitle(-10))
		full := writeBytes(t, src, edits)
		off, patch, ok, err := (&writer{}).InPlacePatch(context.Background(), bytes.NewReader(src), int64(len(src)), edits)
		if err != nil || !ok {
			t.Fatalf("faststart %v: InPlacePatch ok %v, err %v", faststart, ok, err)
		}
		patched := slices.Clone(src)
		copy(patched[off:], patch)
		if !bytes.Equal(patched, full) {
			t.Errorf("faststart %v: in-place result differs from WriteFile", faststart)
		}
	}
}

func TestWriter_InPlaceNotOK(t *testing.T) {
	for _, src := range [][]byte{titled(true), titled(false)} {
		_, _, ok, err := (&writer{}).InPlacePatch(context.Background(), bytes.NewReader(src), int64(len(src)), editsFor(t, src, growTitle(100)))
		if err != nil || ok {
			t.Errorf("grow: ok %v, err %v; want false, nil", ok, err)
		}
	}
}

func TestWriter_AudioDigest(t *testing.T) {
	for _, path := range []string{"../../testdata/sample.m4a", "../../testdata/sample.m4b"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		h := sha256.New()
		for _, a := range topAtoms(t, data) {
			if a.typ == atomMdat {
				h.Write(data[a.start+8 : a.end])
			}
		}
		if !bytes.Equal(digestOf(t, data), h.Sum(nil)) {
			t.Errorf("%s: digest differs from the oracle", path)
		}
	}
	src := titled(true)
	flipped := slices.Clone(src)
	flipped[topAtomOf(t, src, atomMdat).start+100] ^= 0xFF
	if bytes.Equal(digestOf(t, src), digestOf(t, flipped)) {
		t.Error("digest ignores an audio byte")
	}
}

func TestWriter_Deterministic(t *testing.T) {
	src := buildMP4(true, ilstText("\xA9nam", "t"), covrItem(covrImage{covrJPEG, []byte{1}}))
	edits := editsFor(t, src, func(tg *types.Tags) {
		tg.Title = strings.Repeat("x", 500)
		tg.Narrator = "N"
		tg.Set("ZZZ", "1", "2")
	}, types.ArtworkOp{Kind: types.ArtworkOpAdd, Art: types.Artwork{MIMEType: mimeTypePNG, Data: testPNG(t), Type: types.ArtworkFrontCover}})
	if a, b := writeBytes(t, src, edits), writeBytes(t, src, edits); !bytes.Equal(a, b) {
		t.Error("two writes differ")
	}
}

// moovChild returns the serialized atom at path in data's moov.
func moovChild(t *testing.T, data []byte, path ...string) []byte {
	t.Helper()
	a := findPath(moovTreeOf(t, data), path...)
	if a == nil {
		t.Fatalf("no %v", path)
	}
	return a.appendTo(nil)
}

func TestWriter_ChapterTrackPreserved(t *testing.T) {
	src, err := os.ReadFile("../../testdata/sample.m4b")
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*types.Tags){
		"title":   func(tg *types.Tags) { tg.Title = "New" },
		"comment": func(tg *types.Tags) { tg.Comment = strings.Repeat("c", 4096) },
	} {
		out := writeTags(t, src, mutate)
		before, after := parseBytes(t, src).Chapters, parseBytes(t, out).Chapters
		if len(before) != 2 || !slices.Equal(before, after) {
			t.Errorf("%s: chapters %+v, want %+v", name, after, before)
		}
		if !bytes.Equal(moovChild(t, out, atomUdta, "chpl"), moovChild(t, src, atomUdta, "chpl")) {
			t.Errorf("%s: chpl changed", name)
		}
		if !bytes.Equal(moovChild(t, out, atomTrak, "tref"), moovChild(t, src, atomTrak, "tref")) {
			t.Errorf("%s: tref changed", name)
		}
		if !bytes.Equal(digestOf(t, out), digestOf(t, src)) {
			t.Errorf("%s: audio digest changed", name)
		}
	}
}

func TestWriter_ContextCanceled(t *testing.T) {
	src := titled(true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var buf bytes.Buffer
	if _, err := (&writer{}).WriteFile(ctx, &buf, bytes.NewReader(src), int64(len(src)), editsFor(t, src, growTitle(1))); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestLayout_FaststartCorpusGrowth(t *testing.T) {
	for _, path := range []string{"../../testdata/sample_faststart.m4a", "../../testdata/sample_faststart.m4b"} {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		grow := func(tg *types.Tags) { tg.Comment = strings.Repeat("c", 4096) }
		l := layoutOf(t, src, grow)
		if l.delta <= 0 {
			t.Fatalf("%s: delta = %d, want growth", path, l.delta)
		}
		out := writeTags(t, src, grow)
		before, after := chunkOffsets(t, src), chunkOffsets(t, out)
		if len(before) == 0 || len(after) != len(before) {
			t.Fatalf("%s: %d chunk offsets, want %d", path, len(after), len(before))
		}
		for i := range before {
			if after[i] != before[i]+uint64(l.delta) {
				t.Errorf("%s: offset %d -> %d, want +%d", path, before[i], after[i], l.delta)
			}
			if n := min(64, uint64(len(src))-before[i]); !bytes.Equal(out[after[i]:after[i]+n], src[before[i]:before[i]+n]) {
				t.Errorf("%s: bytes at shifted offset %d differ", path, after[i])
			}
		}
		if !bytes.Equal(digestOf(t, out), digestOf(t, src)) {
			t.Errorf("%s: audio digest changed", path)
		}
	}
}
