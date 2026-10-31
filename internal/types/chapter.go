package types

import "time"

// Chapter represents a chapter marker in an audio file.
//
// Chapters are supported in:
//   - M4B/M4A files (QuickTime chapter tracks, Nero CHPL format)
//   - MP3 files (ID3v2 CHAP frames, else the OverDrive MediaMarkers TXXX frame)
//   - FLAC files (CHAPTER Vorbis comments, else the CUESHEET metadata block)
//   - Ogg Vorbis/Opus files (CHAPTER Vorbis comments)
//
// Access chapters via file.Chapters:
//
//	file, _ := audiometa.Open("audiobook.mp3")
//	for _, chapter := range file.Chapters {
//	    fmt.Printf("[%d] %s: %s - %s\n",
//	        chapter.Index,
//	        chapter.Title,
//	        chapter.StartTime,
//	        chapter.EndTime)
//	}
type Chapter struct {
	Title     string        `json:"title"`
	Index     int           `json:"index"`
	StartTime time.Duration `json:"start_time"`
	EndTime   time.Duration `json:"end_time"`

	// Source records which container structure the chapter was read from.
	// Empty means unknown.
	Source ChapterSource `json:"source,omitempty"`
}

// ChapterSource identifies which container structure a chapter list was read from.
// Values are stable strings, safe to persist and compare.
type ChapterSource string

const (
	// ChapterSourceQuickTime marks chapters read from an MP4 chapter text track (tref/chap).
	ChapterSourceQuickTime ChapterSource = "quicktime"
	// ChapterSourceNero marks chapters read from an MP4 udta/chpl atom.
	ChapterSourceNero ChapterSource = "nero"
	// ChapterSourceID3CHAP marks chapters read from ID3v2 CHAP frames.
	ChapterSourceID3CHAP ChapterSource = "id3_chap"
	// ChapterSourceOverDrive marks chapters read from the ID3v2 TXXX "OverDrive MediaMarkers" frame.
	ChapterSourceOverDrive ChapterSource = "overdrive"
	// ChapterSourceCueSheet marks chapters read from a FLAC CUESHEET block.
	ChapterSourceCueSheet ChapterSource = "flac_cuesheet"
	// ChapterSourceVorbisComment marks chapters read from CHAPTERxxx Vorbis comments (FLAC, Ogg Vorbis, Opus).
	ChapterSourceVorbisComment ChapterSource = "vorbis_comment"
)
