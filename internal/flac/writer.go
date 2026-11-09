package flac

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/picture"
	"github.com/simonhull/audiometa/internal/registry"
	"github.com/simonhull/audiometa/internal/trailer"
	"github.com/simonhull/audiometa/internal/types"
	"github.com/simonhull/audiometa/internal/vorbis"
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
	off     int64 // payload offset in the source, for error messages; unset for a block the writer created
}

// fmeta is the metadata block list of a file being rewritten.
type fmeta struct {
	blocks     []fblock
	path       string
	prefix     int64 // bytes before the "fLaC" marker: a leading ID3v2 tag, kept verbatim; usually 0
	audioStart int64 // first byte after the last metadata block
	oldMetaLen int64 // bytes of metadata blocks in the source: audioStart - prefix - 4
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
	if _, err := writeutil.CopyRange(ctx, cw, src, 0, m.prefix); err != nil {
		return cw.N, err
	}
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
	_, start, err := walkBlocks(ctx, sr, size, nil)
	if err != nil {
		return nil, err
	}
	end, err := trailer.Start(ctx, sr, start, size)
	if err != nil {
		return nil, err
	}
	return writeutil.DigestRange(ctx, src, start, end-start)
}

// PreservedDigest returns the SHA-256 of any leading ID3v2 tag followed by
// everything after the last metadata block, including any trailing tags: the
// bytes WriteFile copies verbatim.
func (w *writer) PreservedDigest(ctx context.Context, src io.ReaderAt, size int64) ([]byte, error) {
	ranges, err := w.PreservedRanges(ctx, src, size)
	if err != nil {
		return nil, err
	}
	return writeutil.DigestRanges(ctx, src, ranges)
}

// PreservedRanges returns the leading ID3v2 tag (empty when there is none)
// and everything after the last metadata block.
func (w *writer) PreservedRanges(ctx context.Context, src io.ReaderAt, size int64) ([]registry.Range, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	prefix, start, err := walkBlocks(ctx, binutil.NewSafeReader(src, size, ""), size, nil)
	if err != nil {
		return nil, err
	}
	return []registry.Range{{Off: 0, End: prefix}, {Off: start, End: size}}, nil
}

// InPlacePatch returns the new metadata blocks to write just after the "fLaC"
// marker (offset 4, or 4 past a leading ID3v2 tag) when they fill exactly the space of the old ones (possibly with a resized PADDING
// block). The patch is then exactly what WriteFile writes before the audio.
func (w *writer) InPlacePatch(ctx context.Context, src io.ReaderAt, size int64, edits *types.EditSet) (*registry.Patch, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m, meta, err := buildMeta(ctx, binutil.NewSafeReader(src, size, ""), size, edits)
	if err != nil {
		return nil, err
	}
	if int64(len(meta)) != m.oldMetaLen {
		return nil, nil
	}
	return &registry.Patch{Off: m.prefix + int64(len(flacMagic)), Data: meta}, nil
}

// buildMeta reads the source blocks, applies edits and serializes the new
// metadata blocks (without the "fLaC" marker).
func buildMeta(ctx context.Context, sr *binutil.SafeReader, size int64, edits *types.EditSet) (*fmeta, []byte, error) {
	m, err := readFMeta(ctx, sr, size)
	if err != nil {
		return nil, nil, err
	}
	if err := m.apply(ctx, edits); err != nil {
		return nil, nil, err
	}
	meta, err := m.serialize()
	if err != nil {
		return nil, nil, err
	}
	return m, meta, nil
}

// walkBlocks follows the metadata block chain, calling visit (when non-nil)
// with each block's type, payload offset and payload length. It returns the
// offset of the "fLaC" marker (the size of a leading ID3v2 tag, usually 0)
// and the offset just past the last block. A missing "fLaC" marker, a first
// block other than STREAMINFO, a block running past the end of the file, or a
// chain that never reaches its last block is a CorruptedFileError.
func walkBlocks(ctx context.Context, sr *binutil.SafeReader, size int64, visit func(typ uint8, off, n int64) error) (prefix, end int64, err error) {
	if size < int64(len(flacMagic)) {
		return 0, 0, corrupted(sr.Path(), 0, errNoMagic.Error())
	}
	prefix, err = streamStart(sr, size)
	switch {
	case errors.Is(err, errNoMagic):
		return 0, 0, corrupted(sr.Path(), 0, errNoMagic.Error())
	case err != nil:
		return 0, 0, err
	}
	first := prefix + int64(len(flacMagic))
	for b, err := range blocks(ctx, sr, prefix, size) {
		switch {
		case errors.Is(err, errNoLastBlock):
			return 0, 0, corrupted(sr.Path(), b.off, err.Error())
		case err != nil:
			return 0, 0, err
		case b.off == first && b.typ != blockTypeStreamInfo:
			return 0, 0, corrupted(sr.Path(), b.off, "first metadata block is not STREAMINFO")
		case b.n > size-b.payload():
			return 0, 0, corrupted(sr.Path(), b.off, fmt.Sprintf("metadata block of %d bytes runs past the end of the file", b.n))
		}
		if visit != nil {
			if err := visit(b.typ, b.payload(), b.n); err != nil {
				return 0, 0, err
			}
		}
		if b.last {
			return prefix, b.end(), nil
		}
	}
	return 0, 0, corrupted(sr.Path(), size, errNoLastBlock.Error()) // not reached: blocks ends with the last block or an error
}

// readFMeta reads every metadata block of the file behind sr.
func readFMeta(ctx context.Context, sr *binutil.SafeReader, size int64) (*fmeta, error) {
	m := &fmeta{path: sr.Path()}
	prefix, end, err := walkBlocks(ctx, sr, size, func(typ uint8, off, n int64) error {
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
	m.prefix = prefix
	m.audioStart = end
	m.oldMetaLen = end - prefix - int64(len(flacMagic))
	return m, nil
}

// apply applies tag edits (when there are any) and then the artwork
// operations. Without tag edits every VORBIS_COMMENT block stays verbatim.
func (m *fmeta) apply(ctx context.Context, edits *types.EditSet) error {
	if len(edits.Keys) > 0 || len(edits.Fields) > 0 {
		if err := m.applyTags(ctx, edits); err != nil {
			return err
		}
	}
	return m.applyArtwork(edits.Artwork)
}

// applyTags applies the key and field edits to the file's VORBIS_COMMENT
// block, creating one when the file has none.
func (m *fmeta) applyTags(ctx context.Context, edits *types.EditSet) error {
	var at []int
	for i, b := range m.blocks {
		if b.typ == blockTypeVorbisComment {
			at = append(at, i)
		}
	}
	if len(at) > 1 {
		return fmt.Errorf("%w: file has %d VORBIS_COMMENT blocks", types.ErrUnsupportedWrite, len(at))
	}
	v := &vcomment{vorbis.Comments{Vendor: []byte("audiometa")}}
	if len(at) == 1 {
		b := m.blocks[at[0]]
		c, err := vorbis.Split(ctx, b.payload)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			return corrupted(m.path, b.off, "malformed VORBIS_COMMENT block: "+err.Error())
		}
		v = &vcomment{c}
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
		m.blocks[at[0]].payload = v.Encode()
		return nil
	}
	if len(v.Entries) > 0 {
		m.blocks = slices.Insert(m.blocks, m.newCommentIndex(), fblock{typ: blockTypeVorbisComment, payload: v.Encode()})
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
			if err := checkBlockLen(picture.EncodedLen(op.Art)); err != nil {
				return fmt.Errorf("%w: PICTURE block", err)
			}
			payload, err := picture.Encode(op.Art)
			if err != nil {
				return err
			}
			m.blocks = slices.Insert(m.blocks, m.pictureIndex(), fblock{typ: blockTypePicture, payload: payload})
		case types.ArtworkOpRemove:
			m.blocks = slices.DeleteFunc(m.blocks, func(b fblock) bool {
				return b.typ == blockTypePicture && types.ArtworkTypeFromPictureType(picture.Type(b.payload)) == op.Type
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
