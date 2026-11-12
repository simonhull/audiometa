package ogg

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash"
	"io"

	ibinary "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/registry"
	"github.com/simonhull/audiometa/internal/types"
	"github.com/simonhull/audiometa/internal/vorbis"
	"github.com/simonhull/audiometa/internal/writeutil"
)

// writer rewrites the comment header of an Ogg Vorbis or Opus file. It
// implements [registry.TagWriter].
//
// Only the comment packet changes: the identification page is copied as it is,
// the comment packet (with the setup packet after it, for Vorbis) is laid out
// on fresh pages, and every later page keeps its payload, granule position,
// flags and serial number byte for byte. When the new header pages are as many
// as the old ones, the later pages are copied untouched; otherwise their
// sequence numbers and checksums are recomputed, nothing else. Bytes after the
// last readable page are copied through.
//
// The vendor string and any data after the comments are kept, so Opus padding
// and binary data survive. The writer adds none: an edit that grows the
// comment header grows the file. Chained and multiplexed files, which hold
// more than one logical stream, are refused with [types.ErrUnsupportedWrite].
// CHAPTERnnn keys are not editable, as for FLAC.
//
// The writer does not implement [registry.PreservedRanger]. A rewrite that
// changes the number of header pages renumbers every later page, so those
// pages' sequence numbers and checksums, which sit between the bytes that must
// be preserved, move. [writer.PreservedDigest] therefore hashes the logical
// content (see there) rather than byte ranges of the file.
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
	r.pages = paginate(packets, l.serial, l.firstSeq+1, l.eos)
	headerBytes := l.idEnd
	for _, p := range r.pages {
		headerBytes += int64(pageFixedHeader + len(p.Lacing) + len(p.Data))
	}
	if headerBytes > maxHeaderBytes {
		return nil, fmt.Errorf("%w: the comment header would take %d bytes of pages, more than the %d a reader accepts", types.ErrUnsupportedWrite, headerBytes, maxHeaderBytes)
	}
	r.keepsNumbers = len(r.pages) == l.headerPages && r.pages[len(r.pages)-1].SequenceNumber == l.lastSeq
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
	return append([]byte(magic), v.Encode()...), nil
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

// PreservedDigest returns the SHA-256 of what a rewrite must not change:
// page 0 as stored; for Vorbis, the setup packet; then, for every page of the
// stream after its header pages, its granule position, flags, serial number,
// segment table and payload; then any bytes after the last readable page. It
// leaves out the sequence numbers and checksums of the later pages, which a
// rewrite changes when the header pages' count does, and the page layout of
// the header packets, which a rewrite replaces.
//
// It returns an error wrapping [types.ErrUnsupportedWrite] when the file holds
// a second logical stream.
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
	for _, p := range l.packets[2:] {
		writeLengthPrefixed(h, p)
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
		binary.LittleEndian.PutUint64(fixed[:8], uint64(page.GranulePosition)) //nolint:gosec // G115: the granule position is a signed field; the cast is its bit pattern
		fixed[8] = page.HeaderType
		binary.LittleEndian.PutUint32(fixed[9:13], page.SerialNumber)
		fixed[13] = byte(len(page.Lacing)) //nolint:gosec // G115: a page has at most 255 lacing values
		h.Write(fixed[:])
		h.Write(page.Lacing)
		h.Write(page.Data)
	}
	h.Write([]byte{0xFF}) // end of pages
	if _, err := writeutil.CopyRange(ctx, h, src, s.off, size-s.off); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// writeLengthPrefixed hashes b preceded by its length, so adjacent items
// cannot be confused.
func writeLengthPrefixed(h hash.Hash, b []byte) {
	var n [8]byte
	binary.LittleEndian.PutUint64(n[:], uint64(len(b)))
	_, _ = h.Write(n[:])
	_, _ = h.Write(b)
}

func init() {
	registry.RegisterWriter(types.FormatOpus, &writer{codec: opusCodec})
}
