// Package m4a parses MP4-family audio files (M4A, M4B).
//
// It reads iTunes ilst metadata (including freeform ---- atoms), technical
// properties from the sound track's sample description, QuickTime chapter
// tracks and Nero chpl chapters, and covr artwork. It registers itself for
// [types.FormatM4A] and [types.FormatM4B].
package m4a

import (
	"context"
	stdbinary "encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// errAtomNotFound is wrapped by findAtom when the container holds no atom of
// the requested type. Callers use errors.Is to tell "absent" from "corrupt".
var errAtomNotFound = errors.New("atom not found")

// Atom represents an MP4/M4A/M4B atom (box).
type Atom struct {
	Type     string
	Size     int64
	Offset   int64
	Extended bool
}

// DataSize returns the size of the atom's data (excluding header).
func (a *Atom) DataSize() int64 {
	headerSize := int64(8)
	if a.Extended {
		headerSize = 16
	}
	if a.Size < headerSize {
		return 0
	}
	return a.Size - headerSize
}

// DataOffset returns the file offset where the atom's data starts.
func (a *Atom) DataOffset() int64 {
	headerSize := int64(8)
	if a.Extended {
		headerSize = 16
	}
	return a.Offset + headerSize
}

// atomTrak is the track container atom type.
const atomTrak = "trak"

// errAtomHeaderTruncated is returned by DecodeAtomHeader when its input ends
// inside the header.
var errAtomHeaderTruncated = errors.New("truncated atom header")

// SizeZero says what an atom size field of 0 means where a header is read.
type SizeZero uint8

const (
	// SizeZeroInvalid rejects size 0: inside a container no atom may use it.
	SizeZeroInvalid SizeZero = iota
	// SizeZeroToEnd reads size 0 as "to the end of the file", which ISO/IEC
	// 14496-12 allows only for the last top-level atom.
	SizeZeroToEnd
)

// AtomHeader is a decoded atom header.
type AtomHeader struct {
	Type      string
	Size      int64 // the whole atom, header included
	HeaderLen int64 // 8, or 16 with a 64-bit size
}

// AtomSizeError reports an atom whose declared size cannot be right: smaller
// than its header, too large to address, or larger than the room its container
// has left.
type AtomSizeError struct {
	Type string
	Size uint64 // as declared
	Room int64  // bytes left in the container from the atom's start

	headerLen int64
}

// Error describes the impossible size.
func (e *AtomSizeError) Error() string {
	switch {
	case e.Size > math.MaxInt64:
		return fmt.Sprintf("extended atom size %d exceeds the addressable range", e.Size)
	case int64(e.Size) < e.headerLen: // Size is at most MaxInt64 here: the case above took the rest
		return fmt.Sprintf("invalid atom size %d (minimum is %d)", e.Size, e.headerLen)
	default:
		return fmt.Sprintf("atom %q size %d exceeds its container (%d bytes remain)", e.Type, e.Size, e.Room)
	}
}

// DecodeAtomHeader decodes the header at the start of b, which holds at
// least the first 8 bytes of the atom (16 when it uses a 64-bit size), for
// an atom with room bytes left in its container.
//
// It returns an *AtomSizeError for an impossible size and errAtomHeaderTruncated
// when b is too short. A size of 0 is an error unless zero is SizeZeroToEnd, in
// which case the atom takes all of room. Otherwise the size is at least the
// header's length and at most room, so a caller that advances by Size always
// moves strictly forward and never past the container.
func DecodeAtomHeader(b []byte, room int64, zero SizeZero) (AtomHeader, error) {
	if len(b) < 8 {
		return AtomHeader{}, errAtomHeaderTruncated
	}
	h := AtomHeader{Type: string(b[4:8]), HeaderLen: 8}
	size := uint64(stdbinary.BigEndian.Uint32(b))
	switch size {
	case 0:
		if zero == SizeZeroToEnd && room >= h.HeaderLen {
			h.Size = room
			return h, nil
		}
	case 1:
		if len(b) < 16 {
			return AtomHeader{}, errAtomHeaderTruncated
		}
		size, h.HeaderLen = stdbinary.BigEndian.Uint64(b[8:16]), 16
	}
	if size > math.MaxInt64 || int64(size) < h.HeaderLen || int64(size) > room {
		return AtomHeader{}, &AtomSizeError{Type: h.Type, Size: size, Room: room, headerLen: h.HeaderLen}
	}
	h.Size = int64(size)
	return h, nil
}

// readAtomHeader reads the atom header at offset. parentEnd is the exclusive end
// of the enclosing container. An atom that does not fit inside its parent is
// rejected, so a caller that advances by atom.Size always moves strictly
// forward and never past parentEnd. A size of 0 is corrupt.
func readAtomHeader(sr *binary.SafeReader, offset, parentEnd int64) (*Atom, error) {
	return readHeader(sr, offset, parentEnd, SizeZeroInvalid)
}

// readTopAtomHeader is readAtomHeader for an atom at the top level of a file
// of size bytes, where size 0 runs to the end of the file.
func readTopAtomHeader(sr *binary.SafeReader, offset, size int64) (*Atom, error) {
	return readHeader(sr, offset, size, SizeZeroToEnd)
}

// readHeader reads the header at offset through DecodeAtomHeader.
func readHeader(sr *binary.SafeReader, offset, parentEnd int64, zero SizeZero) (*Atom, error) {
	// Up to 16 bytes: the container's remainder, and at least the 8 of a plain header.
	var buf [16]byte
	n := int(max(min(parentEnd-offset, int64(len(buf))), 8))
	if err := sr.ReadAt(buf[:n], offset, "atom header"); err != nil {
		return nil, err
	}
	h, err := DecodeAtomHeader(buf[:n], parentEnd-offset, zero)
	if err != nil {
		return nil, &types.CorruptedFileError{Offset: offset, Reason: err.Error()}
	}
	return &Atom{Type: h.Type, Size: h.Size, Offset: offset, Extended: h.HeaderLen == 16}, nil
}

// findAtom returns the first atom of type typ in [start, end). The error wraps
// errAtomNotFound when there is none, is ctx.Err() when ctx is done, and is a
// *types.CorruptedFileError when a header in the range is malformed.
func findAtom(ctx context.Context, sr *binary.SafeReader, start, end int64, typ string) (*Atom, error) {
	return scanAtoms(ctx, sr, start, end, typ, SizeZeroInvalid)
}

// findTopAtom is findAtom over the top level of a file of size bytes, where
// the last atom's size field may be 0.
func findTopAtom(ctx context.Context, sr *binary.SafeReader, size int64, typ string) (*Atom, error) {
	return scanAtoms(ctx, sr, 0, size, typ, SizeZeroToEnd)
}

// scanAtoms is findAtom with the reading of size 0 chosen by zero.
func scanAtoms(ctx context.Context, sr *binary.SafeReader, start, end int64, typ string, zero SizeZero) (*Atom, error) {
	offset := start

	for i := 0; offset < end; i++ {
		if err := binary.Canceled(ctx, i); err != nil {
			return nil, err
		}

		atom, err := readHeader(sr, offset, end, zero)
		if err != nil {
			return nil, err
		}

		if atom.Type == typ {
			return atom, nil
		}

		// readHeader guarantees 8 <= atom.Size <= end-offset.
		offset += atom.Size
	}

	return nil, fmt.Errorf("atom '%s': %w", typ, errAtomNotFound)
}

// child returns a's first child atom of type typ; see findAtom for the
// errors. A meta atom's children follow 4 bytes of version and flags.
func (a *Atom) child(ctx context.Context, sr *binary.SafeReader, typ string) (*Atom, error) {
	start := a.DataOffset()
	if a.Type == atomMeta {
		start += 4
	}
	return findAtom(ctx, sr, start, a.DataOffset()+a.DataSize(), typ)
}

// path follows names down from a, as child does at each level.
func (a *Atom) path(ctx context.Context, sr *binary.SafeReader, names ...string) (*Atom, error) {
	cur := a
	for _, name := range names {
		next, err := cur.child(ctx, sr, name)
		if err != nil {
			return nil, err
		}
		cur = next
	}
	return cur, nil
}
