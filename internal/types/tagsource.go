package types

// TagKind names a tag container format. Its values are stable strings, safe
// to persist and compare.
type TagKind string

// Tag container kinds reported in [File.TagSources].
const (
	// TagID3v2 is an ID3v2.2, 2.3 or 2.4 tag.
	TagID3v2 TagKind = "id3v2"
	// TagID3v1 is the 128-byte ID3v1 trailer.
	TagID3v1 TagKind = "id3v1"
	// TagAPEv2 is an APEv1 or APEv2 tag. It is reported, never read as tags.
	TagAPEv2 TagKind = "apev2"
	// TagLyrics3 is a Lyrics3v2 block. It is reported, never read as tags.
	TagLyrics3 TagKind = "lyrics3"
	// TagVorbisComment is a Vorbis comment block or header: FLAC, Ogg Vorbis, Opus.
	TagVorbisComment TagKind = "vorbis-comment"
	// TagMP4 is the moov/udta/meta/ilst metadata list of M4A and M4B files.
	TagMP4 TagKind = "mp4"
	// TagRIFFInfo is the LIST/INFO chunk of WAV files.
	TagRIFFInfo TagKind = "riff-info"
	// TagAIFFText is the NAME, AUTH, (c) and ANNO chunks of AIFF files.
	TagAIFFText TagKind = "aiff-text"
)

// TagSource describes one tag container stored in a file.
//
// Version is the ID3v2 major version byte as stored (2, 3 or 4 for a tag
// audiometa reads) for [TagID3v2], and 0 for kinds without versions. Used
// reports whether audiometa read the container's values into the file's Tags.
// It is false for a container audiometa does not read (APEv2, Lyrics3, an
// ID3v2 tag in front of a FLAC stream or after the audio, an ID3v2 tag of an
// unsupported version or too damaged to read) and for an ID3v1 trailer that
// supplied nothing: the ID3v2 tag made it unnecessary, another tag block
// follows it, or it was empty. Used is per container, not per field: when two
// containers are both used, which one supplied a given field follows the
// format's precedence (ID3v2 before ID3v1; ID3 before native text).
type TagSource struct {
	Kind    TagKind
	Version int
	Used    bool
}

// AddTagSource appends s to f.TagSources. Parsers call it in the order the
// list reports: the containers the reader consults first, then those it does
// not read. It is a function, not a method, so types embedding File do not
// promote it.
func AddTagSource(f *File, s TagSource) {
	if f.TagSources == nil {
		f.TagSources = make([]TagSource, 0, maxTagSources)
	}
	f.TagSources = append(f.TagSources, s)
}

// maxTagSources is the most containers a file usually holds; the list grows
// past it for a file with more trailing blocks.
const maxTagSources = 4
