// Package trailer finds the tags that tools append after the audio of MP3 and
// FLAC files: ID3v1, APEv2, Lyrics3v2 and ID3v2 tags with a footer.
package trailer

import (
	"encoding/binary"
	"fmt"
	"io"
	"strconv"
)

const (
	id3v1Size     = 128
	apeFooterSize = 32
	apeHasHeader  = 1 << 31
	lyricsTail    = 15 // 6-digit size + "LYRICS200"
	id3v2Footer   = 10
)

// Start returns where the trailing tags of src[start:end) begin, or end if
// there are none. It peels complete tag blocks off the end of the range until
// none matches; a block that is incomplete or would reach before start is
// left in place, as audio.
func Start(src io.ReaderAt, start, end int64) (int64, error) {
	for {
		n, err := trailingTag(src, start, end)
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
func trailingTag(src io.ReaderAt, start, end int64) (int64, error) {
	for _, size := range []func(io.ReaderAt, int64, int64) (int64, error){id3v1Len, apeLen, lyrics3v2Len, id3v2FooterLen} {
		n, err := size(src, start, end)
		if err != nil || n > 0 {
			return n, err
		}
	}
	return 0, nil
}

func id3v1Len(src io.ReaderAt, start, end int64) (int64, error) {
	ok, err := hasAt(src, start, end, end-id3v1Size, "TAG")
	if !ok || err != nil {
		return 0, err
	}
	return id3v1Size, nil
}

func apeLen(src io.ReaderAt, start, end int64) (int64, error) {
	footer, err := read(src, start, end, end-apeFooterSize, apeFooterSize)
	if footer == nil || err != nil || string(footer[:8]) != "APETAGEX" {
		return 0, err
	}
	size := int64(binary.LittleEndian.Uint32(footer[12:16]))
	if binary.LittleEndian.Uint32(footer[20:24])&apeHasHeader != 0 {
		size += apeFooterSize
	}
	if size < apeFooterSize || size > end-start {
		return 0, nil
	}
	return size, nil
}

func lyrics3v2Len(src io.ReaderAt, start, end int64) (int64, error) {
	tail, err := read(src, start, end, end-lyricsTail, lyricsTail)
	if tail == nil || err != nil || string(tail[6:]) != "LYRICS200" {
		return 0, err
	}
	body, convErr := strconv.ParseInt(string(tail[:6]), 10, 64)
	if convErr != nil {
		return 0, nil //nolint:nilerr // a non-numeric size means this is not a Lyrics3v2 tag
	}
	size := body + lyricsTail
	ok, err := hasAt(src, start, end, end-size, "LYRICSBEGIN")
	if !ok || err != nil {
		return 0, err
	}
	return size, nil
}

func id3v2FooterLen(src io.ReaderAt, start, end int64) (int64, error) {
	footer, err := read(src, start, end, end-id3v2Footer, id3v2Footer)
	if footer == nil || err != nil || string(footer[:3]) != "3DI" {
		return 0, err
	}
	s := footer[6:10]
	size := int64(s[0]&0x7F)<<21 | int64(s[1]&0x7F)<<14 | int64(s[2]&0x7F)<<7 | int64(s[3]&0x7F)
	size += 2 * id3v2Footer // header and footer
	ok, err := hasAt(src, start, end, end-size, "ID3")
	if !ok || err != nil {
		return 0, err
	}
	return size, nil
}

// hasAt reports whether magic sits at off, inside [start, end).
func hasAt(src io.ReaderAt, start, end, off int64, magic string) (bool, error) {
	b, err := read(src, start, end, off, len(magic))
	return b != nil && string(b) == magic, err
}

// read returns n bytes at off, or nil when they do not fit inside [start, end).
func read(src io.ReaderAt, start, end, off int64, n int) ([]byte, error) {
	if off < start || off+int64(n) > end {
		return nil, nil
	}
	b := make([]byte, n)
	if _, err := src.ReadAt(b, off); err != nil {
		return nil, fmt.Errorf("read trailing tag at %d: %w", off, err)
	}
	return b, nil
}
