package m4a

import (
	"context"
	"strings"

	"github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/parsing"
	"github.com/simonhull/audiometa/internal/types"
)

// parseAudiobookTags extracts narrator, series, publisher, etc. from custom atoms.
func parseAudiobookTags(ctx context.Context, sr *binary.SafeReader, ilstAtom *Atom, file *types.File) error {
	offset := ilstAtom.DataOffset()
	end := offset + int64(ilstAtom.DataSize())

	// Collect all custom tags for series part resolution
	customTags := make(map[string]string)

	// Scan through ilst for custom atoms (----)
	for i := 0; offset < end; i++ {
		if err := binary.Canceled(ctx, i); err != nil {
			return err
		}

		atom, err := readAtomHeader(sr, offset, end)
		if err != nil {
			// Skip corrupted atoms
			break
		}

		// Check if this is a custom atom (----)
		// Type: 0x2D2D2D2D = "----"
		if atom.Type == "----" {
			// Parse custom atom and collect tags
			if fieldName, value, err := parseCustomAtomWithTags(sr, atom, file); err == nil {
				customTags[fieldName] = value
			}
		}

		offset += int64(atom.Size)
	}

	// Apply fallbacks
	// If no custom Narrator atom, use Composer as fallback
	if file.Tags.Narrator == "" && len(file.Tags.Composers) > 0 {
		file.Tags.Narrator = file.Tags.Composers[0]
	}

	// Grouping -> Series, then the series part from custom atoms, title, album, path.
	parsing.ResolveSeriesFallbacks(&file.Tags, sr.Path(), seriesSource(file, customTags))

	return nil
}

// recordFreeformRaw stores a ---- atom in the raw tag map under the upper-cased
// freeform name, matching the Vorbis key convention.
func recordFreeformRaw(file *types.File, name, value string) {
	if name != "" && value != "" {
		file.Tags.AddRaw(strings.ToUpper(name), value)
	}
}

// parseCustomAtomWithTags parses a ---- custom atom and returns the field name and value.
func parseCustomAtomWithTags(sr *binary.SafeReader, customAtom *Atom, file *types.File) (string, string, error) {
	offset := customAtom.DataOffset()
	end := offset + int64(customAtom.DataSize())

	var namespace, fieldName, value string

	// Parse child atoms (mean, name, data)
	for offset < end {
		atom, err := readAtomHeader(sr, offset, end)
		if err != nil {
			break
		}

		switch atom.Type {
		case "mean":
			// Namespace (usually "com.apple.iTunes")
			// Skip version+flags (4 bytes)
			dataOffset := atom.DataOffset() + 4
			dataSize := int64(atom.DataSize()) - 4
			if dataSize > 0 && sr.CheckLen(dataOffset, int(dataSize), 1, "mean namespace") == nil {
				buf := make([]byte, dataSize)
				if err := sr.ReadAt(buf, dataOffset, "mean namespace"); err == nil {
					namespace = string(buf)
				}
			}

		case "name":
			// Field name (e.g., "Narrator", "Series")
			// Skip version+flags (4 bytes)
			dataOffset := atom.DataOffset() + 4
			dataSize := int64(atom.DataSize()) - 4
			if dataSize > 0 && sr.CheckLen(dataOffset, int(dataSize), 1, "name field") == nil {
				buf := make([]byte, dataSize)
				if err := sr.ReadAt(buf, dataOffset, "name field"); err == nil {
					fieldName = string(buf)
				}
			}

		case "data":
			// Value - parse the data atom directly
			// Skip version (1 byte) + flags (3 bytes) + reserved (4 bytes) = 8 bytes
			valueOffset := atom.DataOffset() + 8
			valueSize := int64(atom.DataSize()) - 8
			if valueSize > 0 && sr.CheckLen(valueOffset, int(valueSize), 1, "data value") == nil {
				buf := make([]byte, valueSize)
				if err := sr.ReadAt(buf, valueOffset, "data value"); err == nil {
					value = strings.TrimRight(string(buf), "\x00")
					value = strings.TrimSpace(value)
				}
			}
		}

		offset += int64(atom.Size)
	}

	// Map field name to metadata field
	// Namespace is usually "com.apple.iTunes" but can vary
	_ = namespace // Unused for now, but parsed for potential future filtering
	mapAudiobookField(fieldName, value, file)
	recordFreeformRaw(file, fieldName, value)

	return fieldName, value, nil
}

// to allow multi-source validation and fallback.
func mapAudiobookField(fieldName, value string, file *types.File) {
	// Normalize field name (case-insensitive)
	fieldName = strings.ToLower(fieldName)

	switch fieldName {
	case "subtitle":
		file.Tags.Subtitle = value
	case "narrator":
		file.Tags.Narrator = value
	case "series":
		file.Tags.Series = value
	// "series part", "seriespart", "part" - intentionally NOT set here
	// These are collected in customTags and resolved via resolveSeriesPart()
	case "publisher":
		file.Tags.Publisher = value
	case "isbn":
		file.Tags.ISBN = value
	case "asin", "audible_asin":
		file.Tags.ASIN = value
	case "language", "lang":
		file.Tags.Language = value
	case "description":
		if file.Tags.Description == "" {
			file.Tags.Description = value
		}
	case "mvnm", "movement name", "movement":
		if file.Tags.Series == "" {
			file.Tags.Series = value
		}
	case "mvin", "movement number", "movement index":
		if file.Tags.SeriesPart == "" {
			file.Tags.SeriesPart = value
		}
	}
}

// lookupFold returns the first non-empty value whose key equals key case-insensitively.
func lookupFold(tags map[string]string, key string) string {
	for k, v := range tags {
		if v != "" && strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}

// seriesSource builds the extra series-part candidates M4A consults.
// Priority: custom atoms > title parsing > album parsing (then path, applied by the helper).
func seriesSource(file *types.File, customTags map[string]string) parsing.SeriesSource {
	return parsing.SeriesSource{
		ExplicitParts: []string{
			lookupFold(customTags, "Series Part"),
			lookupFold(customTags, "Series Position"),
			lookupFold(customTags, "Part"),
			lookupFold(customTags, "Volume"),
		},
		Title: file.Tags.Title,
		Album: file.Tags.Album,
	}
}
