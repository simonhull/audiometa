package types

import "time"

// Chapter represents a chapter marker in an audio file.
//
// Chapters are supported in:
//   - M4B/M4A files (QuickTime chapter tracks, Nero CHPL format)
//   - MP3 files (ID3v2 CHAP frames)
//   - FLAC files (CUESHEET metadata block)
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
	Index     int           `json:"index"`
	Title     string        `json:"title"`
	StartTime time.Duration `json:"start_time"`
	EndTime   time.Duration `json:"end_time"`
}
