// Package iff parses and writes the chunked containers of the IFF family: WAV
// (RIFF, its big-endian twin RIFX, and RF64 for files past 4 GiB) and AIFF
// (AIFF and AIFF-C).
//
// Audio properties come from the fmt chunk (WAV) or the COMM chunk (AIFF).
// Tags come from an embedded ID3v2 chunk ("id3 " or "ID3 "), read by the MP3
// package, and from the format's own text: the LIST/INFO chunk of a WAV file,
// and the NAME, AUTH, "(c) " and ANNO chunks of an AIFF file. Both are kept in
// the raw tags as stored; where both hold a field, ID3 wins.
//
// Writing puts the edited tags in the ID3v2 chunk and keeps the native text
// that maps onto an edited field in step. Every other chunk, the audio
// included, is copied byte for byte. It registers itself for
// [types.FormatWAV] and [types.FormatAIFF].
package iff

import (
	"context"
	"encoding/binary"
	"fmt"
	"slices"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// Sizes of the container's fixed parts.
const (
	headerSize      = 12 // magic, size and form type
	chunkHeaderSize = 8  // ID and size
	ds64MinSize     = 28 // RIFF size, data size, sample count, table length
	ds64EntrySize   = 12 // chunk ID and 64-bit size
	// maxChunks bounds the chunk list a hostile file can make us hold.
	maxChunks = 1 << 16
	// unsetSize is the 32-bit size a streaming writer leaves for "unknown",
	// and RF64's pointer to the ds64 chunk.
	unsetSize = 0xFFFFFFFF
)

// Container magic numbers and form types.
const (
	magicRIFF = "RIFF"
	magicRIFX = "RIFX"
	magicRF64 = "RF64"
	magicFORM = "FORM"
	formWAVE  = "WAVE"
	formAIFF  = "AIFF"
	formAIFC  = "AIFC"
)

// Chunk IDs.
const (
	idDS64    = "ds64"
	idData    = "data"
	idSSND    = "SSND"
	idLIST    = "LIST"
	idID3WAV  = "id3 "
	idID3AIFF = "ID3 "
)

// byteOrder is the byte order of a container's sizes.
type byteOrder interface {
	binary.ByteOrder
	binary.AppendByteOrder
}

// chunk is one top-level chunk.
type chunk struct {
	id       string
	listType string // the first four payload bytes of a LIST chunk
	off      int64  // offset of the chunk header
	size     int64  // payload size as declared (from ds64 for an RF64 chunk)
}

// data returns the offset of the chunk's payload.
func (c chunk) data() int64 { return c.off + chunkHeaderSize }

// container is the chunk list of one file.
type container struct {
	sr       *binutil.SafeReader
	size     int64 // of the file
	order    byteOrder
	magic    string
	formType string
	chunks   []chunk
	warnings []types.Warning
	// tail is where the form's chunks end. Anything from there to the end of
	// the file is not a chunk, and a write copies it as it is.
	tail int64
	// broken is set when a chunk inside the form is cut short or unreadable:
	// the file can still be read, but a rewrite could misplace data.
	broken bool
}

// ds64 is the RF64 chunk holding the 64-bit sizes.
type ds64 struct {
	riffSize, dataSize uint64
	table              map[string]uint64
}

// walk reads the container header and lists the chunks of the file.
//
// It returns a [*types.CorruptedFileError] when the file is not an IFF
// container at all, or an RF64 file lacks its ds64 chunk. Sloppy but usable
// structure (a wrong form size, a last chunk cut short, junk after the form)
// becomes a warning instead.
func walk(ctx context.Context, sr *binutil.SafeReader, size int64) (*container, error) {
	if size < headerSize {
		return nil, corrupted(sr, 0, "file too short for a container header")
	}
	var hdr [headerSize]byte
	if err := sr.ReadAt(hdr[:], 0, "container header"); err != nil {
		return nil, err
	}
	c := &container{sr: sr, size: size, magic: string(hdr[:4]), formType: string(hdr[8:12])}
	switch c.magic {
	case magicRIFF, magicRF64:
		c.order = binary.LittleEndian
	case magicRIFX, magicFORM:
		c.order = binary.BigEndian
	default:
		return nil, corrupted(sr, 0, fmt.Sprintf("unknown container %q", c.magic))
	}

	declared := chunkHeaderSize + int64(c.order.Uint32(hdr[4:8]))
	var ds *ds64
	if c.magic == magicRF64 {
		var err error
		if ds, err = readDS64(sr, c.order); err != nil {
			return nil, err
		}
		declared = chunkHeaderSize + int64(min(ds.riffSize, uint64(size))) //nolint:gosec // G115: bounded by the int64 file size
	}
	switch {
	case declared < headerSize:
		// A streaming writer that never came back to fill in the size.
		c.warn(4, fmt.Sprintf("%s size %d is unset; chunks are read to the end of the file", c.magic, declared-chunkHeaderSize))
		declared = size
	case declared > size:
		c.warn(4, fmt.Sprintf("%s form declares %d bytes but the file has %d", c.magic, declared, size))
	}

	if err := c.walkChunks(ctx, size, declared, ds); err != nil {
		return nil, err
	}
	return c, nil
}

// walkChunks lists the chunks from the end of the header. Inside the declared
// form a chunk that is cut short or unreadable stops the walk with a warning;
// past it, the same is taken for trailing data, which some taggers leave.
func (c *container) walkChunks(ctx context.Context, size, declared int64, ds *ds64) error {
	off := int64(headerSize)
	pastEnd := false
	for i := 0; off < size; i++ {
		if err := binutil.Canceled(ctx, i); err != nil {
			return err
		}
		inside := off < declared
		ch, ok, err := c.readChunk(off, size, inside, ds)
		if err != nil {
			return err
		}
		if !ok {
			break
		}
		if len(c.chunks) == maxChunks {
			c.warn(off, fmt.Sprintf("more than %d chunks; the rest are ignored", maxChunks))
			c.broken = true
			break
		}
		if !inside && !pastEnd {
			pastEnd = true
			c.warn(off, fmt.Sprintf("chunk %q lies past the declared end of the %s form", ch.id, c.magic))
		}
		c.chunks = append(c.chunks, ch)
		off = ch.data() + ch.size + ch.size&1
	}
	c.tail = min(off, size)
	return nil
}

// readChunk reads the chunk header at off. ok is false when the walk should
// stop there: the bytes are not a chunk, or the chunk is cut short. A
// truncated chunk inside the form is still returned, with ok false after it
// is appended, so its payload can be read up to the end of the file.
func (c *container) readChunk(off, size int64, inside bool, ds *ds64) (chunk, bool, error) {
	if size-off < chunkHeaderSize {
		if inside {
			c.warn(off, fmt.Sprintf("%d stray bytes at the end of the file", size-off))
		}
		return chunk{}, false, nil
	}
	var hdr [chunkHeaderSize]byte
	if err := c.sr.ReadAt(hdr[:], off, "chunk header"); err != nil {
		return chunk{}, false, err
	}
	ch := chunk{id: string(hdr[:4]), off: off, size: int64(c.order.Uint32(hdr[4:]))}
	if !validID(hdr[:4]) {
		if inside {
			c.warn(off, "unreadable chunk header; the rest of the file is ignored")
			c.broken = true
		}
		return chunk{}, false, nil
	}
	if ds != nil && ch.size == unsetSize {
		n, ok := ds.size(ch.id)
		if !ok {
			return chunk{}, false, corrupted(c.sr, off, fmt.Sprintf("RF64 chunk %q has no size in ds64", ch.id))
		}
		ch.size = int64(min(n, uint64(size))) //nolint:gosec // G115: bounded by the int64 file size
	}
	if ch.id == idLIST && ch.size >= 4 && ch.data()+4 <= size {
		var lt [4]byte
		if err := c.sr.ReadAt(lt[:], ch.data(), "LIST type"); err != nil {
			return chunk{}, false, err
		}
		ch.listType = string(lt[:])
	}
	if end := ch.data() + ch.size; end > size {
		if !inside {
			return chunk{}, false, nil
		}
		c.warn(off, fmt.Sprintf("chunk %q is cut short: %d of %d bytes", ch.id, max(size-ch.data(), 0), ch.size))
		c.broken = true
		c.chunks = append(c.chunks, ch)
		return chunk{}, false, nil
	}
	return ch, true, nil
}

// readDS64 reads the ds64 chunk that must follow an RF64 header.
func readDS64(sr *binutil.SafeReader, order byteOrder) (*ds64, error) {
	var hdr [chunkHeaderSize + ds64MinSize]byte
	if err := sr.ReadAt(hdr[:], headerSize, "ds64 chunk"); err != nil {
		return nil, corrupted(sr, headerSize, "RF64 file without a ds64 chunk")
	}
	if string(hdr[:4]) != idDS64 {
		return nil, corrupted(sr, headerSize, "RF64 file without a ds64 chunk")
	}
	size := int64(order.Uint32(hdr[4:8]))
	if size < ds64MinSize {
		return nil, corrupted(sr, headerSize, fmt.Sprintf("ds64 chunk of %d bytes is too short", size))
	}
	p := hdr[chunkHeaderSize:]
	ds := &ds64{riffSize: order.Uint64(p[0:]), dataSize: order.Uint64(p[8:]), table: map[string]uint64{}}
	entries := int64(order.Uint32(p[24:]))
	entries = min(entries, (size-ds64MinSize)/ds64EntrySize)
	off := int64(headerSize + chunkHeaderSize + ds64MinSize)
	for range entries {
		var e [ds64EntrySize]byte
		if err := sr.ReadAt(e[:], off, "ds64 table entry"); err != nil {
			return nil, err
		}
		ds.table[string(e[:4])] = order.Uint64(e[4:])
		off += ds64EntrySize
	}
	return ds, nil
}

// size returns the 64-bit size ds64 gives the chunk id.
func (d *ds64) size(id string) (uint64, bool) {
	if id == idData {
		return d.dataSize, true
	}
	n, ok := d.table[id]
	return n, ok
}

// validID reports whether b is a chunk ID: four printable ASCII characters.
func validID(b []byte) bool {
	for _, c := range b {
		if c < ' ' || c > '~' {
			return false
		}
	}
	return true
}

// first returns the first chunk with one of the ids, or false.
func (c *container) first(ids ...string) (chunk, bool) {
	for _, ch := range c.chunks {
		if slices.Contains(ids, ch.id) {
			return ch, true
		}
	}
	return chunk{}, false
}

// infoIndex returns the index of the first LIST chunk of type INFO, or -1.
func (c *container) infoIndex() int {
	return slices.IndexFunc(c.chunks, func(ch chunk) bool { return ch.id == idLIST && ch.listType == "INFO" })
}

// present returns how many of the chunk's payload bytes the file holds.
func (c *container) present(ch chunk) int64 {
	return max(min(ch.size, c.size-ch.data()), 0)
}

// warn records a structural warning at off.
func (c *container) warn(off int64, msg string) {
	c.warnings = types.AppendWarning(c.warnings, types.Warning{Stage: types.StageMetadata, Offset: off, Message: msg})
}

// corrupted returns a CorruptedFileError at off.
func corrupted(sr *binutil.SafeReader, off int64, reason string) error {
	return &types.CorruptedFileError{Path: sr.Path(), Offset: off, Reason: reason}
}
