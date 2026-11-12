// Package imagemeta sniffs image MIME types, pixel dimensions and color depth from raw bytes.
package imagemeta

import "encoding/binary"

// MIME types the dimension and color depth sniffers understand.
const (
	MIMEJPEG = "image/jpeg"
	MIMEPNG  = "image/png"
	MIMEGIF  = "image/gif"
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
		return MIMEGIF
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
		f, _ := jpegFrame(data)
		return f.width, f.height
	case MIMEPNG:
		return pngDimensions(data)
	case MIMEGIF:
		return gifDimensions(data)
	default:
		return 0, 0
	}
}

// ColorDepth reports an image's color depth in bits per pixel and, for a palette image, the
// number of palette colors (0 otherwise). It reads PNG, JPEG and GIF headers. It returns 0, 0
// for unsupported types, malformed data, and palette images whose palette cannot be read, so
// a nonzero depth always comes with a usable color count.
func ColorDepth(data []byte, mimeType string) (depth, colors int) {
	switch mimeType {
	case MIMEJPEG:
		f, ok := jpegFrame(data)
		if !ok {
			return 0, 0
		}
		return f.precision * f.components, 0
	case MIMEPNG:
		return pngColorDepth(data)
	case MIMEGIF:
		return gifColorDepth(data)
	default:
		return 0, 0
	}
}

// jpegFrameInfo holds the fields of a JPEG start-of-frame header.
type jpegFrameInfo struct {
	width, height         int
	precision, components int
}

// jpegFrame reads the first SOF0, SOF1 or SOF2 marker. The bool is false when none is found.
func jpegFrame(data []byte) (jpegFrameInfo, bool) {
	// JPEG structure: markers are 0xFF followed by marker type
	// SOF markers contain dimensions: SOF0 (0xC0), SOF1 (0xC1), SOF2 (0xC2)
	// SOF format: FF Cn [2 length] [1 precision] [2 height] [2 width] [1 components]
	for i := range len(data) - 9 {
		if data[i] != 0xFF {
			continue
		}
		if marker := data[i+1]; marker == 0xC0 || marker == 0xC1 || marker == 0xC2 {
			return jpegFrameInfo{
				precision:  int(data[i+4]),
				height:     int(binary.BigEndian.Uint16(data[i+5:])),
				width:      int(binary.BigEndian.Uint16(data[i+7:])),
				components: int(data[i+9]),
			}, true
		}
	}
	return jpegFrameInfo{}, false
}

// PNG layout: an 8-byte signature, then chunks of [4 length] [4 type] [data] [4 CRC]. IHDR
// comes first: [4 width] [4 height] [1 bit depth] [1 color type] ...
const (
	pngSigLen     = 8
	pngIHDRData   = pngSigLen + 8 // start of the IHDR chunk's data
	pngIHDRLen    = 13
	pngChunkHead  = 8 // length and type
	pngChunkCRC   = 4
	pngColorGray  = 0
	pngColorRGB   = 2
	pngColorIndex = 3
	pngColorGrayA = 4
	pngColorRGBA  = 6
	pngPaletteRGB = 3 // bytes per palette entry
)

var pngSig = [pngSigLen]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

// pngIHDR returns the PNG's IHDR data, or nil when data is not a PNG with a whole IHDR.
func pngIHDR(data []byte) []byte {
	if len(data) < pngIHDRData+pngIHDRLen || [pngSigLen]byte(data[:pngSigLen]) != pngSig {
		return nil
	}
	return data[pngIHDRData : pngIHDRData+pngIHDRLen]
}

// pngDimensions extracts dimensions from PNG data.
func pngDimensions(data []byte) (int, int) {
	// The dimensions end 8 bytes into IHDR's data, so an IHDR cut short after them still counts.
	if len(data) < pngIHDRData+8 || [pngSigLen]byte(data[:pngSigLen]) != pngSig {
		return 0, 0
	}
	return int(binary.BigEndian.Uint32(data[pngIHDRData:])), int(binary.BigEndian.Uint32(data[pngIHDRData+4:]))
}

// pngColorDepth reads the bits per pixel from IHDR (bit depth times channels) and, for an
// indexed image, the palette size from the PLTE chunk.
func pngColorDepth(data []byte) (depth, colors int) {
	ihdr := pngIHDR(data)
	if ihdr == nil {
		return 0, 0
	}
	bits := int(ihdr[8])
	switch ihdr[9] {
	case pngColorGray:
		return bits, 0
	case pngColorRGB:
		return 3 * bits, 0
	case pngColorGrayA:
		return 2 * bits, 0
	case pngColorRGBA:
		return 4 * bits, 0
	case pngColorIndex:
		if n := pngPaletteColors(data[pngIHDRData+pngIHDRLen+pngChunkCRC:]); n > 0 {
			return bits, n
		}
	}
	return 0, 0
}

// pngPaletteColors walks the chunks in rest (those after IHDR) up to PLTE, returning its
// number of entries, or 0 when there is none before the image data.
func pngPaletteColors(rest []byte) int {
	for len(rest) >= pngChunkHead {
		n := uint64(binary.BigEndian.Uint32(rest))
		switch string(rest[4:pngChunkHead]) {
		case "PLTE":
			return int(min(n, uint64(len(rest))-pngChunkHead)) / pngPaletteRGB //nolint:gosec // G115: the minimum is bounded by len(rest)
		case "IDAT", "IEND":
			return 0
		}
		next := pngChunkHead + n + pngChunkCRC
		if next > uint64(len(rest)) {
			return 0
		}
		rest = rest[next:]
	}
	return 0
}

// GIF layout: a 6-byte header, then the logical screen descriptor: [2 width] [2 height]
// [1 packed] [1 background] [1 aspect]. The packed byte's top bit says a global color table
// follows and its low three bits N give the table 2^(N+1) entries.
const (
	gifHeaderLen = 6
	gifScreenLen = 7
	gifTableFlag = 0x80
	gifSizeMask  = 0x07
)

// gifDimensions extracts the logical screen size from GIF data.
func gifDimensions(data []byte) (int, int) {
	if len(data) < gifHeaderLen+4 {
		return 0, 0
	}
	screen := data[gifHeaderLen:]
	return int(binary.LittleEndian.Uint16(screen)), int(binary.LittleEndian.Uint16(screen[2:]))
}

// gifColorDepth derives the depth and palette size from the global color table's size. A GIF
// without a global color table has no palette to read here, so it reports 0, 0.
func gifColorDepth(data []byte) (depth, colors int) {
	if len(data) < gifHeaderLen+gifScreenLen {
		return 0, 0
	}
	packed := data[gifHeaderLen+4]
	if packed&gifTableFlag == 0 {
		return 0, 0
	}
	depth = int(packed&gifSizeMask) + 1
	return depth, 1 << depth
}
