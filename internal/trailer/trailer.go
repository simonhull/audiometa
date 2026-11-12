// Package trailer finds the tags that tools append after the audio of MP3 and
// FLAC files: ID3v1, APEv2, Lyrics3v2 and ID3v2 tags with a footer.
package trailer

import (
	"bytes"
	"context"
	"encoding/binary"
	"slices"
	"strconv"

	binutil "github.com/simonhull/audiometa/internal/binary"
)

const (
	id3v1Size     = 128
	apeFooterSize = 32
	apeHasHeader  = 1 << 31
	apeIsHeader   = 1 << 29
	lyricsTail    = 15 // 6-digit size + "LYRICS200"
	id3v2Footer   = 10
	id3v2HasFoot  = 0x10
)

// Kind names a trailing tag format.
type Kind uint8

// The trailing tag formats [Scan] recognizes.
const (
	KindID3v1   Kind = iota + 1 // 128-byte "TAG" block
	KindAPEv2                   // APEv1 or APEv2, with or without a header
	KindLyrics3                 // Lyrics3v2: "LYRICSBEGIN" ... "LYRICS200"
	KindID3v2                   // ID3v2.4 tag with a footer, appended after the audio
)

// Block is one trailing tag block: Size bytes at Off.
type Block struct {
	Kind Kind
	Off  int64
	Size int64
}

// structuredTags recognize tags with long, structured markers. They are
// tried before id3v1Len, so a stray "TAG" inside an APEv2 or Lyrics3v2 tag
// is not taken for ID3v1.
var structuredTags = [...]struct {
	kind Kind
	size func(*binutil.SafeReader, int64, int64) (int64, error)
}{
	{KindAPEv2, apeLen}, {KindLyrics3, lyrics3v2Len}, {KindID3v2, id3v2FooterLen},
}

// Start returns where the trailing tags of sr[start:end) begin, or end if
// there are none. It peels complete tag blocks off the end of the range until
// none matches; a block that is incomplete, inconsistent or would reach before
// start is left in place, as audio. It stops early when ctx is done.
func Start(ctx context.Context, sr *binutil.SafeReader, start, end int64) (int64, error) {
	return peel(ctx, sr, start, end, nil)
}

// Scan is [Start] that also names the blocks it peels, in file order. The
// result is nil when there are none.
func Scan(ctx context.Context, sr *binutil.SafeReader, start, end int64) (int64, []Block, error) {
	var blocks []Block
	audioEnd, err := peel(ctx, sr, start, end, &blocks)
	if err != nil {
		return 0, nil, err
	}
	slices.Reverse(blocks) // peeled last to first
	return audioEnd, blocks, nil
}

// peel is the loop behind [Start] and [Scan]; it appends each block to blocks
// unless that is nil.
func peel(ctx context.Context, sr *binutil.SafeReader, start, end int64, blocks *[]Block) (int64, error) {
	for i := 0; ; i++ {
		if err := binutil.Canceled(ctx, i); err != nil {
			return 0, err
		}
		kind, n, err := trailingTag(sr, start, end)
		if err != nil {
			return 0, err
		}
		if n == 0 {
			return end, nil
		}
		end -= n
		if blocks != nil {
			*blocks = append(*blocks, Block{Kind: kind, Off: end, Size: n})
		}
	}
}

// trailingTag returns the kind and size of the tag block that ends at end, or
// size 0.
func trailingTag(sr *binutil.SafeReader, start, end int64) (Kind, int64, error) {
	kind, n, err := structuredTag(sr, start, end)
	if err != nil || n > 0 {
		return kind, n, err
	}
	n, err = id3v1Len(sr, start, end)
	return KindID3v1, n, err
}

// structuredTag returns the kind and size of the structured tag block that ends
// at end, or size 0.
func structuredTag(sr *binutil.SafeReader, start, end int64) (Kind, int64, error) {
	for _, t := range structuredTags {
		n, err := t.size(sr, start, end)
		if err != nil || n > 0 {
			return t.kind, n, err
		}
	}
	return 0, 0, nil
}

// EndsWithID3v1 reports whether the last trailing tag block of
// sr[start:end) is an ID3v1 tag, using the same rules as [Start]: a "TAG"
// inside an APEv2 or Lyrics3v2 tag does not count.
//
// # Errors
//
// It returns the error of a failed read of sr.
func EndsWithID3v1(sr *binutil.SafeReader, start, end int64) (bool, error) {
	_, n, err := structuredTag(sr, start, end)
	if err != nil || n > 0 {
		return false, err
	}
	n, err = id3v1Len(sr, start, end)
	return n > 0, err
}

func id3v1Len(sr *binutil.SafeReader, start, end int64) (int64, error) {
	ok, err := hasAt(sr, start, end, end-id3v1Size, "TAG")
	if !ok || err != nil {
		return 0, err
	}
	return id3v1Size, nil
}

func apeLen(sr *binutil.SafeReader, start, end int64) (int64, error) {
	footer, err := read(sr, start, end, end-apeFooterSize, apeFooterSize)
	if footer == nil || err != nil || !apePart(footer, false) {
		return 0, err
	}
	size := int64(binary.LittleEndian.Uint32(footer[12:16]))
	if size < apeFooterSize {
		return 0, nil
	}
	if binary.LittleEndian.Uint32(footer[20:24])&apeHasHeader == 0 {
		return fit(size, start, end), nil
	}
	size += apeFooterSize
	header, err := read(sr, start, end, end-size, apeFooterSize)
	if header == nil || err != nil || !apePart(header, true) || !bytes.Equal(header[8:16], footer[8:16]) {
		return 0, err
	}
	return size, nil
}

// apePart reports whether b is an APEv1/APEv2 header (isHeader) or footer:
// the "APETAGEX" marker, a known version, and flag bit 29 saying which.
func apePart(b []byte, isHeader bool) bool {
	if string(b[:8]) != "APETAGEX" {
		return false
	}
	if v := binary.LittleEndian.Uint32(b[8:12]); v != 1000 && v != 2000 {
		return false
	}
	return binary.LittleEndian.Uint32(b[20:24])&apeIsHeader != 0 == isHeader
}

func lyrics3v2Len(sr *binutil.SafeReader, start, end int64) (int64, error) {
	tail, err := read(sr, start, end, end-lyricsTail, lyricsTail)
	if tail == nil || err != nil || string(tail[6:]) != "LYRICS200" || !allDigits(tail[:6]) {
		return 0, err
	}
	body, _ := strconv.Atoi(string(tail[:6])) // six ASCII digits always parse
	size := int64(body) + lyricsTail
	ok, err := hasAt(sr, start, end, end-size, "LYRICSBEGIN")
	if !ok || err != nil {
		return 0, err
	}
	return size, nil
}

func id3v2FooterLen(sr *binutil.SafeReader, start, end int64) (int64, error) {
	footer, err := read(sr, start, end, end-id3v2Footer, id3v2Footer)
	if footer == nil || err != nil || string(footer[:3]) != "3DI" ||
		footer[3] != 4 || footer[5]&id3v2HasFoot == 0 || !synchsafe(footer[6:10]) {
		return 0, err
	}
	s := footer[6:10]
	size := (int64(s[0])<<21 | int64(s[1])<<14 | int64(s[2])<<7 | int64(s[3])) + 2*id3v2Footer
	header, err := read(sr, start, end, end-size, id3v2Footer)
	if header == nil || err != nil || string(header[:3]) != "ID3" || !bytes.Equal(header[3:], footer[3:]) {
		return 0, err
	}
	return size, nil
}

// fit returns size if a block of that size ending at end stays inside
// [start, end), and 0 otherwise.
func fit(size, start, end int64) int64 {
	if size > end-start {
		return 0
	}
	return size
}

func allDigits(b []byte) bool {
	for _, c := range b {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// synchsafe reports whether every byte of b leaves its high bit clear.
func synchsafe(b []byte) bool {
	for _, c := range b {
		if c&0x80 != 0 {
			return false
		}
	}
	return true
}

// hasAt reports whether magic sits at off, inside [start, end).
func hasAt(sr *binutil.SafeReader, start, end, off int64, magic string) (bool, error) {
	b, err := read(sr, start, end, off, len(magic))
	return b != nil && string(b) == magic, err
}

// read returns n bytes at off, or nil when they do not fit inside [start, end).
func read(sr *binutil.SafeReader, start, end, off int64, n int) ([]byte, error) {
	if off < start || off+int64(n) > end {
		return nil, nil
	}
	b := make([]byte, n)
	if err := sr.ReadAt(b, off, "trailing tag"); err != nil {
		return nil, err
	}
	return b, nil
}
