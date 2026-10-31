package m4a

import (
	"context"
	"strings"

	"github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// parseAudiobookTags extracts narrator, series, publisher, etc. from custom atoms.
func parseAudiobookTags(ctx context.Context, sr *binary.SafeReader, ilstAtom *Atom, file *types.File) error {
	offset := ilstAtom.DataOffset()
	end := offset + ilstAtom.DataSize()

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

		offset += atom.Size
	}

	// Series part comes only from explicit custom atoms; nothing is inferred.
	applyExplicitSeriesPart(file, customTags)

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
	end := offset + customAtom.DataSize()

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
			dataSize := atom.DataSize() - 4
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
			dataSize := atom.DataSize() - 4
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
			valueSize := atom.DataSize() - 8
			if valueSize > 0 && sr.CheckLen(valueOffset, int(valueSize), 1, "data value") == nil {
				buf := make([]byte, valueSize)
				if err := sr.ReadAt(buf, valueOffset, "data value"); err == nil {
					value = strings.TrimRight(string(buf), "\x00")
					value = strings.TrimSpace(value)
				}
			}
		}

		offset += atom.Size
	}

	// Map field name to metadata field
	// Namespace is usually "com.apple.iTunes" but can vary
	_ = namespace // Unused for now, but parsed for potential future filtering
	mapAudiobookField(fieldName, value, file)
	recordFreeformRaw(file, fieldName, value)

	return fieldName, value, nil
}

// freeformTextFields maps lowercased freeform atom names whose value is assigned verbatim.
var freeformTextFields = map[string]func(*types.Tags) *string{
	"subtitle":     func(t *types.Tags) *string { return &t.Subtitle },
	"narrator":     func(t *types.Tags) *string { return &t.Narrator },
	"series":       func(t *types.Tags) *string { return &t.Series },
	"publisher":    func(t *types.Tags) *string { return &t.Publisher },
	"isbn":         func(t *types.Tags) *string { return &t.ISBN },
	"asin":         func(t *types.Tags) *string { return &t.ASIN },
	"audible_asin": func(t *types.Tags) *string { return &t.ASIN },
	"language":     func(t *types.Tags) *string { return &t.Language },
	"lang":         func(t *types.Tags) *string { return &t.Language },
}

// to allow multi-source validation and fallback.
func mapAudiobookField(fieldName, value string, file *types.File) {
	// An explicit freeform atom overrides values taken from standard atoms,
	// but among repeated freeform atoms of the same name the first wins.
	// Callers record the atom in the raw tags only after this returns.
	seen := len(file.Tags.Get(strings.ToUpper(fieldName))) > 0
	fieldName = strings.ToLower(fieldName)
	if field, ok := freeformTextFields[fieldName]; ok {
		if !seen && value != "" {
			*field(&file.Tags) = value
		}
		return
	}

	switch fieldName {
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

// applyExplicitSeriesPart sets SeriesPart from the first explicit custom atom
// present, in priority order, unless an MVIN atom already filled it.
func applyExplicitSeriesPart(file *types.File, customTags map[string]string) {
	if file.Tags.SeriesPart != "" {
		return
	}
	for _, name := range []string{"Series Part", "Series Position"} {
		if v := lookupFold(customTags, name); v != "" {
			file.Tags.SeriesPart = v
			return
		}
	}
}
