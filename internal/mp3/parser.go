package mp3

import (
	"context"
	"fmt"
	"io"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/registry"
	"github.com/simonhull/audiometa/internal/types"
)

// parser implements the audiometa.FormatParser interface.
type parser struct{}

// Parse parses a single MP3 file and extracts metadata.
func (p *parser) Parse(ctx context.Context, r io.ReaderAt, size int64, path string) (*types.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Create safe reader
	sr := binutil.NewSafeReader(r, size, path)

	// Initialize file
	file := &types.File{
		Path:   path,
		Format: types.FormatMP3,
		Size:   size,
		Tags:   types.Tags{},
		Audio:  types.AudioInfo{},
	}

	// Parse ID3v2 tag (if present)
	tagSize, err := parseID3v2(sr, file)
	if err != nil {
		// Not an ID3v2 file or parse error - try to find MP3 frames anyway
		file.Warnings = append(file.Warnings, types.Warning{
			Stage:   "metadata",
			Message: fmt.Sprintf("ID3v2 parsing failed: %v", err),
			Err:     err,
		})
		tagSize = 0
	}

	// Parse MP3 frame headers for technical info (bitrate, duration, etc.)
	if err := parseTechnicalInfo(sr, tagSize, size, file); err != nil {
		file.Warnings = append(file.Warnings, types.Warning{
			Stage:   "technical",
			Message: fmt.Sprintf("failed to parse MP3 technical info: %v", err),
			Err:     err,
		})
	}

	// Apply fallbacks
	// If no narrator from TXXX, use composer
	if file.Tags.Narrator == "" && len(file.Tags.Composers) > 0 {
		file.Tags.Narrator = file.Tags.Composers[0]
	}

	return file, nil
}

// ExtractArtwork extracts embedded artwork from MP3 files.
func (p *parser) ExtractArtwork(ctx context.Context, r io.ReaderAt, size int64, path string) ([]types.Artwork, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return extractArtwork(r, size, path)
}

// init registers the MP3 parser.
func init() {
	registry.Register(types.FormatMP3, &parser{})
}
