package m4a

import (
	"fmt"

	"github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

const (
	mimeTypeJPEG = "image/jpeg"
	mimeTypePNG  = "image/png"
	mimeTypeBMP  = "image/bmp"
)

// extractArtwork extracts embedded cover art from M4A/M4B files.
// Navigates: moov → udta → meta → ilst → covr → data atoms.
func extractArtwork(sr *binary.SafeReader, size int64) ([]types.Artwork, error) {
	var artwork []types.Artwork

	// Find moov atom (movie container)
	moovAtom, err := findAtom(sr, 0, size, "moov")
	if err != nil {
		// No moov = no metadata = no artwork
		return nil, nil
	}

	// Find udta atom (user data) inside moov
	udtaAtom, err := findAtom(sr, moovAtom.DataOffset(), moovAtom.DataOffset()+int64(moovAtom.DataSize()), "udta")
	if err != nil {
		return nil, nil
	}

	// Find meta atom inside udta
	metaAtom, err := findAtom(sr, udtaAtom.DataOffset(), udtaAtom.DataOffset()+int64(udtaAtom.DataSize()), "meta")
	if err != nil {
		return nil, nil
	}

	// meta atom has 4 bytes of version+flags before the data
	metaDataOffset := metaAtom.DataOffset() + 4
	metaDataEnd := metaAtom.DataOffset() + int64(metaAtom.DataSize())

	// Find ilst atom (iTunes metadata list) inside meta
	ilstAtom, err := findAtom(sr, metaDataOffset, metaDataEnd, "ilst")
	if err != nil {
		return nil, nil
	}

	// Find covr atom inside ilst
	covrAtom, err := findAtom(sr, ilstAtom.DataOffset(), ilstAtom.DataOffset()+int64(ilstAtom.DataSize()), "covr")
	if err != nil {
		// No covr atom = no artwork (not an error)
		return nil, nil
	}

	// Parse all data atoms inside covr
	offset := covrAtom.DataOffset()
	end := offset + int64(covrAtom.DataSize())

	for offset < end {
		dataAtom, err := readAtomHeader(sr, offset)
		if err != nil {
			break
		}

		// Only process data atoms
		if dataAtom.Type == "data" {
			art, err := parseCovrData(sr, dataAtom)
			if err != nil {
				// Log but continue to next data atom
				// Graceful degradation: some artwork is better than none
				offset += int64(dataAtom.Size)
				continue
			}
			artwork = append(artwork, art)
		}

		offset += int64(dataAtom.Size)

		// Prevent infinite loop
		if dataAtom.Size == 0 {
			break
		}
	}

	return artwork, nil
}

// parseCovrData extracts artwork from a single covr data atom.
func parseCovrData(sr *binary.SafeReader, dataAtom *Atom) (types.Artwork, error) {
	// data atom structure:
	// [1 byte] version
	// [3 bytes] flags (byte 3 indicates MIME type)
	// [4 bytes] reserved
	// [remaining] image data

	offset := dataAtom.DataOffset()

	// Read version + flags (4 bytes total)
	versionFlags, err := binary.Read[uint32](sr, offset, "data version+flags")
	if err != nil {
		return types.Artwork{}, err
	}

	// Extract flags byte 3 (MIME type indicator)
	flagsByte3 := uint8(versionFlags & 0xFF)
	mimeType := flagsToMIMEType(flagsByte3)

	// Skip reserved (4 bytes)
	offset += 4 + 4

	// Read image data (rest of atom)
	imageSize := int64(dataAtom.DataSize()) - 8
	if imageSize <= 0 {
		return types.Artwork{}, fmt.Errorf("invalid image size: %d", imageSize)
	}

	imageData := make([]byte, imageSize)
	if err := sr.ReadAt(imageData, offset, "cover image data"); err != nil {
		return types.Artwork{}, err
	}

	// Detect dimensions from image data if possible
	width, height := detectImageDimensions(imageData, mimeType)

	return types.Artwork{
		MIMEType:    mimeType,
		Data:        imageData,
		Type:        types.ArtworkFrontCover, // covr is typically front cover
		Description: "",                      // M4A doesn't store artwork descriptions
		Width:       width,
		Height:      height,
	}, nil
}

// flagsToMIMEType converts M4A flags byte to MIME type.
func flagsToMIMEType(flags byte) string {
	switch flags {
	case 0x0D: // JPEG
		return mimeTypeJPEG
	case 0x0E: // PNG
		return mimeTypePNG
	case 0x1B: // BMP
		return mimeTypeBMP
	default:
		// Default to JPEG (most common)
		return mimeTypeJPEG
	}
}

// detectImageDimensions extracts width/height from image data.
// Supports JPEG and PNG. Returns 0, 0 if unable to detect.
func detectImageDimensions(data []byte, mimeType string) (int, int) {
	switch mimeType {
	case mimeTypeJPEG:
		return detectJPEGDimensions(data)
	case mimeTypePNG:
		return detectPNGDimensions(data)
	default:
		return 0, 0
	}
}

// detectJPEGDimensions extracts dimensions from JPEG data.
func detectJPEGDimensions(data []byte) (int, int) {
	// JPEG structure: markers are 0xFF followed by marker type
	// SOF markers contain dimensions: SOF0 (0xC0), SOF1 (0xC1), SOF2 (0xC2)
	for i := 0; i < len(data)-9; i++ {
		if data[i] != 0xFF {
			continue
		}

		marker := data[i+1]
		// Check for SOF markers (Start Of Frame)
		if marker == 0xC0 || marker == 0xC1 || marker == 0xC2 {
			// SOF format: FF Cn [2 bytes length] [1 byte precision] [2 bytes height] [2 bytes width]
			if i+9 <= len(data) {
				height := int(data[i+5])<<8 | int(data[i+6])
				width := int(data[i+7])<<8 | int(data[i+8])
				return width, height
			}
		}
	}
	return 0, 0
}

// detectPNGDimensions extracts dimensions from PNG data.
func detectPNGDimensions(data []byte) (int, int) {
	// PNG structure: 8-byte signature + IHDR chunk
	// IHDR is at bytes 8-24: [4 len] [4 "IHDR"] [4 width] [4 height] [...]
	if len(data) < 24 {
		return 0, 0
	}

	// Verify PNG signature
	pngSig := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	for i := range 8 {
		if data[i] != pngSig[i] {
			return 0, 0
		}
	}

	// Read IHDR dimensions (big-endian)
	width := int(data[16])<<24 | int(data[17])<<16 | int(data[18])<<8 | int(data[19])
	height := int(data[20])<<24 | int(data[21])<<16 | int(data[22])<<8 | int(data[23])

	return width, height
}
