package types

import (
	ebinary "encoding/binary"
	"errors"
	"io"

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
	if sr.ReadAt(h[:], 0, "ID3v2 header") != nil {
		return 0
	}
	return id3v2Size(h[:])
}

// id3v2Size is [LeadingID3v2Size] for the first bytes of a file: 0 unless head
// starts with a complete ID3v2 header.
func id3v2Size(head []byte) int64 {
	if len(head) < 10 || string(head[:3]) != "ID3" {
		return 0
	}
	n := 10 + (int64(head[6]&0x7F)<<21 | int64(head[7]&0x7F)<<14 | int64(head[8]&0x7F)<<7 | int64(head[9]&0x7F))
	if head[3] == 4 && head[5]&0x10 != 0 {
		n += 10
	}
	return n
}

// hasMagicAt reports whether magic sits at off in the file r reads from. head
// holds the file's first bytes and answers without a read when it covers the
// magic; otherwise exactly len(magic) bytes are read.
func hasMagicAt(r io.ReaderAt, size int64, head []byte, off int64, magic string) bool {
	end := off + int64(len(magic))
	if off <= 0 || end > size {
		return false
	}
	if end <= int64(len(head)) {
		return string(head[off:end]) == magic
	}
	b := make([]byte, len(magic))
	n, err := r.ReadAt(b, off)
	return n == len(b) && (err == nil || errors.Is(err, io.EOF)) && string(b) == magic
}

// DetectFormat determines the audio file format by examining magic bytes.
//
// Supported formats: FLAC, MP3, M4A, M4B, Ogg Vorbis, Opus, WAV (RIFF, RIFX and
// RF64), AIFF (AIFF and AIFF-C)
//
// Detection is based on file signatures (magic bytes) at the beginning of the
// file. An ID3v2 tag followed by "fLaC" is a FLAC file.
// Format detection does not validate the entire file structure.
func DetectFormat(r io.ReaderAt, size int64, path string) (Format, error) {
	// File must be at least 4 bytes for any meaningful detection
	if size < 4 {
		return FormatUnknown, &UnsupportedFormatError{
			Path:   path,
			Reason: "file too small",
		}
	}

	// One read serves every check: the first bytes hold all the magic numbers
	// and the whole ftyp atom (at most maxFtypSize bytes).
	head := make([]byte, min(size, maxFtypSize))
	if n, err := r.ReadAt(head, 0); n < len(head) || (err != nil && !errors.Is(err, io.EOF)) {
		return FormatUnknown, errHeaderUnreadable(path)
	}

	// The families have distinct leading bytes, so the order only matters
	// between FLAC and the ID3v2 prefix, which detectID3 resolves itself.
	if f, ok := detectFLAC(head); ok {
		return f, nil
	}
	if f, ok := detectID3(r, size, head); ok {
		return f, nil
	}
	if f, ok := detectMPEGSync(head); ok {
		return f, nil
	}
	if f, ok := detectOgg(head, size); ok {
		return f, nil
	}
	if f, ok := detectRIFF(head, size); ok {
		return f, nil
	}
	if f, ok := detectFORM(head, size); ok {
		return f, nil
	}
	return detectMP4(head, path)
}

// detectFLAC reports a native FLAC stream ("fLaC" at offset 0). head holds at
// least 4 bytes.
func detectFLAC(head []byte) (Format, bool) {
	if string(head[:4]) == "fLaC" {
		return FormatFLAC, true
	}
	return FormatUnknown, false
}

// detectID3 classifies a file that starts with an ID3v2 tag: an MP3 file or,
// against the FLAC spec but written by some taggers, a FLAC stream whose "fLaC"
// follows the tag. The look past the tag costs a read only when the tag
// reaches beyond head.
func detectID3(r io.ReaderAt, size int64, head []byte) (Format, bool) {
	if string(head[:3]) != "ID3" {
		return FormatUnknown, false
	}
	if hasMagicAt(r, size, head, id3v2Size(head), "fLaC") {
		return FormatFLAC, true
	}
	return FormatMP3, true
}

// detectMPEGSync reports an MP3 file without an ID3 tag, recognized by the
// 11-bit frame sync (0xFFE or 0xFFF).
func detectMPEGSync(head []byte) (Format, bool) {
	if head[0] == 0xFF && head[1]&0xE0 == 0xE0 {
		return FormatMP3, true
	}
	return FormatUnknown, false
}

// detectOgg classifies an Ogg file ("OggS") as Opus or Ogg Vorbis. The first
// packet starts after the 27-byte page header and the segment table (at most
// 255 bytes), so it lies inside head; "OpusHead" there selects Opus.
func detectOgg(head []byte, size int64) (Format, bool) {
	if string(head[:4]) != "OggS" {
		return FormatUnknown, false
	}
	// Minimum needed: 27 (header) + 1 (segment table) + 8 (OpusHead) = 36 bytes
	if size >= 36 {
		packetOffset := 27 + int(head[26])
		if packetOffset+8 <= len(head) && string(head[packetOffset:packetOffset+8]) == "OpusHead" {
			return FormatOpus, true
		}
	}
	return FormatOgg, true
}

// detectRIFF reports a WAV file: RIFF, its big-endian twin RIFX or RF64 with
// the "WAVE" form type.
func detectRIFF(head []byte, size int64) (Format, bool) {
	switch string(head[:4]) {
	case "RIFF", "RIFX", "RF64":
	default:
		return FormatUnknown, false
	}
	if size >= 12 && string(head[8:12]) == "WAVE" {
		return FormatWAV, true
	}
	return FormatUnknown, false
}

// detectFORM reports an AIFF file: FORM with the "AIFF" or "AIFC" form type.
func detectFORM(head []byte, size int64) (Format, bool) {
	if string(head[:4]) != "FORM" || size < 12 {
		return FormatUnknown, false
	}
	if tag := string(head[8:12]); tag == "AIFF" || tag == "AIFC" {
		return FormatAIFF, true
	}
	return FormatUnknown, false
}

// detectMP4 classifies an ISO-BMFF file by its ftyp atom (size, then type),
// the last family tried: anything else is an unsupported format.
func detectMP4(head []byte, path string) (Format, error) {
	if len(head) < 8 {
		return FormatUnknown, errHeaderUnreadable(path)
	}
	atomSize := ebinary.BigEndian.Uint32(head)
	if string(head[4:8]) != "ftyp" {
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

	return detectMP4Brand(head, int64(atomSize), path)
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
// FormatM4B; otherwise any brand in mp4AudioBrands selects FormatM4A. head
// holds the file's first bytes: min(file size, maxFtypSize) of them.
func detectMP4Brand(head []byte, ftypSize int64, path string) (Format, error) {
	// ftyp payload: major brand(4), minor version(4), compatible brands(4 each).
	end := min(ftypSize, int64(len(head)))
	if end < 12 {
		return FormatUnknown, &UnsupportedFormatError{Path: path, Reason: "failed to read major brand"}
	}
	buf := head[8:end]

	// The major brand is buf[0:4]; the minor version buf[4:8] is skipped.
	brands := func(yield func(brand []byte) bool) {
		if !yield(buf[0:4]) {
			return
		}
		for i := 8; i+4 <= len(buf); i += 4 {
			if !yield(buf[i : i+4]) {
				return
			}
		}
	}
	var audio bool
	for brand := range brands {
		if string(brand) == "M4B " {
			return FormatM4B, nil
		}
		audio = audio || mp4AudioBrands[string(brand)]
	}
	if audio {
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
