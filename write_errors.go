package audiometa

import "github.com/simonhull/audiometa/internal/types"

// Errors returned by the write API ([File.WriteTo], [Write] and the artwork
// edit methods). They are always returned wrapped with detail, so test for
// them with [errors.Is], never with ==.
var (
	// ErrUnsupportedWrite is returned (wrapped) by [File.WriteTo] and [Write]
	// when the file's format has no writer, when an edit cannot be represented
	// in the format (a typed field the format has no tag for, a picture type
	// the format cannot store), or when f.Chapters was changed: chapters are
	// preserved but cannot be edited.
	ErrUnsupportedWrite = types.ErrUnsupportedWrite

	// ErrDRMProtected is returned (wrapped) by [File.WriteTo] and [Write] for
	// encrypted or DRM-protected files. Such files are never rewritten.
	ErrDRMProtected = types.ErrDRMProtected

	// ErrTagTooLarge is returned (wrapped) by [File.WriteTo] and [Write] when
	// the serialized metadata would exceed a limit of the format, such as the
	// 16 MiB limit of a FLAC metadata block or the 256 MiB limit of an ID3v2
	// tag.
	ErrTagTooLarge = types.ErrTagTooLarge

	// ErrConflictingEdits is returned (wrapped) by [File.WriteTo] and [Write]
	// when the pending edits contradict each other: a typed field and the raw
	// key it is read from were both changed to different values (for example
	// Tags.Set("TIT2", "A") and Tags.Title = "B" on an MP3), Artist was set to
	// something other than the first of Artists, or Year disagrees with the
	// year of Date.
	ErrConflictingEdits = types.ErrConflictingEdits

	// ErrInvalidArtwork is returned (wrapped) by [File.AddArtwork] and
	// [File.ReplaceArtwork] when the image is rejected: it has no data, its
	// Type is not a valid [ArtworkType], its Description is not valid UTF-8,
	// or its MIMEType is empty and cannot be detected from the data.
	ErrInvalidArtwork = types.ErrInvalidArtwork

	// ErrConcurrentModification is returned (wrapped) by [Write] when the file
	// changed on disk (its size or modification time) between the moment
	// Write opened it and the moment it would have been replaced. The file is
	// left as the other writer left it.
	ErrConcurrentModification = types.ErrConcurrentModification

	// ErrVerifyFailed is returned (wrapped) by [Write] when the new file,
	// read back before it replaces the original, cannot be parsed, has
	// different audio data or has different chapters. The original file is
	// left untouched.
	ErrVerifyFailed = types.ErrVerifyFailed
)
