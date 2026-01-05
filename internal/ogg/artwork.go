package ogg

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"strings"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// ExtractArtwork extracts embedded artwork from Ogg Vorbis/Opus files.
//
// Ogg files can embed artwork via the METADATA_BLOCK_PICTURE Vorbis comment,
// which contains a base64-encoded FLAC picture block.
func (p *parser) ExtractArtwork(r io.ReaderAt, size int64, path string) ([]types.Artwork, error) {
	sr := binutil.NewSafeReader(r, size, path)

	// Parse file to get Vorbis comments
	file, err := p.Parse(r, size, path)
	if err != nil {
		return nil, fmt.Errorf("parse file: %w", err)
	}

	var artwork []types.Artwork

	// Look for METADATA_BLOCK_PICTURE tags
	for key, values := range file.Tags.All() {
		if !strings.EqualFold(key, "METADATA_BLOCK_PICTURE") {
			continue
		}

		for _, value := range values {
			pic, err := parseMetadataBlockPicture(value)
			if err != nil {
				// Skip invalid pictures but continue
				continue
			}
			artwork = append(artwork, pic)
		}
	}

	// Also check raw comments in case they weren't parsed into Tags.raw
	// (defensive - they should be there)
	_ = sr // silence unused warning; sr could be used for direct parsing if needed

	return artwork, nil
}

// parseMetadataBlockPicture decodes a METADATA_BLOCK_PICTURE value.
//
// The value is base64-encoded data containing a FLAC picture block:
//   - 4 bytes: picture type (uint32 BE)
//   - 4 bytes: MIME type length
//   - N bytes: MIME type string
//   - 4 bytes: description length
//   - N bytes: description string (UTF-8)
//   - 4 bytes: width
//   - 4 bytes: height
//   - 4 bytes: color depth
//   - 4 bytes: colors used (0 for non-indexed)
//   - 4 bytes: image data length
//   - N bytes: image data
func parseMetadataBlockPicture(base64Value string) (types.Artwork, error) {
	// Decode base64
	data, err := base64.StdEncoding.DecodeString(base64Value)
	if err != nil {
		return types.Artwork{}, fmt.Errorf("invalid base64: %w", err)
	}

	// Minimum size: 4 + 4 + 0 + 4 + 0 + 4 + 4 + 4 + 4 + 4 + 0 = 32 bytes
	if len(data) < 32 {
		return types.Artwork{}, fmt.Errorf("picture block too small: %d bytes", len(data))
	}

	offset := 0

	// Read picture type (32-bit big-endian)
	pictureType := binary.BigEndian.Uint32(data[offset:])
	offset += 4

	// Read MIME type length
	mimeLength := binary.BigEndian.Uint32(data[offset:])
	offset += 4

	if offset+int(mimeLength) > len(data) {
		return types.Artwork{}, fmt.Errorf("MIME type length exceeds data")
	}

	// Read MIME type
	mimeType := string(data[offset : offset+int(mimeLength)])
	offset += int(mimeLength)

	// Read description length
	if offset+4 > len(data) {
		return types.Artwork{}, fmt.Errorf("unexpected end of data")
	}
	descLength := binary.BigEndian.Uint32(data[offset:])
	offset += 4

	if offset+int(descLength) > len(data) {
		return types.Artwork{}, fmt.Errorf("description length exceeds data")
	}

	// Read description
	description := string(data[offset : offset+int(descLength)])
	offset += int(descLength)

	// Read width, height (skip color depth and indexed colors)
	if offset+16 > len(data) {
		return types.Artwork{}, fmt.Errorf("unexpected end of data")
	}
	width := binary.BigEndian.Uint32(data[offset:])
	offset += 4
	height := binary.BigEndian.Uint32(data[offset:])
	offset += 4

	// Skip color depth and indexed colors
	offset += 8

	// Read picture data length
	if offset+4 > len(data) {
		return types.Artwork{}, fmt.Errorf("unexpected end of data")
	}
	dataLength := binary.BigEndian.Uint32(data[offset:])
	offset += 4

	if offset+int(dataLength) > len(data) {
		return types.Artwork{}, fmt.Errorf("picture data length exceeds data")
	}

	// Read picture data
	pictureData := data[offset : offset+int(dataLength)]

	// Map FLAC picture type to types.ArtworkType
	var artType types.ArtworkType
	switch pictureType {
	case 3:
		artType = types.ArtworkFrontCover
	case 4:
		artType = types.ArtworkBackCover
	default:
		artType = types.ArtworkOther
	}

	return types.Artwork{
		Data:        pictureData,
		MIMEType:    mimeType,
		Type:        artType,
		Description: description,
		Width:       int(width),
		Height:      int(height),
	}, nil
}
