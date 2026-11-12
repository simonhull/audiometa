package mp3

import (
	"context"
	"fmt"
	"iter"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// id3v2Tag is an ID3v2 tag opened for walking. sr reads the tag from its
// 10-byte header at offset 0; for an ID3v2.2 or ID3v2.3 tag with tag-level
// unsynchronisation it reads a de-unsynchronised copy, and header.Size is the
// size of that copy's body. The frames lie in [start, end) of sr.
type id3v2Tag struct {
	sr         *binutil.SafeReader
	header     id3v2Header
	start, end int64
	size       int64           // on-disk size of the tag (header, body, footer), before any de-unsynchronisation
	coding     frameSizeCoding // how the tag's ID3v2.4 frame sizes are encoded
}

// frameSizeCoding is how the frame sizes of an ID3v2.4 tag are encoded. The
// standard is synchsafe; some older iTunes versions wrote plain big-endian
// integers. ID3v2.2 and ID3v2.3 sizes are always plain, whatever the coding.
type frameSizeCoding uint8

const (
	sizeSynchsafe frameSizeCoding = iota // the standard, and the zero value
	sizeBigEndian
)

// v24SizeCoding decides how the frame sizes of the ID3v2.4 frames in
// [start, end) of sr are encoded, as mutagen does for a whole tag: the frame
// headers are walked once with synchsafe sizes and once with plain sizes, and
// each walk counts the frames whose ID is four characters of A-Z or 0-9,
// stopping at padding, an invalid ID or a frame running past end. Plain sizes
// are chosen when they find more frames, or as many while the synchsafe walk
// overran end and the plain one did not; otherwise the standard synchsafe.
func v24SizeCoding(sr *binutil.SafeReader, start, end int64) frameSizeCoding {
	ss, ssOverran := countFrames(sr, start, end, sizeSynchsafe)
	be, beOverran := countFrames(sr, start, end, sizeBigEndian)
	if be > ss || (be == ss && ssOverran && !beOverran) {
		return sizeBigEndian
	}
	return sizeSynchsafe
}

// countFrames counts the well-formed frames at the start of [start, end) when
// their sizes are decoded with coding, and reports whether the walk stopped
// because a frame ran past end.
func countFrames(sr *binutil.SafeReader, start, end int64, coding frameSizeCoding) (n int, overran bool) { //nolint:nonamedreturns // the names document the results
	var hdr [10]byte
	for off := start; off+10 <= end; n++ {
		if sr.ReadAt(hdr[:], off, "frame header") != nil || !validFrameID(hdr[:4]) {
			return n, false
		}
		off += 10 + int64(decodeFrameSize(4, coding, hdr[4:8]))
		if off > end {
			return n, true
		}
	}
	return n, false
}

// validFrameID reports whether id is four characters of A-Z or 0-9.
func validFrameID(id []byte) bool {
	if len(id) != 4 {
		return false
	}
	for _, c := range id {
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// openID3v2Tag reads the ID3v2 header at the start of sr and prepares the
// tag's frames for walking. Recoverable problems go to warn: a compressed
// ID3v2.2 tag (its frames are not walked) and an unsynchronised body that
// cannot be read (its frames are walked as stored). The error is that of
// parseID3v2Header; for a recognized tag with an unsupported version the
// returned tag's size is still set, so callers can skip over it.
func openID3v2Tag(sr *binutil.SafeReader, warn func(types.Warning)) (id3v2Tag, error) {
	header, err := parseID3v2Header(sr)
	if err != nil {
		if header.Version != 0 {
			return id3v2Tag{size: header.totalSize()}, err
		}
		return id3v2Tag{}, err
	}
	t := id3v2Tag{sr: sr, header: header, size: header.totalSize()}
	if header.Version == 2 && header.Flags&flagV22Compression != 0 {
		warn(types.Warning{Stage: types.StageMetadata, Message: "ID3v2.2 tag is compressed; frames ignored"})
		return t, nil // start == end: no frames to walk
	}
	if (header.Version == 2 || header.Version == 3) && header.Flags&flagUnsync != 0 {
		// The whole body is unsynchronised, so undo it before walking frames.
		var w *types.Warning
		t.sr, t.header, w = deunsyncTag(sr, header)
		if w != nil {
			warn(*w)
		}
	}
	t.start = skipExtendedHeader(t.sr, t.header)
	t.end = 10 + int64(t.header.Size)
	if t.header.Version == 4 {
		t.coding = v24SizeCoding(t.sr, t.start, t.end)
	}
	return t, nil
}

// frames yields, in tag order, the frames whose (ID3v2.3-normalized) ID
// wanted accepts; other frames are stepped over without reading their
// payload. A yielded frame's Data is its body with the per-frame flags
// applied (see frameBody). A wanted frame whose payload cannot be read, or
// that is compressed or encrypted, goes to warn and is skipped. The walk ends
// at padding or at a frame header that cannot be read. When ctx is done it
// yields ctx.Err() once and stops.
func (t id3v2Tag) frames(ctx context.Context, wanted func(id string) bool, warn func(types.Warning)) iter.Seq2[id3v2Frame, error] {
	return func(yield func(id3v2Frame, error) bool) {
		for i, off := 0, t.start; off < t.end; i++ {
			if err := binutil.Canceled(ctx, i); err != nil {
				yield(id3v2Frame{}, err)
				return
			}
			id, size, flags, headerLen, ok := readFrameHeader(t.sr, t.header.Version, t.coding, off)
			if !ok {
				return
			}
			frameOff := off
			off += headerLen + int64(size)
			if !wanted(id) {
				continue
			}
			data, ok := t.body(id, frameOff, frameOff+headerLen, size, flags, warn)
			if !ok {
				continue
			}
			if !yield(id3v2Frame{ID: id, Size: size, Flags: flags, Data: data}, nil) {
				return
			}
		}
	}
}

// body reads the size-byte payload at off of the frame whose header is at
// frameOff, and applies the frame's flags. It returns false, after passing a
// warning to warn, when the payload cannot be read or the frame is compressed
// or encrypted.
func (t id3v2Tag) body(id string, frameOff, off int64, size uint32, flags uint16, warn func(types.Warning)) ([]byte, bool) {
	err := t.sr.CheckLen(off, int(size), 1, "ID3 frame")
	var data []byte
	if err == nil {
		data = make([]byte, size)
		err = t.sr.ReadAt(data, off, "frame data")
	}
	if err != nil {
		warn(types.Warning{
			Stage:   types.StageMetadata,
			Message: fmt.Sprintf("failed to read frame %s: %v", id, err),
			Err:     err,
		})
		return nil, false
	}
	data, skip := frameBody(t.header.Version, t.header.Flags, flags, data)
	if skip != "" {
		warn(types.Warning{
			Stage:   types.StageMetadata,
			Message: fmt.Sprintf("skipped %s frame %s", skip, id),
			Offset:  frameOff,
		})
		return nil, false
	}
	return data, true
}
