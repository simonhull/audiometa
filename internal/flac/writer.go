package flac

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"slices"
	"unicode/utf8"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/registry"
	"github.com/simonhull/audiometa/internal/trailer"
	"github.com/simonhull/audiometa/internal/types"
	"github.com/simonhull/audiometa/internal/writeutil"
)

// Writer limits.
const (
	maxBlockLen    = 0xFFFFFF // largest payload a 24-bit block length can describe
	defaultPadding = 8192     // padding added when the metadata outgrows its space (libFLAC's default)
	flagLastBlock  = 0x80     // is-last bit of a metadata block header's first byte
	flacMagic      = "fLaC"
)

// writer rewrites the metadata blocks of a FLAC file. It implements
// registry.TagWriter and registry.InPlaceWriter.
//
// Only the VORBIS_COMMENT comments for the keys and fields the caller edited
// change, and only the PICTURE blocks the artwork operations touch. Every
// other block (STREAMINFO, SEEKTABLE, APPLICATION, CUESHEET, unknown types,
// untouched pictures) keeps its payload byte for byte and its relative order;
// the vendor string is kept too. PADDING blocks are merged into one trailing
// block that absorbs any growth, so the audio usually stays where it was.
// When the metadata outgrows its space it gets a fresh 8192-byte padding
// block and the audio moves.
//
// Chapters (CHAPTERnnn comments, CUESHEET) are read-only: editing or deleting
// a CHAPTERnnn key returns ErrUnsupportedWrite. Deleting every key leaves a
// VORBIS_COMMENT block holding only the vendor string.
type writer struct{}

// fblock is one metadata block of a file being rewritten.
type fblock struct {
	typ     uint8
	payload []byte
	off     int64 // payload offset in the source; -1 for a block the writer created
}

// fmeta is the metadata block list of a file being rewritten.
type fmeta struct {
	blocks     []fblock
	path       string
	audioStart int64 // first byte after the last metadata block
	oldMetaLen int64 // bytes of metadata blocks in the source: audioStart - 4
}

// WriteFile streams src with edits applied to out.
func (w *writer) WriteFile(ctx context.Context, out io.Writer, src io.ReaderAt, size int64, edits *types.EditSet) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	m, meta, err := buildMeta(ctx, binutil.NewSafeReader(src, size, ""), size, edits)
	if err != nil {
		return 0, err
	}
	cw := &writeutil.CountingWriter{W: out}
	if _, err := cw.Write([]byte(flacMagic)); err != nil {
		return cw.N, err
	}
	if _, err := cw.Write(meta); err != nil {
		return cw.N, err
	}
	if _, err := writeutil.CopyRange(ctx, cw, src, m.audioStart, size-m.audioStart); err != nil {
		return cw.N, err
	}
	return cw.N, nil
}

// AudioDigest returns the SHA-256 of everything after the last metadata block.
func (w *writer) AudioDigest(ctx context.Context, src io.ReaderAt, size int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sr := binutil.NewSafeReader(src, size, "")
	start, err := walkBlocks(ctx, sr, size, nil)
	if err != nil {
		return nil, err
	}
	end, err := trailer.Start(ctx, sr, start, size)
	if err != nil {
		return nil, err
	}
	return writeutil.DigestRange(ctx, src, start, end-start)
}

// InPlacePatch returns the new metadata blocks to write at offset 4 when they
// fill exactly the space of the old ones (possibly with a resized PADDING
// block). The patch is then exactly what WriteFile writes before the audio.
func (w *writer) InPlacePatch(ctx context.Context, src io.ReaderAt, size int64, edits *types.EditSet) (int64, []byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return 0, nil, false, err
	}
	m, meta, err := buildMeta(ctx, binutil.NewSafeReader(src, size, ""), size, edits)
	if err != nil {
		return 0, nil, false, err
	}
	if int64(len(meta)) != m.oldMetaLen {
		return 0, nil, false, nil
	}
	return int64(len(flacMagic)), meta, true, nil
}

// buildMeta reads the source blocks, applies edits and serializes the new
// metadata blocks (without the "fLaC" marker).
func buildMeta(ctx context.Context, sr *binutil.SafeReader, size int64, edits *types.EditSet) (*fmeta, []byte, error) {
	m, err := readFMeta(ctx, sr, size)
	if err != nil {
		return nil, nil, err
	}
	if err := m.apply(edits); err != nil {
		return nil, nil, err
	}
	meta, err := m.serialize()
	if err != nil {
		return nil, nil, err
	}
	return m, meta, nil
}

// walkBlocks follows the metadata block chain, calling visit (when non-nil)
// with each block's type, payload offset and payload length, and returns the
// offset just past the last block. A missing "fLaC" marker, a first block
// other than STREAMINFO, a block running past the end of the file, or a chain
// that never reaches its last block is a CorruptedFileError.
func walkBlocks(ctx context.Context, sr *binutil.SafeReader, size int64, visit func(typ uint8, off, n int64) error) (int64, error) {
	var magic [4]byte
	if size < int64(len(magic)) {
		return 0, corrupted(sr.Path(), 0, "invalid FLAC magic bytes")
	}
	if err := sr.ReadAt(magic[:], 0, "FLAC magic bytes"); err != nil {
		return 0, err
	}
	if string(magic[:]) != flacMagic {
		return 0, corrupted(sr.Path(), 0, "invalid FLAC magic bytes")
	}
	off := int64(len(magic))
	for i := 0; ; i++ {
		if err := binutil.Canceled(ctx, i); err != nil {
			return 0, err
		}
		if size-off < 4 {
			return 0, corrupted(sr.Path(), off, "metadata block chain ends before its last block")
		}
		header, err := sr.Read[uint32](off, "metadata block header")
		if err != nil {
			return 0, err
		}
		typ := uint8(header >> 24 & 0x7F)
		n := int64(header & maxBlockLen)
		if i == 0 && typ != blockTypeStreamInfo {
			return 0, corrupted(sr.Path(), off, "first metadata block is not STREAMINFO")
		}
		if n > size-off-4 {
			return 0, corrupted(sr.Path(), off, fmt.Sprintf("metadata block of %d bytes runs past the end of the file", n))
		}
		if visit != nil {
			if err := visit(typ, off+4, n); err != nil {
				return 0, err
			}
		}
		off += 4 + n
		if header>>31 == 1 {
			return off, nil
		}
	}
}

// readFMeta reads every metadata block of the file behind sr.
func readFMeta(ctx context.Context, sr *binutil.SafeReader, size int64) (*fmeta, error) {
	m := &fmeta{path: sr.Path()}
	end, err := walkBlocks(ctx, sr, size, func(typ uint8, off, n int64) error {
		if err := sr.CheckLen(off, int(n), 1, "metadata block"); err != nil {
			return err
		}
		payload := make([]byte, n)
		if err := sr.ReadAt(payload, off, "metadata block"); err != nil {
			return err
		}
		m.blocks = append(m.blocks, fblock{typ: typ, payload: payload, off: off})
		return nil
	})
	if err != nil {
		return nil, err
	}
	m.audioStart = end
	m.oldMetaLen = end - int64(len(flacMagic))
	return m, nil
}

// apply applies tag edits (when there are any) and then the artwork
// operations. Without tag edits every VORBIS_COMMENT block stays verbatim.
func (m *fmeta) apply(edits *types.EditSet) error {
	if len(edits.Keys) > 0 || len(edits.Fields) > 0 {
		if err := m.applyTags(edits); err != nil {
			return err
		}
	}
	return m.applyArtwork(edits.Artwork)
}

// applyTags applies the key and field edits to the file's VORBIS_COMMENT
// block, creating one when the file has none.
func (m *fmeta) applyTags(edits *types.EditSet) error {
	var at []int
	for i, b := range m.blocks {
		if b.typ == blockTypeVorbisComment {
			at = append(at, i)
		}
	}
	if len(at) > 1 {
		return fmt.Errorf("%w: file has %d VORBIS_COMMENT blocks", types.ErrUnsupportedWrite, len(at))
	}
	v := &vcomment{vendor: []byte("audiometa")}
	if len(at) == 1 {
		b := m.blocks[at[0]]
		var err error
		if v, err = parseVC(b.payload); err != nil {
			return corrupted(m.path, b.off, "malformed VORBIS_COMMENT block: "+err.Error())
		}
	}
	for _, e := range edits.Keys {
		if err := v.applyKey(e.Key, e.Values); err != nil {
			return err
		}
	}
	for _, e := range edits.Fields {
		if err := v.applyField(e, edits); err != nil {
			return err
		}
	}
	if len(at) == 1 {
		m.blocks[at[0]].payload = v.bytes()
		return nil
	}
	if len(v.comments) > 0 {
		m.blocks = slices.Insert(m.blocks, m.newCommentIndex(), fblock{typ: blockTypeVorbisComment, payload: v.bytes(), off: -1})
	}
	return nil
}

// newCommentIndex is where a new VORBIS_COMMENT block goes: after
// STREAMINFO, or after a SEEKTABLE that directly follows it.
func (m *fmeta) newCommentIndex() int {
	if len(m.blocks) > 1 && m.blocks[1].typ == blockTypeSeekTable {
		return 2
	}
	return min(1, len(m.blocks))
}

// applyArtwork applies the artwork operations, in order, to the PICTURE blocks.
func (m *fmeta) applyArtwork(ops []types.ArtworkOp) error {
	for _, op := range ops {
		switch op.Kind {
		case types.ArtworkOpAdd:
			payload, err := encodePicture(op.Art)
			if err != nil {
				return err
			}
			m.blocks = slices.Insert(m.blocks, m.pictureIndex(), fblock{typ: blockTypePicture, payload: payload, off: -1})
		case types.ArtworkOpRemove:
			m.blocks = slices.DeleteFunc(m.blocks, func(b fblock) bool {
				return b.typ == blockTypePicture && types.ArtworkTypeFromPictureType(pictureType(b.payload)) == op.Type
			})
		case types.ArtworkOpRemoveAll:
			m.blocks = slices.DeleteFunc(m.blocks, func(b fblock) bool { return b.typ == blockTypePicture })
		default:
			return fmt.Errorf("%w: unknown artwork operation %d", types.ErrUnsupportedWrite, op.Kind)
		}
	}
	return nil
}

// pictureIndex is where a new PICTURE block goes: after the last PICTURE, or
// after the last non-PADDING block when there is none.
func (m *fmeta) pictureIndex() int {
	if i := m.lastIndex(func(b fblock) bool { return b.typ == blockTypePicture }); i >= 0 {
		return i + 1
	}
	return m.lastIndex(func(b fblock) bool { return b.typ != blockTypePadding }) + 1
}

// lastIndex returns the index of the last block match selects, or -1.
func (m *fmeta) lastIndex(match func(fblock) bool) int {
	for i, b := range slices.Backward(m.blocks) {
		if match(b) {
			return i
		}
	}
	return -1
}

// pictureType returns a PICTURE payload's picture type: its first four bytes,
// big-endian. A payload too short to hold one reads as 0 (Other).
func pictureType(payload []byte) uint32 {
	if len(payload) < 4 {
		return 0
	}
	return binary.BigEndian.Uint32(payload)
}

// encodePicture builds a PICTURE payload: type, MIME, description, width,
// height, depth (0), colors (0) and data, with big-endian 32-bit lengths.
func encodePicture(a types.Artwork) ([]byte, error) {
	for _, c := range []byte(a.MIMEType) {
		if c < 0x20 || c > 0x7E {
			return nil, fmt.Errorf("%w: MIME type %q is not printable ASCII", types.ErrUnsupportedWrite, a.MIMEType)
		}
	}
	if a.Type < types.ArtworkOther || a.Type > types.ArtworkPublisherLogotype {
		return nil, fmt.Errorf("%w: picture type %d", types.ErrUnsupportedWrite, a.Type)
	}
	if !utf8.ValidString(a.Description) {
		return nil, fmt.Errorf("%w: artwork description is not valid UTF-8", types.ErrUnsupportedWrite)
	}
	if err := checkBlockLen(32 + len(a.MIMEType) + len(a.Description) + len(a.Data)); err != nil {
		return nil, fmt.Errorf("%w: PICTURE block", err)
	}
	out := make([]byte, 0, 32+len(a.MIMEType)+len(a.Description)+len(a.Data))
	out = binary.BigEndian.AppendUint32(out, uint32(a.Type))
	out = appendBytes(out, []byte(a.MIMEType))
	out = appendBytes(out, []byte(a.Description))
	out = binary.BigEndian.AppendUint32(out, dimension(a.Width))
	out = binary.BigEndian.AppendUint32(out, dimension(a.Height))
	out = binary.BigEndian.AppendUint32(out, 0) // color depth: unknown
	out = binary.BigEndian.AppendUint32(out, 0) // colors used: not indexed
	return appendBytes(out, a.Data), nil
}

// appendBytes appends b preceded by its big-endian 32-bit length. Callers
// bound len(b) by maxBlockLen first.
func appendBytes(out, b []byte) []byte {
	out = binary.BigEndian.AppendUint32(out, uint32(len(b))) //nolint:gosec // G115: callers bound len(b) by maxBlockLen
	return append(out, b...)
}

// dimension clamps an image dimension to the uint32 range (negative is 0).
func dimension(n int) uint32 {
	return uint32(min(max(int64(n), 0), math.MaxUint32))
}

// serialize encodes the blocks (without the "fLaC" marker), applying the
// padding policy: every PADDING block is dropped, then one trailing PADDING
// block fills the old metadata space when the rest fits in it with room for
// a block header; when the rest fills it exactly there is no padding; and
// otherwise a fresh defaultPadding block is added. Only the final block has
// the is-last bit.
func (m *fmeta) serialize() ([]byte, error) {
	blocks := slices.DeleteFunc(slices.Clone(m.blocks), func(b fblock) bool { return b.typ == blockTypePadding })
	var newLen int64
	for _, b := range blocks {
		if err := checkBlockLen(len(b.payload)); err != nil {
			return nil, fmt.Errorf("%w: metadata block of type %d", err, b.typ)
		}
		newLen += 4 + int64(len(b.payload))
	}
	switch room := m.oldMetaLen - newLen; {
	case room == 0:
	case room >= 4 && room-4 <= maxBlockLen:
		blocks = append(blocks, fblock{typ: blockTypePadding, payload: make([]byte, room-4)})
	default:
		blocks = append(blocks, fblock{typ: blockTypePadding, payload: make([]byte, defaultPadding)})
	}

	total := 0
	for _, b := range blocks {
		total += 4 + len(b.payload)
	}
	out := make([]byte, 0, total)
	for i, b := range blocks {
		h := b.typ &^ flagLastBlock
		if i == len(blocks)-1 {
			h |= flagLastBlock
		}
		n := len(b.payload)
		out = append(out, h, byte(n>>16), byte(n>>8), byte(n)) //nolint:gosec // G115: splitting a checked 24-bit length into bytes
		out = append(out, b.payload...)
	}
	return out, nil
}

// checkBlockLen returns ErrTagTooLarge when a block payload of n bytes does
// not fit the 24-bit block length.
func checkBlockLen(n int) error {
	if n > maxBlockLen {
		return fmt.Errorf("%w: FLAC metadata block of %d bytes exceeds %d", types.ErrTagTooLarge, n, maxBlockLen)
	}
	return nil
}

// corrupted returns a CorruptedFileError for the file at path.
func corrupted(path string, off int64, reason string) error {
	return &types.CorruptedFileError{Path: path, Offset: off, Reason: reason}
}

func init() { registry.RegisterWriter(types.FormatFLAC, &writer{}) }
