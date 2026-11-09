package flac

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/picture"
	"github.com/simonhull/audiometa/internal/registry"
	"github.com/simonhull/audiometa/internal/replaygain"
	"github.com/simonhull/audiometa/internal/trailer"
	"github.com/simonhull/audiometa/internal/types"
	"github.com/simonhull/audiometa/internal/vorbis"
)

// Codec and container name.
const codecName = "FLAC"

// Metadata block types.
const (
	blockTypeStreamInfo    = 0
	blockTypePadding       = 1
	blockTypeApplication   = 2
	blockTypeSeekTable     = 3
	blockTypeVorbisComment = 4
	blockTypeCueSheet      = 5
	blockTypePicture       = 6
)

// parser implements the audiometa.FormatParser interface for FLAC files.
type parser struct{}

// Parse parses a FLAC file and extracts metadata.
func (p *parser) Parse(ctx context.Context, r io.ReaderAt, size int64, path string) (*types.File, error) { //nolint:gocyclo // Block processing requires multiple conditional branches
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Create safe reader
	sr := binary.NewSafeReader(r, size, path)

	// The "fLaC" marker starts the file, or follows a leading ID3v2 tag.
	start, err := streamStart(sr, size)
	switch {
	case errors.Is(err, errNoMagic):
		return nil, &types.CorruptedFileError{
			Path:   path,
			Offset: 0,
			Reason: "invalid FLAC magic bytes",
		}
	case err != nil:
		return nil, fmt.Errorf("read FLAC magic: %w", err)
	}

	// Initialize file
	file := &types.File{
		Path:   path,
		Format: types.FormatFLAC,
		Size:   size,
		Tags:   types.Tags{},
		Audio:  types.AudioInfo{},
	}
	if start > 0 {
		file.Warnings = types.AppendWarning(file.Warnings, types.Warning{
			Stage:   types.StageMetadata,
			Message: fmt.Sprintf("non-standard ID3v2 tag of %d bytes before the FLAC stream; its frames are ignored", start),
		})
	}

	var vorbisComments []string
	var audioStart int64 // first audio byte; 0 until the chain's last block is read
	for b, err := range blocks(ctx, sr, start, size) {
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			file.Warnings = types.AppendWarning(file.Warnings, types.Warning{
				Stage:   types.StageMetadata,
				Message: fmt.Sprintf("metadata block chain at offset %d: %v", b.off, err),
				Err:     err,
				Offset:  b.off,
			})
			break
		}
		offset, blockLength := b.payload(), b.n
		switch b.typ {
		case blockTypeStreamInfo:
			if err := parseStreamInfo(sr, offset, blockLength, file); err != nil {
				file.Warnings = types.AppendWarning(file.Warnings, types.Warning{
					Stage:   types.StageMetadata,
					Message: fmt.Sprintf("failed to parse STREAMINFO: %v", err),
					Err:     err,
					Offset:  offset,
				})
			}

		case blockTypeVorbisComment:
			comments, err := parseVorbisComment(ctx, sr, b, size, file)
			vorbisComments = append(vorbisComments, comments...)
			if err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return nil, ctxErr
				}
				file.Warnings = types.AppendWarning(file.Warnings, types.Warning{
					Stage:   types.StageMetadata,
					Message: fmt.Sprintf("failed to parse Vorbis comments: %v", err),
					Err:     err,
					Offset:  offset,
				})
			}

		case blockTypePicture:
			// Pictures are handled lazily via ExtractArtwork()
			// For now, just skip

		case blockTypePadding:
			// Padding blocks are ignored

		case blockTypeApplication:
			// Application blocks are ignored for now

		case blockTypeSeekTable:
			// Seek table is not needed for metadata extraction

		case blockTypeCueSheet:
			if err := parseCueSheet(sr, offset, blockLength, file); err != nil {
				file.Warnings = types.AppendWarning(file.Warnings, types.Warning{
					Stage:   types.StageChapters,
					Message: fmt.Sprintf("failed to parse CUESHEET: %v", err),
					Err:     err,
					Offset:  offset,
				})
			}

		default:
			// Unknown block type - skip it
		}

		if b.last {
			audioStart = b.end()
		}
	}

	// CHAPTERxxx= comments carry titles and are what audiobook taggers write; they take
	// precedence over CUESHEET track markers. Runs after the loop so block order doesn't matter
	// and STREAMINFO's duration is known.
	if chapters := vorbis.ParseChapters(vorbisComments, file.Audio.Duration); len(chapters) > 0 {
		file.Chapters = chapters
	}

	// Approximate bitrate from audio frames only (metadata such as cover art excluded).
	// Unknown when the metadata chain never reached its last block.
	// Trailing APEv2, Lyrics3v2 and ID3v1 tags are not audio either.
	if audioStart > 0 && audioStart < size && file.Audio.Duration > 0 {
		audioEnd, err := trailer.Start(ctx, sr, audioStart, size)
		if err != nil {
			return nil, err
		}
		file.Audio.Bitrate = types.AverageBitrate(audioEnd-audioStart, file.Audio.Duration)
	}

	// Set container and codec
	file.Audio.Container = codecName
	file.Audio.Codec = codecName
	file.Audio.Lossless = true

	replaygain.Apply(file)

	return file, nil
}

// ExtractArtwork extracts embedded artwork from FLAC files.
func (p *parser) ExtractArtwork(ctx context.Context, r io.ReaderAt, size int64, path string) ([]types.Artwork, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sr := binary.NewSafeReader(r, size, path)

	start, err := streamStart(sr, size)
	if err != nil {
		return nil, nil // as for a broken chain below: the parser reports it
	}

	var artwork []types.Artwork

	for b, err := range blocks(ctx, sr, start, size) {
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			break // a broken chain ends the scan; the parser reports it
		}
		if b.typ != blockTypePicture {
			continue
		}
		// An unreadable or unparseable picture is skipped; the block chain is
		// still followed normally.
		payload, err := readPayload(sr, b, size)
		if err != nil {
			continue
		}
		if pic, err := picture.Decode(payload); err == nil {
			artwork = append(artwork, pic)
		}
	}

	return artwork, nil
}

// parseStreamInfo extracts audio info from STREAMINFO block.
func parseStreamInfo(sr *binary.SafeReader, offset, blockLength int64, file *types.File) error {
	// STREAMINFO is exactly 34 bytes
	if blockLength != 34 {
		return fmt.Errorf("invalid STREAMINFO size: %d (expected 34)", blockLength)
	}

	// Read all 34 bytes
	data := make([]byte, 34)
	if err := sr.ReadAt(data, offset, "STREAMINFO block"); err != nil {
		return err
	}

	// Parse fields (all big-endian)
	// Bytes 0-1: Min block size (16 bits)
	// Bytes 2-3: Max block size (16 bits)
	// Bytes 4-6: Min frame size (24 bits)
	// Bytes 7-9: Max frame size (24 bits)

	// Bytes 10-17: Sample rate (20 bits), channels (3 bits), bits per sample (5 bits), total samples (36 bits)
	// This is a bit-packed 64-bit value
	packed := uint64(data[10])<<56 | uint64(data[11])<<48 | uint64(data[12])<<40 | uint64(data[13])<<32 |
		uint64(data[14])<<24 | uint64(data[15])<<16 | uint64(data[16])<<8 | uint64(data[17])

	sampleRate := (packed >> 44) & 0xFFFFF       // Top 20 bits
	channels := ((packed >> 41) & 0x7) + 1       // Next 3 bits, stored as (channels - 1)
	bitsPerSample := ((packed >> 36) & 0x1F) + 1 // Next 5 bits, stored as (bits - 1)
	totalSamples := packed & 0xFFFFFFFFF         // Bottom 36 bits

	// Calculate duration
	if sampleRate > 0 {
		durationSeconds := float64(totalSamples) / float64(sampleRate)
		file.Audio.Duration = time.Duration(durationSeconds * float64(time.Second))
	}

	if totalSamples == 0 {
		file.Warnings = types.AppendWarning(file.Warnings, types.Warning{
			Stage:   types.StageTechnical,
			Message: "STREAMINFO total samples is 0 (unknown); duration not available",
			Offset:  offset,
		})
	}

	// Set audio properties
	file.Audio.SampleRate = int(sampleRate)
	file.Audio.Channels = int(channels)
	file.Audio.BitDepth = int(bitsPerSample)

	return nil
}

// parseVorbisComment decodes the VORBIS_COMMENT block b and maps each
// comment onto file. Nothing outside the block's payload is read: a length
// that runs past it ends the block with an error wrapping [vorbis.ErrTruncated]. The
// comments decoded before an error are still applied and returned.
func parseVorbisComment(ctx context.Context, sr *binary.SafeReader, b block, size int64, file *types.File) ([]string, error) {
	payload, err := readPayload(sr, b, size)
	if err != nil {
		return nil, err
	}
	c, err := vorbis.Split(ctx, payload)
	return vorbis.Apply(c.Entries, file), err
}

// init registers the FLAC parser.
func init() {
	registry.Register(types.FormatFLAC, &parser{})
}
