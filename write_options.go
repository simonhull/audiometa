package audiometa

import "github.com/simonhull/audiometa/internal/registry"

// WriteOption configures [Write] and [WriteContext].
//
// It is a separate type from [Option], so open options cannot be passed to
// Write by mistake, and vice versa. The With functions in this package that
// return a WriteOption create them.
type WriteOption func(*writeOptions)

// writeOptions holds the configuration of one Write call.
type writeOptions struct {
	inPlace         bool // try an in-place metadata patch before the atomic rewrite
	preserveModTime bool // give the written file the original modification time
	id3v1           ID3v1Mode
}

// WithInPlace lets [Write] overwrite the metadata region of the original file
// when the edited metadata fits in the existing space (for example ID3v2 or
// FLAC padding), instead of rewriting the whole file. For large audiobooks
// that avoids copying gigabytes of audio.
//
// It is a hint: when the format or the edits cannot be applied in place, or
// the file's permissions do not allow opening it for writing, Write falls
// back to the default atomic rewrite. In-place writes are not crash-atomic: a
// crash mid-write can leave a damaged tag (the audio is never touched). Write
// keeps a copy of the overwritten bytes and, if verification fails, restores
// them and the modification time.
//
// An in-place write verifies the patched file by structure, without
// re-reading the audio: the patch must stay outside the audio and the other
// preserved bytes, and after the write the file must still parse to the same
// format and chapters, with the preserved bytes starting and ending where they
// did. Retagging a large audiobook in place therefore reads only its metadata.
// The atomic rewrite still reads the result back and compares every preserved
// byte.
func WithInPlace() WriteOption {
	return func(o *writeOptions) { o.inPlace = true }
}

// WithPreserveModTime makes [Write] keep the file's modification time.
//
// By default the written file has a new modification time, which is what
// library managers and sync tools use to detect changes; use this option only
// when that detection is unwanted.
func WithPreserveModTime() WriteOption {
	return func(o *writeOptions) { o.preserveModTime = true }
}

// ID3v1Mode says what [Write] does with an MP3 file's ID3v1 trailer.
//
// The trailer is the 128 bytes at the very end of the file that begin with "TAG".
// A trailer that is followed by another tag (for example an APEv2 tag after it)
// is not recognized, and every mode leaves it alone.
type ID3v1Mode = registry.ID3v1Mode

const (
	// ID3v1Keep copies an existing ID3v1 trailer unchanged. It is the default.
	ID3v1Keep = registry.ID3v1Keep
	// ID3v1Update rewrites an existing ID3v1 trailer from the file's tags after
	// the edit: title, artist, album, year, comment and track number, each cut
	// to its ID3v1 field width, with characters outside Latin-1 replaced by '?'.
	// The genre byte is kept. A file without an ID3v1 trailer does not get one.
	ID3v1Update = registry.ID3v1Update
	// ID3v1Remove drops an existing ID3v1 trailer.
	ID3v1Remove = registry.ID3v1Remove
)

// WithID3v1 sets what [Write] does with an MP3 file's ID3v1 trailer; see
// [ID3v1Mode]. Other formats ignore it.
//
// With [ID3v1Update] or [ID3v1Remove], a file whose trailer would change is
// written even when the edit function changes no tags, and [WithInPlace] falls
// back to the atomic rewrite, because the trailer lies at the end of the file.
// Values that only the trailer held are copied into the ID3v2 tag first, so
// removing the trailer loses no data.
func WithID3v1(mode ID3v1Mode) WriteOption {
	return func(o *writeOptions) { o.id3v1 = mode }
}
