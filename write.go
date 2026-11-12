package audiometa

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/simonhull/audiometa/internal/atomicfile"
	"github.com/simonhull/audiometa/internal/registry"
	"github.com/simonhull/audiometa/internal/types"
	"github.com/simonhull/audiometa/internal/writeutil"
)

// lookupWriter is swapped by tests.
var lookupWriter = registry.GetWriter

// WriteTo writes a complete copy of the file with all pending edits applied
// to w, and returns the number of bytes written. It implements [io.WriterTo].
//
// The pending edits are the changes made to f.Tags since [Open], whether
// through Tags.Set, Tags.Delete or by assigning typed fields such as
// f.Tags.Title, and the artwork edits recorded by [File.AddArtwork],
// [File.ReplaceArtwork], [File.RemoveArtwork] and [File.RemoveAllArtwork].
// Only the tags those edits touch change: every other tag, unknown frames
// and atoms, chapters and the audio data are preserved. Setting a typed
// field rewrites the one tag it was read from (or the format's usual tag if
// it had none); clearing it removes every tag it can be read from. No field
// is ever filled in from another.
//
// The original file is never modified and f is unchanged: the edits stay
// pending, and calling WriteTo again writes identical bytes. With no pending
// edits the output is byte-identical to the input. To replace the file on
// disk, use [Write].
//
// WriteTo reads f.Tags and f.Chapters without synchronization; do not modify
// them while it runs. The artwork edit methods may be called concurrently.
//
// # Errors
//
// The returned error wraps one of the following, to be tested with
// [errors.Is] or [errors.As]:
//
//   - [ErrUnsupportedWrite]: the format has no writer, an edit cannot be
//     represented in the format, or f.Chapters was changed;
//   - [ErrConflictingEdits]: a typed field and its raw key were edited to
//     different values, or related fields disagree;
//   - [ErrDRMProtected]: the file is encrypted or DRM-protected;
//   - [ErrTagTooLarge]: the edited metadata exceeds a format limit;
//   - [*CorruptedFileError]: the file's structure prevents rewriting it, or
//     the writer panicked;
//   - errors from w, from reading the file (which fails after [File.Close])
//     and, for [File.WriteToContext], ctx.Err().
//
// Bytes may already have been written to w when an error is returned.
func (f *File) WriteTo(w io.Writer) (int64, error) {
	return f.WriteToContext(context.Background(), w)
}

// WriteToContext is like [File.WriteTo] but stops early when ctx is done,
// returning an error that wraps ctx.Err().
func (f *File) WriteToContext(ctx context.Context, w io.Writer) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	tw, err := writerFor(f.src.format)
	if err != nil {
		return 0, err
	}
	edits, err := f.buildEdits(ctx)
	if err != nil {
		return 0, err
	}
	if edits.Empty() {
		return writeutil.CopyRange(ctx, w, f.reader, 0, f.src.size)
	}
	return writeSafely(ctx, tw, w, f.reader, f.src.size, f.src.path, edits)
}

// Write opens the audio file at path, calls edit to change its tags and
// artwork, and replaces the file with the edited version.
//
// edit receives the freshly opened [File] and changes it as for
// [File.WriteTo]: through f.Tags and the artwork edit methods. Because the
// file is read inside Write, the edits always apply to what is on disk now,
// not to a copy read earlier. If edit leaves no pending edits, Write returns
// nil and does not touch the file. The File passed to edit is only valid
// during the call; do not keep it.
//
// The replacement is atomic: the new contents are written to a temporary
// file in the same directory, synced, verified by reading them back (the file
// must parse, its audio data must be bit-identical and its chapters
// unchanged), and renamed over the original; the directory is then synced.
// The temporary file is named "." + the original's name (cut to its first
// 100 bytes) + ".audiometa-<digits>.tmp"; if the process dies before the
// rename it is left behind and can be deleted.
// The original file is never opened for writing, so a crash or error at any
// point leaves either the old or the new file, never a mix. The new file
// keeps the original's permission bits and, where the process may set them,
// its owner and group. Symlinks are followed and their target replaced; the
// link itself is kept. Because the new contents are a new file, other hard
// links to the original keep the old contents, and extended attributes, ACLs
// and file flags are not carried over; an edit [WithInPlace] applies in place
// keeps all of them. [WithInPlace] trades that atomicity for speed, and
// [WithPreserveModTime] keeps the modification time.
//
// Write does not lock the file. Just before it renames the new file into
// place (or, with [WithInPlace], before it overwrites anything), it checks
// that the file is still the one it opened, with the same size and
// modification time and, on Linux and macOS, the same inode change time. If
// not, Write returns [ErrConcurrentModification] and leaves the file as the
// other writer left it. A change after that check is not detected, and a
// process that only changes the file's permissions, owner or extended
// attributes also triggers the error. Concurrent Writes to the same path are
// not otherwise coordinated: serialize them. Writes to different files may
// run concurrently.
//
// # Errors
//
// If edit returns an error, Write returns it unchanged and does not touch the
// file. Otherwise the error is one that [Open] or [File.WriteTo] can return,
// or wraps:
//
//   - [ErrConcurrentModification]: the file changed on disk during Write;
//   - [ErrVerifyFailed]: the new contents failed read-back verification;
//   - an [*os.PathError] or [*os.LinkError] from creating, syncing or
//     renaming the temporary file, for example when the directory is not
//     writable.
//
// In every error case the original file is left as it was, except that an
// error syncing the directory is reported after the rename has happened.
func Write(path string, edit func(f *File) error, opts ...WriteOption) error {
	return WriteContext(context.Background(), path, edit, opts...)
}

// WriteContext is like [Write] but stops early when ctx is done, returning an
// error that wraps ctx.Err(). Cancellation never leaves a partly written
// file: the temporary file is removed and the original is untouched.
func WriteContext(ctx context.Context, path string, edit func(f *File) error, opts ...WriteOption) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var o writeOptions
	for _, opt := range opts {
		opt(&o)
	}

	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	pre, err := os.Stat(target) // a clean error before opening; the reference stat comes from the handle
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if !pre.Mode().IsRegular() {
		return notRegular(path)
	}

	f, err := OpenContext(ctx, target)
	if err != nil {
		return err
	}
	defer f.Close() // may run after BeforeRename closed it; the error is ignored

	tw, err := writerFor(f.src.format)
	if err != nil {
		return err
	}
	if err := edit(f); err != nil {
		return err
	}
	edits, err := f.buildEdits(ctx)
	if err != nil {
		return err
	}
	if edits.Empty() {
		return nil
	}

	job := &writeJob{
		f: f, tw: tw, edits: edits, opts: o,
		target: target, st0: f.src.stat, // the handle's stat; a successful Open always sets it
		ref: verifyRef{format: f.src.format, chapters: slices.Clone(f.Chapters)},
	}

	if o.inPlace {
		done, err := job.writeInPlace(ctx)
		if done || err != nil {
			return err
		}
	}
	return job.writeAtomic(ctx)
}

// writeJob is one Write call after the edits have been collected.
type writeJob struct {
	f      *File
	tw     registry.TagWriter
	edits  *types.EditSet
	st0    os.FileInfo // the opened file's stat, from its handle
	target string      // path with symlinks resolved
	ref    verifyRef
	opts   writeOptions
}

// reference computes the digest of the source's preserved bytes, once. Only
// jobs that verify by digest call it: the atomic rewrite, and an in-place
// patch by a writer that cannot report its preserved ranges.
func (j *writeJob) reference(ctx context.Context) error {
	if j.ref.digest != nil {
		return nil
	}
	digest, err := safely(j.target, func() ([]byte, error) { return j.tw.PreservedDigest(ctx, j.f.reader, j.f.src.size) })
	if err != nil {
		return fmt.Errorf("digest source audio: %w", err)
	}
	j.ref.digest = digest
	return nil
}

// verifyRef is what a written file must match.
type verifyRef struct {
	digest   []byte
	chapters []Chapter
	format   Format
}

// unchanged returns ErrConcurrentModification unless the file at the target
// path is still the one Write opened, unmodified.
func (j *writeJob) unchanged() error {
	st, err := os.Stat(j.target)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrConcurrentModification, err)
	}
	return j.sameAsOpened(st)
}

// sameAsOpened returns ErrConcurrentModification unless st describes the file
// Write opened (same file, size and modification time, and the same inode
// change time where the platform reports one). Changing a file's contents,
// or setting its mtime back afterwards, bumps its change time.
func (j *writeJob) sameAsOpened(st os.FileInfo) error {
	if !os.SameFile(j.st0, st) || st.Size() != j.st0.Size() || !st.ModTime().Equal(j.st0.ModTime()) {
		return fmt.Errorf("%w: %s", ErrConcurrentModification, j.target)
	}
	if c0, ok := changeTime(j.st0); ok {
		if c, ok := changeTime(st); ok && !c.Equal(c0) {
			return fmt.Errorf("%w: %s: inode change time differs", ErrConcurrentModification, j.target)
		}
	}
	return nil
}

// writeAtomic streams the edited file into a temp file, verifies it and
// renames it over the target.
func (j *writeJob) writeAtomic(ctx context.Context) error {
	if err := j.reference(ctx); err != nil {
		return err
	}
	return atomicfile.Replace(j.target,
		func(w io.Writer) error {
			_, err := writeSafely(ctx, j.tw, w, j.f.reader, j.f.src.size, j.target, j.edits)
			return err
		},
		atomicfile.Options{
			PreserveModTime: j.opts.preserveModTime,
			Verify: func(tmp *os.File) error {
				st, err := tmp.Stat()
				if err != nil {
					return fmt.Errorf("%w: %w", ErrVerifyFailed, err)
				}
				return verifyFile(ctx, tmp, st.Size(), j.target, j.tw, j.ref)
			},
			BeforeRename: func() error {
				_ = j.f.Close() // Windows cannot rename over an open file
				return j.unchanged()
			},
		})
}

// writeInPlace patches the metadata region of the target when the writer
// supports it and the edits fit. done is false when the caller should fall
// back to writeAtomic.
func (j *writeJob) writeInPlace(ctx context.Context) (done bool, err error) {
	ipw, ok := j.tw.(registry.InPlaceWriter)
	if !ok {
		return false, nil
	}
	p, err := safely(j.target, func() (*registry.Patch, error) { return ipw.InPlacePatch(ctx, j.f.reader, j.f.src.size, j.edits) })
	if err != nil {
		return true, err
	}
	if p == nil {
		return false, nil
	}
	if p.Off < 0 || p.Off+int64(len(p.Data)) > j.f.src.size {
		return true, fmt.Errorf("in-place patch [%d, %d) outside file of %d bytes", p.Off, p.Off+int64(len(p.Data)), j.f.src.size)
	}
	verify, err := j.inPlaceVerifier(ctx, p)
	if err != nil {
		return true, err
	}
	rw, err := os.OpenFile(j.target, os.O_RDWR, 0)
	if errors.Is(err, fs.ErrPermission) {
		return false, nil // read-only: the atomic rewrite needs only a writable directory
	}
	if err != nil {
		return true, fmt.Errorf("open for in-place write: %w", err)
	}
	return true, j.patch(rw, p.Off, p.Data, verify)
}

// inPlaceVerifier returns the check that decides whether the patched file may
// stay, and refuses before any byte is written a patch that cannot pass it.
//
// A writer that reports its preserved ranges is checked by structure: the
// patch must lie outside the ranges computed on the source, and the ranges
// computed again on the patched file must equal them, so the audio and the
// other preserved bytes are neither touched nor moved, without reading them.
// Any other writer is checked by the digest of its preserved bytes before and
// after.
func (j *writeJob) inPlaceVerifier(ctx context.Context, p *registry.Patch) (func(*os.File) error, error) {
	pr, ok := j.tw.(registry.PreservedRanger)
	if !ok {
		if err := j.reference(ctx); err != nil {
			return nil, err
		}
		return func(rw *os.File) error {
			return verifyFile(ctx, rw, j.st0.Size(), j.target, j.tw, j.ref)
		}, nil
	}
	r0, err := safely(j.target, func() ([]registry.Range, error) { return pr.PreservedRanges(ctx, j.f.reader, j.f.src.size) })
	if err != nil {
		return nil, fmt.Errorf("preserved ranges of source: %w", err)
	}
	patch := registry.Range{Off: p.Off, End: p.Off + int64(len(p.Data))}
	for _, r := range r0 {
		if patch.Overlaps(r) {
			return nil, fmt.Errorf("%w: in-place patch %v overlaps preserved bytes %v", ErrVerifyFailed, patch, r)
		}
	}
	return func(rw *os.File) error {
		size := j.st0.Size()
		if err := verifyParsed(ctx, rw, size, j.target, j.ref); err != nil {
			return err
		}
		r1, err := safely(j.target, func() ([]registry.Range, error) { return pr.PreservedRanges(ctx, rw, size) })
		if err != nil {
			return fmt.Errorf("%w: preserved ranges of written file: %w", ErrVerifyFailed, err)
		}
		if !slices.Equal(r0, r1) {
			return fmt.Errorf("%w: preserved ranges moved from %v to %v", ErrVerifyFailed, r0, r1)
		}
		return nil
	}, nil
}

// patch overwrites len(patch) bytes of rw at off, checks the result with
// verify and restores the original bytes if that fails. It closes rw.
func (j *writeJob) patch(rw *os.File, off int64, patch []byte, verify func(*os.File) error) (err error) {
	written, restored := false, false
	defer func() {
		if cerr := rw.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close after in-place write: %w", cerr)
		}
		// A restored file gets its mtime back too, so a failed Write leaves it
		// as it was. A failed restore leaves the new mtime: the file is damaged.
		if (err == nil && j.opts.preserveModTime) || (err != nil && written && restored) {
			if terr := os.Chtimes(j.target, time.Time{}, j.st0.ModTime()); terr != nil {
				err = errors.Join(err, fmt.Errorf("restore modification time: %w", terr))
			}
		}
	}()

	st, err := rw.Stat()
	if err != nil {
		return fmt.Errorf("stat for in-place write: %w", err)
	}
	if err := j.sameAsOpened(st); err != nil {
		return err
	}
	backup := make([]byte, len(patch))
	if _, err := rw.ReadAt(backup, off); err != nil {
		return fmt.Errorf("back up metadata region: %w", err)
	}
	written = true
	if err := writeSync(rw, patch, off); err != nil {
		rerr := restore(rw, backup, off)
		restored = rerr == nil
		return errors.Join(fmt.Errorf("in-place write: %w", err), rerr)
	}
	if err := verify(rw); err != nil {
		rerr := restore(rw, backup, off)
		restored = rerr == nil
		return errors.Join(err, rerr)
	}
	return nil
}

// writeSync writes b at off and syncs the file.
func writeSync(f *os.File, b []byte, off int64) error {
	if _, err := f.WriteAt(b, off); err != nil {
		return err
	}
	return f.Sync()
}

// restore writes the backed-up bytes back after a failed in-place write.
func restore(f *os.File, backup []byte, off int64) error {
	if err := writeSync(f, backup, off); err != nil {
		return fmt.Errorf("restore original metadata region: %w", err)
	}
	return nil
}

// verifyFile re-parses a written file and checks it against ref. labelPath is
// the original path, so path-dependent parser behavior sees the real name.
// Every failure wraps ErrVerifyFailed.
func verifyFile(ctx context.Context, r io.ReaderAt, size int64, labelPath string, tw registry.TagWriter, ref verifyRef) error {
	if err := verifyParsed(ctx, r, size, labelPath, ref); err != nil {
		return err
	}
	digest, err := safely(labelPath, func() ([]byte, error) { return tw.PreservedDigest(ctx, r, size) })
	if err != nil {
		return fmt.Errorf("%w: digest written audio: %w", ErrVerifyFailed, err)
	}
	if !bytes.Equal(digest, ref.digest) {
		return fmt.Errorf("%w: audio data changed", ErrVerifyFailed)
	}
	return nil
}

// verifyParsed re-parses a written file and checks its format and chapters
// against ref. Every failure wraps ErrVerifyFailed.
func verifyParsed(ctx context.Context, r io.ReaderAt, size int64, labelPath string, ref verifyRef) error {
	parsed, err := openReader(ctx, r, size, labelPath)
	if err != nil {
		return fmt.Errorf("%w: re-read: %w", ErrVerifyFailed, err)
	}
	if parsed.file.Format != ref.format {
		return fmt.Errorf("%w: format changed from %s to %s", ErrVerifyFailed, ref.format, parsed.file.Format)
	}
	if !slices.EqualFunc(parsed.file.Chapters, ref.chapters, chapterEqual) {
		return fmt.Errorf("%w: chapters changed", ErrVerifyFailed)
	}
	return nil
}

// writerFor returns the registered writer for format, or an error wrapping
// ErrUnsupportedWrite.
func writerFor(format Format) (registry.TagWriter, error) {
	tw := lookupWriter(format)
	if tw == nil {
		return nil, fmt.Errorf("%w: no writer for %s files", ErrUnsupportedWrite, format)
	}
	return tw, nil
}

// buildEdits re-parses the source and diffs it against f's current state.
func (f *File) buildEdits(ctx context.Context) (*types.EditSet, error) {
	orig, err := safely(f.src.path, func() (*types.File, error) { return f.parser.Parse(ctx, f.reader, f.src.size, f.src.path) })
	if err != nil {
		return nil, fmt.Errorf("re-read %s: %w", f.src.format, err)
	}
	types.BindFormat(&orig.Tags, f.src.format)

	if !slices.EqualFunc(f.Chapters, orig.Chapters, chapterEqual) {
		return nil, fmt.Errorf("%w: chapter editing is not supported", ErrUnsupportedWrite)
	}
	keys, fields, err := types.Diff(&orig.Tags, &f.Tags, f.src.format)
	if err != nil {
		return nil, err
	}
	return &types.EditSet{
		Keys:    keys,
		Fields:  fields,
		Artwork: f.pendingArtwork(),
		Orig:    &orig.Tags,
		Final:   &f.Tags,
	}, nil
}

// chapterEqual compares every field of two chapters.
func chapterEqual(a, b Chapter) bool {
	return a.Index == b.Index && a.Title == b.Title &&
		a.StartTime == b.StartTime && a.EndTime == b.EndTime && a.Source == b.Source
}

// writeSafely calls tw.WriteFile, converting a panic into a CorruptedFileError. It returns
// the bytes that reached w, which the counting writer knows even when WriteFile panics.
func writeSafely(ctx context.Context, tw registry.TagWriter, w io.Writer, r io.ReaderAt, size int64, path string, edits *types.EditSet) (int64, error) {
	cw := &writeutil.CountingWriter{W: w}
	_, err := safely(path, func() (int64, error) { return tw.WriteFile(ctx, cw, r, size, edits) })
	return cw.N, err
}
