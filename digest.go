package audiometa

import (
	"context"
	"crypto/sha256"
	"fmt"
)

// AudioDigest returns the SHA-256 of the file's audio data, leaving out every
// byte of metadata: tags, artwork, chapters and padding. Editing metadata,
// with audiometa or any other tagger, does not change it; changing the audio
// does. Two files with identical audio have the same digest, so it can
// recognize a file that was renamed, moved or retagged.
//
// What counts as audio depends on the format: MP3 frames between the ID3v2
// tag and any ID3v1 trailer; FLAC frames after the last metadata block; the
// payloads of every M4A/M4B mdat atom; and for Ogg Vorbis and Opus the packet
// bytes of the first logical stream after its header packets.
//
// The digest is computed from the file on disk, not from unsaved edits, and
// is not cached: each call reads all of the audio. It is safe for concurrent
// use. The error is an [*UnsupportedFormatError] if the format has no digest,
// or wraps the read error, for example [os.ErrClosed] after [File.Close] or a
// [*CorruptedFileError]. Use [File.AudioDigestContext] to make it cancellable.
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
		return sum, &UnsupportedFormatError{Path: f.Path, Reason: fmt.Sprintf("no audio digest for %s files", f.Format)}
	}
	digest, err := digestSafely(ctx, d, f.reader, f.Size, f.Path)
	if err != nil {
		return sum, fmt.Errorf("audio digest of %s: %w", f.Path, err)
	}
	if len(digest) != sha256.Size {
		return sum, fmt.Errorf("audio digest of %s: digester returned %d bytes, want %d", f.Path, len(digest), sha256.Size)
	}
	return [sha256.Size]byte(digest), nil
}
