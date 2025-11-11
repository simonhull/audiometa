package types

import "fmt"

// Artwork represents embedded artwork (cover art, images).
//
// Artwork can include album covers, artist photos, and other images
// embedded in audio files. Multiple artworks per file are supported.
type Artwork struct {
	// Type of artwork (front cover, back cover, artist photo, etc.)
	Type ArtworkType

	// MIME type of the image data
	MIMEType string // "image/jpeg", "image/png", "image/gif"

	// Description of the artwork (optional)
	Description string

	// Image binary data
	Data []byte

	// Dimensions (if available in metadata, otherwise 0)
	Width  int // Pixels
	Height int // Pixels
}

// ArtworkType categorizes the purpose/content of artwork.
//
// Types are based on ID3v2 APIC frame picture types and FLAC picture types.
// See: https://id3.org/id3v2.4.0-frames (APIC frame)
//
//go:generate stringer -type=ArtworkType -linecomment
type ArtworkType int

const (
	ArtworkOther ArtworkType = iota // Other
	ArtworkIcon                      // File icon (32x32 PNG)
	ArtworkOtherIcon                 // Other file icon
	ArtworkFrontCover                // Front cover
	ArtworkBackCover                 // Back cover
	ArtworkLeaflet                   // Leaflet page
	ArtworkMedia                     // Media (CD/vinyl label)
	ArtworkLeadArtist                // Lead artist/performer/soloist
	ArtworkArtist                    // Artist/performer
	ArtworkConductor                 // Conductor
	ArtworkBand                      // Band/orchestra
	ArtworkComposer                  // Composer
	ArtworkLyricist                  // Lyricist/text writer
	ArtworkRecordingLocation         // Recording location
	ArtworkDuringRecording           // During recording
	ArtworkDuringPerformance         // During performance
	ArtworkVideoCapture              // Movie/video screen capture
	ArtworkBrightFish                // A bright colored fish
	ArtworkIllustration              // Illustration
	ArtworkBandLogotype              // Band/artist logotype
	ArtworkPublisherLogotype         // Publisher/studio logotype
)

// String returns a human-readable description of the artwork.
//
// Example output: "Front cover (1200x1200 JPEG, 245KB)"
func (a Artwork) String() string {
	size := len(a.Data)
	sizeStr := formatSize(size)

	// Format dimensions
	dims := ""
	if a.Width > 0 && a.Height > 0 {
		dims = fmt.Sprintf("%dx%d ", a.Width, a.Height)
	}

	// Format MIME type
	format := mimeToFormat(a.MIMEType)

	return fmt.Sprintf("%s (%s%s, %s)", a.Type, dims, format, sizeStr)
}

// formatSize formats byte size in human-readable form.
func formatSize(bytes int) string {
	const (
		KB = 1024
		MB = 1024 * KB
	)

	switch {
	case bytes >= MB:
		return fmt.Sprintf("%.1fMB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%dKB", bytes/KB)
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}

// mimeToFormat converts MIME type to short format name.
func mimeToFormat(mime string) string {
	switch mime {
	case "image/jpeg":
		return "JPEG"
	case "image/png":
		return "PNG"
	case "image/gif":
		return "GIF"
	case "image/bmp":
		return "BMP"
	case "image/tiff":
		return "TIFF"
	case "image/webp":
		return "WebP"
	default:
		return "Image"
	}
}

// RawTag represents an unparsed tag value.
//
// RawTag preserves the original binary representation and encoding
// information for tags that are not mapped to standard Tag fields.
type RawTag struct {
	Key      string     // Tag key (format-specific)
	Value    []byte     // Raw binary value
	Encoding string     // Text encoding: "UTF-8", "UTF-16LE", "ISO-8859-1", etc.
	Type     RawTagType // Value type hint
}

// RawTagType indicates the semantic type of a raw tag value.
type RawTagType int

const (
	RawTagText    RawTagType = iota // Text string
	RawTagBinary                    // Binary data
	RawTagImage                     // Image data
	RawTagCounter                   // Numeric counter
	RawTagURL                       // URL string
)

// String returns the raw tag value as a string.
//
// For non-text types, returns a placeholder description.
func (r RawTag) String() string {
	switch r.Type {
	case RawTagText, RawTagURL:
		return string(r.Value)
	case RawTagImage:
		return fmt.Sprintf("<image: %d bytes>", len(r.Value))
	case RawTagBinary:
		return fmt.Sprintf("<binary: %d bytes>", len(r.Value))
	case RawTagCounter:
		return fmt.Sprintf("<counter: %d bytes>", len(r.Value))
	default:
		return fmt.Sprintf("<%d bytes>", len(r.Value))
	}
}
