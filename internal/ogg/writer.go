package ogg

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	ibinary "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/registry"
	"github.com/simonhull/audiometa/internal/types"
	"github.com/simonhull/audiometa/internal/vorbis"
	"github.com/simonhull/audiometa/internal/writeutil"
)

// writer rewrites the comment header of an Ogg Vorbis or Opus file. It
// implements [registry.TagWriter], [registry.InPlaceWriter] and
// [registry.PreservedRanger].
//
// Only the comment packet changes: the identification page is copied as it is,
// the comment packet (with the setup packet after it, for Vorbis) is laid out
// on header pages, and every later page keeps its payload, granule position,
// flags and serial number byte for byte. When the edited comment packet is as
// long as the old one the header pages keep their layout, so only payload bytes
// and checksums change; otherwise the packets are paginated afresh. When the
// new header pages end at the sequence number the old ones did, the later pages
// are copied untouched; otherwise their sequence numbers and checksums are
// recomputed, nothing else. Bytes after the last whole page are copied through.
//
// The vendor string and any data after the comments are kept. The writer adds
// no padding: an edit that grows the comment header grows the file. An Opus
// packet whose data after the comments is padding (RFC 7845 §5.2: it starts
// with a byte whose lowest bit is 0) keeps its length when the edited comments
// still fit in it, the padding absorbing the change; data with the lowest bit
// set is binary data and is kept as it is. Chained and multiplexed files, which
// hold more than one logical stream, are refused with
// [types.ErrUnsupportedWrite]. CHAPTERnnn keys are not editable, as for FLAC.
//
// The preserved bytes are everything but the header pages and the sequence
// numbers and checksums of the pages after them (see [writer.PreservedRanges]).
// A Vorbis setup packet, which a rewrite moves, is not among them: the writer
// never parses it and carries it from memory to its new pages.
type writer struct {
	digester

	codec *codec
}

// rewrite is the plan for one WriteFile: which pages replace the old header
// pages and how the rest of the file follows them.
type rewrite struct {
	l     *layout
	pages []*oggPage // the new header pages after page 0
	// same is true when the edited comment packet equals the old one: the file
	// is copied as it is, with whatever page layout its encoder chose.
	same bool
	// sameLayout is true when the edited comment packet is as long as the old
	// one: pages holds the old header pages with new payload and checksums.
	sameLayout bool
	// keepsNumbers is true when the new header pages end at the sequence number
	// the old ones did: every later page is copied untouched.
	keepsNumbers bool
}

// plan reads src, applies edits to its comment header and decides the new page
// layout. It validates the whole file: it returns before any output exists.
func (w *writer) plan(ctx context.Context, src io.ReaderAt, size int64, edits *types.EditSet) (*rewrite, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	l, err := readLayout(ctx, src, size, "")
	if err != nil {
		return nil, err
	}
	if l.codec != w.codec {
		return nil, corruptedAt("", 0, "file is not an "+w.codec.idName+" stream")
	}
	if err := scanStreams(ctx, src, size, l.audioStart, l.serial); err != nil {
		return nil, err
	}
	comment, err := w.editedComment(ctx, l, edits)
	if err != nil {
		return nil, err
	}
	r := &rewrite{l: l, same: bytes.Equal(comment, l.packets[1])}
	if r.same {
		return r, nil
	}
	packets := append([][]byte{comment}, l.packets[2:]...)
	if len(comment) == len(l.packets[1]) {
		r.pages, r.sameLayout, r.keepsNumbers = relayout(l, packets), true, true
		return r, nil
	}
	r.pages = paginate(packets, l.serial, l.firstSeq+1, l.eos)
	headerBytes := l.idEnd
	for _, p := range r.pages {
		headerBytes += int64(pageFixedHeader + len(p.Lacing) + len(p.Data))
	}
	if headerBytes > maxHeaderBytes {
		return nil, fmt.Errorf("%w: the comment header would take %d bytes of pages, more than the %d a reader accepts", types.ErrUnsupportedWrite, headerBytes, maxHeaderBytes)
	}
	r.keepsNumbers = len(r.pages) == len(l.pages) && r.pages[len(r.pages)-1].SequenceNumber == l.lastSeq
	return r, nil
}

// editedComment returns the comment packet of l with edits applied.
func (w *writer) editedComment(ctx context.Context, l *layout, edits *types.EditSet) ([]byte, error) {
	packet, magic := l.packets[1], w.codec.commentMagic
	if !bytes.HasPrefix(packet, []byte(magic)) {
		return nil, corruptedAt("", l.idEnd, "the comment header does not start with its "+w.codec.commentName+" marker")
	}
	c, err := vorbis.Split(ctx, packet[len(magic):])
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, corruptedAt("", l.idEnd, "malformed "+w.codec.commentName+": "+err.Error())
	}
	v := vorbis.NewEditor(w.codec.format, c)
	for _, e := range edits.Keys {
		if err := v.ApplyKey(e.Key, e.Values); err != nil {
			return nil, err
		}
	}
	for _, e := range edits.Fields {
		if err := v.ApplyField(e, edits); err != nil {
			return nil, err
		}
	}
	if err := applyArtwork(v, edits.Artwork); err != nil {
		return nil, err
	}
	out := append([]byte(magic), v.Encode()...)
	if bytes.Equal(out, packet) || w.codec != opusCodec || len(c.Tail) == 0 || c.Tail[0]&1 != 0 {
		return out, nil
	}
	// The data after the comments is padding: it absorbs the change in length.
	core := vorbis.Comments{Vendor: v.Vendor, Entries: v.Entries}
	v.Tail = opusPadding(len(packet) - len(magic) - len(core.Encode()))
	return append([]byte(magic), v.Encode()...), nil
}

// opusPadding returns n zero bytes, or nil when n is not positive.
func opusPadding(n int) []byte {
	if n <= 0 {
		return nil
	}
	return make([]byte, n)
}

// WriteFile streams src with edits applied to out. It validates src before it
// writes a byte; see [registry.TagWriter].
func (w *writer) WriteFile(ctx context.Context, out io.Writer, src io.ReaderAt, size int64, edits *types.EditSet) (int64, error) {
	r, err := w.plan(ctx, src, size, edits)
	if err != nil {
		return 0, err
	}
	cw := &writeutil.CountingWriter{W: out}
	err = r.write(ctx, cw, src, size)
	return cw.N, err
}

// flushSize is how much re-encoded page data accumulates before it is written.
const flushSize = 256 << 10

// write streams the new file to cw.
func (r *rewrite) write(ctx context.Context, cw *writeutil.CountingWriter, src io.ReaderAt, size int64) error {
	if r.same {
		_, err := writeutil.CopyRange(ctx, cw, src, 0, size)
		return err
	}
	if _, err := writeutil.CopyRange(ctx, cw, src, 0, r.l.idEnd); err != nil {
		return err
	}
	var buf []byte
	for _, p := range r.pages {
		buf = appendPage(buf, p)
	}
	if _, err := cw.Write(buf); err != nil {
		return err
	}
	if r.keepsNumbers {
		_, err := writeutil.CopyRange(ctx, cw, src, r.l.audioStart, size-r.l.audioStart)
		return err
	}
	return r.renumber(ctx, cw, src, size, buf[:0])
}

// renumber copies the pages after the header pages to cw, numbering them on
// from the last new header page and recomputing their checksums. Bytes that
// are not a page are copied as they are. buf is scratch space.
func (r *rewrite) renumber(ctx context.Context, cw *writeutil.CountingWriter, src io.ReaderAt, size int64, buf []byte) error {
	s := newPageStreamAt(src, size, "", true, r.l.audioStart)
	seq := r.pages[len(r.pages)-1].SequenceNumber
	for i := 0; s.off < size; i++ {
		if err := ibinary.Canceled(ctx, i); err != nil {
			return err
		}
		page, _, err := s.next()
		if err != nil {
			break // not a page: the rest is trailing data
		}
		seq++
		page.SequenceNumber = seq
		if buf = appendPage(buf, page); len(buf) >= flushSize {
			if _, err := cw.Write(buf); err != nil {
				return err
			}
			buf = buf[:0]
		}
	}
	if _, err := cw.Write(buf); err != nil {
		return err
	}
	_, err := writeutil.CopyRange(ctx, cw, src, s.off, size-s.off)
	return err
}

// Offsets in a page of the bytes that carry its sequence number and checksum:
// the page header's fields before and after them are preserved.
const (
	pageHeaderTypeOffset = 5  // start of the header type, granule position and serial number
	pageSerialEnd        = 18 // end of the serial number, where the sequence number starts
	pageCRCEnd           = 26 // end of the checksum, where the segment count starts
)

// PreservedRanges returns what a rewrite must not change, in file order: page
// 0 as stored; for every page after the header pages, its header type,
// granule position and serial number, then its segment table and payload,
// which leaves out the sequence number and the checksum; then any bytes after
// the last whole page. A rewrite changes those two fields of every later page
// when the header pages' count changes, and replaces the header pages.
//
// It reads page headers only. It returns an error wrapping
// [types.ErrUnsupportedWrite] when the file holds a second logical stream.
func (w *writer) PreservedRanges(ctx context.Context, src io.ReaderAt, size int64) ([]registry.Range, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	l, err := readLayout(ctx, src, size, "")
	if err != nil {
		return nil, err
	}
	ranges := []registry.Range{{Off: 0, End: l.idEnd}}
	end, err := walkPages(ctx, src, size, l.audioStart, l.serial, func(p pageSpan) {
		ranges = append(ranges,
			registry.Range{Off: p.start + pageHeaderTypeOffset, End: p.start + pageSerialEnd},
			registry.Range{Off: p.start + pageCRCEnd, End: p.end})
	})
	if err != nil {
		return nil, err
	}
	if end < size {
		ranges = append(ranges, registry.Range{Off: end, End: size})
	}
	return ranges, nil
}

// PreservedDigest returns the SHA-256 of the bytes [writer.PreservedRanges]
// lists, hashed as one stream. It reads the whole file in one pass.
func (w *writer) PreservedDigest(ctx context.Context, src io.ReaderAt, size int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	l, err := readLayout(ctx, src, size, "")
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	if _, err := writeutil.CopyRange(ctx, h, src, 0, l.idEnd); err != nil {
		return nil, err
	}
	s := newPageStreamAt(src, size, "", true, l.audioStart)
	for i := 0; s.off < size; i++ {
		if err := ibinary.Canceled(ctx, i); err != nil {
			return nil, err
		}
		page, _, err := s.next()
		if err != nil {
			break // not a page: the rest is trailing data
		}
		if page.SerialNumber != l.serial {
			return nil, errMultiStream
		}
		var fixed [14]byte
		fixed[0] = page.HeaderType
		binary.LittleEndian.PutUint64(fixed[1:9], uint64(page.GranulePosition)) //nolint:gosec // G115: the granule position is a signed field; the cast is its bit pattern
		binary.LittleEndian.PutUint32(fixed[9:13], page.SerialNumber)
		fixed[13] = byte(len(page.Lacing)) //nolint:gosec // G115: a page has at most 255 lacing values
		h.Write(fixed[:])
		h.Write(page.Lacing)
		h.Write(page.Data)
	}
	if _, err := writeutil.CopyRange(ctx, h, src, s.off, size-s.off); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// InPlacePatch returns the header pages to write over the old ones when the
// edited comment packet is as long as the old one, so that the pages keep their
// layout and only payload bytes and checksums change. The patch covers the
// first to the last page that changes, and applying it gives what WriteFile
// writes. It returns nil when the edits change the packet's length.
func (w *writer) InPlacePatch(ctx context.Context, src io.ReaderAt, size int64, edits *types.EditSet) (*registry.Patch, error) {
	r, err := w.plan(ctx, src, size, edits)
	if err != nil {
		return nil, err
	}
	l := r.l
	switch {
	case r.same:
		return &registry.Patch{Off: l.idEnd}, nil // nothing changes
	case !r.sameLayout:
		return nil, nil
	}
	old := make([]byte, l.audioStart-l.idEnd)
	if n, err := src.ReadAt(old, l.idEnd); n < len(old) {
		if err == nil || errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return nil, fmt.Errorf("read the header pages: %w", err)
	}
	var fresh []byte
	for _, p := range r.pages {
		fresh = appendPage(fresh, p)
	}
	if len(fresh) != len(old) {
		return nil, nil // not reached: the pages keep their lacing
	}
	first, last := -1, -1
	for i, hp := range l.pages {
		a, b := hp.start-l.idEnd, hp.end-l.idEnd
		if !bytes.Equal(old[a:b], fresh[a:b]) {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 {
		return &registry.Patch{Off: l.idEnd}, nil
	}
	a, b := l.pages[first].start-l.idEnd, l.pages[last].end-l.idEnd
	return &registry.Patch{Off: l.pages[first].start, Data: fresh[a:b]}, nil
}

var (
	_ registry.TagWriter       = (*writer)(nil)
	_ registry.InPlaceWriter   = (*writer)(nil)
	_ registry.PreservedRanger = (*writer)(nil)
)

func init() {
	registry.RegisterWriter(types.FormatOgg, &writer{codec: vorbisCodec})
	registry.RegisterWriter(types.FormatOpus, &writer{codec: opusCodec})
}
