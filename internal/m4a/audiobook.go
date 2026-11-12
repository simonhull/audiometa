package m4a

import (
	"context"

	"github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// parseAudiobookTags records the freeform (----) atoms of the ilst atom (narrator,
// series, publisher, ...) in the raw tags. A malformed atom header ends the scan of
// its container; what was read before it is recorded, and the first such error is
// returned.
func parseAudiobookTags(ctx context.Context, sr *binary.SafeReader, ilstAtom *Atom, file *types.File) error {
	offset := ilstAtom.DataOffset()
	end := offset + ilstAtom.DataSize()

	var firstErr error

	// Scan through ilst for custom atoms (----)
	for i := 0; offset < end; i++ {
		if err := binary.Canceled(ctx, i); err != nil {
			return err
		}

		atom, err := readAtomHeader(sr, offset, end)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			break
		}

		// A freeform atom has the type "----" (0x2D2D2D2D).
		if atom.Type == "----" {
			if err := parseCustomAtom(sr, atom, file); firstErr == nil {
				firstErr = err
			}
		}

		offset += atom.Size
	}

	return firstErr
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

// parseCustomAtom parses a ---- custom atom and records every non-empty value of
// its data atoms in the raw tags under the upper-cased freeform name. A malformed
// child header ends the scan: what was read before it is recorded, and the error
// is returned.
func parseCustomAtom(sr *binary.SafeReader, customAtom *Atom, file *types.File) error {
	offset := customAtom.DataOffset()
	end := offset + customAtom.DataSize()

	var fieldName string
	var values []string
	var headerErr error

	// Parse child atoms (mean, name, data). The namespace in mean (usually
	// "com.apple.iTunes") does not take part in the key.
	for offset < end {
		atom, err := readAtomHeader(sr, offset, end)
		if err != nil {
			headerErr = err
			break
		}

		switch atom.Type {
		case "name":
			// Field name (e.g., "Narrator", "Series"), after version and flags.
			fieldName = readAtomText(sr, atom, 4, "name field")
		case "data":
			// Decoded by data type like every other ilst item.
			if v, err := parseDataValue(sr, atom); err == nil && v != "" && types.TakeValue(file) {
				values = append(values, v)
			}
		}

		offset += atom.Size
	}

	recordFreeformRaw(file, fieldName, values)
	return headerErr
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
