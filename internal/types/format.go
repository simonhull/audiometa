package types

import (
	"io"
	"slices"

	"github.com/simonhull/audiometa/internal/binary"
)

// Format represents the detected audio format.
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

// LeadingID3v2Size returns the on-disk size of the ID3v2 tag at the start of
// sr: its 10-byte header, its body and, for ID3v2.4 with the footer flag, a
// 10-byte footer. It returns 0 when sr does not start with an ID3v2 header.
func LeadingID3v2Size(sr *binary.SafeReader) int64 {
	var h [10]byte
	if sr.ReadAt(h[:], 0, "ID3v2 header") != nil || string(h[:3]) != "ID3" {
		return 0
	}
	n := 10 + (int64(h[6]&0x7F)<<21 | int64(h[7]&0x7F)<<14 | int64(h[8]&0x7F)<<7 | int64(h[9]&0x7F))
	if h[3] == 4 && h[5]&0x10 != 0 {
		n += 10
	}
	return n
}

// hasMagicAt reports whether magic sits at off in sr.
func hasMagicAt(sr *binary.SafeReader, off int64, magic string) bool {
	b := make([]byte, len(magic))
	return off > 0 && sr.ReadAt(b, off, "magic bytes") == nil && string(b) == magic
}

// DetectFormat determines the audio file format by examining magic bytes.
//
// Supported formats: FLAC, MP3, M4A, M4B, Ogg Vorbis, Opus, WAV (RIFF, RIFX and
// RF64), AIFF (AIFF and AIFF-C)
//
// Detection is based on file signatures (magic bytes) at the beginning of the
// file. An ID3v2 tag followed by "fLaC" is a FLAC file.
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
		return FormatUnknown, errHeaderUnreadable(path)
	}

	// Check for FLAC (fLaC = 0x664C6143)
	if string(magic) == "fLaC" {
		return FormatFLAC, nil
	}

	// An ID3v2 tag starts an MP3 file, or, against the FLAC spec but written
	// by some taggers, sits in front of a FLAC stream.
	if string(magic[:3]) == "ID3" {
		if hasMagicAt(sr, LeadingID3v2Size(sr), "fLaC") {
			return FormatFLAC, nil
		}
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

	// Check for WAV (RIFF....WAVE), its big-endian twin RIFX and RF64.
	if (string(magic) == "RIFF" || string(magic) == "RIFX" || string(magic) == "RF64") && size >= 12 {
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
	atomSize, err := sr.Read[uint32](0, "ftyp atom size")
	if err != nil {
		return FormatUnknown, errHeaderUnreadable(path)
	}

	// Read ftyp atom type (next 4 bytes)
	atomType, err := sr.Read[uint32](4, "ftyp atom type")
	if err != nil {
		return FormatUnknown, errHeaderUnreadable(path)
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

	return detectMP4Brand(sr, int64(atomSize), size, path)
}

// maxFtypSize bounds how much of the ftyp atom is read for brand detection.
// Real ftyp atoms list a handful of brands; 1 KiB is generous.
const maxFtypSize = 1024

// mp4AudioBrands are the ISO-BMFF brands (major or compatible) accepted as
// M4A. "M4B " is handled separately because it selects FormatM4B.
var mp4AudioBrands = map[string]bool{
	"M4A ": true, // iTunes audio
	"M4P ": true, // iTunes protected (FairPlay) audio
	"mp41": true, // MP4 v1
	"mp42": true, // MP4 v2
	"isom": true, // ISO base media
	"iso2": true, // ISO base media, 2nd edition
}

// detectMP4Brand classifies an ftyp atom (starting at offset 0, ftypSize
// bytes long) by its major and compatible brands: any "M4B " brand selects
// FormatM4B; otherwise any brand in mp4AudioBrands selects FormatM4A.
func detectMP4Brand(sr *binary.SafeReader, ftypSize, fileSize int64, path string) (Format, error) {
	// ftyp payload: major brand(4), minor version(4), compatible brands(4 each).
	end := min(ftypSize, fileSize, maxFtypSize)
	if end < 12 {
		return FormatUnknown, &UnsupportedFormatError{Path: path, Reason: "failed to read major brand"}
	}
	buf := make([]byte, end-8)
	if err := sr.ReadAt(buf, 8, "ftyp brands"); err != nil {
		return FormatUnknown, &UnsupportedFormatError{Path: path, Reason: "failed to read major brand"}
	}

	brands := []string{string(buf[0:4])}
	for i := 8; i+4 <= len(buf); i += 4 {
		brands = append(brands, string(buf[i:i+4]))
	}

	if slices.Contains(brands, "M4B ") {
		return FormatM4B, nil
	}
	if slices.ContainsFunc(brands, func(b string) bool { return mp4AudioBrands[b] }) {
		return FormatM4A, nil
	}

	return FormatUnknown, &UnsupportedFormatError{
		Path:   path,
		Reason: "unsupported file brand",
	}
}

// errHeaderUnreadable reports that the bytes needed for format detection could not be read.
func errHeaderUnreadable(path string) *UnsupportedFormatError {
	return &UnsupportedFormatError{Path: path, Reason: "failed to read file header"}
}
