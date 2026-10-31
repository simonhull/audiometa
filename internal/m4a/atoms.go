// Package m4a provides M4A/M4B audio file parsing
package m4a

import (
	"errors"
	"fmt"

	"github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// errAtomNotFound is wrapped by findAtom when the container holds no atom of
// the requested type. Callers use errors.Is to tell "absent" from "corrupt".
var errAtomNotFound = errors.New("atom not found")

// Atom represents an MP4/M4A/M4B atom (box).
type Atom struct {
	Type     string
	Size     uint64
	Offset   int64
	Extended bool
}

// DataSize returns the size of the atom's data (excluding header).
func (a *Atom) DataSize() uint64 {
	headerSize := uint64(8)
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

// IsContainer returns true if this atom type can contain other atoms.
func (a *Atom) IsContainer() bool {
	containerTypes := map[string]bool{
		"moov":   true, // Movie container
		"udta":   true, // User data
		"meta":   true, // Metadata container
		"ilst":   true, // iTunes metadata list
		atomTrak: true, // Track container
		"mdia":   true, // Media container
		"minf":   true, // Media information
		"stbl":   true, // Sample table
	}
	return containerTypes[a.Type]
}

// readAtomHeader reads the atom header at offset. parentEnd is the exclusive end
// of the enclosing container (the file size for top-level atoms). An atom that
// does not fit inside its parent is rejected, so a caller that advances by
// atom.Size always moves strictly forward and never past parentEnd.
func readAtomHeader(sr *binary.SafeReader, offset, parentEnd int64) (*Atom, error) {
	// Read size (4 bytes)
	size32, err := binary.Read[uint32](sr, offset, "atom size")
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
		size64, err := binary.Read[uint64](sr, offset+8, "extended atom size")
		if err != nil {
			return nil, err
		}
		atom.Size = size64
		atom.Extended = true
	} else {
		atom.Size = uint64(size32)
		atom.Extended = false
	}

	// Validate atom size: at least its own header, at most what the parent has left.
	minSize := uint64(8)
	if atom.Extended {
		minSize = 16
	}
	if atom.Size < minSize {
		return nil, &types.CorruptedFileError{
			Offset: offset,
			Reason: fmt.Sprintf("invalid atom size %d (minimum is %d)", atom.Size, minSize),
		}
	}
	if offset >= parentEnd || atom.Size > uint64(parentEnd-offset) {
		return nil, &types.CorruptedFileError{
			Offset: offset,
			Reason: fmt.Sprintf("atom %q size %d exceeds its container (%d bytes remain)", atom.Type, atom.Size, parentEnd-offset),
		}
	}

	return atom, nil
}

// Returns the first matching atom or an error if not found.
func findAtom(sr *binary.SafeReader, start, end int64, atomType string) (*Atom, error) {
	offset := start

	for offset < end {
		atom, err := readAtomHeader(sr, offset, end)
		if err != nil {
			return nil, err
		}

		if atom.Type == atomType {
			return atom, nil
		}

		// readAtomHeader guarantees 8 <= atom.Size <= end-offset.
		offset += int64(atom.Size)
	}

	return nil, fmt.Errorf("atom '%s': %w", atomType, errAtomNotFound)
}
