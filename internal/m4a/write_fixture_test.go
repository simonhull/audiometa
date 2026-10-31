package m4a

import (
	"bytes"
	"context"
	"encoding/binary"
	"slices"
	"testing"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// atom builds an atom with an 8-byte header around the concatenated parts.
func atom(typ string, parts ...[]byte) []byte {
	body := bytes.Join(parts, nil)
	return append(binary.BigEndian.AppendUint32(nil, uint32(8+len(body))), append([]byte(typ), body...)...)
}

// extAtom builds an atom with a 16-byte (64-bit size) header.
func extAtom(typ string, parts ...[]byte) []byte {
	body := bytes.Join(parts, nil)
	b := binary.BigEndian.AppendUint32(nil, 1)
	b = append(b, typ...)
	b = binary.BigEndian.AppendUint64(b, uint64(16+len(body)))
	return append(b, body...)
}

// dataAtom builds an ilst data atom of the given type with locale 0.
func dataAtom(dataType uint32, value []byte) []byte {
	return atom("data", binary.BigEndian.AppendUint32(nil, dataType), []byte{0, 0, 0, 0}, value)
}

// ilstText builds an ilst item with one UTF-8 data atom per value.
func ilstText(typ string, values ...string) []byte {
	var data [][]byte
	for _, v := range values {
		data = append(data, dataAtom(dataTypeUTF8, []byte(v)))
	}
	return atom(typ, data...)
}

// ilstInt builds an ilst item holding one integer data atom.
func ilstInt(typ string, dataType uint32, value []byte) []byte {
	return atom(typ, dataAtom(dataType, value))
}

// ilstFreeform builds a ---- item with mean com.apple.iTunes.
func ilstFreeform(name string, values ...string) []byte {
	parts := [][]byte{atom("mean", []byte{0, 0, 0, 0}, []byte(freeformMean)), atom("name", []byte{0, 0, 0, 0}, []byte(name))}
	for _, v := range values {
		parts = append(parts, dataAtom(dataTypeUTF8, []byte(v)))
	}
	return atom("----", parts...)
}

// covrImage is one covr data atom: an image and its type code (13 JPEG, 14 PNG).
type covrImage struct {
	code uint32
	data []byte
}

// covrItem builds a covr item with one data atom per image.
func covrItem(images ...covrImage) []byte {
	var data [][]byte
	for _, img := range images {
		data = append(data, dataAtom(img.code, img.data))
	}
	return atom("covr", data...)
}

// stcoAtom builds an stco with the given chunk offsets.
func stco64Atom(offsets ...uint64) []byte {
	b := binary.BigEndian.AppendUint32([]byte{0, 0, 0, 0}, uint32(len(offsets)))
	for _, o := range offsets {
		b = binary.BigEndian.AppendUint32(b, uint32(o))
	}
	return atom("stco", b)
}

// co64Atom builds a co64 with the given chunk offsets.
func co64Atom(offsets ...uint64) []byte {
	b := binary.BigEndian.AppendUint32([]byte{0, 0, 0, 0}, uint32(len(offsets)))
	for _, o := range offsets {
		b = binary.BigEndian.AppendUint64(b, o)
	}
	return atom("co64", b)
}

// stsdAtom builds an stsd holding one sample entry with the given fourcc.
func stsdAtom(entries ...string) []byte {
	b := binary.BigEndian.AppendUint32([]byte{0, 0, 0, 0}, uint32(len(entries)))
	for _, e := range entries {
		b = append(b, atom(e, make([]byte, 28))...)
	}
	return atom("stsd", b)
}

// mdatPayload is the recognizable audio payload of buildMP4 files.
func mdatPayload() []byte {
	b := make([]byte, 256)
	for i := range b {
		b[i] = byte(i*7 + 3)
	}
	return b
}

// mp4Opts configures buildMP4With.
type mp4Opts struct {
	faststart bool
	items     [][]byte // ilst items; nil with noUdta builds no udta
	noUdta    bool
	co64      bool
	entries   []string // stsd sample entries; default mp4a
	extra     []uint64 // chunk offsets appended after the mdat payload's
	topExtra  []byte   // atoms appended after everything (e.g. a moof)
	moovExtra []byte   // atoms appended to moov after udta (e.g. an mvex)
}

// buildMP4 builds a minimal MP4: ftyp, a moov with one sound trak whose
// chunk offset points at the mdat payload and udta/meta(hdlr, ilst), and an
// mdat of 256 recognizable bytes. Faststart files put moov (followed by an
// 8-byte free) before mdat.
func buildMP4(faststart bool, ilstItems ...[]byte) []byte {
	return buildMP4With(mp4Opts{faststart: faststart, items: ilstItems})
}

// buildMP4With builds an MP4 as configured by o.
func buildMP4With(o mp4Opts) []byte {
	ftyp := atom("ftyp", []byte("M4A "), []byte{0, 0, 0, 0}, []byte("M4A isom"))
	mdat := atom("mdat", mdatPayload())
	moov := buildMoov(o, 0)
	var payloadOff uint64
	if o.faststart {
		payloadOff = uint64(len(ftyp) + len(moov) + 8 + 8)
	} else {
		payloadOff = uint64(len(ftyp) + 8)
	}
	moov = buildMoov(o, payloadOff)
	if o.faststart {
		return slices.Concat(ftyp, moov, atom("free"), mdat, o.topExtra)
	}
	return slices.Concat(ftyp, mdat, moov, o.topExtra)
}

// buildMoov builds the moov of buildMP4With with the mdat payload at payloadOff.
func buildMoov(o mp4Opts, payloadOff uint64) []byte {
	offsets := append([]uint64{payloadOff}, o.extra...)
	chunks := stco64Atom(offsets...)
	if o.co64 {
		chunks = co64Atom(offsets...)
	}
	entries := o.entries
	if entries == nil {
		entries = []string{"mp4a"}
	}
	stbl := atom("stbl", stsdAtom(entries...), atom("stts", make([]byte, 8)), chunks)
	trak := atom("trak", atom("tkhd", make([]byte, 84)), atom("mdia", atom("minf", stbl)))
	var udta []byte
	if !o.noUdta {
		udta = atom("udta", atom("meta", []byte{0, 0, 0, 0}, mdirHdlr, atom("ilst", o.items...)))
	}
	return atom("moov", atom("mvhd", make([]byte, 100)), trak, udta, o.moovExtra)
}

// topAtom is one top-level atom of a test file.
type topAtom struct {
	typ        string
	start, end int
}

// topAtoms lists data's top-level atoms (32-bit sizes, or 0 for the rest of the file).
func topAtoms(t *testing.T, data []byte) []topAtom {
	t.Helper()
	var out []topAtom
	for off := 0; off < len(data); {
		if len(data)-off < 8 {
			t.Fatalf("trailing %d bytes", len(data)-off)
		}
		size := int(binary.BigEndian.Uint32(data[off:]))
		if size == 0 { // extends to the end of the file
			size = len(data) - off
		}
		if size < 8 || off+size > len(data) {
			t.Fatalf("bad top-level atom size %d at %d", size, off)
		}
		out = append(out, topAtom{string(data[off+4 : off+8]), off, off + size})
		off += size
	}
	return out
}

// topTypes lists data's top-level atom types.
func topTypes(t *testing.T, data []byte) []string {
	t.Helper()
	var types []string
	for _, a := range topAtoms(t, data) {
		types = append(types, a.typ)
	}
	return types
}

// topAtomOf returns the first top-level atom of type typ.
func topAtomOf(t *testing.T, data []byte, typ string) topAtom {
	t.Helper()
	for _, a := range topAtoms(t, data) {
		if a.typ == typ {
			return a
		}
	}
	t.Fatalf("no top-level %s", typ)
	return topAtom{}
}

// moovTreeOf parses data's moov into a tree.
func moovTreeOf(t *testing.T, data []byte) *matom {
	t.Helper()
	m := topAtomOf(t, data, atomMoov)
	sr := srOf(data)
	root, err := readMoovTree(sr, &Atom{Type: atomMoov, Offset: int64(m.start), Size: int64(m.end - m.start)})
	if err != nil {
		t.Fatalf("readMoovTree: %v", err)
	}
	return root
}

// chunkOffsets returns the chunk offsets of every stco/co64 in data's moov.
func chunkOffsets(t *testing.T, data []byte) []uint64 {
	t.Helper()
	var out []uint64
	_ = moovTreeOf(t, data).walk(func(a *matom) error {
		switch a.typ {
		case atomStco:
			for i := range int(binary.BigEndian.Uint32(a.payload[4:])) {
				out = append(out, uint64(binary.BigEndian.Uint32(a.payload[8+4*i:])))
			}
		case atomCo64:
			for i := range int(binary.BigEndian.Uint32(a.payload[4:])) {
				out = append(out, binary.BigEndian.Uint64(a.payload[8+8*i:]))
			}
		}
		return nil
	})
	return out
}

// ilstItems returns the serialized items of data's ilst.
func ilstItems(t *testing.T, data []byte) [][]byte {
	t.Helper()
	ilst := findPath(moovTreeOf(t, data), atomUdta, atomMeta, atomIlst)
	if ilst == nil {
		t.Fatal("no ilst")
	}
	var out [][]byte
	for _, c := range ilst.children {
		out = append(out, c.appendTo(nil))
	}
	return out
}

// itemOf returns the serialized ilst item of type typ, or nil.
func itemOf(t *testing.T, data []byte, typ string) []byte {
	t.Helper()
	for _, it := range ilstItems(t, data) {
		if string(it[4:8]) == typ {
			return it
		}
	}
	return nil
}

// srOf wraps data in a SafeReader.
func srOf(data []byte) *binutil.SafeReader {
	return binutil.NewSafeReader(bytes.NewReader(data), int64(len(data)), "t.m4a")
}

// boundTags parses src and binds its tags to the M4A format.
func boundTags(t *testing.T, src []byte) *types.Tags {
	t.Helper()
	tags := parseBytes(t, src).Tags
	types.BindFormat(&tags, types.FormatM4A)
	return &tags
}

// editsFor builds the EditSet that mutate (applied to src's tags) produces,
// plus the given artwork operations.
func editsFor(t *testing.T, src []byte, mutate func(*types.Tags), art ...types.ArtworkOp) *types.EditSet {
	t.Helper()
	orig := boundTags(t, src)
	cur := orig.Clone()
	types.BindFormat(cur, types.FormatM4A)
	if mutate != nil {
		mutate(cur)
	}
	keys, fields, err := types.Diff(orig, cur, types.FormatM4A)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	return &types.EditSet{Keys: keys, Fields: fields, Artwork: art, Orig: orig, Final: cur}
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

// tryWrite runs the writer over src.
func tryWrite(src []byte, edits *types.EditSet) ([]byte, error) {
	var buf bytes.Buffer
	n, err := (&writer{}).WriteFile(context.Background(), &buf, bytes.NewReader(src), int64(len(src)), edits)
	if err == nil && n != int64(buf.Len()) {
		panic("WriteFile miscounted its output")
	}
	return buf.Bytes(), err
}

// writeTags applies mutate to src's tags and returns the written file.
func writeTags(t *testing.T, src []byte, mutate func(*types.Tags), art ...types.ArtworkOp) []byte {
	t.Helper()
	return writeBytes(t, src, editsFor(t, src, mutate, art...))
}

// tryTags applies mutate to src's tags and returns the writer's result.
func tryTags(t *testing.T, src []byte, mutate func(*types.Tags), art ...types.ArtworkOp) ([]byte, error) {
	t.Helper()
	return tryWrite(src, editsFor(t, src, mutate, art...))
}

// artworkOf extracts data's artwork.
func artworkOf(t *testing.T, data []byte) []types.Artwork {
	t.Helper()
	art, err := (&parser{}).ExtractArtwork(context.Background(), bytes.NewReader(data), int64(len(data)), "t.m4a")
	if err != nil {
		t.Fatalf("ExtractArtwork: %v", err)
	}
	return art
}

// payloadAt returns the 256 bytes at off.
func payloadAt(t *testing.T, data []byte, off uint64) []byte {
	t.Helper()
	if off+256 > uint64(len(data)) {
		t.Fatalf("offset %d past the end of a %d-byte file", off, len(data))
	}
	return data[off : off+256]
}
