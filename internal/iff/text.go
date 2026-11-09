package iff

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	binutil "github.com/simonhull/audiometa/internal/binary"

	"github.com/simonhull/audiometa/internal/types"
)

// entry is one native text tag: a subchunk of a WAV LIST/INFO chunk, or an
// AIFF text chunk. Its id is its raw tag key.
type entry struct {
	id, value string
	// raw is the payload as stored in the source, written back unchanged
	// while the entry is not edited; nil for an entry a write sets.
	raw []byte
}

// infoIDs are the LIST/INFO subchunk IDs of the RIFF specification and the
// track IDs taggers add. A raw key among them is written to the INFO chunk,
// as is any key the file's INFO chunk already holds; other keys go to ID3.
var infoIDs = setOf(`
	IARL IART ICMS ICMT ICOP ICRD ICRP IDIM IDPI IENG IGNR IKEY ILGT IMED INAM IPLT IPRD
	ISBJ ISFT ISHP ISRC ISRF ITCH ITRK IPRT`)

// aiffTextIDs are the AIFF text chunks: name, author, copyright, annotation.
var aiffTextIDs = setOf("NAME AUTH ANNO")

// keyAIFFCopyright is the AIFF copyright chunk, whose ID has a space in it.
const keyAIFFCopyright = "(c) "

// setOf builds a set of the whitespace-separated words of s.
func setOf(s string) map[string]bool {
	m := map[string]bool{}
	for w := range strings.FieldsSeq(s) {
		m[w] = true
	}
	return m
}

// isAIFFText reports whether id is an AIFF text chunk.
func isAIFFText(id string) bool { return aiffTextIDs[id] || id == keyAIFFCopyright }

// readInfo returns the subchunks of the LIST/INFO chunk ch, in file order.
// A subchunk that is cut short or unreadable ends the list with a warning.
func (c *container) readInfo(ch chunk) ([]entry, error) {
	payload, err := c.payload(ch, "LIST/INFO chunk")
	if err != nil {
		return nil, err
	}
	if err := c.sr.ReadAt(payload, ch.data(), "LIST/INFO chunk"); err != nil {
		return nil, err
	}
	var entries []entry
	for off := 4; off < len(payload); {
		if len(payload)-off < chunkHeaderSize {
			break // Padding after the last subchunk.
		}
		id := payload[off : off+4]
		size64 := int64(c.order.Uint32(payload[off+4 : off+8]))
		start := off + chunkHeaderSize
		if !validID(id) || size64 > int64(len(payload)-start) {
			c.warn(ch.data()+int64(off), fmt.Sprintf("unreadable INFO subchunk %q; the rest of the list is ignored", id))
			c.infoDamaged = true
			break
		}
		size := int(size64)
		raw := payload[start : start+size]
		entries = append(entries, entry{id: string(id), value: decodeText(raw), raw: raw})
		off = start + size + size&1
	}
	return entries, nil
}

// readAIFFText returns the AIFF text chunks, in file order.
func (c *container) readAIFFText() ([]entry, error) {
	var entries []entry
	for _, ch := range c.chunks {
		if !isAIFFText(ch.id) {
			continue
		}
		b, err := c.payload(ch, ch.id+" chunk")
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry{id: ch.id, value: decodeText(b), raw: b})
	}
	return entries, nil
}

// payload reads the payload of a text chunk, refusing one too big to hold.
func (c *container) payload(ch chunk, what string) ([]byte, error) {
	n := c.present(ch)
	if n > binutil.MaxAllocBytes {
		return nil, corrupted(c.sr, ch.off, fmt.Sprintf("%s of %d bytes is too big", what, n))
	}
	if n == 0 {
		return []byte{}, nil // an empty chunk at the end of the file sits at the size, where reads are refused
	}
	b := make([]byte, n)
	if err := c.sr.ReadAt(b, ch.data(), what); err != nil {
		return nil, err
	}
	return b, nil
}

// decodeText returns the text of a native tag: up to its first NUL, as
// UTF-8 when it is valid UTF-8 and as Latin-1 otherwise, since the formats
// name no encoding and old tools wrote their code page.
func decodeText(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	if utf8.Valid(b) {
		return string(b)
	}
	runes := make([]rune, len(b))
	for i, c := range b {
		runes[i] = rune(c)
	}
	return string(runes)
}

// addRaw records the entries as raw tags, and falls back to the native comment
// when the ID3 tag has none. Every other field the entries can fill is bound
// to its key (types.FieldBindings), so BindFormat fills it, ID3 first.
func addRaw(file *types.File, entries []entry, commentID string) {
	for _, e := range entries {
		if !types.TakeValue(file) {
			break
		}
		file.Tags.AddRaw(e.id, e.value)
		if e.id == commentID && file.Tags.Comment == "" {
			file.Tags.Comment = e.value
		}
	}
}

// encodeInfo returns the payload of a LIST/INFO chunk holding entries: each a
// NUL-terminated subchunk, padded to an even length.
func encodeInfo(entries []entry, order byteOrder) []byte {
	out := []byte("INFO")
	for _, e := range entries {
		p := e.raw
		if p == nil {
			p = append([]byte(e.value), 0)
		}
		out = appendChunk(out, e.id, p, order)
	}
	return out
}

// appendChunk appends a chunk with payload p, and its pad byte, to b.
func appendChunk(b []byte, id string, p []byte, order byteOrder) []byte {
	b = append(b, id...)
	b = order.AppendUint32(b, uint32(len(p))) //nolint:gosec // G115: checkChunkSize bounds every payload
	b = append(b, p...)
	if len(p)%2 == 1 {
		b = append(b, 0)
	}
	return b
}

// setEntries replaces every entry of id with one entry per value, at the place
// of the first; with no values it removes them. A new id goes at the end.
// changed is false, and entries are returned as they are, when id already
// holds exactly values.
func setEntries(entries []entry, id string, values []string) (_ []entry, changed bool) {
	if slices.Equal(valuesOf(entries, id), values) {
		return entries, false
	}
	at := slices.IndexFunc(entries, func(e entry) bool { return e.id == id })
	entries = slices.DeleteFunc(entries, func(e entry) bool { return e.id == id })
	if at < 0 {
		at = len(entries)
	}
	add := make([]entry, len(values))
	for i, v := range values {
		add[i] = entry{id: id, value: v}
	}
	return slices.Insert(entries, at, add...), true
}

// valuesOf returns the values of id in entries, in order.
func valuesOf(entries []entry, id string) []string {
	var out []string
	for _, e := range entries {
		if e.id == id {
			out = append(out, e.value)
		}
	}
	return out
}

// has reports whether entries hold id.
func has(entries []entry, id string) bool {
	return slices.ContainsFunc(entries, func(e entry) bool { return e.id == id })
}
