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
// It is a hint: when the format or the edits cannot be applied in place,
// Write falls back to the default atomic rewrite. In-place writes are not
// crash-atomic: a crash mid-write can leave a damaged tag (the audio is never
// touched). Write keeps a copy of the overwritten bytes and restores them if
// verification fails.
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
