package m4a

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// Atom types shared by the reader and the writer.
const (
	atomData     = "data"
	itemArtist   = "\xA9ART" // ©ART
	itemGenre    = "\xA9gen" // ©gen
	itemComposer = "\xA9wrt" // ©wrt
	itemLongDesc = "ldes"
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
	return strconv.FormatInt(int64(u<<shift)>>shift, 10) //nolint:gosec // G115: deliberate two's-complement reinterpretation; the arithmetic shift sign-extends the len(b)-byte value
}

// parseMetadataValues returns the non-empty text or integer value of every
// data atom of an iTunes metadata tag atom, in order. Data atoms of other
// types (images, UTF-16, floats) are skipped. A malformed child atom ends the
// list with its error, and the values before it are returned with it. Values
// past the file's tag value cap are not collected.
func parseMetadataValues(sr *binary.SafeReader, tagAtom *Atom, file *types.File) ([]string, error) {
	var values []string
	off, end := tagAtom.DataOffset(), tagAtom.DataOffset()+tagAtom.DataSize()
	for off < end {
		child, err := readAtomHeader(sr, off, end)
		if err != nil {
			return values, err
		}
		if child.Type == atomData {
			value, err := parseItemValue(sr, tagAtom, child)
			if err != nil {
				return values, err
			}
			if value != "" {
				if !types.TakeValue(file) {
					return values, nil
				}
				values = append(values, value)
			}
		}
		// readAtomHeader guarantees 8 <= child.Size <= end-off.
		off += child.Size
	}
	return values, nil
}

// parseItemValue returns the text or integer value of the data atom data of
// the item tagAtom, decoding a known integer item (©mvi, tmpo, rtng, ...)
// labeled as text or implicit as the integer it holds.
func parseItemValue(sr *binary.SafeReader, tagAtom, data *Atom) (string, error) {
	value, err := parseDataValue(sr, data)
	if _, isInt := knownIntItems[tagAtom.Type]; err == nil && isInt && strings.ContainsFunc(value, unicode.IsControl) {
		return parseBinaryInteger(sr, data)
	}
	return value, err
}

// parseBinaryInteger decodes an integer item whose payload is labeled as text
// or implicit, as some taggers write it. Read as text, 10 stored in two bytes
// comes out as "\x00". A payload no integer fits yields "".
func parseBinaryInteger(sr *binary.SafeReader, dataAtom *Atom) (string, error) {
	buf := make([]byte, dataAtom.DataSize()-8)
	if err := sr.ReadAt(buf, dataAtom.DataOffset()+8, "binary integer item"); err != nil {
		return "", err
	}
	return decodeBEInteger(buf, false), nil
}

// parseDataValue decodes one data atom: text (types 0 and 1, trimmed) or a
// big-endian integer (types 21 and 22) as decimal text. Other types yield "".
func parseDataValue(sr *binary.SafeReader, dataAtom *Atom) (string, error) {
	// version (1 byte) + type indicator (3 bytes), then reserved/locale (4 bytes)
	vf, err := sr.Read[uint32](dataAtom.DataOffset(), "data type indicator")
	if err != nil {
		return "", err
	}
	typeIndicator := vf & 0x00FFFFFF

	valueOffset := dataAtom.DataOffset() + 8
	valueSize := dataAtom.DataSize() - 8

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
	if err := sr.CheckLen(valueOffset, int(valueSize), 1, "metadata value"); err != nil {
		return "", err
	}
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
func extractIlstMetadata(ctx context.Context, sr *binary.SafeReader, ilstAtom *Atom, file *types.File) error {
	offset := ilstAtom.DataOffset()
	end := offset + ilstAtom.DataSize()

	var haveTrack, haveDisc bool
	for i := 0; offset < end; i++ {
		if err := binary.Canceled(ctx, i); err != nil {
			return err
		}

		// Read tag atom
		tagAtom, err := readAtomHeader(sr, offset, end)
		if err != nil {
			return err
		}

		switch tagAtom.Type {
		case "trkn":
			// Track number requires special binary parsing
			trk, err := parseTrackNumber(ctx, sr, tagAtom)
			if err == nil && !haveTrack {
				haveTrack = true
				file.Tags.TrackNumber = trk.Number
				file.Tags.TrackTotal = trk.Total
			}
		case "disk":
			if d, err := parseTrackNumber(ctx, sr, tagAtom); err == nil && !haveDisc {
				haveDisc = true
				file.Tags.DiscNumber = d.Number
				file.Tags.DiscTotal = d.Total
			}
		case "covr", "----":
			// covr: artwork, extracted lazily by ExtractArtwork; never read during Open.
			// ----: freeform atoms, handled by parseAudiobookTags.
		case keyGnre:
			// ID3v1 genre index: recorded raw only, never mapped into Genres.
			if index, ok := parseGnre(ctx, sr, tagAtom); ok && types.TakeValue(file) {
				file.Tags.AddRaw(keyGnre, strconv.Itoa(index))
			}
		default:
			mapTextItem(sr, tagAtom, file)
		}

		// Move to next tag
		offset += tagAtom.Size
	}

	return nil
}

// mapTextItem records every value of a text or integer ilst item in the raw
// tags under the item's atom type. A damaged item adds a warning and still
// records the values read before the damage. Typed fields are derived from the
// raw tags once the whole ilst is read.
func mapTextItem(sr *binary.SafeReader, tagAtom *Atom, file *types.File) {
	values, err := parseMetadataValues(sr, tagAtom, file)
	if err != nil {
		// Keep the values read before the damage.
		types.Warn(file, types.StageMetadata, 0, err, fmt.Sprintf("failed to parse tag %s: %v", tagAtom.Type, err))
	}
	for _, v := range values {
		file.Tags.AddRaw(tagAtom.Type, v)
	}
}

// keyGnre is the ilst item holding an ID3v1 genre index plus one.
const keyGnre = "gnre"

// parseGnre decodes a gnre item: its first data atom holds a 16-bit
// big-endian ID3v1 genre index plus one. ok is false when the item is
// malformed or holds 0 (no genre).
func parseGnre(ctx context.Context, sr *binary.SafeReader, atom *Atom) (index int, ok bool) {
	dataAtom, err := atom.child(ctx, sr, atomData)
	if err != nil || dataAtom.DataSize() != 8+2 {
		return 0, false
	}
	v, err := sr.Read[uint16](dataAtom.DataOffset()+8, "gnre value")
	if err != nil || v == 0 {
		return 0, false
	}
	return int(v) - 1, true
}

// trackData holds track number information.
type trackData struct {
	Number int
	Total  int
}

// errShortNumberAtom reports a trkn or disk data atom too short to hold a number.
var errShortNumberAtom = errors.New("track number atom too short")

// parseTrackNumber extracts track number and total from a trkn or disk atom,
// reading only inside its data atom: fewer than 4 payload bytes is an error,
// 4-5 bytes give the number alone, and 6 or more give number and total.
func parseTrackNumber(ctx context.Context, sr *binary.SafeReader, atom *Atom) (trackData, error) {
	result := trackData{}

	// Find data atom
	dataAtom, err := atom.child(ctx, sr, atomData)
	if err != nil {
		return result, err
	}

	// Version (1) + flags (3) + reserved (4) = 8 bytes precede the payload.
	payload := dataAtom.DataSize() - 8
	if payload < 4 {
		return result, errShortNumberAtom
	}
	offset := dataAtom.DataOffset() + 8

	// Track number structure:
	// [2 bytes] reserved
	// [2 bytes] track number
	// [2 bytes] track total
	// [2 bytes] reserved

	offset += 2 // skip reserved

	trackNum, err := sr.Read[uint16](offset, "track number")
	if err != nil {
		return result, err
	}
	result.Number = int(trackNum)
	offset += 2

	if payload < 6 {
		return result, nil
	}

	trackTotal, err := sr.Read[uint16](offset, "track total")
	if err != nil {
		return result, err
	}
	result.Total = int(trackTotal)

	return result, nil
}
