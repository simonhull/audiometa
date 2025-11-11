package m4a

import (
	"io"

	"github.com/simonhull/audiometa"
	"github.com/simonhull/audiometa/internal/binary"
)

// parser implements the audiometa.FormatParser interface
type parser struct{}

// Parse parses an M4A/M4B file and extracts metadata
func (p *parser) Parse(r io.ReaderAt, size int64, path string) (*audiometa.File, error) {
	// Create safe reader
	sr := binary.NewSafeReader(r, size, path)

	// Detect format
	format, err := audiometa.DetectFormat(r, size, path)
	if err != nil {
		return nil, err
	}

	// Initialize file
	file := &audiometa.File{
		Path:   path,
		Format: format,
		Size:   size,
		Tags:   audiometa.Tags{},
		Audio:  audiometa.AudioInfo{},
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
		file.Warnings = append(file.Warnings, audiometa.Warning{
			Stage:   "metadata",
			Message: err.Error(),
		})
	}

	// Parse technical info (duration, bitrate, codec, sample rate, channels)
	if err := parseTechnicalInfo(sr, moovAtom, file); err != nil {
		file.Warnings = append(file.Warnings, audiometa.Warning{
			Stage:   "technical",
			Message: err.Error(),
		})
	}

	// Parse chapters
	chapters, err := parseChapters(sr, moovAtom, file.Audio.Duration)
	if err != nil {
		file.Warnings = append(file.Warnings, audiometa.Warning{
			Stage:   "chapters",
			Message: err.Error(),
		})
	} else if len(chapters) > 0 {
		file.Chapters = chapters
	}

	// Parse audiobook-specific tags (narrator, series, publisher, etc.)
	if ilstAtom != nil {
		if err := parseAudiobookTags(sr, ilstAtom, file); err != nil {
			file.Warnings = append(file.Warnings, audiometa.Warning{
				Stage:   "metadata",
				Message: err.Error(),
			})
		}
	}

	return file, nil
}

// ExtractArtwork extracts embedded artwork from M4A/M4B files
func (p *parser) ExtractArtwork(r io.ReaderAt, size int64, path string) ([]audiometa.Artwork, error) {
	// TODO: Implement artwork extraction from covr atom
	return nil, nil
}

// init registers the M4A/M4B parser
func init() {
	p := &parser{}
	audiometa.RegisterParser(audiometa.FormatM4A, p)
	audiometa.RegisterParser(audiometa.FormatM4B, p)
}
