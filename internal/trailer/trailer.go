// Package trailer finds the tags that tools append after the audio of MP3 and
// FLAC files: ID3v1, APEv2, Lyrics3v2 and ID3v2 tags with a footer.
package trailer

import (
	"bytes"
	"context"
	"encoding/binary"
	"strconv"

	binutil "github.com/simonhull/audiometa/internal/binary"
)

const (
	id3v1Size     = 128
	apeFooterSize = 32
	apeHasHeader  = 1 << 31
	lyricsTail    = 15 // 6-digit size + "LYRICS200"
	id3v2Footer   = 10
	id3v2HasFoot  = 0x10
)

// tagLens are tried in order. Tags with long, structured markers come first,
// so a stray "TAG" inside an APEv2 or Lyrics3v2 tag is not taken for ID3v1.
var tagLens = [...]func(*binutil.SafeReader, int64, int64) (int64, error){
	apeLen, lyrics3v2Len, id3v2FooterLen, id3v1Len,
}

// Start returns where the trailing tags of sr[start:end) begin, or end if
// there are none. It peels complete tag blocks off the end of the range until
// none matches; a block that is incomplete, inconsistent or would reach before
// start is left in place, as audio. It stops early when ctx is done.
func Start(ctx context.Context, sr *binutil.SafeReader, start, end int64) (int64, error) {
	for i := 0; ; i++ {
		if err := binutil.Canceled(ctx, i); err != nil {
			return 0, err
		}
		n, err := trailingTag(sr, start, end)
		if err != nil {
			return 0, err
		}
		if n == 0 {
			return end, nil
		}
		end -= n
	}
}

// trailingTag returns the size of the tag block that ends at end, or 0.
func trailingTag(sr *binutil.SafeReader, start, end int64) (int64, error) {
	for _, size := range tagLens {
		n, err := size(sr, start, end)
		if err != nil || n > 0 {
			return n, err
		}
	}
	return 0, nil
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
	if footer == nil || err != nil || string(footer[:8]) != "APETAGEX" {
		return 0, err
	}
	if v := binary.LittleEndian.Uint32(footer[8:12]); v != 1000 && v != 2000 {
		return 0, nil
	}
	size := int64(binary.LittleEndian.Uint32(footer[12:16]))
	if size < apeFooterSize {
		return 0, nil
	}
	if binary.LittleEndian.Uint32(footer[20:24])&apeHasHeader == 0 {
		return fit(size, start, end), nil
	}
	size += apeFooterSize
	ok, err := hasAt(sr, start, end, end-size, "APETAGEX")
	if !ok || err != nil {
		return 0, err
	}
	return size, nil
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
