package mp3

import (
	"context"
	"fmt"
	"io"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/registry"
	"github.com/simonhull/audiometa/internal/replaygain"
	"github.com/simonhull/audiometa/internal/trailer"
	"github.com/simonhull/audiometa/internal/types"
)

// parser implements the audiometa.FormatParser interface.
type parser struct{}

// Parse parses a single MP3 file and extracts metadata.
func (p *parser) Parse(ctx context.Context, r io.ReaderAt, size int64, path string) (*types.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Create safe reader
	sr := binutil.NewSafeReader(r, size, path)

	// Initialize file
	file := &types.File{
		Path:   path,
		Format: types.FormatMP3,
		Size:   size,
		Tags:   types.Tags{},
		Audio:  types.AudioInfo{Container: "MP3"},
	}

	// ID3v2 tag. A file without one (untagged, or with only an ID3v1
	// trailer) is valid and gets no warning; a tag that is there but cannot
	// be read does, and the frame scan below still runs.
	var tagSize int64
	if hasID3Magic(sr, size) {
		var err error
		if tagSize, err = parseID3v2(ctx, sr, file); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			file.Warnings = types.AppendWarning(file.Warnings, types.Warning{
				Stage:   types.StageMetadata,
				Message: fmt.Sprintf("ID3v2 parsing failed: %v", err),
				Err:     err,
			})
		}
	}

	// Trailing tags (APEv2, Lyrics3v2, ID3v1, ...) are never part of the audio.
	// The ID3v1 trailer is also a fallback for tags, when it is the last block.
	audioEnd, err := trailer.Start(ctx, sr, tagSize, size)
	if err != nil {
		return nil, err
	}
	if last, err := trailer.EndsWithID3v1(sr, tagSize, size); err != nil {
		return nil, err
	} else if last && id3v1Fallback(&file.Tags) {
		if v1, ok := readID3v1(sr, size); ok {
			applyID3v1(file, v1)
		}
	}

	// Parse MP3 frame headers for technical info (bitrate, duration, etc.)
	if err := parseTechnicalInfo(ctx, sr, tagSize, audioEnd, file); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		file.Warnings = types.AppendWarning(file.Warnings, types.Warning{
			Stage:   types.StageTechnical,
			Message: fmt.Sprintf("failed to parse MP3 technical info: %v", err),
			Err:     err,
		})
	}

	closeOverDriveChapters(file)
	replaygain.Apply(file)

	return file, nil
}

// closeOverDriveChapters ends the last OverDrive chapter at the file's end:
// MediaMarkers carry only start times, and the duration is known only after the
// technical pass.
func closeOverDriveChapters(file *types.File) {
	n := len(file.Chapters)
	if n == 0 {
		return
	}
	last := &file.Chapters[n-1]
	if last.Source == types.ChapterSourceOverDrive && last.EndTime == 0 && file.Audio.Duration > last.StartTime {
		last.EndTime = file.Audio.Duration
	}
}

// ExtractArtwork extracts embedded artwork from MP3 files.
func (p *parser) ExtractArtwork(ctx context.Context, r io.ReaderAt, size int64, path string) ([]types.Artwork, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return extractArtwork(ctx, r, size, path)
}

// init registers the MP3 parser.
func init() {
	registry.Register(types.FormatMP3, &parser{})
}
