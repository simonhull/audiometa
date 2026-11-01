package audiometa

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/simonhull/audiometa/internal/registry"
)

// lookupDigester is swapped by tests.
var lookupDigester = registry.GetDigester

// AudioDigest returns the SHA-256 of the file's audio data. Tags, artwork and
// padding are left out, so editing them with audiometa never changes the
// digest, while changing the audio does. Files with byte-identical audio data
// share a digest, which identifies a file that was renamed, moved or retagged.
//
// What is hashed depends on the format:
//
//   - MP3: everything between the ID3v2 tag and any trailing ID3v1, APEv2,
//     Lyrics3v2 or footer-terminated ID3v2 tags.
//   - FLAC: everything after the last metadata block, up to any such
//     trailing tags.
//   - M4A/M4B: the payload of every mdat atom, including the samples of a
//     QuickTime chapter track.
//   - Ogg Vorbis and Opus: the packets of the first logical stream after its
//     header packets.
//
// Trailing tags that other tools add or edit there do not change the digest
// either. A QuickTime chapter track's titles are stored as M4B samples, so
// rewriting them does.
//
// AudioDigest hashes the file as it was opened, not unsaved edits or later
// changes to the path, and reads all of the audio on every call. It is safe
// for concurrent use. The error wraps an [*UnsupportedFormatError] if the
// format has no digest, a [*CorruptedFileError] or [*OutOfBoundsError] if the
// file has no audio data or its structure is broken or cut short, or the read
// error, for example [os.ErrClosed] after [File.Close]. Use
// [File.AudioDigestContext] to make it cancellable.
func (f *File) AudioDigest() ([sha256.Size]byte, error) {
	return f.AudioDigestContext(context.Background())
}

// AudioDigestContext is like [File.AudioDigest] but stops early when ctx is
// done, returning an error that wraps ctx.Err().
//
// Use it to bound how long hashing can take, for example when fingerprinting
// a library in a worker pool.
func (f *File) AudioDigestContext(ctx context.Context) ([sha256.Size]byte, error) {
	var sum [sha256.Size]byte
	d := lookupDigester(f.Format)
	if d == nil {
		return sum, fmt.Errorf("audio digest: %w", &UnsupportedFormatError{Path: f.Path, Reason: fmt.Sprintf("no digest for %s files", f.Format)})
	}
	digest, err := digestSafely(ctx, d, f.reader, f.Size, f.Path)
	if err != nil {
		return sum, fmt.Errorf("audio digest: %w", withPath(err, f.Path))
	}
	if len(digest) != sha256.Size {
		return sum, fmt.Errorf("audio digest: digester returned %d bytes, want %d", len(digest), sha256.Size)
	}
	if sum = [sha256.Size]byte(digest); sum == sha256.Sum256(nil) {
		return [sha256.Size]byte{}, fmt.Errorf("audio digest: %w", &CorruptedFileError{Path: f.Path, Reason: "no audio data"})
	}
	return sum, nil
}

// withPath fills in the Path of a structural error that a digester raised
// without one, so it reads like the errors [Open] returns.
func withPath(err error, path string) error {
	if e, ok := errors.AsType[*CorruptedFileError](err); ok && e.Path == "" {
		e.Path = path
	}
	if e, ok := errors.AsType[*OutOfBoundsError](err); ok && e.Path == "" {
		e.Path = path
	}
	return err
}
