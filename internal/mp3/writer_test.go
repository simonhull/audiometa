package mp3

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/registry"
	"github.com/simonhull/audiometa/internal/types"
)

// srOf wraps data in a SafeReader.
func srOf(data []byte) *binutil.SafeReader {
	return binutil.NewSafeReader(bytes.NewReader(data), int64(len(data)), "t.mp3")
}

func TestWriter_Registered(t *testing.T) {
	if _, ok := registry.GetWriter(types.FormatMP3).(*writer); !ok {
		t.Error("no MP3 writer registered")
	}
}

func TestWriter_ID3v1Verbatim(t *testing.T) {
	v1 := id3v1Trailer("Old Title", "Old Artist", "Album", "1999", "c", 7)
	src := append(file(id3Tag(3, id3Frame(3, "TIT2", latin1Text("T")), id3Frame(3, "TPE1", latin1Text("A")))), v1...)
	out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) {
		tg.Title = "New Title"
		tg.Artist = "New Artist"
	}))
	if !bytes.Equal(out[len(out)-id3v1Size:], src[len(src)-id3v1Size:]) {
		t.Error("ID3v1 trailer changed")
	}
}

func TestWriter_AudioRegionVerbatim(t *testing.T) {
	audio := bytes.Repeat(mpegFrame, 4096/len(mpegFrame))
	tag := id3TagFlags(4, 0, 16, id3Frame(4, "TIT2", latin1Text("T")))
	src := append(append(slices0(tag), audio...), id3v1Trailer("T", "", "", "", "", 0)...)

	out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Title = "A much longer title than before" }))
	newTagEnd := framesOf(t, out).oldTotal
	if !bytes.Equal(out[newTagEnd:len(out)-id3v1Size], audio) {
		t.Error("audio region changed")
	}
}

// slices0 returns a copy of b.
func slices0(b []byte) []byte { return append([]byte(nil), b...) }

func TestWriter_Deterministic(t *testing.T) {
	src := file(id3Tag(4, id3Frame(4, "TIT2", latin1Text("T")), id3Frame(4, "TXXX", txxxPayload(3, "A", "1"))))
	edits := editsFor(t, src, func(tg *types.Tags) {
		tg.Title = "X"
		tg.Narrator = "N"
		tg.Set("ZZZ", "1", "2")
	}, types.ArtworkOp{Kind: types.ArtworkOpAdd, Art: types.Artwork{MIMEType: "image/png", Data: testPNG(t), Type: types.ArtworkFrontCover}})
	if a, b := writeBytes(t, src, edits), writeBytes(t, src, edits); !bytes.Equal(a, b) {
		t.Error("two writes differ")
	}
}

func TestWriter_InPlaceEqualsFullRewrite(t *testing.T) {
	src := append(file(id3TagFlags(3, 0, 256, id3Frame(3, "TIT2", latin1Text("Title")))), id3v1Trailer("T", "", "", "", "", 0)...)
	edits := editsFor(t, src, func(tg *types.Tags) { tg.Title = "Another title" })
	full := writeBytes(t, src, edits)

	off, patch, ok, err := (&writer{}).InPlacePatch(context.Background(), bytes.NewReader(src), int64(len(src)), edits)
	if err != nil || !ok {
		t.Fatalf("InPlacePatch: ok %v, err %v", ok, err)
	}
	patched := slices0(src)
	copy(patched[off:], patch)
	if !bytes.Equal(patched, full) {
		t.Error("in-place result differs from WriteFile")
	}
}

func TestWriter_InPlaceNotOK(t *testing.T) {
	tests := map[string][]byte{
		"grows":    file(id3Tag(3, id3Frame(3, "TIT2", latin1Text("Title")))),
		"untagged": audioTail,
	}
	for name, src := range tests {
		edits := editsFor(t, src, func(tg *types.Tags) { tg.Title = "A title that does not fit in the old tag" })
		_, _, ok, err := (&writer{}).InPlacePatch(context.Background(), bytes.NewReader(src), int64(len(src)), edits)
		if ok || err != nil {
			t.Errorf("%s: ok %v, err %v; want false, nil", name, ok, err)
		}
	}
}

func TestWriter_AudioDigest(t *testing.T) {
	tag := id3Tag(4, id3Frame(4, "TIT2", latin1Text("T")))
	footer := id3TagFlags(4, 0x10, 0, id3Frame(4, "TIT2", latin1Text("T")))
	footer = append(footer, []byte("3DI\x04\x00\x10\x00\x00\x00\x00")...)
	v1 := id3v1Trailer("T", "", "", "", "", 0)
	tests := []struct {
		name       string
		data       []byte
		start, end int
	}{
		{"tagged", file(tag), len(tag), len(tag) + len(audioTail)},
		{"untagged", audioTail, 0, len(audioTail)},
		{"v1 trailer", append(file(tag), v1...), len(tag), len(tag) + len(audioTail)},
		{"v2.4 footer", file(footer), len(footer), len(footer) + len(audioTail)},
	}
	for _, tt := range tests {
		got, err := (&writer{}).AudioDigest(context.Background(), bytes.NewReader(tt.data), int64(len(tt.data)))
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if want := sha256.Sum256(tt.data[tt.start:tt.end]); !bytes.Equal(got, want[:]) {
			t.Errorf("%s: digest mismatch", tt.name)
		}
	}
}

func TestWriter_Corpus(t *testing.T) {
	for _, name := range []string{"sample.mp3", "sample_v24.mp3", "sample_mono_vbr.mp3"} {
		src, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		out := writeBytes(t, src, editsFor(t, src, nil))
		if !bytes.Equal(out, src) {
			t.Errorf("%s: rewrite with no edits is not byte-identical", name)
		}
		before, after := framesOf(t, src).frames, framesOf(t, out).frames
		for i := range max(len(before), len(after)) {
			if i >= len(before) || i >= len(after) || !sameFrames(before[i:i+1], after[i:i+1]) {
				t.Fatalf("%s: frame %d differs (%v vs %v)", name, i, frameIDs(before), frameIDs(after))
			}
		}
		if p := parseBytes(t, out); p.Tags.Title != "Test Title" && name != "sample_mono_vbr.mp3" {
			t.Errorf("%s: Title = %q", name, p.Tags.Title)
		}
	}
}

func TestWriter_TruncatedTagRefused(t *testing.T) {
	src := id3Tag(3, id3Frame(3, "TIT2", latin1Text("Title")))
	src = src[:len(src)-3]
	if _, err := tryWrite(src, &types.EditSet{Orig: &types.Tags{}, Final: &types.Tags{}}); err == nil {
		t.Error("writing a truncated tag succeeded")
	}
}

func TestPreservedDigestCoversTrailingTags(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../testdata/sample.mp3")
	if err != nil {
		t.Fatal(err)
	}
	ape := slices.Concat([]byte("APETAGEX"), []byte{0xD0, 0x07, 0, 0, 32, 0, 0, 0}, make([]byte, 16))
	cut := len(data) // the sample has no ID3v1 trailer
	tagged := slices.Concat(data[:cut], ape, data[cut:])
	damaged := slices.Clone(tagged)
	damaged[cut+28] ^= 0xFF // inside the APE footer's reserved bytes

	w := &writer{}
	digest := func(b []byte, f func(context.Context, io.ReaderAt, int64) ([]byte, error)) []byte {
		sum, err := f(t.Context(), bytes.NewReader(b), int64(len(b)))
		if err != nil {
			t.Fatal(err)
		}
		return sum
	}
	if !bytes.Equal(digest(tagged, w.AudioDigest), digest(damaged, w.AudioDigest)) {
		t.Error("AudioDigest must ignore the trailing APE tag")
	}
	if bytes.Equal(digest(tagged, w.PreservedDigest), digest(damaged, w.PreservedDigest)) {
		t.Error("PreservedDigest must change when the trailing APE tag is damaged")
	}
}
