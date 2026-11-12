package ogg

import (
	"context"

	"github.com/simonhull/audiometa/internal/types"
	"github.com/simonhull/audiometa/internal/vorbis"
)

// applyCommentHeader decodes the body of a Vorbis or Opus comment header (everything after
// its magic) onto file. A header that runs past its packet adds one StageMetadata warning
// and keeps the comments decoded before it; the only error returned is the context's.
func applyCommentHeader(ctx context.Context, body []byte, file *types.File) error {
	c, err := vorbis.Split(ctx, body)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		types.Warn(file, types.StageMetadata, 0, err, "failed to parse Vorbis comments: "+err.Error())
	}
	if comments := vorbis.Apply(c.Entries, file); len(comments) > 0 {
		file.Chapters = vorbis.ParseChapters(comments, file.Audio.Duration)
	}
	return nil
}
