package mp3

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/registry"
	"github.com/simonhull/audiometa/internal/trailer"
	"github.com/simonhull/audiometa/internal/types"
	"github.com/simonhull/audiometa/internal/writeutil"
)

// writer rewrites the ID3v2 tag of an MP3 file. It implements
// registry.TagWriter, registry.InPlaceWriter and registry.ID3v1Configurer.
//
// The tag keeps its ID3v2 version (2.3 or 2.4); an ID3v2.2 tag, or a file
// without one, gets an ID3v2.4 tag. Only the frames for the keys and fields
// the caller edited change: every other frame (unknown IDs, PRIV, CHAP, CTOC,
// APIC, compressed frames) is re-emitted byte for byte, except that
// unsynchronisation is undone. The extended header and the ID3v2.4 footer are
// dropped. When the frames fit in the existing tag it keeps its exact size;
// otherwise it gets new padding.
//
// Everything after the tag, including the audio and any APEv2 or Lyrics3 tag,
// is copied verbatim. So is the ID3v1 trailer, unless the writer was made with
// WithID3v1: ID3v1Update then rewrites an existing trailer from the edited
// tags and ID3v1Remove drops it. A file without a trailer never gets one. The
// default, ID3v1Keep, never changes the trailer, so after deleting TIT2 (or
// TPE1) a reader that falls back to ID3v1 may show its stale title (or artist).
//
// Deleting every raw key leaves an ID3v2 tag holding only the frames that are
// not raw keys (APIC, CHAP, CTOC, COMM, PRIV, ...) plus padding; the tag
// itself is kept, even when empty.
type writer struct {
	id3v1 registry.ID3v1Mode // what to do with an existing ID3v1 trailer
}

// WithID3v1 returns a copy of w that applies mode to an existing ID3v1
// trailer.
func (w *writer) WithID3v1(mode registry.ID3v1Mode) registry.TagWriter {
	c := *w
	c.id3v1 = mode
	return &c
}

// ChangesTrailer reports whether WriteFile would write a different ID3v1
// trailer than src has.
func (w *writer) ChangesTrailer(ctx context.Context, src io.ReaderAt, size int64, edits *types.EditSet) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if w.id3v1 == registry.ID3v1Keep {
		return false, nil
	}
	sr := binutil.NewSafeReader(src, size, "")
	_, end, hasV1, err := audioRange(sr, size)
	if err != nil || !hasV1 {
		return false, err
	}
	_, changed, err := w.trailer(sr, end, edits)
	return changed, err
}

// WriteFile streams src with edits applied to w.
func (w *writer) WriteFile(ctx context.Context, out io.Writer, src io.ReaderAt, size int64, edits *types.EditSet) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	sr := binutil.NewSafeReader(src, size, "")
	edits, err := withID3v1Seed(ctx, sr, size, edits)
	if err != nil {
		return 0, err
	}
	t, tag, err := buildTag(ctx, sr, size, edits)
	if err != nil {
		return 0, err
	}
	start, end, hasV1, err := audioRange(sr, size)
	if err != nil {
		return 0, err
	}
	if start != t.oldTotal {
		return 0, corrupted(sr, 0, "ID3v2 tag size disagrees with the audio range")
	}

	cw := &writeutil.CountingWriter{W: out}
	if _, err := cw.Write(tag); err != nil {
		return cw.N, err
	}
	if _, err := writeutil.CopyRange(ctx, cw, src, start, end-start); err != nil {
		return cw.N, err
	}
	if hasV1 {
		trailer, _, err := w.trailer(sr, end, edits)
		if err != nil {
			return cw.N, err
		}
		if _, err := cw.Write(trailer); err != nil {
			return cw.N, err
		}
	}
	return cw.N, nil
}

// AudioDigest returns the SHA-256 of the bytes between the ID3v2 tag and the
// ID3v1 trailer.
func (w *writer) AudioDigest(ctx context.Context, src io.ReaderAt, size int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sr := binutil.NewSafeReader(src, size, "")
	start, _, _, err := audioRange(sr, size)
	if err != nil {
		return nil, err
	}
	// Scan from the end of the file, not audioRange's end: trailer.Start
	// tells an ID3v1 trailer from "TAG" inside an APEv2 or Lyrics3 tag.
	end, err := trailer.Start(ctx, sr, start, size)
	if err != nil {
		return nil, err
	}
	return writeutil.DigestRange(ctx, src, start, end-start)
}

// PreservedDigest returns the SHA-256 of everything after the ID3v2 tag up to
// an ID3v1 trailer, including any APEv2 and Lyrics3 tags: the bytes WriteFile
// always copies verbatim. The trailer is left out because a write may update
// or remove it.
func (w *writer) PreservedDigest(ctx context.Context, src io.ReaderAt, size int64) ([]byte, error) {
	ranges, err := w.PreservedRanges(ctx, src, size)
	if err != nil {
		return nil, err
	}
	return writeutil.DigestRanges(ctx, src, ranges)
}

// PreservedRanges returns everything after the ID3v2 tag up to an ID3v1
// trailer.
func (w *writer) PreservedRanges(ctx context.Context, src io.ReaderAt, size int64) ([]registry.Range, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	start, end, _, err := audioRange(binutil.NewSafeReader(src, size, ""), size)
	if err != nil {
		return nil, err
	}
	return []registry.Range{{Off: start, End: end}}, nil
}

// InPlacePatch returns the new tag to write at offset 0 when the file has an
// ID3v2 tag and the edited frames fit in its space. The patch is then
// exactly what WriteFile writes before the audio. It returns nil when the
// write would change the ID3v1 trailer, which lies at the end of the file.
func (w *writer) InPlacePatch(ctx context.Context, src io.ReaderAt, size int64, edits *types.EditSet) (*registry.Patch, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sr := binutil.NewSafeReader(src, size, "")
	edits, err := withID3v1Seed(ctx, sr, size, edits)
	if err != nil {
		return nil, err
	}
	if changed, err := w.ChangesTrailer(ctx, src, size, edits); err != nil || changed {
		return nil, err
	}
	t, tag, err := buildTag(ctx, sr, size, edits)
	if err != nil {
		return nil, err
	}
	if t.oldTotal == 0 || int64(len(tag)) != t.oldTotal {
		return nil, nil
	}
	return &registry.Patch{Data: tag}, nil
}

// trailer returns the ID3v1 trailer a write puts after the audio ending at
// end, and whether that differs from the trailer src has there: nil, true for
// ID3v1Remove, the rewritten trailer for ID3v1Update, the existing one
// otherwise. Without edits.Final, ID3v1Update keeps the trailer.
func (w *writer) trailer(sr *binutil.SafeReader, end int64, edits *types.EditSet) (out []byte, changed bool, err error) {
	var old [id3v1Size]byte
	if err := sr.ReadAt(old[:], end, "ID3v1 tag"); err != nil {
		return nil, false, err
	}
	switch {
	case w.id3v1 == registry.ID3v1Remove:
		return nil, true, nil
	case w.id3v1 == registry.ID3v1Update && edits.Final != nil:
		enc := encodeID3v1(edits.Final, old)
		return enc[:], enc != old, nil
	default:
		return old[:], false, nil
	}
}

// buildTag reads the source tag, refuses DRM, applies edits and serializes
// the new tag.
func buildTag(ctx context.Context, sr *binutil.SafeReader, size int64, edits *types.EditSet) (*wtag, []byte, error) {
	t, err := readWTag(ctx, sr, size)
	if err != nil {
		return nil, nil, err
	}
	if err := t.drmCheck(); err != nil {
		return nil, nil, err
	}
	if err := t.apply(edits); err != nil {
		return nil, nil, err
	}
	tag, err := t.serialize()
	if err != nil {
		return nil, nil, err
	}
	return t, tag, nil
}

// audioRange returns the audio region [start, end): after the ID3v2 tag (and
// its footer) and before an ID3v1 trailer (one that is the last tag block, not a "TAG" inside an APEv2 or Lyrics3v2 tag). Any "ID3" header counts as a tag,
// whatever its version, so the range matches what a write leaves untouched.
func audioRange(sr *binutil.SafeReader, size int64) (start, end int64, hasV1 bool, err error) {
	end = size
	if size >= 10 {
		var h [10]byte
		if err := sr.ReadAt(h[:], 0, "ID3v2 header"); err != nil {
			return 0, 0, false, err
		}
		if bytes.Equal(h[:3], []byte("ID3")) {
			start = 10 + int64(decodeSynchsafe(h[6:10]))
			if h[3] == 4 && h[5]&0x10 != 0 {
				start += 10
			}
		}
	}
	if end-start >= id3v1Size {
		hasV1, err = trailer.EndsWithID3v1(sr, start, end)
		if err != nil {
			return 0, 0, false, err
		}
		if hasV1 {
			end -= id3v1Size
		}
	}
	if start > end {
		return 0, 0, false, corrupted(sr, 0, fmt.Sprintf("ID3v2 tag of %d bytes runs past the audio end %d", start, end))
	}
	return start, end, hasV1, nil
}

func init() { registry.RegisterWriter(types.FormatMP3, &writer{}) }

// withID3v1Seed returns edits plus a FieldEdit for every value the reader takes
// from the ID3v1 trailer and edits leaves alone, so that a new or rewritten
// ID3v2 tag keeps them. It returns edits unchanged when the reader would not
// fall back to ID3v1. It never modifies edits.
func withID3v1Seed(ctx context.Context, sr *binutil.SafeReader, size int64, edits *types.EditSet) (*types.EditSet, error) {
	v1, ok := readID3v1(sr, size)
	if !ok {
		return edits, nil
	}
	// The reader also falls back after an ID3v2 error, so only a canceled
	// context stops the seed.
	scratch := &types.File{}
	_, _ = parseID3v2(ctx, sr, scratch)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !id3v1Fallback(&scratch.Tags) {
		return edits, nil
	}

	bindings := types.FieldBindings(types.FormatMP3)
	edited := func(f types.Field) bool {
		if slices.ContainsFunc(edits.Fields, func(e types.FieldEdit) bool { return e.Field == f }) {
			return true
		}
		return slices.ContainsFunc(bindings, func(b types.FieldBinding) bool {
			return b.Field == f && slices.ContainsFunc(b.Keys, func(k string) bool {
				return slices.ContainsFunc(edits.Keys, func(e types.KeyEdit) bool { return strings.EqualFold(e.Key, k) })
			})
		})
	}
	fills := slices.DeleteFunc(id3v1Fills(&scratch.Tags, v1), func(e types.FieldEdit) bool { return edited(e.Field) })
	if len(fills) == 0 {
		return edits, nil
	}

	fields := append(slices.Clone(edits.Fields), fills...)
	slices.SortFunc(fields, func(a, b types.FieldEdit) int { return cmp.Compare(a.Field, b.Field) })
	return &types.EditSet{Keys: edits.Keys, Fields: fields, Artwork: edits.Artwork, Orig: edits.Orig, Final: edits.Final}, nil
}
