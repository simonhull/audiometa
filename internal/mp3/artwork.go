package mp3

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/imagemeta"
	"github.com/simonhull/audiometa/internal/types"
)

var (
	errAPICTooShort    = errors.New("APIC frame too short")
	errAPICNoMIMETerm  = errors.New("APIC MIME type not null-terminated")
	errAPICTruncated   = errors.New("APIC frame truncated after MIME type")
	errAPICNoImageData = errors.New("APIC frame has no image data")
)

// extractArtwork returns the pictures (APIC, or PIC in ID3v2.2) of the ID3v2
// tag at the start of r, in tag order, decoded exactly as metadata frames are.
// Space without a supported ID3v2 tag has none.
func extractArtwork(ctx context.Context, r io.ReaderAt, size int64, path string) ([]types.Artwork, error) {
	tag, err := openID3v2Tag(binutil.NewSafeReader(r, size, path), ignoreWarning)
	if err != nil {
		return nil, nil // no supported ID3v2 tag: no embedded artwork
	}
	parse := parseAPICFrame
	if tag.header.Version == 2 {
		parse = parsePICFrame
	}
	var artwork []types.Artwork
	for frame, err := range tag.frames(ctx, isPictureFrame, ignoreWarning) {
		if err != nil {
			return nil, err
		}
		if art, err := parse(frame.Data); err == nil {
			artwork = append(artwork, art)
		}
	}
	return artwork, nil
}

// isPictureFrame reports whether a frame holds a picture.
func isPictureFrame(id string) bool { return id == frameIDAPIC }

// ignoreWarning drops a warning. ExtractArtwork walks the same tag Open
// already walked, and Open reported its problems.
func ignoreWarning(types.Warning) {}

// parseAPICFrame parses an APIC (Attached Picture) frame.
// Format:
//
//	[1 byte]              Text encoding
//	[null-terminated]     MIME type
//	[1 byte]              Picture type
//	[null-terminated]     Description
//	[remaining]           Picture data
func parseAPICFrame(data []byte) (types.Artwork, error) {
	if len(data) < 4 {
		return types.Artwork{}, errAPICTooShort
	}

	encoding := data[0]
	pos := 1

	// Parse MIME type (always ISO-8859-1 per spec)
	mimeEnd := bytes.IndexByte(data[pos:], 0)
	if mimeEnd < 0 {
		return types.Artwork{}, errAPICNoMIMETerm
	}
	mimeType := string(data[pos : pos+mimeEnd])
	pos += mimeEnd + 1

	// Handle legacy MIME type markers
	switch mimeType {
	case "JPG", "jpg":
		mimeType = imagemeta.MIMEJPEG
	case "PNG", "png":
		mimeType = imagemeta.MIMEPNG
	case "", "-->":
		// Empty or URL reference - try to detect from data
		mimeType = imagemeta.MIMEJPEG // Default, will be overridden if PNG detected
	}

	return parsePictureBody(data, pos, encoding, mimeType)
}

// parsePictureBody parses the part shared by APIC and PIC after the image
// format/MIME: [picture type][description\x00][image data], starting at pos.
func parsePictureBody(data []byte, pos int, encoding byte, mimeType string) (types.Artwork, error) {
	if pos >= len(data) {
		return types.Artwork{}, errAPICTruncated
	}

	// Picture type (1 byte)
	pictureType := data[pos]
	pos++

	// Parse description (encoding-dependent null terminator)
	descEnd := findNullTerminator(data[pos:], encoding)
	description := ""
	if descEnd >= 0 {
		description = decodeText(data[pos:pos+descEnd], encoding)
		pos += descEnd + terminatorSize(encoding)
	}
	// If no null terminator was found, treat remaining bytes as image data
	// (some encoders don't properly null-terminate the description).

	if pos >= len(data) {
		return types.Artwork{}, errAPICNoImageData
	}

	// Remaining bytes are picture data
	imageData := data[pos:]

	// Detect actual MIME type from image magic bytes
	if detectedMIME := imagemeta.MIMEType(imageData); detectedMIME != "" {
		mimeType = detectedMIME
	}

	// Detect dimensions
	width, height := imagemeta.Dimensions(imageData, mimeType)

	return types.Artwork{
		MIMEType:    mimeType,
		Description: description,
		Data:        imageData,
		Type:        types.ArtworkType(pictureType),
		Width:       width,
		Height:      height,
	}, nil
}

// parsePICFrame parses an ID3v2.2 PIC frame:
// [encoding][image format(3), e.g. "JPG"][picture type][description\0][data].
func parsePICFrame(data []byte) (types.Artwork, error) {
	if len(data) < 6 {
		return types.Artwork{}, errAPICTooShort
	}
	mimeType := imagemeta.MIMEJPEG // default; magic-byte sniffing in parsePictureBody overrides it
	if strings.EqualFold(string(data[1:4]), "PNG") {
		mimeType = imagemeta.MIMEPNG
	}
	return parsePictureBody(data, 4, data[0], mimeType)
}
