// Package types provides core data structures for audio file metadata.
//
// This package defines the File, Tags, AudioInfo, Chapter, and Artwork types
// that represent parsed audio file information across all supported formats.
package types

import (
	"io"
)

// File represents an opened audio file with parsed metadata.
//
// File provides access to format-agnostic metadata (Tags), technical
// audio properties (AudioInfo), and optional embedded artwork.
//
// File uses lazy loading - opening a file reads only metadata, not
// audio content or artwork. Call ExtractArtwork() to load images.
//
// Always call Close() when done to release file resources:
//
//	file, err := audiometa.Open("song.flac")
//	if err != nil {
//		return err
//	}
//	defer file.Close()
type File struct {
	Reader_  io.ReaderAt
	Parser_  interface{}
	RawTags_ map[string][]RawTag
	Path     string
	Chapters []Chapter
	Warnings []Warning
	Artwork_ []Artwork
	Tags     Tags
	Audio    AudioInfo
	Format   Format
	Size     int64
}
