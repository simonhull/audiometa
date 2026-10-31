// Package mp3 provides MP3 audio file parsing and ID3 tag extraction.
package mp3

import (
	"bytes"
	"cmp"
	"context"
	"encoding/binary"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/parsing"
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

// totalSize returns the on-disk size of the tag: 10-byte header, body and,
// for ID3v2.4 with the footer flag (0x10), the 10-byte footer.
func (h ID3v2Header) totalSize() int64 {
	n := 10 + int64(h.Size)
	if h.Version == 4 && h.Flags&0x10 != 0 {
		n += 10
	}
	return n
}

// parseID3v2 parses ID3v2 tags and extracts metadata. It returns the on-disk
// size of the tag; for a recognized tag with an unsupported version that is
// still returned (alongside the error) so the audio scan can skip the tag.
func parseID3v2(ctx context.Context, sr *binutil.SafeReader, file *types.File) (int64, error) {
	header, err := parseID3v2Header(sr)
	if err != nil {
		if header.Version != 0 {
			return header.totalSize(), err
		}
		return 0, err
	}

	tagSize := header.totalSize()
	if header.Version == 2 && header.Flags&flagV22Compression != 0 {
		file.Warnings = types.AppendWarning(file.Warnings, types.Warning{
			Stage:   types.StageMetadata,
			Message: "ID3v2.2 tag is compressed; frames ignored",
		})
		return tagSize, nil
	}
	if (header.Version == 2 || header.Version == 3) && header.Flags&flagUnsync != 0 {
		// ID3v2.3 unsynchronises the whole tag body, so undo it before walking frames.
		var warn *types.Warning
		sr, header, warn = deunsyncTag(sr, header)
		if warn != nil {
			file.Warnings = types.AppendWarning(file.Warnings, *warn)
		}
	}

	frameDataOffset := skipExtendedHeader(sr, header)
	st, err := parseID3v2Frames(ctx, sr, file, header, frameDataOffset)
	if err != nil {
		return 0, err
	}
	if file.Tags.Comment == "" {
		file.Tags.Comment = st.describedComment
	}

	// Process chapters
	if len(st.chapters) > 0 {
		file.Chapters = parseChapterFrames(st.chapters, header.Version)
	}

	// Post-parse fallbacks for audiobook series metadata.
	resolveSeriesFallbacks(sr, file)

	// Total tag size including header (as stored on disk, before any de-unsynchronisation)
	return tagSize, nil
}

// Header and frame flag bits.
const (
	flagUnsync = 0x80 // tag header: unsynchronisation (whole tag in v2.2/v2.3, all frames in v2.4)

	flagV22Compression = 0x40 // ID3v2.2 tag header: compressed tag (to be ignored)

	// ID3v2.4 frame format flags (low byte).
	v24FrameGrouping    = 0x0040
	v24FrameCompression = 0x0008
	v24FrameEncryption  = 0x0004
	v24FrameUnsync      = 0x0002
	v24FrameDataLength  = 0x0001

	// ID3v2.3 frame format flags (low byte).
	v23FrameCompression = 0x0080
	v23FrameEncryption  = 0x0040
	v23FrameGrouping    = 0x0020
)

// deunsync reverses ID3 unsynchronisation, collapsing every FF 00 pair to FF.
func deunsync(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		out = append(out, b[i])
		if b[i] == 0xFF && i+1 < len(b) && b[i+1] == 0x00 {
			i++
		}
	}
	return out
}

// deunsyncTag returns a reader over the tag with its body de-unsynchronised
// (the 10-byte header is kept, with Size updated). On failure it returns the
// original reader and header together with a warning.
func deunsyncTag(sr *binutil.SafeReader, header ID3v2Header) (*binutil.SafeReader, ID3v2Header, *types.Warning) {
	fail := func(err error) (*binutil.SafeReader, ID3v2Header, *types.Warning) {
		return sr, header, &types.Warning{
			Stage:   types.StageMetadata,
			Message: fmt.Sprintf("could not read unsynchronised ID3 tag body (%v); frames parsed as stored", err),
			Err:     err,
		}
	}
	if err := sr.CheckLen(10, int(header.Size), 1, "unsynchronised ID3 tag"); err != nil {
		return fail(err)
	}
	body := make([]byte, header.Size)
	if err := sr.ReadAt(body, 10, "unsynchronised ID3 tag"); err != nil {
		return fail(err)
	}
	body = deunsync(body)
	buf := make([]byte, 10, 10+len(body))
	buf = append(buf, body...)
	header.Size = uint32(len(body))
	return binutil.NewSafeReader(bytes.NewReader(buf), int64(len(buf)), sr.Path()), header, nil
}

// frameBody applies the per-frame flags to a frame's raw payload: it undoes
// unsynchronisation and strips the group-identity and data-length prefixes. It
// returns a non-empty reason when the frame is compressed or encrypted and must
// be skipped.
func frameBody(version, tagFlags byte, flags uint16, data []byte) (body []byte, skipReason string) {
	var unsync, dli, group bool
	switch version {
	case 4:
		switch {
		case flags&v24FrameCompression != 0:
			return nil, "compressed"
		case flags&v24FrameEncryption != 0:
			return nil, "encrypted"
		}
		unsync = flags&v24FrameUnsync != 0 || tagFlags&flagUnsync != 0
		dli = flags&v24FrameDataLength != 0
		group = flags&v24FrameGrouping != 0
	case 3:
		switch {
		case flags&v23FrameCompression != 0:
			return nil, "compressed"
		case flags&v23FrameEncryption != 0:
			return nil, "encrypted"
		}
		group = flags&v23FrameGrouping != 0
	}
	if unsync {
		data = deunsync(data)
	}
	if group {
		if len(data) < 1 {
			return nil, "truncated"
		}
		data = data[1:]
	}
	if dli {
		if len(data) < 4 {
			return nil, "truncated"
		}
		data = data[4:]
	}
	return data, ""
}

// resolveSeriesFallbacks applies fallback logic for series metadata after all frames are parsed.
// This mirrors the M4A audiobook tag resolution: Grouping → Series, then path-based series part.
func resolveSeriesFallbacks(sr *binutil.SafeReader, file *types.File) {
	// If no explicit Series, try to extract from Grouping tag.
	// Grouping often contains series info in formats like "Series Name #5".
	if file.Tags.Series == "" && file.Tags.Grouping != "" {
		series, part := parsing.ParseGrouping(file.Tags.Grouping)
		if series != "" {
			file.Tags.Series = series
			if file.Tags.SeriesPart == "" && part != "" {
				file.Tags.SeriesPart = part
			}
		}
	}

	// If series exists but no part, try to extract from file path.
	if file.Tags.Series != "" && file.Tags.SeriesPart == "" {
		if part := parsing.ExtractSeriesPartFromPath(sr.Path()); part != "" {
			file.Tags.SeriesPart = part
		}
	}
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

	// Only support ID3v2.2, ID3v2.3 and ID3v2.4. The populated header is returned
	// with the error so callers can still skip over the tag.
	if header.Version < 2 || header.Version > 4 {
		return header, &types.UnsupportedFormatError{
			Path:   sr.Path(),
			Reason: fmt.Sprintf("unsupported ID3v2 version: 2.%d", header.Version),
		}
	}

	return header, nil
}

// skipExtendedHeader skips the extended header if present and returns frame data offset.
func skipExtendedHeader(sr *binutil.SafeReader, header ID3v2Header) int64 {
	frameDataOffset := int64(10)

	// ID3v2.2 has no extended header (bit 0x40 means compression there).
	if header.Version == 2 || header.Flags&0x40 == 0 {
		return frameDataOffset // No extended header
	}

	extBuf := make([]byte, 4)
	if err := sr.ReadAt(extBuf, frameDataOffset, "extended header size"); err != nil {
		return frameDataOffset // Failed to read, use default offset
	}

	var extHeaderSize uint32
	switch header.Version {
	case 4:
		extHeaderSize = decodeSynchsafe(extBuf)
		frameDataOffset += int64(extHeaderSize)
	case 3:
		extHeaderSize = binary.BigEndian.Uint32(extBuf)
		frameDataOffset += int64(extHeaderSize) + 4
	}

	return frameDataOffset
}

// parseID3v2Frames parses all frames in the ID3v2 tag.
// It returns ctx.Err() if the context is canceled mid-walk.
func parseID3v2Frames(ctx context.Context, sr *binutil.SafeReader, file *types.File, header ID3v2Header, startOffset int64) (*frameState, error) {
	tagEnd := int64(10 + header.Size)
	offset := startOffset
	st := &frameState{}

	for i := 0; offset < tagEnd; i++ {
		if err := binutil.Canceled(ctx, i); err != nil {
			return nil, err
		}

		frame, bytesRead, stop := readSingleFrame(sr, file, header, offset)
		if stop {
			break
		}

		if frame != nil {
			processFrame(*frame, file, st)
		}

		offset += bytesRead
	}

	return st, nil
}

// readSingleFrame reads a single ID3v2 frame.
func readSingleFrame(sr *binutil.SafeReader, file *types.File, header ID3v2Header, offset int64) (*ID3v2Frame, int64, bool) {
	frameID, frameSize, frameFlags, headerLen, ok := readFrameHeader(sr, header.Version, offset)
	if !ok {
		return nil, 0, true
	}

	if !isMetadataFrame(frameID) {
		// processFrame ignores this frame (e.g. APIC artwork, which ExtractArtwork
		// reads lazily), so skip its payload entirely.
		return nil, headerLen + int64(frameSize), false
	}

	// Read frame data
	var frameData []byte
	err := sr.CheckLen(offset+headerLen, int(frameSize), 1, "ID3 frame")
	if err == nil {
		frameData = make([]byte, frameSize)
		err = sr.ReadAt(frameData, offset+headerLen, "frame data")
	}
	if err != nil {
		file.Warnings = types.AppendWarning(file.Warnings, types.Warning{
			Stage:   types.StageMetadata,
			Message: fmt.Sprintf("failed to read frame %s: %v", frameID, err),
			Err:     err,
		})
		return nil, headerLen + int64(frameSize), false
	}

	frameData, skip := frameBody(header.Version, header.Flags, frameFlags, frameData)
	if skip != "" {
		file.Warnings = types.AppendWarning(file.Warnings, types.Warning{
			Stage:   types.StageMetadata,
			Message: fmt.Sprintf("skipped %s frame %s", skip, frameID),
			Offset:  offset,
		})
		return nil, headerLen + int64(frameSize), false
	}

	frame := &ID3v2Frame{
		ID:    frameID,
		Size:  frameSize,
		Flags: frameFlags,
		Data:  frameData,
	}

	return frame, headerLen + int64(frameSize), false
}

// Frame IDs referenced in more than one place.
const (
	frameIDTIT2 = "TIT2"
	frameIDCOMM = "COMM"
	frameIDAPIC = "APIC"
	frameIDGRP1 = "GRP1"
	frameIDMVNM = "MVNM"
	frameIDMVIN = "MVIN"
)

// v22FrameIDs maps ID3v2.2 three-character frame IDs to their ID3v2.3 equivalents.
var v22FrameIDs = map[string]string{
	"TT1": "TIT1", "TT2": frameIDTIT2, "TT3": "TIT3", "TP1": "TPE1", "TP2": "TPE2",
	"TAL": "TALB", "TYE": "TYER", "TRK": "TRCK", "TPA": "TPOS", "TCO": "TCON",
	"TCM": "TCOM", "COM": frameIDCOMM, "TXX": frameIDTXXX, "GP1": frameIDGRP1, "MVN": frameIDMVNM,
	"MVI": frameIDMVIN, "PIC": frameIDAPIC,
}

// readFrameHeader reads the frame header at offset for the tag's version and
// returns the (v2.3-normalised) frame ID, payload size, flags and header length.
// ok is false at padding or when the header cannot be read.
func readFrameHeader(sr *binutil.SafeReader, version byte, offset int64) (id string, size uint32, flags uint16, headerLen int64, ok bool) {
	if version == 2 {
		var buf [6]byte
		if err := sr.ReadAt(buf[:], offset, "frame header"); err != nil || buf[0] == 0 {
			return "", 0, 0, 0, false
		}
		raw := string(buf[0:3])
		id = raw
		if mapped, found := v22FrameIDs[raw]; found {
			id = mapped
		}
		size = uint32(buf[3])<<16 | uint32(buf[4])<<8 | uint32(buf[5])
		return id, size, 0, 6, true
	}

	var buf [10]byte
	if err := sr.ReadAt(buf[:], offset, "frame header"); err != nil || buf[0] == 0 {
		return "", 0, 0, 0, false // unreadable, or padding (null bytes end the frames)
	}
	return string(buf[0:4]), decodeFrameSize(version, buf[4:8]), binary.BigEndian.Uint16(buf[8:10]), 10, true
}

// decodeFrameSize decodes frame size based on ID3v2 version.
func decodeFrameSize(version byte, sizeBytes []byte) uint32 {
	if version == 4 {
		return decodeSynchsafe(sizeBytes)
	}
	return binary.BigEndian.Uint32(sizeBytes)
}

// frameIDTXXX is the user-defined text frame, handled apart from the T*** text frames.
const frameIDTXXX = "TXXX"

// isTextFrameID reports whether parseTextFrame handles this frame ID: every
// standard T*** text frame except TXXX, plus the iTunes non-T text frames
// GRP1 (grouping), MVNM (movement name) and MVIN (movement number).
func isTextFrameID(id string) bool {
	switch id {
	case frameIDTXXX:
		return false
	case frameIDGRP1, frameIDMVNM, frameIDMVIN:
		return true
	}
	return strings.HasPrefix(id, "T")
}

// isMetadataFrame reports whether processFrame consumes this frame's payload.
// Keep in sync with processFrame.
func isMetadataFrame(id string) bool {
	return isTextFrameID(id) || id == frameIDTXXX || id == frameIDCOMM || id == "CHAP"
}

// frameState accumulates results that are resolved after the whole tag is walked.
type frameState struct {
	chapters []ID3v2Frame
	// describedComment is the first COMM text with a non-empty, non-iTunes
	// short description; used only when no COMM has an empty description.
	describedComment string
}

// processFrame processes a single frame based on its ID.
func processFrame(frame ID3v2Frame, file *types.File, st *frameState) {
	switch {
	case isTextFrameID(frame.ID):
		parseTextFrame(frame, file)
	case frame.ID == frameIDTXXX:
		parseTXXXFrame(frame, file)
	case frame.ID == frameIDCOMM:
		parseCommentFrame(frame, file, st)
	case frame.ID == "CHAP":
		st.chapters = append(st.chapters, frame)
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
	values := splitTextValues(decodeText(frame.Data[1:], encoding))
	var text string
	if len(values) > 0 {
		text = values[0]
	}

	for _, v := range values {
		file.Tags.AddRaw(frame.ID, v)
	}

	switch frame.ID {
	case frameIDTIT2: // Title
		file.Tags.Title = text
	case "TIT3": // Subtitle/Description refinement
		file.Tags.Subtitle = text
	case "TPE1": // Artist
		file.Tags.Artist = text
		file.Tags.Artists = append(file.Tags.Artists, values...)
	case "TALB": // Album
		file.Tags.Album = text
	case "TCON": // Genre
		file.Tags.Genres = append(file.Tags.Genres, values...)
	case "TYER": // Year (ID3v2.3)
		if year := parseYear(text); year > 0 {
			file.Tags.Year = year
		}
	case "TDRC": // Recording time (ID3v2.4)
		if year := parseYear(text); year > 0 {
			file.Tags.Year = year
		}
	case "TCOM": // Composer (often used for Narrator)
		file.Tags.Composers = append(file.Tags.Composers, values...)
	case "TRCK": // Track number/total
		file.Tags.TrackNumber, file.Tags.TrackTotal = parseTrackNumber(text)
	case "TPOS": // Disc number/total
		file.Tags.DiscNumber, file.Tags.DiscTotal = parseTrackNumber(text)
	case "TPE2": // Album artist
		file.Tags.AlbumArtist = text
	case "GRP1": // Grouping (often contains series info for audiobooks)
		file.Tags.Grouping = text
	case "TIT1": // Content group description (alternative grouping)
		if file.Tags.Grouping == "" {
			file.Tags.Grouping = text
		}
	case "MVNM": // Movement name (series name in ID3v2.4)
		if file.Tags.Series == "" {
			file.Tags.Series = text
		}
	case "MVIN": // Movement number "N" or "N/count" (series position)
		if file.Tags.SeriesPart == "" {
			part, _, _ := strings.Cut(text, "/")
			file.Tags.SeriesPart = strings.TrimSpace(part)
		}
	}
}

// txxxFieldHandlers maps TXXX description (lowercased) → handler that writes
// the value into the corresponding tag, optionally with a "first wins" rule.
var txxxFieldHandlers = map[string]func(*types.File, string){
	"narrator":        func(f *types.File, v string) { f.Tags.Narrator = v },
	"series":          func(f *types.File, v string) { f.Tags.Series = v },
	"series part":     func(f *types.File, v string) { f.Tags.SeriesPart = v },
	"seriespart":      func(f *types.File, v string) { f.Tags.SeriesPart = v },
	"part":            func(f *types.File, v string) { f.Tags.SeriesPart = v },
	"series-part":     func(f *types.File, v string) { f.Tags.SeriesPart = v },
	"series position": func(f *types.File, v string) { f.Tags.SeriesPart = v },
	"publisher":       func(f *types.File, v string) { f.Tags.Publisher = v },
	"isbn":            func(f *types.File, v string) { f.Tags.ISBN = v },
	"asin":            func(f *types.File, v string) { f.Tags.ASIN = v },
	"audible_asin":    func(f *types.File, v string) { f.Tags.ASIN = v },
	"language":        func(f *types.File, v string) { f.Tags.Language = v },
	"lang":            func(f *types.File, v string) { f.Tags.Language = v },
	"description":     setIfEmpty(func(t *types.Tags) *string { return &t.Description }),
	"mvnm":            setIfEmpty(func(t *types.Tags) *string { return &t.Series }),
	"movement name":   setIfEmpty(func(t *types.Tags) *string { return &t.Series }),
	"movement":        setIfEmpty(func(t *types.Tags) *string { return &t.Series }),
	"show":            setIfEmpty(func(t *types.Tags) *string { return &t.Series }),
	"mvin":            setIfEmpty(func(t *types.Tags) *string { return &t.SeriesPart }),
	"movement number": setIfEmpty(func(t *types.Tags) *string { return &t.SeriesPart }),
	"movement index":  setIfEmpty(func(t *types.Tags) *string { return &t.SeriesPart }),
	"episode_id":      setIfEmpty(func(t *types.Tags) *string { return &t.SeriesPart }),
	"grouping":        setIfEmpty(func(t *types.Tags) *string { return &t.Grouping }),
}

func setIfEmpty(field func(*types.Tags) *string) func(*types.File, string) {
	return func(f *types.File, v string) {
		if p := field(&f.Tags); *p == "" {
			*p = v
		}
	}
}

// Format: [encoding][description\0][value].
func parseTXXXFrame(frame ID3v2Frame, file *types.File) {
	if len(frame.Data) < 2 {
		return
	}

	encoding := frame.Data[0]
	data := frame.Data[1:]

	nullIdx := findNullTerminator(data, encoding)
	if nullIdx < 0 {
		return
	}

	description := decodeText(data[:nullIdx], encoding)
	value := firstValue(decodeText(data[nullIdx+terminatorSize(encoding):], encoding))

	// Raw key convention: the upper-cased TXXX description (matches Vorbis keys).
	if description != "" {
		file.Tags.AddRaw(strings.ToUpper(description), value)
	}

	if handler, ok := txxxFieldHandlers[strings.ToLower(description)]; ok {
		handler(file, value)
	}
}

// parseCommentFrame handles COMM: [encoding][language(3)][short description\0][text].
// iTunes stores internal data (iTunNORM, iTunSMPB, iTunPGAP, iTunes_CDDB_*) in
// described COMM frames; those are ignored. The first COMM with an empty
// description is the user comment; otherwise the first described one is kept
// as a fallback.
func parseCommentFrame(frame ID3v2Frame, file *types.File, st *frameState) {
	if len(frame.Data) < 4 {
		return
	}
	encoding := frame.Data[0]
	data := frame.Data[4:] // skip encoding + language

	var desc, text string
	if nullIdx := findNullTerminator(data, encoding); nullIdx < 0 {
		text = firstValue(decodeText(data, encoding)) // malformed: no description
	} else {
		desc = decodeText(data[:nullIdx], encoding)
		text = firstValue(decodeText(data[nullIdx+terminatorSize(encoding):], encoding))
	}

	switch {
	case text == "", strings.HasPrefix(desc, "iTun"):
		return
	case desc == "":
		if file.Tags.Comment == "" {
			file.Tags.Comment = text
		}
	case st.describedComment == "":
		st.describedComment = text
	}
}

// parseChapterFrames parses CHAP frames and builds chapter list.
// CHAP frame format:
//
//	[element_id\0][start_time(4)][end_time(4)][start_offset(4)][end_offset(4)][subframes...]
func parseChapterFrames(frames []ID3v2Frame, version byte) []types.Chapter {
	type chapterData struct {
		ElementID string
		Title     string
		Index     int
		StartTime uint32
		EndTime   uint32
	}

	chapters := make([]chapterData, 0, len(frames))

	for _, frame := range frames {
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
		title := extractChapterTitleFromSubframes(data[16:], version, elementID)

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

// extractChapterTitleFromSubframes walks the CHAP subframes (sized per the tag
// version) and returns the first non-empty TIT2 title, else fallbackTitle.
func extractChapterTitleFromSubframes(subframeData []byte, version byte, fallbackTitle string) string {
	for len(subframeData) >= 10 {
		if subframeData[0] == 0 { // padding
			break
		}
		id := string(subframeData[0:4])
		size := uint64(decodeFrameSize(version, subframeData[4:8]))
		if size > uint64(len(subframeData)-10) { // truncated/corrupt: stop, don't guess
			break
		}
		body := subframeData[10 : 10+size]
		if id == frameIDTIT2 && len(body) > 0 {
			if title := decodeText(body[1:], body[0]); title != "" {
				return title
			}
		}
		subframeData = subframeData[10+size:]
	}
	return fallbackTitle
}

// decodeText decodes ID3v2 text for the given encoding byte and trims NUL
// terminators. Encodings: 0 ISO-8859-1, 1 UTF-16 with BOM, 2 UTF-16BE, 3 UTF-8.
func decodeText(data []byte, encoding byte) string {
	var s string
	switch encoding {
	case 1:
		s = decodeUTF16(data)
	case 2:
		s = decodeUTF16BE(data)
	default: // 0, 3 and unknown encodings
		s = decodeSingleByteText(data)
	}
	return strings.TrimRight(s, "\x00")
}

// decodeSingleByteText decodes ISO-8859-1 or UTF-8 text. Valid UTF-8 is used
// as-is (ASCII is both, and many taggers write UTF-8 into frames labeled
// ISO-8859-1); anything else is decoded as ISO-8859-1, where each byte is the
// code point of the same value. The result is always valid UTF-8.
func decodeSingleByteText(data []byte) string {
	if utf8.Valid(data) {
		return string(data)
	}
	var b strings.Builder
	b.Grow(len(data) * 2)
	for _, c := range data {
		b.WriteRune(rune(c))
	}
	return b.String()
}

// splitTextValues splits decoded text-frame content into its values. ID3v2.4
// separates multiple values with NUL; UTF-16 values may each carry their own
// BOM, which decodes to U+FEFF and is dropped. Empty values are removed.
func splitTextValues(s string) []string {
	var out []string
	for p := range strings.SplitSeq(s, "\x00") {
		if p = strings.TrimPrefix(p, "\uFEFF"); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// firstValue returns the first value of a possibly multi-valued text, or "".
func firstValue(s string) string {
	if values := splitTextValues(s); len(values) > 0 {
		return values[0]
	}
	return ""
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
		_, _ = fmt.Sscanf(parts[0], "%d", &number)
	}
	if len(parts) >= 2 {
		_, _ = fmt.Sscanf(parts[1], "%d", &total)
	}
	return
}
