// Package types provides core data structures for audio file metadata.
//
// This package defines the File, Tags, AudioInfo, Chapter, and Artwork types
// that represent parsed audio file information across all supported formats.
package types

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
}
