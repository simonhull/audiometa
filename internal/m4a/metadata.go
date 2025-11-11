package m4a

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// parseMetadataTag extracts the string value from an iTunes metadata tag atom
func parseMetadataTag(sr *binary.SafeReader, tagAtom *Atom) (string, error) {
	// Tag atoms contain a "data" atom with the actual value
	// Format: tag atom → data atom → version/flags → value

	if tagAtom.DataSize() == 0 {
		return "", nil
	}

	// Find data atom inside tag
	dataAtom, err := findAtom(sr, tagAtom.DataOffset(), tagAtom.DataOffset()+int64(tagAtom.DataSize()), "data")
	if err != nil {
		// No data atom found - return empty
		return "", nil
	}

	// Skip version (1 byte) + flags (3 bytes) + reserved (4 bytes) = 8 bytes
	valueOffset := dataAtom.DataOffset() + 8
	valueSize := int64(dataAtom.DataSize()) - 8

	if valueSize <= 0 {
		return "", nil
	}

	// Read the string value
	buf := make([]byte, valueSize)
	if err := sr.ReadAt(buf, valueOffset, "metadata value"); err != nil {
		return "", err
	}

	// Trim null bytes and whitespace
	value := string(buf)
	value = strings.TrimRight(value, "\x00")
	value = strings.TrimSpace(value)

	return value, nil
}

// extractIlstMetadata parses all metadata items from the ilst atom
func extractIlstMetadata(sr *binary.SafeReader, ilstAtom *Atom, file *types.File) error {
	offset := ilstAtom.DataOffset()
	end := offset + int64(ilstAtom.DataSize())

	for offset < end {
		// Read tag atom
		tagAtom, err := readAtomHeader(sr, offset)
		if err != nil {
			return err
		}

		// Handle special binary tags
		if tagAtom.Type == "trkn" {
			// Track number requires special binary parsing
			trackData, err := parseTrackNumber(sr, tagAtom)
			if err == nil {
				file.Tags.TrackNumber = trackData.Number
				file.Tags.TrackTotal = trackData.Total
			}
		} else {
			// Parse as text tag
			value, err := parseMetadataTag(sr, tagAtom)
			if err != nil {
				file.Warnings = append(file.Warnings, types.Warning{
					Stage:   "metadata",
					Message: fmt.Sprintf("failed to parse tag %s: %v", tagAtom.Type, err),
				})
			} else {
				// Map tag to metadata field
				mapTagToField(tagAtom.Type, value, file)
			}
		}

		// Move to next tag
		offset += int64(tagAtom.Size)
	}

	return nil
}

// mapTagToField maps an iTunes tag to the appropriate metadata field
// Note: In MP4, © is represented as byte 0xA9, so "©nam" is "\xA9nam" in Go strings
func mapTagToField(tag string, value string, file *types.File) {
	switch tag {
	case "\xA9nam": // Title (©nam)
		file.Tags.Title = value
	case "\xA9ART": // Artist (©ART)
		file.Tags.Artist = value
	case "\xA9alb": // Album (©alb)
		file.Tags.Album = value
	case "\xA9gen": // Genre (©gen)
		file.Tags.Genres = append(file.Tags.Genres, value)
	case "\xA9cmt": // Comment (©cmt)
		file.Tags.Comment = value
	case "\xA9wrt": // Composer (©wrt)
		file.Tags.Composers = append(file.Tags.Composers, value)
	case "\xA9day": // Year (©day)
		if year, err := strconv.Atoi(value); err == nil {
			file.Tags.Year = year
		}
	}
}

// TrackData holds track number information
type TrackData struct {
	Number int
	Total  int
}

// parseTrackNumber extracts track number and total from trkn atom
func parseTrackNumber(sr *binary.SafeReader, atom *Atom) (TrackData, error) {
	result := TrackData{}

	// Find data atom
	dataAtom, err := findAtom(sr, atom.DataOffset(), atom.DataOffset()+int64(atom.DataSize()), "data")
	if err != nil {
		return result, err
	}

	// Skip version (1) + flags (3) + reserved (4) = 8 bytes
	offset := dataAtom.DataOffset() + 8

	// Track number structure:
	// [2 bytes] reserved
	// [2 bytes] track number
	// [2 bytes] track total
	// [2 bytes] reserved

	offset += 2 // skip reserved

	trackNum, err := binary.Read[uint16](sr, offset, "track number")
	if err != nil {
		return result, err
	}
	result.Number = int(trackNum)
	offset += 2

	trackTotal, err := binary.Read[uint16](sr, offset, "track total")
	if err != nil {
		return result, err
	}
	result.Total = int(trackTotal)

	return result, nil
}
