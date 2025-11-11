// Package m4a provides M4A/M4B audio file parsing
package m4a

import (
	"fmt"

	"github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// Atom represents an MP4/M4A/M4B atom (box)
type Atom struct {
	Size     uint64 // Total size including header
	Type     string // 4-character type code
	Offset   int64  // Position in file
	Extended bool   // Whether this uses 64-bit extended size
}

// DataSize returns the size of the atom's data (excluding header)
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

// DataOffset returns the file offset where the atom's data starts
func (a *Atom) DataOffset() int64 {
	headerSize := int64(8)
	if a.Extended {
		headerSize = 16
	}
	return a.Offset + headerSize
}

// IsContainer returns true if this atom type can contain other atoms
func (a *Atom) IsContainer() bool {
	containerTypes := map[string]bool{
		"moov": true, // Movie container
		"udta": true, // User data
		"meta": true, // Metadata container
		"ilst": true, // iTunes metadata list
		"trak": true, // Track container
		"mdia": true, // Media container
		"minf": true, // Media information
		"stbl": true, // Sample table
	}
	return containerTypes[a.Type]
}

// readAtomHeader reads an atom header at the given offset
func readAtomHeader(sr *binary.SafeReader, offset int64) (*Atom, error) {
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

	// Validate atom size
	if atom.Size < 8 {
		return nil, &types.CorruptedFileError{
			Offset: offset,
			Reason: fmt.Sprintf("invalid atom size %d (minimum is 8)", atom.Size),
		}
	}

	return atom, nil
}

// findAtom searches for an atom of the given type within a range
// Returns the first matching atom or an error if not found
func findAtom(sr *binary.SafeReader, start, end int64, atomType string) (*Atom, error) {
	offset := start

	for offset < end {
		atom, err := readAtomHeader(sr, offset)
		if err != nil {
			return nil, err
		}

		if atom.Type == atomType {
			return atom, nil
		}

		// Move to next atom
		offset += int64(atom.Size)

		// Prevent infinite loop on corrupted files
		if atom.Size == 0 {
			return nil, &types.CorruptedFileError{
				Offset: offset,
				Reason: "atom with zero size",
			}
		}
	}

	return nil, fmt.Errorf("atom '%s' not found", atomType)
}
