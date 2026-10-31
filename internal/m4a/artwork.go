package m4a

import (
	"context"
	"fmt"

	"github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/imagemeta"
	"github.com/simonhull/audiometa/internal/types"
)

const (
	mimeTypeJPEG = "image/jpeg"
	mimeTypePNG  = "image/png"
	mimeTypeBMP  = "image/bmp"
)

// extractArtwork extracts embedded cover art from M4A/M4B files.
// Navigates: moov → udta → meta → ilst → covr → data atoms.
func extractArtwork(ctx context.Context, sr *binary.SafeReader, size int64) ([]types.Artwork, error) {
	var artwork []types.Artwork

	// Find moov atom (movie container)
	moovAtom, err := findAtomContext(ctx, sr, 0, size, "moov")
	if err != nil {
		// No moov = no metadata = no artwork
		return nil, nil
	}

	ilstAtom, err := findIlst(ctx, sr, moovAtom)
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

	for i := 0; offset < end; i++ {
		if err := binary.Canceled(ctx, i); err != nil {
			return nil, err
		}

		dataAtom, err := readAtomHeader(sr, offset, end)
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
	versionFlags, err := sr.Read[uint32](offset, "data version+flags")
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

	if err := sr.CheckLen(offset, int(imageSize), 1, "cover image data"); err != nil {
		return types.Artwork{}, err
	}
	imageData := make([]byte, imageSize)
	if err := sr.ReadAt(imageData, offset, "cover image data"); err != nil {
		return types.Artwork{}, err
	}

	// Detect dimensions from image data if possible
	width, height := imagemeta.Dimensions(imageData, mimeType)

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
