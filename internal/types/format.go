package types

import (
	"io"

	"github.com/simonhull/audiometa/internal/binary"
)

// Format represents the detected audio format
//
//go:generate stringer -type=Format -linecomment
type Format int

const (
	// FormatUnknown represents an unknown or unsupported format.
	FormatUnknown Format = iota // Unknown
	// FormatFLAC represents FLAC audio files.
	FormatFLAC // FLAC
	// FormatMP3 represents MP3 audio files.
	FormatMP3 // MP3
	// FormatM4A represents M4A audio files.
	FormatM4A // M4A
	// FormatM4B represents M4B audiobook files.
	FormatM4B // M4B
	// FormatOgg represents Ogg Vorbis audio files.
	FormatOgg // Ogg Vorbis
	// FormatOpus represents Opus audio files.
	FormatOpus // Opus
	// FormatWAV represents WAV audio files.
	FormatWAV // WAV
	// FormatAIFF represents AIFF audio files.
	FormatAIFF // AIFF
)

// Extensions returns common file extensions for this format.
func (f Format) Extensions() []string {
	switch f {
	case FormatFLAC:
		return []string{".flac"}
	case FormatMP3:
		return []string{".mp3"}
	case FormatM4A:
		return []string{".m4a", ".mp4", ".m4p"}
	case FormatM4B:
		return []string{".m4b"}
	case FormatOgg:
		return []string{".ogg", ".oga"}
	case FormatOpus:
		return []string{".opus"}
	case FormatWAV:
		return []string{".wav"}
	case FormatAIFF:
		return []string{".aiff", ".aif"}
	case FormatUnknown:
		return nil
	default:
		return nil
	}
}

// DetectFormat determines the audio file format by examining magic bytes.
//
// Supported formats: FLAC, MP3, M4A, M4B, Ogg Vorbis, Opus, WAV, AIFF
//
// Detection is based on file signatures (magic bytes) at the beginning of the file.
// Format detection does not validate the entire file structure.
func DetectFormat(r io.ReaderAt, size int64, path string) (Format, error) { //nolint:gocyclo // Format detection requires checking multiple magic byte patterns
	// File must be at least 4 bytes for any meaningful detection
	if size < 4 {
		return FormatUnknown, &UnsupportedFormatError{
			Path:   path,
			Reason: "file too small",
		}
	}

	sr := binary.NewSafeReader(r, size, path)

	// Read first 4 bytes for magic number detection
	magic := make([]byte, 4)
	if err := sr.ReadAt(magic, 0, "file magic bytes"); err != nil {
		return FormatUnknown, &UnsupportedFormatError{
			Path:   path,
			Reason: "failed to read file header",
		}
	}

	// Check for FLAC (fLaC = 0x664C6143)
	if string(magic) == "fLaC" {
		return FormatFLAC, nil
	}

	// Check for ID3v2 tag (MP3)
	if string(magic[:3]) == "ID3" {
		return FormatMP3, nil
	}

	// Check for MP3 frame sync (0xFFE or 0xFFF)
	// This catches MP3 files without ID3 tags
	if magic[0] == 0xFF && (magic[1]&0xE0) == 0xE0 {
		return FormatMP3, nil
	}

	// Check for Ogg (OggS) - could be Vorbis or Opus
	if string(magic) == "OggS" { //nolint:nestif // Nested structure is clearer than extracting to separate function
		// Need to read into first Ogg page to find codec magic.
		// Ogg page header: 27 bytes fixed + segment table (variable).
		// Minimum needed: 27 (header) + 1 (segment table) + 8 (OpusHead) = 36 bytes
		if size >= 36 {
			// Read segment count at offset 26
			segCount := make([]byte, 1)
			if err := sr.ReadAt(segCount, 26, "segment count"); err == nil {
				// First packet starts after: 27 (header) + segment_count (segment table)
				packetOffset := int64(27 + int(segCount[0]))
				if packetOffset+8 <= size {
					codecMagic := make([]byte, 8)
					if err := sr.ReadAt(codecMagic, packetOffset, "codec magic"); err == nil {
						if string(codecMagic) == "OpusHead" {
							return FormatOpus, nil
						}
					}
				}
			}
		}
		return FormatOgg, nil
	}

	// Check for RIFF/WAV (RIFF....WAVE)
	if string(magic) == "RIFF" && size >= 12 {
		waveTag := make([]byte, 4)
		if err := sr.ReadAt(waveTag, 8, "WAVE tag"); err == nil {
			if string(waveTag) == "WAVE" {
				return FormatWAV, nil
			}
		}
	}

	// Check for AIFF (FORM....AIFF)
	if string(magic) == "FORM" && size >= 12 {
		aiffTag := make([]byte, 4)
		if err := sr.ReadAt(aiffTag, 8, "AIFF tag"); err == nil {
			if string(aiffTag) == "AIFF" || string(aiffTag) == "AIFC" {
				return FormatAIFF, nil
			}
		}
	}

	// Check for M4B/M4A ftyp atom
	// Read ftyp atom size (first 4 bytes)
	atomSize, err := binary.Read[uint32](sr, 0, "ftyp atom size")
	if err != nil {
		return FormatUnknown, &UnsupportedFormatError{
			Path:   path,
			Reason: "failed to read file header",
		}
	}

	// Read ftyp atom type (next 4 bytes)
	atomType, err := binary.Read[uint32](sr, 4, "ftyp atom type")
	if err != nil {
		return FormatUnknown, &UnsupportedFormatError{
			Path:   path,
			Reason: "failed to read file header",
		}
	}

	// Check if it's an ftyp atom (0x66747970 = "ftyp")
	ftypMagic := uint32(0x66747970)
	if atomType != ftypMagic {
		return FormatUnknown, &UnsupportedFormatError{
			Path:   path,
			Reason: "unsupported file format",
		}
	}

	// ftyp atom must be at least 16 bytes (size + type + brand + version)
	if atomSize < 16 {
		return FormatUnknown, &UnsupportedFormatError{
			Path:   path,
			Reason: "ftyp atom too small",
		}
	}

	// Read major brand (next 4 bytes)
	majorBrand, err := binary.Read[uint32](sr, 8, "major brand")
	if err != nil {
		return FormatUnknown, &UnsupportedFormatError{
			Path:   path,
			Reason: "failed to read major brand",
		}
	}

	// Check for M4B brand (0x4D344220 = "M4B ")
	m4bMagic := uint32(0x4D344220)
	if majorBrand == m4bMagic {
		return FormatM4B, nil
	}

	// Check for M4A brands
	// M4A  = 0x4D344120 = "M4A "
	// mp42 = 0x6D703432 = "mp42"
	// isom = 0x69736F6D = "isom"
	m4aMagic := uint32(0x4D344120)
	mp42Magic := uint32(0x6D703432)
	isomMagic := uint32(0x69736F6D)

	if majorBrand == m4aMagic || majorBrand == mp42Magic || majorBrand == isomMagic {
		return FormatM4A, nil
	}

	// Unsupported brand
	return FormatUnknown, &UnsupportedFormatError{
		Path:   path,
		Reason: "unsupported file brand",
	}
}
