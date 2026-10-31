package audiometa

import (
	"github.com/simonhull/audiometa/internal/types"
)

// Chapter is an alias to types.Chapter for backwards compatibility.
// Re-exporting from internal/types to maintain public API.
type Chapter = types.Chapter

// ChapterSource is an alias to types.ChapterSource.
type ChapterSource = types.ChapterSource

// Chapter sources reported in Chapter.Source.
const (
	ChapterSourceQuickTime     = types.ChapterSourceQuickTime
	ChapterSourceNero          = types.ChapterSourceNero
	ChapterSourceID3CHAP       = types.ChapterSourceID3CHAP
	ChapterSourceOverDrive     = types.ChapterSourceOverDrive
	ChapterSourceCueSheet      = types.ChapterSourceCueSheet
	ChapterSourceVorbisComment = types.ChapterSourceVorbisComment
)
