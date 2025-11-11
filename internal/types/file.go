package types

import (
	"io"
)

// File represents an opened audio file with parsed metadata.
//
// File provides access to format-agnostic metadata (Tags), technical
// audio properties (AudioInfo), and optional embedded artwork.
//
// File uses lazy loading - opening a file reads only metadata, not
// audio content or artwork. Call ExtractArtwork() to load images.
//
// Always call Close() when done to release file resources:
//
//	file, err := audiometa.Open("song.flac")
//	if err != nil {
//		return err
//	}
//	defer file.Close()
type File struct {
	// Path to the audio file
	Path string

	// Detected format (FLAC, MP3, M4A, M4B, etc.)
	Format Format

	// File size in bytes
	Size int64

	// Parsed metadata (format-agnostic)
	Tags Tags

	// Audio technical properties
	Audio AudioInfo

	// Chapters (for audiobooks, CD tracks, etc.)
	Chapters []Chapter

	// Warnings encountered during parsing (non-fatal issues)
	Warnings []Warning

	// Internal state (exported for access from main package, but not for external use)
	// These fields are implementation details and should not be accessed directly by users
	Reader_  io.ReaderAt         // File handle or other reader (internal use only)
	Parser_  interface{}         // Format-specific parser (internal use only)
	Artwork_ []Artwork           // Cached artwork (internal use only)
	RawTags_ map[string][]RawTag // Format-specific raw tags (internal use only)
}
