// Package types provides core data structures for audio file metadata.
//
// This package defines the File, Tags, AudioInfo, Chapter, and Artwork types
// that represent parsed audio file information across all supported formats.
package types

import "fmt"

// File represents an opened audio file with parsed metadata.
//
// File provides access to format-agnostic metadata (Tags), technical
// audio properties (AudioInfo), and chapter markers. It is the data
// payload returned by parsers; the user-facing audiometa.File wraps
// this with runtime state (file handle, parser reference, artwork cache).
//
// All fields are populated by the parser before the file is returned
// to the caller. Callers should treat fields as read-only.
type File struct {
	Path     string
	Chapters []Chapter
	Warnings []Warning
	Tags     Tags
	Audio    AudioInfo
	Format   Format
	Size     int64

	// tagValues counts the tag values TakeValue has admitted; it reaches
	// MaxTagValues+1 once the first refusal has been warned about.
	tagValues int
}

// MaxTagValues is the most tag values one parse records. Values past it are
// dropped with one StageMetadata warning, which bounds the memory a hostile
// file can make the parser spend on per-value overhead.
const MaxTagValues = 1 << 16

// TakeValue reports whether f may record one more parsed tag value, counting
// it if so. The first refusal appends a StageMetadata warning; later ones are
// silent. Parsers call it before decoding or storing each value.
func (f *File) TakeValue() bool {
	switch {
	case f.tagValues < MaxTagValues:
		f.tagValues++
		return true
	case f.tagValues == MaxTagValues:
		f.tagValues++
		f.Warnings = AppendWarning(f.Warnings, Warning{
			Stage:   StageMetadata,
			Message: fmt.Sprintf("more than %d tag values; the rest are ignored", MaxTagValues),
		})
	}
	return false
}
