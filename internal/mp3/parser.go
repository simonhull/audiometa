package mp3

import (
	"context"
	"fmt"
	"io"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/registry"
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
		Audio:  types.AudioInfo{},
	}

	// Parse ID3v2 tag (if present)
	tagSize, err := parseID3v2(ctx, sr, file)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		// Not an ID3v2 file or parse error - try to find MP3 frames anyway
		file.Warnings = types.AppendWarning(file.Warnings, types.Warning{
			Stage:   types.StageMetadata,
			Message: fmt.Sprintf("ID3v2 parsing failed: %v", err),
			Err:     err,
		})
	}

	// ID3v1 trailer: a fallback for tags, and never part of the audio.
	v1, hasV1 := readID3v1(sr, size)
	if hasV1 && file.Tags.Title == "" && file.Tags.Artist == "" {
		applyID3v1(file, v1)
	}
	audioEnd := size
	if hasV1 {
		audioEnd -= id3v1Size
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
