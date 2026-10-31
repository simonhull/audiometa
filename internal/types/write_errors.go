package types

import "errors"

// Write-path sentinel errors. The root package re-exports each one; format
// writers return them wrapped with detail via fmt.Errorf("%w: ...").
var (
	// ErrUnsupportedWrite reports that the format has no writer, or that an
	// edit cannot be represented in the format (a field, a key, a picture type).
	ErrUnsupportedWrite = errors.New("audiometa: write not supported")
	// ErrDRMProtected reports encrypted or DRM-protected content. Such files are never rewritten.
	ErrDRMProtected = errors.New("audiometa: file is DRM-protected or encrypted")
	// ErrTagTooLarge reports serialized metadata that exceeds a format limit.
	ErrTagTooLarge = errors.New("audiometa: metadata exceeds format size limit")
	// ErrConflictingEdits reports a typed field and its raw key edited to different values.
	ErrConflictingEdits = errors.New("audiometa: conflicting tag edits")
	// ErrInvalidArtwork reports artwork rejected by AddArtwork or ReplaceArtwork.
	ErrInvalidArtwork = errors.New("audiometa: invalid artwork")
	// ErrConcurrentModification reports that the file changed on disk during Write.
	ErrConcurrentModification = errors.New("audiometa: file changed during write")
	// ErrVerifyFailed reports that a written file failed read-back verification.
	ErrVerifyFailed = errors.New("audiometa: written file failed verification")
)
