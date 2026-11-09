// Package mp3 parses MP3 files.
//
// It reads ID3v2.2, ID3v2.3 and ID3v2.4 tags (undoing unsynchronisation and
// skipping compressed or encrypted frames), falls back to an ID3v1 trailer,
// reads chapters from CHAP frames or the OverDrive MediaMarkers TXXX frame,
// extracts APIC artwork lazily, and derives technical properties from the
// first MPEG frame and any Xing/Info or VBRI header. It registers itself for
// [types.FormatMP3].
package mp3

import (
	"bytes"
	"cmp"
	"context"
	"encoding/binary"
	"fmt"
	"iter"
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
	Version byte // Major version (2, 3 or 4)
	Flags   byte
	Size    uint32 // Tag size (excluding header), synchsafe
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
	warn := func(w types.Warning) { file.Warnings = types.AppendWarning(file.Warnings, w) }
	tag, err := openID3v2Tag(sr, warn)
	if err != nil {
		return tag.size, err
	}
	st := &frameState{}
	for frame, err := range tag.frames(ctx, isMetadataFrame, warn) {
		if err != nil {
			return 0, err
		}
		processFrame(frame, file, st)
	}
	if file.Tags.Comment == "" {
		file.Tags.Comment = st.describedComment
	}
	if len(st.chapters) > 0 {
		// CHAP frames take precedence over OverDrive MediaMarkers (set by a TXXX handler).
		file.Chapters = parseChapterFrames(st.chapters, tag.header.Version, tag.coding)
	}
	// On-disk size of the tag, before any de-unsynchronisation.
	return tag.size, nil
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
	header.Size = uint32(len(body)) //nolint:gosec // G115: deunsync only removes bytes, so len(body) <= the original uint32 header.Size
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

// Frame IDs referenced in more than one place.
const (
	frameIDTIT2 = "TIT2"
	frameIDCOMM = "COMM"
	frameIDAPIC = "APIC"
	frameIDUSLT = "USLT"
	frameIDGRP1 = "GRP1"
	frameIDMVNM = "MVNM"
	frameIDMVIN = "MVIN"
	frameIDUFID = "UFID"
)

// ufidKeyPrefix starts the raw key of a UFID frame, which "UFID:" followed by
// the frame's owner names: "UFID:http://musicbrainz.org".
const ufidKeyPrefix = "UFID:"

// v22FrameIDs maps ID3v2.2 three-character frame IDs to their ID3v2.3 equivalents.
var v22FrameIDs = map[string]string{
	"TT1": "TIT1", "TT2": frameIDTIT2, "TT3": "TIT3", "TP1": "TPE1", "TP2": "TPE2",
	"TAL": "TALB", "TYE": "TYER", "TRK": "TRCK", "TPA": "TPOS", "TCO": "TCON",
	"TCM": "TCOM", "COM": frameIDCOMM, "TXX": frameIDTXXX, "GP1": frameIDGRP1, "MVN": frameIDMVNM,
	"MVI": frameIDMVIN, "PIC": frameIDAPIC, "ULT": frameIDUSLT, "UFI": frameIDUFID,
}

// readFrameHeader reads the frame header at offset for the tag's version and
// returns the (v2.3-normalised) frame ID, payload size, flags and header length.
// ok is false at padding or when the header cannot be read.
func readFrameHeader(sr *binutil.SafeReader, version byte, coding frameSizeCoding, offset int64) (id string, size uint32, flags uint16, headerLen int64, ok bool) {
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
	return string(buf[0:4]), decodeFrameSize(version, coding, buf[4:8]), binary.BigEndian.Uint16(buf[8:10]), 10, true
}

// decodeFrameSize decodes a frame size: synchsafe in an ID3v2.4 tag unless
// the tag's coding says its sizes are plain, big-endian otherwise.
func decodeFrameSize(version byte, coding frameSizeCoding, sizeBytes []byte) uint32 {
	if version == 4 && coding == sizeSynchsafe {
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
	return isTextFrameID(id) || id == frameIDTXXX || id == frameIDCOMM || id == frameIDUSLT || id == frameIDUFID || id == "CHAP"
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
	case frame.ID == frameIDUSLT:
		parseLyricsFrame(frame, file)
	case frame.ID == frameIDUFID:
		parseUFIDFrame(frame, file)
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
	values := takeTextValues(file, decodeText(frame.Data[1:], encoding))
	var text string
	if len(values) > 0 {
		text = values[0]
	}

	for _, v := range values {
		file.Tags.AddRaw(frame.ID, v)
	}

	switch frame.ID {
	case frameIDTIT2: // Title
		if file.Tags.Title == "" {
			file.Tags.Title = text
		}
	case "TIT3": // Subtitle/Description refinement
		if file.Tags.Subtitle == "" {
			file.Tags.Subtitle = text
		}
	case "TPE1": // Artist
		if file.Tags.Artist == "" {
			file.Tags.Artist = text
		}
		file.Tags.Artists = append(file.Tags.Artists, values...)
	case "TALB": // Album
		if file.Tags.Album == "" {
			file.Tags.Album = text
		}
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
	case "TCOM": // Composer
		file.Tags.Composers = append(file.Tags.Composers, values...)
	case "TRCK": // Track number/total
		if file.Tags.TrackNumber == 0 && file.Tags.TrackTotal == 0 {
			file.Tags.TrackNumber, file.Tags.TrackTotal = parseTrackNumber(text)
		}
	case "TPOS": // Disc number/total
		if file.Tags.DiscNumber == 0 && file.Tags.DiscTotal == 0 {
			file.Tags.DiscNumber, file.Tags.DiscTotal = parseTrackNumber(text)
		}
	case "TPE2": // Album artist
		if file.Tags.AlbumArtist == "" {
			file.Tags.AlbumArtist = text
		}
	case "GRP1": // Grouping (iTunes)
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

// applyMediaMarkers turns an OverDrive MediaMarkers TXXX value into chapters.
func applyMediaMarkers(f *types.File, value string) {
	chapters, err := parseMediaMarkers(value)
	if err != nil {
		types.Warn(f, types.StageChapters, 0, err, fmt.Sprintf("ignoring OverDrive MediaMarkers: %v", err))
		return
	}
	if len(chapters) > 0 {
		f.Chapters = chapters
	}
}

// txxxFieldHandlers maps TXXX description (lowercased) → handler that writes
// the value into the corresponding tag, optionally with a "first wins" rule.
var txxxFieldHandlers = map[string]func(*types.File, string){
	mediaMarkersDescription: applyMediaMarkers,
	"narrator":              func(f *types.File, v string) { f.Tags.Narrator = v },
	"series":                func(f *types.File, v string) { f.Tags.Series = v },
	"series part":           func(f *types.File, v string) { f.Tags.SeriesPart = v },
	"seriespart":            func(f *types.File, v string) { f.Tags.SeriesPart = v },
	"series-part":           func(f *types.File, v string) { f.Tags.SeriesPart = v },
	"series position":       func(f *types.File, v string) { f.Tags.SeriesPart = v },
	"publisher":             func(f *types.File, v string) { f.Tags.Publisher = v },
	"isbn":                  func(f *types.File, v string) { f.Tags.ISBN = v },
	"asin":                  func(f *types.File, v string) { f.Tags.ASIN = v },
	"audible_asin":          func(f *types.File, v string) { f.Tags.ASIN = v },
	"language":              func(f *types.File, v string) { f.Tags.Language = v },
	"lang":                  func(f *types.File, v string) { f.Tags.Language = v },
	"description":           setIfEmpty(func(t *types.Tags) *string { return &t.Description }),
	"mvnm":                  setIfEmpty(func(t *types.Tags) *string { return &t.Series }),
	"movement name":         setIfEmpty(func(t *types.Tags) *string { return &t.Series }),
	"movement":              setIfEmpty(func(t *types.Tags) *string { return &t.Series }),
	"mvin":                  setIfEmpty(func(t *types.Tags) *string { return &t.SeriesPart }),
	"movement number":       setIfEmpty(func(t *types.Tags) *string { return &t.SeriesPart }),
	"movement index":        setIfEmpty(func(t *types.Tags) *string { return &t.SeriesPart }),
	"grouping":              setIfEmpty(func(t *types.Tags) *string { return &t.Grouping }),
}

func setIfEmpty(field func(*types.Tags) *string) func(*types.File, string) {
	return func(f *types.File, v string) {
		if p := field(&f.Tags); *p == "" {
			*p = v
		}
	}
}

// parseTXXXFrame handles TXXX: [encoding][description\0][value{\0value}]. Every
// value goes to the raw tags, and the first fills the typed field, if any. A
// frame with no value keeps its raw key, holding "", so it can still be deleted.
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
	values := takeTextValues(file, decodeText(data[nullIdx+terminatorSize(encoding):], encoding))
	if len(values) == 0 {
		// An empty value is still a value; no values at all means the cap is reached.
		if !types.TakeValue(file) {
			return
		}
		values = []string{""}
	}

	// Raw key convention: the upper-cased TXXX description (matches Vorbis keys);
	// the description as stored is kept as the key's name.
	if description != "" {
		for _, v := range values {
			file.Tags.AddRawNamed(description, v)
		}
	}

	if handler, ok := txxxFieldHandlers[strings.ToLower(description)]; ok {
		handler(file, values[0])
	}
}

// parseUFIDFrame handles UFID: [owner\0][identifier], the identifier up to 64
// bytes. A frame whose identifier is text (valid UTF-8 without NUL, as a
// MusicBrainz recording ID is) is recorded under the raw key "UFID:" + owner;
// a binary identifier, or a frame with no owner, is not.
func parseUFIDFrame(frame ID3v2Frame, file *types.File) {
	owner, id, ok := parseUFID(frame.Data)
	if !ok || id == "" || !utf8.ValidString(id) || strings.ContainsRune(id, 0) || !types.TakeValue(file) {
		return
	}
	file.Tags.AddRaw(ufidKeyPrefix+owner, id)
}

// parseUFID splits a UFID body into its owner, decoded as ISO-8859-1, and its
// identifier. ok is false when there is no owner or no terminator after it.
func parseUFID(body []byte) (owner, id string, ok bool) {
	nul := bytes.IndexByte(body, 0)
	if nul <= 0 {
		return "", "", false
	}
	return decodeText(body[:nul], encLatin1), string(body[nul+1:]), true
}

// parseCommentFrame handles COMM: [encoding][language(3)][short description\0][text].
// iTunes stores internal data (iTunNORM, iTunSMPB, iTunPGAP, iTunes_CDDB_*) in
// described COMM frames; those are ignored. The first COMM with an empty
// description is the user comment; otherwise the first described one is kept
// as a fallback.
func parseCommentFrame(frame ID3v2Frame, file *types.File, st *frameState) {
	c, ok := parseLangText(frame.Data)
	if !ok {
		return
	}

	if c.text == "" || strings.HasPrefix(c.desc, "iTun") || !types.TakeValue(file) {
		return
	}

	switch {
	case c.desc == "":
		if file.Tags.Comment == "" {
			file.Tags.Comment = c.text
		}
	case st.describedComment == "":
		st.describedComment = c.text
	}
}

// parseLyricsFrame handles USLT, which has the layout of COMM. Every non-empty
// text is recorded under the raw key USLT; the first one, whatever its
// language or description, fills Lyrics.
func parseLyricsFrame(frame ID3v2Frame, file *types.File) {
	c, ok := parseLangText(frame.Data)
	if !ok || c.text == "" || !types.TakeValue(file) {
		return
	}
	file.Tags.AddRaw(frameIDUSLT, c.text)
	if file.Tags.Lyrics == "" {
		file.Tags.Lyrics = c.text
	}
}

// langText is the content of a COMM or USLT frame. lang is the three stored
// bytes, kept as they are.
type langText struct {
	lang, desc, text string
}

// parseLangText decodes a COMM or USLT body:
// [encoding][language(3)][short description\0][text]. A body with no
// terminator is malformed and read as text with no description. ok is false
// when the body is too short to hold a language.
func parseLangText(body []byte) (langText, bool) {
	if len(body) < 4 {
		return langText{}, false
	}
	encoding, data := body[0], body[4:]
	c := langText{lang: string(body[1:4])}
	if nullIdx := findNullTerminator(data, encoding); nullIdx < 0 {
		c.text = firstValue(decodeText(data, encoding))
	} else {
		c.desc = decodeText(data[:nullIdx], encoding)
		c.text = firstValue(decodeText(data[nullIdx+terminatorSize(encoding):], encoding))
	}
	return c, true
}

// parseChapterFrames parses CHAP frames and builds chapter list.
// CHAP frame format:
//
//	[element_id\0][start_time(4)][end_time(4)][start_offset(4)][end_offset(4)][subframes...]
func parseChapterFrames(frames []ID3v2Frame, version byte, coding frameSizeCoding) []types.Chapter {
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
		title := extractChapterTitleFromSubframes(data[16:], version, coding, elementID)

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
			Source:    types.ChapterSourceID3CHAP,
		}
	}

	return result
}

// extractChapterTitleFromSubframes walks the CHAP subframes (sized per the tag
// version) and returns the first non-empty TIT2 title, else fallbackTitle.
func extractChapterTitleFromSubframes(subframeData []byte, version byte, coding frameSizeCoding, fallbackTitle string) string {
	for len(subframeData) >= 10 {
		if subframeData[0] == 0 { // padding
			break
		}
		id := string(subframeData[0:4])
		size := uint64(decodeFrameSize(version, coding, subframeData[4:8]))
		// A frame running past the data is truncated or corrupt: stop, don't guess.
		if size > uint64(len(subframeData)-10) { //nolint:gosec // G115: the loop condition guarantees len(subframeData) >= 10
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

// textValues yields the values of decoded text-frame content. ID3v2.4
// separates multiple values with NUL; UTF-16 values may each carry their own
// BOM, which decodes to U+FEFF and is dropped. Empty values are skipped.
func textValues(s string) iter.Seq[string] {
	return func(yield func(string) bool) {
		for p := range strings.SplitSeq(s, "\x00") {
			if p = strings.TrimPrefix(p, "\uFEFF"); p != "" && !yield(p) {
				return
			}
		}
	}
}

// takeTextValues returns the values of s that file's tag value cap admits, in
// order. It stops splitting at the first refusal.
func takeTextValues(file *types.File, s string) []string {
	var out []string
	for v := range textValues(s) {
		if !types.TakeValue(file) {
			break
		}
		out = append(out, v)
	}
	return out
}

// firstValue returns the first value of a possibly multi-valued text, or "".
func firstValue(s string) string {
	for v := range textValues(s) {
		return v
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

// validID3Year reports whether year is one the reader keeps from an ID3 tag
// (1900 to 2100), and so the only years the writer can store. The same range
// is applied to WAV and AIFF by types.decodeYear, which cannot import this
// package; keep the two in step.
func validID3Year(year int) bool {
	return year >= 1900 && year <= 2100
}

// parseYear extracts year from various date formats.
func parseYear(text string) int {
	// YYYY format
	if len(text) >= 4 {
		var year int
		if _, err := fmt.Sscanf(text[:4], "%d", &year); err == nil {
			if validID3Year(year) {
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
