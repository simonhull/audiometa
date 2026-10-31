package flac

import (
	"bytes"
	"context"
	"slices"
	"testing"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// audioTail stands in for the audio frames after the metadata blocks.
var audioTail = bytes.Repeat([]byte{0xFF, 0xF8, 0x69, 0x18, 0x00, 0x00, 0xBF, 0x03}, 8)

// srOf wraps data in a SafeReader.
func srOf(data []byte) *binutil.SafeReader {
	return binutil.NewSafeReader(bytes.NewReader(data), int64(len(data)), "t.flac")
}

// metaOf reads data's metadata blocks.
func metaOf(t *testing.T, data []byte) *fmeta {
	t.Helper()
	m, err := readFMeta(context.Background(), srOf(data), int64(len(data)))
	if err != nil {
		t.Fatalf("readFMeta: %v", err)
	}
	return m
}

// blockTypes lists the types of a block list.
func blockTypes(blocks []fblock) []uint8 {
	out := make([]uint8, len(blocks))
	for i, b := range blocks {
		out[i] = b.typ
	}
	return out
}

// blocksOfType returns the blocks of type typ.
func blocksOfType(blocks []fblock, typ uint8) []fblock {
	return slices.DeleteFunc(slices.Clone(blocks), func(b fblock) bool { return b.typ != typ })
}

// parseBytes parses data as FLAC.
func parseBytes(t *testing.T, data []byte) *types.File {
	t.Helper()
	f, err := (&parser{}).Parse(context.Background(), bytes.NewReader(data), int64(len(data)), "t.flac")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return f
}

// editsFor builds the EditSet that mutate (applied to src's tags) produces,
// plus the given artwork operations.
func editsFor(t *testing.T, src []byte, mutate func(*types.Tags), art ...types.ArtworkOp) *types.EditSet {
	t.Helper()
	orig := parseBytes(t, src).Tags
	types.BindFormat(&orig, types.FormatFLAC)
	cur := orig.Clone()
	types.BindFormat(cur, types.FormatFLAC)
	if mutate != nil {
		mutate(cur)
	}
	keys, fields, err := types.Diff(&orig, cur, types.FormatFLAC)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	return &types.EditSet{Keys: keys, Fields: fields, Artwork: art, Orig: &orig, Final: cur}
}

// tryWrite runs the writer over src.
func tryWrite(src []byte, edits *types.EditSet) ([]byte, error) {
	var buf bytes.Buffer
	n, err := (&writer{}).WriteFile(context.Background(), &buf, bytes.NewReader(src), int64(len(src)), edits)
	if err == nil && n != int64(buf.Len()) {
		panic("WriteFile miscounted its output")
	}
	return buf.Bytes(), err
}

// writeBytes runs the writer over src and returns its output.
func writeBytes(t *testing.T, src []byte, edits *types.EditSet) []byte {
	t.Helper()
	out, err := tryWrite(src, edits)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return out
}

// commentsOf returns the raw comments of data's only VORBIS_COMMENT block.
func commentsOf(t *testing.T, data []byte) *vcomment {
	t.Helper()
	vcs := blocksOfType(metaOf(t, data).blocks, blockTypeVorbisComment)
	if len(vcs) != 1 {
		t.Fatalf("%d VORBIS_COMMENT blocks, want 1", len(vcs))
	}
	v, err := parseVC(vcs[0].payload)
	if err != nil {
		t.Fatalf("parseVC: %v", err)
	}
	return v
}

// strs converts raw comments to strings.
func strs(comments [][]byte) []string {
	out := make([]string, len(comments))
	for i, c := range comments {
		out[i] = string(c)
	}
	return out
}

// header is a decoded metadata block header.
type header struct {
	typ  uint8
	last bool
	n    int
}

// headersOf walks the block headers of serialized metadata (without "fLaC").
func headersOf(t *testing.T, meta []byte) []header {
	t.Helper()
	var hs []header
	for off := 0; off < len(meta); {
		if len(meta)-off < 4 {
			t.Fatalf("truncated block header at %d", off)
		}
		h := header{typ: meta[off] & 0x7F, last: meta[off]&0x80 != 0, n: int(meta[off+1])<<16 | int(meta[off+2])<<8 | int(meta[off+3])}
		hs = append(hs, h)
		off += 4 + h.n
	}
	return hs
}

// simpleFLAC builds STREAMINFO, a VORBIS_COMMENT of comments, PADDING(pad,
// last) and audioTail.
func simpleFLAC(pad int, comments ...string) []byte {
	return append(buildFLAC(
		flacBlock(false, blockTypeStreamInfo, streamInfoPayload(44100, 44100)),
		flacBlock(false, blockTypeVorbisComment, vorbisCommentPayload(comments...)),
		flacBlock(true, blockTypePadding, make([]byte, pad)),
	), audioTail...)
}
