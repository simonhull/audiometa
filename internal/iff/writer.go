package iff

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/simonhull/audiometa/internal/mp3"
	"github.com/simonhull/audiometa/internal/types"
	"github.com/simonhull/audiometa/internal/writeutil"
)

// maxFormSize is the largest form a 32-bit size field can declare.
const maxFormSize = unsetSize - 1

// writer rewrites the tags of a WAV or AIFF file. It implements
// registry.TagWriter.
//
// Edits go to the ID3v2 chunk, which keeps its version (a file without one gets
// ID3v2.4, appended as the last chunk), except raw keys that are native text:
// on WAV the INFO IDs, and any key the file's LIST/INFO chunk already holds; on
// AIFF the NAME, AUTH, "(c) " and ANNO chunks. Those are written where they
// are. A typed-field edit also rewrites the native text that holds the same
// field, so another reader of it never sees a stale value: on WAV INAM, IART,
// IPRD, IGNR, ICRD, ITRK (or IPRT) and ICMT, on AIFF NAME, AUTH and ANNO. It
// takes the value the new ID3 tag holds, and writes it only when the file has
// native text already, so a file without any gets none.
//
// Every other chunk is copied byte for byte, in order, with its pad byte
// zeroed; so is anything after the form. The form's size, and the RF64 ds64
// RIFF size, are recomputed. A file whose chunks are cut short or unreadable
// is refused, since a rewrite could move audio into a tag.
type writer struct {
	format types.Format
}

// rewrite is a planned rewrite of one container.
type rewrite struct {
	c       *container
	id3     []byte  // the new ID3 tag; nil leaves the file without one
	id3ID   string  // the chunk ID it goes in
	id3At   int     // index of the source's ID3 chunk, or -1
	text    []entry // the new native text tags
	textAt  int     // index of the source's INFO chunk or first AIFF text chunk, or -1
	keepID3 bool    // the edits leave the ID3 chunk as it is
}

// WriteFile streams src with edits applied to out.
func (w *writer) WriteFile(ctx context.Context, out io.Writer, src io.ReaderAt, size int64, edits *types.EditSet) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	c, err := w.open(ctx, src, size)
	if err != nil {
		return 0, err
	}
	rw, err := w.plan(ctx, c, src, edits)
	if err != nil {
		return 0, err
	}
	return rw.emit(ctx, out, src)
}

// open walks src and refuses a container a rewrite could damage.
func (w *writer) open(ctx context.Context, src io.ReaderAt, size int64) (*container, error) {
	c, err := (&parser{format: w.format}).open(ctx, src, size, "")
	if err != nil {
		return nil, err
	}
	if c.broken {
		return nil, corrupted(c.sr, 0, "chunks are cut short or unreadable; the file cannot be rewritten safely")
	}
	return c, nil
}

// plan applies edits to the source's tags.
func (w *writer) plan(ctx context.Context, c *container, src io.ReaderAt, edits *types.EditSet) (*rewrite, error) {
	text, _, err := c.text()
	if err != nil {
		return nil, err
	}
	rw := newRewrite(c)
	id3Edits := &types.EditSet{Fields: edits.Fields, Artwork: edits.Artwork, Orig: edits.Orig, Final: edits.Final}
	textKeys := map[string]bool{}
	for _, e := range edits.Keys {
		if !w.isTextKey(e.Key, text) {
			id3Edits.Keys = append(id3Edits.Keys, e)
			continue
		}
		if err := checkText(e.Key, e.Values); err != nil {
			return nil, err
		}
		text = setEntries(text, e.Key, e.Values)
		textKeys[e.Key] = true
	}

	if err := rw.editID3(ctx, src, id3Edits); err != nil {
		return nil, err
	}
	if rw.textAt >= 0 && len(edits.Fields) > 0 {
		if text, err = rw.mirror(ctx, w.format, text, edits.Fields, textKeys); err != nil {
			return nil, err
		}
	}
	rw.text = text
	return rw, nil
}

// newRewrite locates the tag chunks of c.
func newRewrite(c *container) *rewrite {
	rw := &rewrite{c: c, id3ID: idID3WAV, id3At: -1, textAt: -1}
	if c.magic == magicFORM {
		rw.id3ID = idID3AIFF
	}
	for i, ch := range c.chunks {
		switch {
		case rw.id3At < 0 && (ch.id == idID3WAV || ch.id == idID3AIFF):
			rw.id3At, rw.id3ID = i, ch.id
		case rw.textAt < 0 && rw.isText(i):
			rw.textAt = i
		}
	}
	return rw
}

// editID3 builds the new ID3 tag, or keeps the old one when no edit touches it.
func (rw *rewrite) editID3(ctx context.Context, src io.ReaderAt, edits *types.EditSet) error {
	if edits.Empty() && rw.id3At >= 0 {
		rw.keepID3 = true
		return nil
	}
	var (
		r io.ReaderAt
		n int64
	)
	if tag, ok := rw.c.id3(src); ok {
		r, n = tag, tag.Size()
	}
	tag, err := mp3.EditEmbeddedTag(ctx, r, n, edits)
	if err != nil {
		return fmt.Errorf("ID3 chunk: %w", err)
	}
	rw.id3 = tag
	return nil
}

// isText reports whether chunk i of the source holds native text tags.
func (rw *rewrite) isText(i int) bool {
	ch := rw.c.chunks[i]
	if rw.c.magic == magicFORM {
		return isAIFFText(ch.id)
	}
	info, ok := rw.c.infoList()
	return ok && ch.off == info.off
}

// isTextKey reports whether a raw key edit goes to the native text tags.
func (w *writer) isTextKey(key string, text []entry) bool {
	if w.format == types.FormatAIFF {
		return isAIFFText(key)
	}
	return infoIDs[key] || has(text, key)
}

// checkText refuses a native text value a reader would cut short.
func checkText(key string, values []string) error {
	for _, v := range values {
		if strings.IndexByte(v, 0) >= 0 {
			return fmt.Errorf("%w: %s value holds a NUL", types.ErrUnsupportedWrite, strings.TrimSpace(key))
		}
	}
	return nil
}

// mirror is native text that holds a typed field.
type mirror struct {
	field types.Field
	ids   []string // the native keys, the one written to a file without any first
	value func(tag *types.Tags, old string) []string
}

// The mirrors of each format.
var (
	wavMirrors = []mirror{
		{types.FieldTitle, []string{"INAM"}, frame("TIT2")},
		{types.FieldArtist, []string{"IART"}, frame("TPE1")},
		{types.FieldArtists, []string{"IART"}, frame("TPE1")},
		{types.FieldAlbum, []string{"IPRD"}, frame("TALB")},
		{types.FieldGenres, []string{"IGNR"}, frame("TCON")},
		{types.FieldYear, []string{"ICRD"}, year},
		{types.FieldTrackNumber, []string{"ITRK", "IPRT"}, track},
		{types.FieldComment, []string{keyICMT}, comment},
	}
	aiffMirrors = []mirror{
		{types.FieldTitle, []string{"NAME"}, frame("TIT2")},
		{types.FieldArtist, []string{"AUTH"}, frame("TPE1")},
		{types.FieldArtists, []string{"AUTH"}, frame("TPE1")},
		{types.FieldComment, []string{keyANNO}, comment},
	}
)

// frame mirrors the values of an ID3 frame.
func frame(id string) func(*types.Tags, string) []string {
	return func(t *types.Tags, _ string) []string { return t.Get(id) }
}

// year mirrors the year, keeping whatever follows the year in the old date.
func year(t *types.Tags, old string) []string {
	if t.Year == 0 {
		return nil
	}
	y := fmt.Sprintf("%04d", t.Year)
	if len(old) > 4 && strings.Trim(old[:4], "0123456789") == "" {
		return []string{y + old[4:]}
	}
	return []string{y}
}

// track mirrors the track number. Native text holds no total.
func track(t *types.Tags, _ string) []string {
	if t.TrackNumber == 0 {
		return nil
	}
	return []string{strconv.Itoa(t.TrackNumber)}
}

// comment mirrors the comment.
func comment(t *types.Tags, _ string) []string {
	if t.Comment == "" {
		return nil
	}
	return []string{t.Comment}
}

// mirror rewrites the native text of every edited field from the new ID3 tag.
// A native key that a raw key edit set is left as that edit made it.
func (rw *rewrite) mirror(ctx context.Context, format types.Format, text []entry, fields []types.FieldEdit, skip map[string]bool) ([]entry, error) {
	tag := &types.File{}
	if err := mp3.ReadEmbeddedTag(ctx, bytes.NewReader(rw.id3), int64(len(rw.id3)), "", tag); err != nil {
		return nil, err
	}
	mirrors := wavMirrors
	if format == types.FormatAIFF {
		mirrors = aiffMirrors
	}
	for _, e := range fields {
		for _, m := range mirrors {
			if m.field != e.Field {
				continue
			}
			ids := presentIDs(text, m.ids)
			if len(ids) == 0 {
				ids = m.ids[:1]
			}
			for _, id := range ids {
				if skip[id] {
					continue
				}
				text = setEntries(text, id, m.value(&tag.Tags, firstValue(text, id)))
			}
		}
	}
	return text, nil
}

// presentIDs returns the ids that text holds.
func presentIDs(text []entry, ids []string) []string {
	var out []string
	for _, id := range ids {
		if has(text, id) {
			out = append(out, id)
		}
	}
	return out
}

// firstValue returns the first value of id in text, or "".
func firstValue(text []entry, id string) string {
	for _, e := range text {
		if e.id == id {
			return e.value
		}
	}
	return ""
}

// piece is part of the output: bytes, or a range of the source.
type piece struct {
	b      []byte
	off, n int64
}

func (p piece) len() int64 {
	if p.b != nil {
		return int64(len(p.b))
	}
	return p.n
}

// emit writes the new file.
func (rw *rewrite) emit(ctx context.Context, out io.Writer, src io.ReaderAt) (int64, error) {
	c := rw.c
	pieces, ds64At, err := rw.pieces()
	if err != nil {
		return 0, err
	}
	form := int64(4)
	for _, p := range pieces {
		form += p.len()
	}
	hdr := []byte(c.magic)
	switch {
	case c.magic == magicRF64:
		hdr = c.order.AppendUint32(hdr, unsetSize)
		if ds64At >= 0 {
			pieces[ds64At].b = c.order.AppendUint64(pieces[ds64At].b[:chunkHeaderSize], uint64(form))
		}
	case form > maxFormSize:
		return 0, fmt.Errorf("%w: the file would pass the 4 GiB a %s form can hold", types.ErrTagTooLarge, c.magic)
	default:
		hdr = c.order.AppendUint32(hdr, uint32(form))
	}
	hdr = append(hdr, c.formType...)

	cw := &writeutil.CountingWriter{W: out}
	if _, err := cw.Write(hdr); err != nil {
		return cw.N, err
	}
	pieces = append(pieces, piece{off: c.tail, n: c.size - c.tail})
	for _, p := range pieces {
		if p.b != nil {
			if _, err := cw.Write(p.b); err != nil {
				return cw.N, err
			}
			continue
		}
		if _, err := writeutil.CopyRange(ctx, cw, src, p.off, p.n); err != nil {
			return cw.N, err
		}
	}
	return cw.N, nil
}

// pieces lists the chunks of the new form. ds64At is the index of the piece
// holding the ds64 header and RIFF size, which emit fills in, or -1.
func (rw *rewrite) pieces() ([]piece, int, error) {
	var pieces []piece
	ds64At := -1
	textDone := false
	for i, ch := range rw.c.chunks {
		switch {
		case i == rw.id3At:
			pieces = append(pieces, rw.id3Pieces(ch)...)
		case rw.isText(i):
			if !textDone {
				textDone = true
				pieces = append(pieces, piece{b: rw.textChunks()})
			}
		case rw.isDS64(i):
			ds64At = len(pieces)
			b := make([]byte, chunkHeaderSize+8)
			copyHeader(b, ch, rw.c.order)
			pieces = append(pieces, piece{b: b}, piece{off: ch.data() + 8, n: ch.size - 8})
		default:
			pieces = append(pieces, verbatim(ch)...)
		}
	}
	if !textDone && len(rw.text) > 0 {
		pieces = append(pieces, piece{b: rw.textChunks()})
	}
	if rw.id3At < 0 && rw.id3 != nil {
		pieces = append(pieces, piece{b: appendChunk(nil, rw.id3ID, rw.id3, rw.c.order)})
	}
	for _, p := range pieces {
		if int64(len(p.b)) > maxFormSize {
			return nil, -1, fmt.Errorf("%w: a chunk would pass 4 GiB", types.ErrTagTooLarge)
		}
	}
	return pieces, ds64At, nil
}

// id3Pieces is the ID3 chunk of the new form: the old one, the new tag, or
// nothing.
func (rw *rewrite) id3Pieces(ch chunk) []piece {
	switch {
	case rw.keepID3:
		return verbatim(ch)
	case rw.id3 != nil:
		return []piece{{b: appendChunk(nil, rw.id3ID, rw.id3, rw.c.order)}}
	}
	return nil
}

// isDS64 reports whether chunk i is the ds64 chunk of an RF64 file.
func (rw *rewrite) isDS64(i int) bool {
	return i == 0 && rw.c.magic == magicRF64 && rw.c.chunks[0].id == idDS64
}

// verbatim copies ch from the source, followed by a zero pad byte when its
// size is odd: the source may lack one at its end.
func verbatim(ch chunk) []piece {
	p := []piece{{off: ch.off, n: chunkHeaderSize + ch.size}}
	if ch.size%2 == 1 {
		p = append(p, piece{b: []byte{0}})
	}
	return p
}

// copyHeader writes the chunk header of ch into b.
func copyHeader(b []byte, ch chunk, order byteOrder) {
	copy(b, ch.id)
	order.PutUint32(b[4:], uint32(ch.size)) //nolint:gosec // G115: ds64 is far below 4 GiB
}

// textChunks encodes the native text tags: one LIST/INFO chunk on WAV, a
// chunk per tag on AIFF. No tags give no chunk.
func (rw *rewrite) textChunks() []byte {
	if len(rw.text) == 0 {
		return []byte{}
	}
	if rw.c.magic != magicFORM {
		return appendChunk(nil, idLIST, encodeInfo(rw.text, rw.c.order), rw.c.order)
	}
	var b []byte
	for _, e := range rw.text {
		b = appendChunk(b, e.id, []byte(e.value), rw.c.order)
	}
	return b
}

// AudioDigest returns the SHA-256 of the samples: the payload of the WAV data
// chunk, or of the AIFF SSND chunk after its offset and block size.
func (w *writer) AudioDigest(ctx context.Context, src io.ReaderAt, size int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c, err := (&parser{format: w.format}).open(ctx, src, size, "")
	if err != nil {
		return nil, err
	}
	id, skip := idData, int64(0)
	if w.format == types.FormatAIFF {
		id, skip = idSSND, ssndHeaderSize
	}
	ch, ok := c.first(id)
	if !ok {
		return writeutil.DigestRange(ctx, src, 0, 0)
	}
	n := c.present(ch) - skip
	if n <= 0 {
		return writeutil.DigestRange(ctx, src, 0, 0)
	}
	return writeutil.DigestRange(ctx, src, ch.data()+skip, n)
}

// PreservedDigest returns the SHA-256 of every byte WriteFile copies: each
// chunk other than the tags, header and payload, in order (for ds64, all but
// the RIFF size), and anything after the form.
func (w *writer) PreservedDigest(ctx context.Context, src io.ReaderAt, size int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c, err := w.open(ctx, src, size)
	if err != nil {
		return nil, err
	}
	rw := newRewrite(c)
	h := sha256.New()
	for i, ch := range c.chunks {
		switch {
		case i == rw.id3At || rw.isText(i):
		case rw.isDS64(i):
			var b [chunkHeaderSize]byte
			copyHeader(b[:], ch, c.order)
			h.Write(b[:])
			if _, err := writeutil.CopyRange(ctx, h, src, ch.data()+8, ch.size-8); err != nil {
				return nil, err
			}
		default:
			if _, err := writeutil.CopyRange(ctx, h, src, ch.off, chunkHeaderSize+ch.size); err != nil {
				return nil, err
			}
		}
	}
	if _, err := writeutil.CopyRange(ctx, h, src, c.tail, size-c.tail); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}
