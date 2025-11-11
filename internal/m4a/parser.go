package m4a

import (
	"io"

	"github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/registry"
	"github.com/simonhull/audiometa/internal/types"
)

// parser implements the audiometa.FormatParser interface
type parser struct{}

// detectM4Format determines if this is M4A or M4B by checking the ftyp atom
func detectM4Format(sr *binary.SafeReader, size int64) types.Format {
	// Try to find ftyp atom
	ftypAtom, err := findAtom(sr, 0, size, "ftyp")
	if err != nil {
		// No ftyp found, default to M4A
		return types.FormatM4A
	}

	// Read the major brand (4 bytes after ftyp header)
	majorBrand := make([]byte, 4)
	if err := sr.ReadAt(majorBrand, ftypAtom.DataOffset(), "ftyp major brand"); err != nil {
		return types.FormatM4A
	}

	// Check for M4B-specific brands
	brand := string(majorBrand)
	if brand == "M4B " || brand == "M4A " {
		if brand == "M4B " {
			return types.FormatM4B
		}
		return types.FormatM4A
	}

	// Check compatible brands for M4B
	if ftypAtom.DataSize() > 8 {
		compatibleBrands := make([]byte, ftypAtom.DataSize()-8)
		if err := sr.ReadAt(compatibleBrands, ftypAtom.DataOffset()+8, "ftyp compatible brands"); err == nil {
			// Check for M4B in compatible brands
			for i := 0; i+4 <= len(compatibleBrands); i += 4 {
				if string(compatibleBrands[i:i+4]) == "M4B " {
					return types.FormatM4B
				}
			}
		}
	}

	// Default to M4A
	return types.FormatM4A
}

// Parse parses an M4A/M4B file and extracts metadata
func (p *parser) Parse(r io.ReaderAt, size int64, path string) (*types.File, error) {
	// Create safe reader
	sr := binary.NewSafeReader(r, size, path)

	// Detect format internally (check for ftyp atom to determine M4A vs M4B)
	format := detectM4Format(sr, size)

	// Initialize file
	file := &types.File{
		Path:   path,
		Format: format,
		Size:   size,
		Tags:   types.Tags{},
		Audio:  types.AudioInfo{},
	}

	// Find moov atom (movie container)
	moovAtom, err := findAtom(sr, 0, size, "moov")
	if err != nil {
		// No moov atom - return basic file info
		return file, nil
	}

	// Find udta atom (user data) inside moov
	udtaAtom, err := findAtom(sr, moovAtom.DataOffset(), moovAtom.DataOffset()+int64(moovAtom.DataSize()), "udta")
	if err != nil {
		// No udta - return basic file info
		return file, nil
	}

	// Find meta atom inside udta
	metaAtom, err := findAtom(sr, udtaAtom.DataOffset(), udtaAtom.DataOffset()+int64(udtaAtom.DataSize()), "meta")
	if err != nil {
		// No meta - return basic file info
		return file, nil
	}

	// meta atom has 4 bytes of version+flags before the data
	metaDataOffset := metaAtom.DataOffset() + 4
	metaDataEnd := metaAtom.DataOffset() + int64(metaAtom.DataSize())

	// Find ilst atom (iTunes metadata list) inside meta
	ilstAtom, err := findAtom(sr, metaDataOffset, metaDataEnd, "ilst")
	if err != nil {
		// No ilst - return basic file info
		return file, nil
	}

	// Extract metadata from ilst
	if err := extractIlstMetadata(sr, ilstAtom, file); err != nil {
		file.Warnings = append(file.Warnings, types.Warning{
			Stage:   "metadata",
			Message: err.Error(),
		})
	}

	// Parse technical info (duration, bitrate, codec, sample rate, channels)
	if err := parseTechnicalInfo(sr, moovAtom, file); err != nil {
		file.Warnings = append(file.Warnings, types.Warning{
			Stage:   "technical",
			Message: err.Error(),
		})
	}

	// Parse chapters
	chapters, err := parseChapters(sr, moovAtom, file.Audio.Duration)
	if err != nil {
		file.Warnings = append(file.Warnings, types.Warning{
			Stage:   "chapters",
			Message: err.Error(),
		})
	} else if len(chapters) > 0 {
		file.Chapters = chapters
	}

	// Parse audiobook-specific tags (narrator, series, publisher, etc.)
	if ilstAtom != nil {
		if err := parseAudiobookTags(sr, ilstAtom, file); err != nil {
			file.Warnings = append(file.Warnings, types.Warning{
				Stage:   "metadata",
				Message: err.Error(),
			})
		}
	}

	return file, nil
}

// ExtractArtwork extracts embedded artwork from M4A/M4B files
func (p *parser) ExtractArtwork(r io.ReaderAt, size int64, path string) ([]types.Artwork, error) {
	// TODO: Implement artwork extraction from covr atom
	return nil, nil
}

// init registers the M4A/M4B parser
func init() {
	p := &parser{}
	registry.Register(types.FormatM4A, p)
	registry.Register(types.FormatM4B, p)
}
