package m4a

import (
	"context"
	"errors"
	"io"

	"github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/registry"
	"github.com/simonhull/audiometa/internal/types"
)

// parser implements the audiometa.FormatParser interface.
type parser struct{}

// detectM4Format classifies the file as M4A or M4B using the same ftyp brand
// rules as types.DetectFormat, so Parse and the public Open path agree.
// Files DetectFormat rejects (no ftyp, unknown brand) default to M4A.
func detectM4Format(r io.ReaderAt, size int64, path string) types.Format {
	if format, err := types.DetectFormat(r, size, path); err == nil && format == types.FormatM4B {
		return types.FormatM4B
	}
	return types.FormatM4A
}

// Parse parses an M4A/M4B file and extracts metadata.
func (p *parser) Parse(ctx context.Context, r io.ReaderAt, size int64, path string) (*types.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Create safe reader
	sr := binary.NewSafeReader(r, size, path)

	// Detect format internally (check for ftyp atom to determine M4A vs M4B)
	format := detectM4Format(r, size, path)

	// Initialize file
	file := &types.File{
		Path:   path,
		Format: format,
		Size:   size,
		Tags:   types.Tags{},
		Audio:  types.AudioInfo{},
	}

	// Find moov atom (movie container)
	moovAtom, err := findAtomContext(ctx, sr, 0, size, "moov")
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		// No moov atom - return basic file info
		return file, nil
	}

	// Locate the iTunes metadata list. Its absence is normal (untagged file)
	// and must not stop technical info or chapters from being parsed.
	ilstAtom, err := findIlst(ctx, sr, moovAtom)
	if err != nil && !errors.Is(err, errAtomNotFound) {
		file.Warnings = types.AppendWarning(file.Warnings, types.Warning{
			Stage:   types.StageMetadata,
			Message: err.Error(),
			Err:     err,
		})
	}

	// Extract metadata from ilst
	if ilstAtom != nil {
		if err := extractIlstMetadata(ctx, sr, ilstAtom, file); err != nil {
			file.Warnings = types.AppendWarning(file.Warnings, types.Warning{
				Stage:   types.StageMetadata,
				Message: err.Error(),
				Err:     err,
			})
		}
	}

	// Parse technical info (duration, bitrate, codec, sample rate, channels)
	if err := parseTechnicalInfo(sr, moovAtom, file); err != nil {
		file.Warnings = types.AppendWarning(file.Warnings, types.Warning{
			Stage:   types.StageTechnical,
			Message: err.Error(),
			Err:     err,
		})
	}

	// Parse chapters
	chapters, err := parseChapters(ctx, sr, moovAtom, file.Audio.Duration)
	if err != nil {
		file.Warnings = types.AppendWarning(file.Warnings, types.Warning{
			Stage:   types.StageChapters,
			Message: err.Error(),
			Err:     err,
		})
	} else if len(chapters) > 0 {
		file.Chapters = chapters
	}

	// Parse audiobook-specific tags (narrator, series, publisher, etc.)
	if ilstAtom != nil {
		if err := parseAudiobookTags(ctx, sr, ilstAtom, file); err != nil {
			file.Warnings = types.AppendWarning(file.Warnings, types.Warning{
				Stage:   types.StageMetadata,
				Message: err.Error(),
				Err:     err,
			})
		}
	}

	// Stages that observed cancellation record it as a warning; report it as the result.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return file, nil
}

// findIlst navigates moov → udta → meta → ilst and returns the ilst atom.
// The returned error wraps errAtomNotFound when any level is simply absent.
func findIlst(ctx context.Context, sr *binary.SafeReader, moovAtom *Atom) (*Atom, error) {
	udtaAtom, err := findAtomContext(ctx, sr, moovAtom.DataOffset(), moovAtom.DataOffset()+int64(moovAtom.DataSize()), "udta")
	if err != nil {
		return nil, err
	}

	metaAtom, err := findAtomContext(ctx, sr, udtaAtom.DataOffset(), udtaAtom.DataOffset()+int64(udtaAtom.DataSize()), "meta")
	if err != nil {
		return nil, err
	}

	// meta is a full atom: 4 bytes of version+flags precede its children.
	metaDataOffset := metaAtom.DataOffset() + 4
	metaDataEnd := metaAtom.DataOffset() + int64(metaAtom.DataSize())

	return findAtomContext(ctx, sr, metaDataOffset, metaDataEnd, "ilst")
}

// ExtractArtwork extracts embedded artwork from M4A/M4B files.
func (p *parser) ExtractArtwork(ctx context.Context, r io.ReaderAt, size int64, path string) ([]types.Artwork, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sr := binary.NewSafeReader(r, size, path)
	return extractArtwork(ctx, sr, size)
}

// init registers the M4A/M4B parser.
func init() {
	p := &parser{}
	registry.Register(types.FormatM4A, p)
	registry.Register(types.FormatM4B, p)
}
