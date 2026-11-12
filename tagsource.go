package audiometa

import "github.com/simonhull/audiometa/internal/types"

// TagKind names a tag container format. Its values are stable strings, safe to
// persist and compare.
type TagKind = types.TagKind

// Tag container kinds reported in File.TagSources:
//
//   - TagID3v2 ("id3v2"): an ID3v2.2, 2.3 or 2.4 tag.
//   - TagID3v1 ("id3v1"): the 128-byte ID3v1 trailer.
//   - TagAPEv2 ("apev2"): an APEv1 or APEv2 tag, stored but never read.
//   - TagLyrics3 ("lyrics3"): a Lyrics3v2 block, stored but never read.
//   - TagVorbisComment ("vorbis-comment"): FLAC, Ogg Vorbis and Opus comments.
//   - TagMP4 ("mp4"): the moov/udta/meta/ilst list of M4A and M4B files.
//   - TagRIFFInfo ("riff-info"): the LIST/INFO chunk of WAV files.
//   - TagAIFFText ("aiff-text"): the NAME, AUTH, (c) and ANNO chunks of AIFF files.
const (
	TagID3v2         = types.TagID3v2
	TagID3v1         = types.TagID3v1
	TagAPEv2         = types.TagAPEv2
	TagLyrics3       = types.TagLyrics3
	TagVorbisComment = types.TagVorbisComment
	TagMP4           = types.TagMP4
	TagRIFFInfo      = types.TagRIFFInfo
	TagAIFFText      = types.TagAIFFText
)

// TagSource describes one tag container stored in a file. File.TagSources
// lists them, so a caller can tell which tags a file carries, and which of
// them its [Tags] were read from, without parsing the file itself.
//
// Version is the ID3v2 major version byte as stored (2, 3 or 4 for a tag
// audiometa reads) for [TagID3v2], and 0 for kinds without versions. Used
// reports whether audiometa read the container's values into [Tags]. It is
// false for a container audiometa does not read (APEv2, Lyrics3, an ID3v2 tag
// in front of a FLAC stream or after the audio, an ID3v2 tag of an unsupported
// version or too damaged to read) and for an ID3v1 trailer that supplied
// nothing: the ID3v2 tag made it unnecessary, another tag block follows it, or
// it was empty. Used is per container, not per field: when two containers are
// both used, which one supplied a given field follows the format's precedence
// (ID3v2 before ID3v1; ID3 before native text).
//
// A write keeps an ID3v2 tag's version (2.3 or 2.4); an ID3v2.2 tag, or a file
// without ID3v2, gets ID3v2.4. Containers audiometa does not read are kept
// byte for byte. A TagSource is a read-only report: no option or edit changes
// it.
type TagSource = types.TagSource
