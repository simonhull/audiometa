package audiometa

import (
	"github.com/simonhull/audiometa/internal/types"
)

// Chapter is a chapter marker: a 1-based Index, a Title, StartTime and
// EndTime offsets from the start of the audio, and the [ChapterSource] it
// was read from.
//
// File.Chapters lists chapters in playback order. Where a format stores only
// start times, each EndTime is the next chapter's StartTime and the last one
// ends at the file's duration.
//
// Chapters are read from:
//
//   - M4A/M4B: the QuickTime chapter text track, else the Nero chpl atom.
//   - MP3: ID3v2 CHAP frames, else the OverDrive MediaMarkers TXXX frame.
//   - FLAC: CHAPTERxxx Vorbis comments, else the CUESHEET block.
//   - Ogg Vorbis and Opus: CHAPTERxxx Vorbis comments.
type Chapter = types.Chapter

// ChapterSource names the container structure a [Chapter] was read from. Its
// values are stable strings, safe to persist and compare; the empty string
// means unknown.
type ChapterSource = types.ChapterSource

// Chapter sources reported in Chapter.Source:
//
//   - ChapterSourceQuickTime ("quicktime"): an MP4 chapter text track.
//   - ChapterSourceNero ("nero"): an MP4 udta/chpl atom.
//   - ChapterSourceID3CHAP ("id3_chap"): ID3v2 CHAP frames.
//   - ChapterSourceOverDrive ("overdrive"): the ID3v2 TXXX "OverDrive
//     MediaMarkers" frame.
//   - ChapterSourceCueSheet ("flac_cuesheet"): a FLAC CUESHEET block.
//   - ChapterSourceVorbisComment ("vorbis_comment"): CHAPTERxxx Vorbis
//     comments in FLAC, Ogg Vorbis or Opus.
const (
	ChapterSourceQuickTime     = types.ChapterSourceQuickTime
	ChapterSourceNero          = types.ChapterSourceNero
	ChapterSourceID3CHAP       = types.ChapterSourceID3CHAP
	ChapterSourceOverDrive     = types.ChapterSourceOverDrive
	ChapterSourceCueSheet      = types.ChapterSourceCueSheet
	ChapterSourceVorbisComment = types.ChapterSourceVorbisComment
)
