// Package m4a parses MP4-family audio files (M4A, M4B).
//
// It reads iTunes ilst metadata (including freeform ---- atoms), technical
// properties from the sound track's sample description, QuickTime chapter
// tracks and Nero chpl chapters, and covr artwork. It registers itself for
// [types.FormatM4A] and [types.FormatM4B].
package m4a

import (
	"context"
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

// readAtomHeader reads the atom header at offset. parentEnd is the exclusive end
// of the enclosing container (the file size for top-level atoms). An atom that
// does not fit inside its parent is rejected, so a caller that advances by
// atom.Size always moves strictly forward and never past parentEnd.
func readAtomHeader(sr *binary.SafeReader, offset, parentEnd int64) (*Atom, error) {
	// Read size (4 bytes)
	size32, err := sr.Read[uint32](offset, "atom size")
	if err != nil {
		return nil, err
	}

	// Read type (4 bytes)
	typeBytes := make([]byte, 4)
	if err := sr.ReadAt(typeBytes, offset+4, "atom type"); err != nil {
		return nil, err
	}
	atomType := string(typeBytes)

	atom := &Atom{
		Type:   atomType,
		Offset: offset,
	}

	// Handle extended size (size == 1 means 64-bit size follows)
	if size32 == 1 {
		size64, err := sr.Read[uint64](offset+8, "extended atom size")
		if err != nil {
			return nil, err
		}
		if size64 > math.MaxInt64 {
			return nil, &types.CorruptedFileError{
				Offset: offset,
				Reason: fmt.Sprintf("extended atom size %d exceeds the addressable range", size64),
			}
		}
		atom.Size = int64(size64)
		atom.Extended = true
	} else {
		atom.Size = int64(size32)
		atom.Extended = false
	}

	// Validate atom size: at least its own header, at most what the parent has left.
	minSize := int64(8)
	if atom.Extended {
		minSize = 16
	}
	if atom.Size < minSize {
		return nil, &types.CorruptedFileError{
			Offset: offset,
			Reason: fmt.Sprintf("invalid atom size %d (minimum is %d)", atom.Size, minSize),
		}
	}
	if offset >= parentEnd || atom.Size > parentEnd-offset {
		return nil, &types.CorruptedFileError{
			Offset: offset,
			Reason: fmt.Sprintf("atom %q size %d exceeds its container (%d bytes remain)", atom.Type, atom.Size, parentEnd-offset),
		}
	}

	return atom, nil
}

// Returns the first matching atom or an error if not found.
func findAtom(sr *binary.SafeReader, start, end int64, atomType string) (*Atom, error) {
	return findAtomContext(context.Background(), sr, start, end, atomType)
}

// findAtomContext is findAtom with periodic cancellation checks, for scans over
// atom lists that can be arbitrarily long (the top level of a file).
func findAtomContext(ctx context.Context, sr *binary.SafeReader, start, end int64, atomType string) (*Atom, error) {
	offset := start

	for i := 0; offset < end; i++ {
		if err := binary.Canceled(ctx, i); err != nil {
			return nil, err
		}

		atom, err := readAtomHeader(sr, offset, end)
		if err != nil {
			return nil, err
		}

		if atom.Type == atomType {
			return atom, nil
		}

		// readAtomHeader guarantees 8 <= atom.Size <= end-offset.
		offset += atom.Size
	}

	return nil, fmt.Errorf("atom '%s': %w", atomType, errAtomNotFound)
}
