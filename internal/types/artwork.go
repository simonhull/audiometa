package types

import "fmt"

// Artwork represents embedded artwork (cover art, images).
//
// Artwork can include album covers, artist photos, and other images
// embedded in audio files. Multiple artworks per file are supported.
type Artwork struct {
	MIMEType    string
	Description string
	Data        []byte
	Type        ArtworkType
	Width       int
	Height      int
}

// ArtworkType categorizes the purpose/content of artwork.
//
// Types are based on ID3v2 APIC frame picture types and FLAC picture types.
// See: https://id3.org/id3v2.4.0-frames (APIC frame)
//
//go:generate stringer -type=ArtworkType -linecomment
type ArtworkType int

const (
	// ArtworkOther represents other/unspecified artwork.
	ArtworkOther ArtworkType = iota // Other
	// ArtworkIcon represents a file icon (32x32 PNG).
	ArtworkIcon // File icon (32x32 PNG)
	// ArtworkOtherIcon represents another file icon.
	ArtworkOtherIcon // Other file icon
	// ArtworkFrontCover represents front cover artwork.
	ArtworkFrontCover // Front cover
	// ArtworkBackCover represents back cover artwork.
	ArtworkBackCover // Back cover
	// ArtworkLeaflet represents leaflet page artwork.
	ArtworkLeaflet // Leaflet page
	// ArtworkMedia represents media artwork (CD/vinyl label).
	ArtworkMedia // Media (CD/vinyl label)
	// ArtworkLeadArtist represents lead artist/performer/soloist artwork.
	ArtworkLeadArtist // Lead artist/performer/soloist
	// ArtworkArtist represents artist/performer artwork.
	ArtworkArtist // Artist/performer
	// ArtworkConductor represents conductor artwork.
	ArtworkConductor // Conductor
	// ArtworkBand represents band/orchestra artwork.
	ArtworkBand // Band/orchestra
	// ArtworkComposer represents composer artwork.
	ArtworkComposer // Composer
	// ArtworkLyricist represents lyricist/text writer artwork.
	ArtworkLyricist // Lyricist/text writer
	// ArtworkRecordingLocation represents recording location artwork.
	ArtworkRecordingLocation // Recording location
	// ArtworkDuringRecording represents during recording artwork.
	ArtworkDuringRecording // During recording
	// ArtworkDuringPerformance represents during performance artwork.
	ArtworkDuringPerformance // During performance
	// ArtworkVideoCapture represents movie/video screen capture artwork.
	ArtworkVideoCapture // Movie/video screen capture
	// ArtworkBrightFish represents a bright colored fish artwork.
	ArtworkBrightFish // A bright colored fish
	// ArtworkIllustration represents illustration artwork.
	ArtworkIllustration // Illustration
	// ArtworkBandLogotype represents band/artist logotype artwork.
	ArtworkBandLogotype // Band/artist logotype
	// ArtworkPublisherLogotype represents publisher/studio logotype artwork.
	ArtworkPublisherLogotype // Publisher/studio logotype
)

// String returns a human-readable representation of the artwork.
// Example output: "Front cover (1200x1200 JPEG, 245KB)".
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
	Key      string
	Encoding string
	Value    []byte
	Type     RawTagType
}

// RawTagType indicates the semantic type of a raw tag value.
type RawTagType int

const (
	// RawTagText represents a text string value.
	RawTagText RawTagType = iota // Text string
	// RawTagBinary represents binary data.
	RawTagBinary // Binary data
	// RawTagImage represents image data.
	RawTagImage // Image data
	// RawTagCounter represents a numeric counter.
	RawTagCounter // Numeric counter
	// RawTagURL represents a URL string.
	RawTagURL // URL string
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
