package m4a

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// Data atom type indicators (bytes 1-3 of the data atom's version/flags word).
const (
	dataTypeImplicit   = 0
	dataTypeUTF8       = 1
	dataTypeBESigned   = 21
	dataTypeBEUnsigned = 22
)

// decodeBEInteger decodes a 1, 2, 3, 4 or 8 byte big-endian integer to decimal text.
// Signed values are sign-extended from their stored width. Any other width yields "".
func decodeBEInteger(b []byte, signed bool) string {
	switch len(b) {
	case 1, 2, 3, 4, 8:
	default:
		return ""
	}
	var u uint64
	for _, c := range b {
		u = u<<8 | uint64(c)
	}
	if !signed {
		return strconv.FormatUint(u, 10)
	}
	shift := uint(64 - 8*len(b))
	return strconv.FormatInt(int64(u<<shift)>>shift, 10)
}

// parseMetadataTag extracts the string value from an iTunes metadata tag atom.
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

	// version (1 byte) + type indicator (3 bytes), then reserved/locale (4 bytes)
	vf, err := binary.Read[uint32](sr, dataAtom.DataOffset(), "data type indicator")
	if err != nil {
		return "", err
	}
	typeIndicator := vf & 0x00FFFFFF

	valueOffset := dataAtom.DataOffset() + 8
	valueSize := int64(dataAtom.DataSize()) - 8

	if valueSize <= 0 {
		return "", nil
	}

	switch typeIndicator {
	case dataTypeImplicit, dataTypeUTF8:
		// text, handled below
	case dataTypeBESigned, dataTypeBEUnsigned:
		if valueSize > 8 {
			return "", nil
		}
		buf := make([]byte, valueSize)
		if err := sr.ReadAt(buf, valueOffset, "metadata integer value"); err != nil {
			return "", err
		}
		return decodeBEInteger(buf, typeIndicator == dataTypeBESigned), nil
	default:
		// images, UTF-16, floats, unknown: not text; never read the payload
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

// extractIlstMetadata parses all metadata items from the ilst atom.
func extractIlstMetadata(sr *binary.SafeReader, ilstAtom *Atom, file *types.File) error {
	offset := ilstAtom.DataOffset()
	end := offset + int64(ilstAtom.DataSize())

	for offset < end {
		// Read tag atom
		tagAtom, err := readAtomHeader(sr, offset, end)
		if err != nil {
			return err
		}

		switch tagAtom.Type {
		case "trkn":
			// Track number requires special binary parsing
			trackData, err := parseTrackNumber(sr, tagAtom)
			if err == nil {
				file.Tags.TrackNumber = trackData.Number
				file.Tags.TrackTotal = trackData.Total
			}
		case "disk":
			if d, err := parseTrackNumber(sr, tagAtom); err == nil {
				file.Tags.DiscNumber = d.Number
				file.Tags.DiscTotal = d.Total
			}
		case "covr", "----":
			// covr: artwork, extracted lazily by ExtractArtwork; never read during Open.
			// ----: freeform atoms, handled by parseAudiobookTags.
		default:
			// Parse as text tag
			value, err := parseMetadataTag(sr, tagAtom)
			if err != nil {
				file.Warnings = append(file.Warnings, types.Warning{
					Stage:   types.StageMetadata,
					Message: fmt.Sprintf("failed to parse tag %s: %v", tagAtom.Type, err),
					Err:     err,
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

// ilstTextFields maps ilst atoms whose value is assigned verbatim to a Tags field.
var ilstTextFields = map[string]func(*types.Tags) *string{
	"\xA9nam": func(t *types.Tags) *string { return &t.Title },       // ©nam
	"\xA9ART": func(t *types.Tags) *string { return &t.Artist },      // ©ART
	"\xA9alb": func(t *types.Tags) *string { return &t.Album },       // ©alb
	"\xA9cmt": func(t *types.Tags) *string { return &t.Comment },     // ©cmt
	"\xA9grp": func(t *types.Tags) *string { return &t.Grouping },    // ©grp - often series info for audiobooks
	"\xA9lyr": func(t *types.Tags) *string { return &t.Lyrics },      // ©lyr
	"aART":    func(t *types.Tags) *string { return &t.AlbumArtist }, // album artist
	"cprt":    func(t *types.Tags) *string { return &t.Copyright },   // copyright
	"ldes":    func(t *types.Tags) *string { return &t.Description }, // long description always wins
}

// applyIlstDate stores a ©day value: Date verbatim, Year from its leading four digits.
func applyIlstDate(file *types.File, value string) {
	file.Tags.Date = value
	if len(value) >= 4 {
		if year, err := strconv.Atoi(value[:4]); err == nil && year > 0 {
			file.Tags.Year = year
		}
	}
}

// Note: In MP4, © is represented as byte 0xA9, so "©nam" is "\xA9nam" in Go strings.
func mapTagToField(tag string, value string, file *types.File) {
	if value == "" {
		return
	}
	if field, ok := ilstTextFields[tag]; ok {
		*field(&file.Tags) = value
		return
	}

	switch tag {
	case "\xA9gen": // Genre (©gen)
		file.Tags.Genres = append(file.Tags.Genres, value)
	case "\xA9wrt": // Composer (©wrt)
		file.Tags.Composers = append(file.Tags.Composers, value)
	case "\xA9day": // Date (©day)
		applyIlstDate(file, value)
	case "desc", "\xA9des": // Description - ldes takes priority regardless of order
		if file.Tags.Description == "" {
			file.Tags.Description = value
		}
	case "\xA9mvn": // Movement Name (©mvn) - used for series in audiobooks
		if file.Tags.Series == "" {
			file.Tags.Series = value
		}
	case "\xA9mvi": // Movement Index (©mvi) - used for series position; 0 means unset
		if value != "0" && file.Tags.SeriesPart == "" {
			file.Tags.SeriesPart = value
		}
	}
}

// TrackData holds track number information.
type TrackData struct {
	Number int
	Total  int
}

// parseTrackNumber extracts track number and total from trkn atom.
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
