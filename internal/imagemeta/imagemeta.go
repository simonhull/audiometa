// Package imagemeta sniffs image MIME types and pixel dimensions from raw bytes.
package imagemeta

// MIME types the dimension sniffer understands.
const (
	MIMEJPEG = "image/jpeg"
	MIMEPNG  = "image/png"
)

// MIMEType detects the image MIME type from magic bytes, or returns "" when unrecognized.
//
//nolint:gocyclo // Flat dispatch on image magic bytes; complexity = number of supported formats.
func MIMEType(data []byte) string {
	switch {
	case len(data) < 4:
		return ""
	case data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return MIMEJPEG
	case data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47:
		return MIMEPNG
	case data[0] == 0x47 && data[1] == 0x49 && data[2] == 0x46:
		return "image/gif"
	case data[0] == 0x42 && data[1] == 0x4D:
		return "image/bmp"
	case len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp"
	default:
		return ""
	}
}

// Dimensions extracts width and height from image data of the given MIME type.
// It returns 0, 0 for unsupported types or malformed data.
func Dimensions(data []byte, mimeType string) (int, int) {
	switch mimeType {
	case MIMEJPEG:
		return jpegDimensions(data)
	case MIMEPNG:
		return pngDimensions(data)
	default:
		return 0, 0
	}
}

// jpegDimensions extracts dimensions from JPEG data.
func jpegDimensions(data []byte) (int, int) {
	// JPEG structure: markers are 0xFF followed by marker type
	// SOF markers contain dimensions: SOF0 (0xC0), SOF1 (0xC1), SOF2 (0xC2)
	for i := range len(data) - 9 {
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

// pngDimensions extracts dimensions from PNG data.
func pngDimensions(data []byte) (int, int) {
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
