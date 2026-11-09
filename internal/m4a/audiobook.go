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

// recordFreeformRaw stores the values of a ---- atom in the raw tag map under
// the upper-cased freeform name, matching the Vorbis key convention, and keeps
// the name as stored as the key's name.
func recordFreeformRaw(file *types.File, name string, values []string) {
	if name == "" {
		return
	}
	for _, v := range values {
		file.Tags.AddRawNamed(name, v)
	}
}

// parseCustomAtomWithTags parses a ---- custom atom and returns the field name
// and its first value. Every non-empty value of its data atoms goes to the raw
// tags; like a repeated atom, only the first can fill a typed field.
func parseCustomAtomWithTags(sr *binary.SafeReader, customAtom *Atom, file *types.File) (string, string, error) {
	offset := customAtom.DataOffset()
	end := offset + customAtom.DataSize()

	var namespace, fieldName string
	var values []string

	// Parse child atoms (mean, name, data)
	for offset < end {
		atom, err := readAtomHeader(sr, offset, end)
		if err != nil {
			break
		}

		switch atom.Type {
		case "mean":
			// Namespace (usually "com.apple.iTunes"), after version and flags.
			namespace = readAtomText(sr, atom, 4, "mean namespace")
		case "name":
			// Field name (e.g., "Narrator", "Series"), after version and flags.
			fieldName = readAtomText(sr, atom, 4, "name field")
		case "data":
			// Value, after version, flags and the reserved word.
			text := readAtomText(sr, atom, 8, "data value")
			if v := strings.TrimSpace(strings.TrimRight(text, "\x00")); v != "" {
				values = append(values, v)
			}
		}

		offset += atom.Size
	}

	// Map field name to metadata field
	// Namespace is usually "com.apple.iTunes" but can vary
	_ = namespace // Unused for now, but parsed for potential future filtering
	var first string
	if len(values) > 0 {
		first = values[0]
	}
	mapAudiobookField(fieldName, first, file)
	recordFreeformRaw(file, fieldName, values)

	return fieldName, first, nil
}

// readAtomText returns an atom's content after skip bytes, or "" when there is
// none or it cannot be read.
func readAtomText(sr *binary.SafeReader, atom *Atom, skip int64, what string) string {
	offset, size := atom.DataOffset()+skip, atom.DataSize()-skip
	if size <= 0 || sr.CheckLen(offset, int(size), 1, what) != nil {
		return ""
	}
	buf := make([]byte, size)
	if err := sr.ReadAt(buf, offset, what); err != nil {
		return ""
	}
	return string(buf)
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
