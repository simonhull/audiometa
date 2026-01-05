package mp3

import (
	"bytes"
	"errors"
	"io"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

var (
	errAPICTooShort    = errors.New("APIC frame too short")
	errAPICNoMIMETerm  = errors.New("APIC MIME type not null-terminated")
	errAPICTruncated   = errors.New("APIC frame truncated after MIME type")
	errAPICNoImageData = errors.New("APIC frame has no image data")
)

// extractArtwork extracts embedded artwork from MP3 files.
// Parses ID3v2 APIC (Attached Picture) frames.
func extractArtwork(r io.ReaderAt, size int64, path string) ([]types.Artwork, error) {
	sr := binutil.NewSafeReader(r, size, path)

	// Parse ID3v2 header
	header, err := parseID3v2Header(sr)
	if err != nil {
		// No ID3v2 tag = no embedded artwork
		return nil, nil
	}

	frameDataOffset := skipExtendedHeader(sr, header)
	tagEnd := int64(10 + header.Size)
	offset := frameDataOffset

	var artwork []types.Artwork

	// Scan through frames looking for APIC
	for offset < tagEnd {
		frame, bytesRead, stop := readFrameForArtwork(sr, header, offset)
		if stop {
			break
		}

		if frame != nil && frame.ID == "APIC" {
			art, err := parseAPICFrame(frame.Data)
			if err == nil {
				artwork = append(artwork, art)
			}
		}

		offset += bytesRead
	}

	return artwork, nil
}

// readFrameForArtwork reads a single frame header and data.
// Similar to readSingleFrame but doesn't need the file parameter.
func readFrameForArtwork(sr *binutil.SafeReader, header ID3v2Header, offset int64) (*ID3v2Frame, int64, bool) {
	frameHeaderBuf := make([]byte, 10)
	if err := sr.ReadAt(frameHeaderBuf, offset, "frame header"); err != nil {
		return nil, 0, true
	}

	// Check for padding (null bytes indicate end of frames)
	if frameHeaderBuf[0] == 0 {
		return nil, 0, true
	}

	// Parse frame header
	frameID := string(frameHeaderBuf[0:4])
	frameSize := decodeFrameSize(header.Version, frameHeaderBuf[4:8])

	// Sanity check frame size
	if frameSize == 0 || frameSize > 100*1024*1024 { // 100MB max
		return nil, 0, true
	}

	// Only read data for APIC frames to save memory
	if frameID != "APIC" {
		return nil, 10 + int64(frameSize), false
	}

	// Read frame data
	frameData := make([]byte, frameSize)
	if err := sr.ReadAt(frameData, offset+10, "APIC frame data"); err != nil {
		return nil, 10 + int64(frameSize), false
	}

	frame := &ID3v2Frame{
		ID:   frameID,
		Size: frameSize,
		Data: frameData,
	}

	return frame, 10 + int64(frameSize), false
}

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
	if mimeType == "JPG" || mimeType == "jpg" {
		mimeType = "image/jpeg"
	} else if mimeType == "PNG" || mimeType == "png" {
		mimeType = "image/png"
	} else if mimeType == "" || mimeType == "-->" {
		// Empty or URL reference - try to detect from data
		mimeType = "image/jpeg" // Default, will be overridden if PNG detected
	}

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
	} else {
		// No null terminator found - treat rest as image data
		// Some encoders don't properly null-terminate the description
	}

	if pos >= len(data) {
		return types.Artwork{}, errAPICNoImageData
	}

	// Remaining bytes are picture data
	imageData := data[pos:]

	// Detect actual MIME type from image magic bytes
	if detectedMIME := detectMIMEType(imageData); detectedMIME != "" {
		mimeType = detectedMIME
	}

	// Detect dimensions
	width, height := detectImageDimensions(imageData, mimeType)

	return types.Artwork{
		MIMEType:    mimeType,
		Description: description,
		Data:        imageData,
		Type:        types.ArtworkType(pictureType),
		Width:       width,
		Height:      height,
	}, nil
}

// detectMIMEType detects image MIME type from magic bytes.
func detectMIMEType(data []byte) string {
	if len(data) < 4 {
		return ""
	}

	// JPEG: FF D8 FF
	if data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "image/jpeg"
	}

	// PNG: 89 50 4E 47
	if data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
		return "image/png"
	}

	// GIF: 47 49 46
	if data[0] == 0x47 && data[1] == 0x49 && data[2] == 0x46 {
		return "image/gif"
	}

	// BMP: 42 4D
	if data[0] == 0x42 && data[1] == 0x4D {
		return "image/bmp"
	}

	// WebP: RIFF....WEBP
	if len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return "image/webp"
	}

	return ""
}

// detectImageDimensions extracts width/height from image data.
func detectImageDimensions(data []byte, mimeType string) (int, int) {
	switch mimeType {
	case "image/jpeg":
		return detectJPEGDimensions(data)
	case "image/png":
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
