package mp3

import (
	"context"
	"io"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// The functions in this file read and write an ID3v2 tag that another
// container embeds, such as the "id3 " chunk of a WAV file or the "ID3 " chunk
// of an AIFF file. r holds the tag alone, from its header at offset 0, and size
// is the length of the space it occupies. Offsets in errors and warnings are
// relative to that space.

// ReadEmbeddedTag parses the ID3v2 tag at the start of r into file, exactly as
// [types.FormatMP3] files are parsed: tags, raw frames and chapters. A tag
// that cannot be read becomes a StageMetadata warning on file, never an
// error; only ctx ending returns one.
func ReadEmbeddedTag(ctx context.Context, r io.ReaderAt, size int64, path string, file *types.File) error {
	sr := binutil.NewSafeReader(r, size, path)
	if _, err := parseID3v2(ctx, sr, file); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		types.Warn(file, types.StageMetadata, 0, err, "embedded ID3v2 tag parsing failed: "+err.Error())
	}
	return nil
}

// EmbeddedArtwork returns the pictures (APIC frames) of the ID3v2 tag at the
// start of r, in tag order. Space without a tag has none.
func EmbeddedArtwork(ctx context.Context, r io.ReaderAt, size int64, path string) ([]types.Artwork, error) {
	return extractArtwork(ctx, r, size, path)
}

// EditEmbeddedTag returns the ID3v2 tag at the start of r with edits applied,
// following the rules of the MP3 writer: the tag keeps its version (an
// ID3v2.2 tag, or space without a tag, gives ID3v2.4), untouched frames are
// copied byte for byte, and a tag whose frames fit keeps its size. Pass size 0
// when there is no tag. It returns nil when there was no tag and the edits
// leave nothing to write.
//
// Its errors are those of the MP3 writer: they wrap [types.ErrUnsupportedWrite],
// [types.ErrDRMProtected] or [types.ErrTagTooLarge], or are a
// [*types.CorruptedFileError].
func EditEmbeddedTag(ctx context.Context, r io.ReaderAt, size int64, edits *types.EditSet) ([]byte, error) {
	_, tag, err := buildTag(ctx, binutil.NewSafeReader(r, size, ""), size, edits)
	return tag, err
}
