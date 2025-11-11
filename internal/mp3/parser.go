package mp3

import (
	"io"

	"github.com/simonhull/audiometa"
	binutil "github.com/simonhull/audiometa/internal/binary"
)

// parser implements the audiometa.FormatParser interface
type parser struct{}

// Parse parses a single MP3 file and extracts metadata
func (p *parser) Parse(r io.ReaderAt, size int64, path string) (*audiometa.File, error) {
	// Create safe reader
	sr := binutil.NewSafeReader(r, size, path)

	// Initialize file
	file := &audiometa.File{
		Path:   path,
		Format: audiometa.FormatMP3,
		Size:   size,
		Tags:   audiometa.Tags{},
		Audio:  audiometa.AudioInfo{},
	}

	// Parse ID3v2 tag (if present)
	tagSize, err := parseID3v2(sr, file)
	if err != nil {
		// Not an ID3v2 file or parse error
		// Try to find MP3 frames anyway
		file.Warnings = append(file.Warnings, audiometa.Warning{
			Stage:   "metadata",
			Message: "ID3v2 parsing failed: " + err.Error(),
		})
		tagSize = 0
	}

	// Parse MP3 frame headers for technical info (bitrate, duration, etc.)
	if err := parseTechnicalInfo(sr, tagSize, size, file); err != nil {
		file.Warnings = append(file.Warnings, audiometa.Warning{
			Stage:   "technical",
			Message: "failed to parse MP3 technical info: " + err.Error(),
		})
	}

	// Apply fallbacks
	// If no narrator from TXXX, use composer
	if file.Tags.Narrator == "" && len(file.Tags.Composers) > 0 {
		file.Tags.Narrator = file.Tags.Composers[0]
	}

	return file, nil
}

// ExtractArtwork extracts embedded artwork from MP3 files
func (p *parser) ExtractArtwork(r io.ReaderAt, size int64, path string) ([]audiometa.Artwork, error) {
	// TODO: Implement artwork extraction from APIC frames
	return nil, nil
}

// init registers the MP3 parser
func init() {
	audiometa.RegisterParser(audiometa.FormatMP3, &parser{})
}
