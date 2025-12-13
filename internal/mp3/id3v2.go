// Package mp3 provides MP3 audio file parsing and ID3 tag extraction.
package mp3

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// ID3v2Header represents an ID3v2 tag header.
type ID3v2Header struct {
	Version      byte // Major version (3 or 4)
	Revision     byte // Minor version
	Flags        byte
	Size         uint32 // Tag size (excluding header), synchsafe
	ExtendedSize uint32 // Extended header size if present
}

// ID3v2Frame represents a single ID3v2 frame.
type ID3v2Frame struct {
	ID    string
	Data  []byte
	Size  uint32
	Flags uint16
}

// parseID3v2 parses ID3v2 tags and extracts metadata.
func parseID3v2(sr *binutil.SafeReader, file *types.File) (int64, error) {
	header, err := parseID3v2Header(sr)
	if err != nil {
		return 0, err
	}

	frameDataOffset := skipExtendedHeader(sr, header)
	chapters := parseID3v2Frames(sr, file, header, frameDataOffset)

	// Process chapters
	if len(chapters) > 0 {
		file.Chapters = parseChapterFrames(chapters, file.Audio.Duration)
	}

	// Total tag size including header
	return int64(10 + header.Size), nil
}

// parseID3v2Header reads and validates the ID3v2 header.
func parseID3v2Header(sr *binutil.SafeReader) (ID3v2Header, error) {
	buf := make([]byte, 10)
	if err := sr.ReadAt(buf, 0, "ID3v2 header"); err != nil {
		return ID3v2Header{}, &types.UnsupportedFormatError{
			Path:   sr.Path(),
			Reason: "failed to read ID3v2 header",
		}
	}

	// Verify "ID3" magic bytes
	if string(buf[0:3]) != "ID3" {
		return ID3v2Header{}, &types.UnsupportedFormatError{
			Path:   sr.Path(),
			Reason: "not an ID3v2 file (missing ID3 header)",
		}
	}

	header := ID3v2Header{
		Version: buf[3],
		Flags:   buf[5],
		Size:    decodeSynchsafe(buf[6:10]),
	}

	// Only support ID3v2.3 and ID3v2.4
	if header.Version != 3 && header.Version != 4 {
		return ID3v2Header{}, &types.UnsupportedFormatError{
			Path:   sr.Path(),
			Reason: fmt.Sprintf("unsupported ID3v2 version: 2.%d", header.Version),
		}
	}

	return header, nil
}

// skipExtendedHeader skips the extended header if present and returns frame data offset.
func skipExtendedHeader(sr *binutil.SafeReader, header ID3v2Header) int64 {
	frameDataOffset := int64(10)

	if header.Flags&0x40 == 0 {
		return frameDataOffset // No extended header
	}

	extBuf := make([]byte, 4)
	if err := sr.ReadAt(extBuf, frameDataOffset, "extended header size"); err != nil {
		return frameDataOffset // Failed to read, use default offset
	}

	var extHeaderSize uint32
	if header.Version == 4 {
		extHeaderSize = decodeSynchsafe(extBuf)
		frameDataOffset += int64(extHeaderSize)
	} else if header.Version == 3 {
		extHeaderSize = binary.BigEndian.Uint32(extBuf)
		frameDataOffset += int64(extHeaderSize) + 4
	}

	return frameDataOffset
}

// parseID3v2Frames parses all frames in the ID3v2 tag.
func parseID3v2Frames(sr *binutil.SafeReader, file *types.File, header ID3v2Header, startOffset int64) []ID3v2Frame {
	tagEnd := int64(10 + header.Size)
	offset := startOffset
	chapters := make([]ID3v2Frame, 0)

	for offset < tagEnd {
		frame, bytesRead, stop := readSingleFrame(sr, file, header, offset)
		if stop {
			break
		}

		if frame != nil {
			processFrame(*frame, file, &chapters)
		}

		offset += bytesRead
	}

	return chapters
}

// readSingleFrame reads a single ID3v2 frame.
func readSingleFrame(sr *binutil.SafeReader, file *types.File, header ID3v2Header, offset int64) (*ID3v2Frame, int64, bool) {
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
	frameFlags := binary.BigEndian.Uint16(frameHeaderBuf[8:10])

	// Read frame data
	frameData := make([]byte, frameSize)
	if err := sr.ReadAt(frameData, offset+10, fmt.Sprintf("frame %s data", frameID)); err != nil {
		file.Warnings = append(file.Warnings, types.Warning{
			Stage:   "metadata",
			Message: fmt.Sprintf("failed to read frame %s: %v", frameID, err),
		})
		return nil, 10 + int64(frameSize), false
	}

	frame := &ID3v2Frame{
		ID:    frameID,
		Size:  frameSize,
		Flags: frameFlags,
		Data:  frameData,
	}

	return frame, 10 + int64(frameSize), false
}

// decodeFrameSize decodes frame size based on ID3v2 version.
func decodeFrameSize(version byte, sizeBytes []byte) uint32 {
	if version == 4 {
		return decodeSynchsafe(sizeBytes)
	}
	return binary.BigEndian.Uint32(sizeBytes)
}

// processFrame processes a single frame based on its ID.
func processFrame(frame ID3v2Frame, file *types.File, chapters *[]ID3v2Frame) {
	switch {
	case strings.HasPrefix(frame.ID, "T") && frame.ID != "TXXX":
		parseTextFrame(frame, file)
	case frame.ID == "TXXX":
		parseTXXXFrame(frame, file)
	case frame.ID == "COMM":
		parseCommentFrame(frame, file)
	case frame.ID == "CHAP":
		*chapters = append(*chapters, frame)
	}
}

// ID3v2 uses 7-bit encoding where bit 7 is always 0.
func decodeSynchsafe(b []byte) uint32 {
	if len(b) != 4 {
		return 0
	}
	return uint32(b[0]&0x7F)<<21 |
		uint32(b[1]&0x7F)<<14 |
		uint32(b[2]&0x7F)<<7 |
		uint32(b[3]&0x7F)
}

// parseTextFrame parses standard text frames (TIT2, TPE1, TALB, etc.)
func parseTextFrame(frame ID3v2Frame, file *types.File) { //nolint:gocyclo // Complexity from many simple cases - intentionally kept together
	if len(frame.Data) < 1 {
		return
	}

	encoding := frame.Data[0]
	text := decodeText(frame.Data[1:], encoding)

	switch frame.ID {
	case "TIT2": // Title
		file.Tags.Title = text
	case "TIT3": // Subtitle/Description refinement
		file.Tags.Subtitle = text
	case "TPE1": // Artist
		file.Tags.Artist = text
	case "TALB": // Album
		file.Tags.Album = text
	case "TCON": // Genre
		if text != "" {
			file.Tags.Genres = append(file.Tags.Genres, text)
		}
	case "TYER": // Year (ID3v2.3)
		if year := parseYear(text); year > 0 {
			file.Tags.Year = year
		}
	case "TDRC": // Recording time (ID3v2.4)
		if year := parseYear(text); year > 0 {
			file.Tags.Year = year
		}
	case "TCOM": // Composer (often used for Narrator)
		if text != "" {
			file.Tags.Composers = append(file.Tags.Composers, text)
		}
	case "TRCK": // Track number/total
		file.Tags.TrackNumber, file.Tags.TrackTotal = parseTrackNumber(text)
	case "TPOS": // Disc number/total
		file.Tags.DiscNumber, file.Tags.DiscTotal = parseTrackNumber(text)
	case "TPE2": // Album artist
		file.Tags.AlbumArtist = text
	}
}

// Format: [encoding][description\0][value].
func parseTXXXFrame(frame ID3v2Frame, file *types.File) {
	if len(frame.Data) < 2 {
		return
	}

	encoding := frame.Data[0]
	data := frame.Data[1:]

	// Find null terminator separating description from value
	nullIdx := findNullTerminator(data, encoding)
	if nullIdx < 0 {
		return
	}

	description := decodeText(data[:nullIdx], encoding)
	value := decodeText(data[nullIdx+terminatorSize(encoding):], encoding)

	// Map common extended metadata fields (TXXX custom tags)
	descLower := strings.ToLower(description)
	switch descLower {
	case "narrator":
		file.Tags.Narrator = value
	case "series":
		file.Tags.Series = value
	case "series part", "seriespart", "part", "series-part":
		file.Tags.SeriesPart = value
	case "series position":
		file.Tags.SeriesPart = value
	case "publisher":
		file.Tags.Publisher = value
	case "isbn":
		file.Tags.ISBN = value
	case "asin", "audible_asin":
		file.Tags.ASIN = value
	case "language", "lang":
		file.Tags.Language = value
	case "description":
		if file.Tags.Description == "" {
			file.Tags.Description = value
		}
	case "mvnm", "movement name", "movement":
		if file.Tags.Series == "" {
			file.Tags.Series = value
		}
	case "mvin", "movement number", "movement index":
		if file.Tags.SeriesPart == "" {
			file.Tags.SeriesPart = value
		}
	}
}

// Format: [encoding][language(3)][short description\0][text].
func parseCommentFrame(frame ID3v2Frame, file *types.File) {
	if len(frame.Data) < 4 {
		return
	}

	encoding := frame.Data[0]
	// Skip language (3 bytes)
	data := frame.Data[4:]

	// Find null terminator separating short description from text
	nullIdx := findNullTerminator(data, encoding)
	if nullIdx < 0 {
		// No null terminator - treat all as comment
		file.Tags.Comment = decodeText(data, encoding)
		return
	}

	// Extract comment text (after short description)
	comment := decodeText(data[nullIdx+terminatorSize(encoding):], encoding)
	file.Tags.Comment = comment
}

// parseChapterFrames parses CHAP frames and builds chapter list.
// CHAP frame format:
//
//	[encoding][element_id\0][start_time(4)][end_time(4)][start_offset(4)][end_offset(4)][subframes...]
func parseChapterFrames(frames []ID3v2Frame, _ time.Duration) []types.Chapter {
	type chapterData struct {
		ElementID string
		Title     string
		Index     int
		StartTime uint32
		EndTime   uint32
	}

	chapters := make([]chapterData, 0, len(frames))

	for _, frame := range frames {
		if len(frame.Data) < 20 {
			continue
		}

		// CHAP frames start directly with element ID (no encoding byte at frame level)
		data := frame.Data

		// Parse element ID (null-terminated string)
		// Element ID uses ISO-8859-1 encoding (single-byte)
		nullIdx := bytes.IndexByte(data, 0)
		if nullIdx < 0 {
			continue
		}

		elementID := string(data[:nullIdx])
		data = data[nullIdx+1:]

		if len(data) < 16 {
			continue
		}

		// Parse times (big-endian uint32, milliseconds)
		startTime := binary.BigEndian.Uint32(data[0:4])
		endTime := binary.BigEndian.Uint32(data[4:8])
		// Skip startOffset and endOffset (usually 0xFFFFFFFF) at data[8:16]

		// Parse subframes for chapter title
		title := extractChapterTitleFromSubframes(data[16:], elementID)

		chapters = append(chapters, chapterData{
			Index:     len(chapters),
			ElementID: elementID,
			StartTime: startTime,
			EndTime:   endTime,
			Title:     title,
		})
	}

	// Sort by start time using modern Go 1.21+ slices package
	slices.SortFunc(chapters, func(a, b chapterData) int {
		return cmp.Compare(a.StartTime, b.StartTime)
	})

	// Convert to types.Chapter
	result := make([]types.Chapter, len(chapters))
	for i, ch := range chapters {
		result[i] = types.Chapter{
			Index:     i + 1,
			Title:     ch.Title,
			StartTime: time.Duration(ch.StartTime) * time.Millisecond,
			EndTime:   time.Duration(ch.EndTime) * time.Millisecond,
		}
	}

	return result
}

// extractChapterTitleFromSubframes extracts chapter title from TIT2 subframe.
func extractChapterTitleFromSubframes(subframeData []byte, fallbackTitle string) string {
	if len(subframeData) < 10 {
		return fallbackTitle
	}

	// Try to parse TIT2 subframe
	subframeID := string(subframeData[0:4])
	if subframeID != "TIT2" {
		return fallbackTitle
	}

	// Try synchsafe size (CHAP subframes may use synchsafe)
	subframeSize := decodeSynchsafe(subframeData[4:8])
	if len(subframeData) < int(10+subframeSize) {
		return fallbackTitle
	}

	titleData := subframeData[10 : 10+subframeSize]
	if len(titleData) == 0 {
		return fallbackTitle
	}

	titleEncoding := titleData[0]
	title := decodeText(titleData[1:], titleEncoding)
	if title == "" {
		return fallbackTitle
	}

	return title
}

// decodeText decodes text based on ID3v2 encoding byte.
func decodeText(data []byte, encoding byte) string {
	if len(data) == 0 {
		return ""
	}

	switch encoding {
	case 0: // ISO-8859-1
		return string(data)

	case 1: // UTF-16 with BOM
		return decodeUTF16(data)

	case 2: // UTF-16BE (ID3v2.4)
		return decodeUTF16BE(data)

	case 3: // UTF-8 (ID3v2.4)
		if utf8.Valid(data) {
			return string(data)
		}
		return string(data) // Return as-is even if invalid

	default:
		// Unknown encoding - try as ISO-8859-1
		return string(data)
	}
}

// decodeUTF16 decodes UTF-16 with BOM.
func decodeUTF16(data []byte) string {
	if len(data) < 2 {
		return ""
	}

	// Check BOM
	if data[0] == 0xFF && data[1] == 0xFE {
		// Little-endian
		return decodeUTF16LE(data[2:])
	} else if data[0] == 0xFE && data[1] == 0xFF {
		// Big-endian
		return decodeUTF16BE(data[2:])
	}

	// No BOM - assume big-endian
	return decodeUTF16BE(data)
}

// decodeUTF16LE decodes UTF-16 little-endian.
func decodeUTF16LE(data []byte) string {
	if len(data)%2 != 0 {
		data = data[:len(data)-1]
	}

	u16 := make([]uint16, len(data)/2)
	for i := range u16 {
		u16[i] = uint16(data[i*2]) | uint16(data[i*2+1])<<8
	}

	return string(utf16.Decode(u16))
}

// decodeUTF16BE decodes UTF-16 big-endian.
func decodeUTF16BE(data []byte) string {
	if len(data)%2 != 0 {
		data = data[:len(data)-1]
	}

	u16 := make([]uint16, len(data)/2)
	for i := range u16 {
		u16[i] = uint16(data[i*2])<<8 | uint16(data[i*2+1])
	}

	return string(utf16.Decode(u16))
}

// findNullTerminator finds the null terminator based on encoding.
func findNullTerminator(data []byte, encoding byte) int {
	switch encoding {
	case 0, 3: // ISO-8859-1, UTF-8 (single-byte null)
		return bytes.IndexByte(data, 0)

	case 1, 2: // UTF-16 (double-byte null)
		for i := 0; i < len(data)-1; i += 2 {
			if data[i] == 0 && data[i+1] == 0 {
				return i
			}
		}
		return -1

	default:
		return bytes.IndexByte(data, 0)
	}
}

// terminatorSize returns the size of the null terminator for the encoding.
func terminatorSize(encoding byte) int {
	switch encoding {
	case 0, 3: // ISO-8859-1, UTF-8
		return 1
	case 1, 2: // UTF-16
		return 2
	default:
		return 1
	}
}

// parseYear extracts year from various date formats.
func parseYear(text string) int {
	// YYYY format
	if len(text) >= 4 {
		var year int
		if _, err := fmt.Sscanf(text[:4], "%d", &year); err == nil {
			if year >= 1900 && year <= 2100 {
				return year
			}
		}
	}
	return 0
}

// parseTrackNumber parses "N" or "N/Total" format.
func parseTrackNumber(text string) (number, total int) {
	parts := strings.Split(text, "/")
	if len(parts) >= 1 {
		_, _ = fmt.Sscanf(parts[0], "%d", &number) //nolint:errcheck // Best effort parsing, zero value is fine
	}
	if len(parts) >= 2 {
		_, _ = fmt.Sscanf(parts[1], "%d", &total) //nolint:errcheck // Best effort parsing, zero value is fine
	}
	return
}
