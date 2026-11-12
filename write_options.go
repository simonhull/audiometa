package audiometa

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
