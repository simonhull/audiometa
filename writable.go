package audiometa

import (
	"context"
	"errors"

	"github.com/simonhull/audiometa/internal/types"
)

// CanWrite reports whether audiometa has a tag writer for format. It looks at
// no file: a file of a writable format can still be refused by
// [File.WriteTo] and [Write], and [File.Writable] says whether one will be.
//
// Today FLAC, MP3, M4A, M4B, WAV and AIFF are writable; Ogg Vorbis, Opus and
// [FormatUnknown] are not.
func CanWrite(format Format) bool {
	return lookupWriter(format) != nil
}

// Writable reports why [File.WriteTo] and [Write] would refuse f for a reason
// that belongs to the file, without writing anything. It returns nil when none
// applies. Otherwise the error wraps one of:
//
//   - [ErrUnsupportedWrite]: the format has no writer, or the file's layout
//     cannot be rewritten, such as a fragmented MP4;
//   - [ErrDRMProtected]: the file is encrypted;
//   - [*CorruptedFileError]: the file's structure prevents rewriting it;
//   - an error from reading the file, which fails after [File.Close].
//
// A nil result is not a promise. A particular edit may still be unrepresentable
// ([ErrUnsupportedWrite]), too large ([ErrTagTooLarge]) or conflicting
// ([ErrConflictingEdits]), the file may change on disk, and the read-back
// verification of [Write] can still fail. Pending edits on f are not consulted,
// and the file's tags are re-read from the source, as a write does.
//
// Writable runs the format's writer on a one-field edit and stops it at its
// first output byte. Writers check the whole file before they emit anything, so
// the cost does not grow with the audio: it reads the metadata and the
// structure around it, a few kilobytes for most files.
func (f *File) Writable() error {
	return f.WritableContext(context.Background())
}

// WritableContext is like [File.Writable] but stops early when ctx is done,
// returning an error that wraps ctx.Err().
func (f *File) WritableContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tw, err := writerFor(f.src.format)
	if err != nil {
		return err
	}
	orig, err := f.reparse(ctx)
	if err != nil {
		return err
	}
	final := orig.Tags.Clone()
	final.Title = probeTitle
	edits := &types.EditSet{
		Fields: []types.FieldEdit{{Field: types.FieldTitle, Values: []string{probeTitle}}},
		Orig:   &orig.Tags,
		Final:  final,
	}
	_, err = writeSafely(ctx, tw, refuseFirstWrite{}, f.reader, f.src.size, f.src.path, edits)
	if errors.Is(err, errDryRun) {
		return nil
	}
	return err
}

// probeTitle is the value of the one-field edit [File.Writable] plans. It is
// longer than an empty title, so every writer takes the path of a real edit.
const probeTitle = "x"

// errDryRun is what refuseFirstWrite returns: the writer got as far as output.
var errDryRun = errors.New("audiometa: dry run reached the first output byte")

// refuseFirstWrite is the sink of a dry run: it refuses the first byte.
type refuseFirstWrite struct{}

func (refuseFirstWrite) Write([]byte) (int, error) { return 0, errDryRun }
