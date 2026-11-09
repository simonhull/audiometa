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
	header     ID3v2Header
	start, end int64
	size       int64 // on-disk size of the tag (header, body, footer), before any de-unsynchronisation
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
	return t, nil
}

// frames yields, in tag order, the frames whose (ID3v2.3-normalized) ID
// wanted accepts; other frames are stepped over without reading their
// payload. A yielded frame's Data is its body with the per-frame flags
// applied (see frameBody). A wanted frame whose payload cannot be read, or
// that is compressed or encrypted, goes to warn and is skipped. The walk ends
// at padding or at a frame header that cannot be read. When ctx is done it
// yields ctx.Err() once and stops.
func (t id3v2Tag) frames(ctx context.Context, wanted func(id string) bool, warn func(types.Warning)) iter.Seq2[ID3v2Frame, error] {
	return func(yield func(ID3v2Frame, error) bool) {
		for i, off := 0, t.start; off < t.end; i++ {
			if err := binutil.Canceled(ctx, i); err != nil {
				yield(ID3v2Frame{}, err)
				return
			}
			id, size, flags, headerLen, ok := readFrameHeader(t.sr, t.header.Version, off)
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
			if !yield(ID3v2Frame{ID: id, Size: size, Flags: flags, Data: data}, nil) {
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
