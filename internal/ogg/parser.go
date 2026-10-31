package ogg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/parsing"
	"github.com/simonhull/audiometa/internal/registry"
	"github.com/simonhull/audiometa/internal/types"
)

const (
	codecVorbis  = "vorbis"
	containerOgg = "Ogg"
)

// parser implements the audiometa.FormatParser interface for Ogg Vorbis files.
type parser struct{}

// Parse parses an Ogg Vorbis file and extracts metadata.
func (p *parser) Parse(ctx context.Context, r io.ReaderAt, size int64, path string) (*types.File, error) { //nolint:gocyclo // Multiple codec types and error handling require branching
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Create safe reader
	sr := binary.NewSafeReader(r, size, path)

	// Verify Ogg magic bytes ("OggS")
	magic := make([]byte, 4)
	if err := sr.ReadAt(magic, 0, "Ogg magic bytes"); err != nil {
		return nil, fmt.Errorf("read Ogg magic: %w", err)
	}
	if string(magic) != capturePattern {
		return nil, &types.CorruptedFileError{
			Path:   path,
			Offset: 0,
			Reason: "invalid Ogg magic bytes",
		}
	}

	// Initialize file
	file := &types.File{
		Path:   path,
		Format: types.FormatOgg,
		Size:   size,
		Tags:   types.Tags{},
		Audio:  types.AudioInfo{},
	}

	packets, err := readHeaderPackets(ctx, sr, size, file, maxHeaderBytes)
	if err != nil {
		return nil, err
	}

	if len(packets) < 2 {
		return nil, fmt.Errorf("not enough packets found (need at least 2, got %d)", len(packets))
	}

	// Detect codec (Vorbis or Opus) from first packet
	codec := detectOggCodec(packets[0])

	switch codec {
	case codecVorbis:
		file.Format = types.FormatOgg
		// Parse Vorbis identification header (packet 0)
		if err := parseVorbisIdentification(packets[0], file); err != nil {
			return nil, fmt.Errorf("failed to parse Vorbis identification header: %w", err)
		}

		// Duration first: the comment parser derives the last chapter's end time from it.
		// Calculate duration from last page's granule position
		if file.Audio.SampleRate > 0 {
			duration, err := calculateDuration(sr, size, file.Audio.SampleRate)
			if err != nil {
				// Non-fatal - add warning
				file.Warnings = types.AppendWarning(file.Warnings, types.Warning{
					Stage:   types.StageTechnical,
					Message: fmt.Sprintf("failed to calculate duration: %v", err),
					Err:     err,
				})
			} else {
				file.Audio.Duration = duration
			}
		}

		// Parse Vorbis comment header (packet 1)
		if err := parseVorbisComment(packets[1], file); err != nil {
			// Non-fatal - add warning
			file.Warnings = types.AppendWarning(file.Warnings, types.Warning{
				Stage:   types.StageMetadata,
				Message: fmt.Sprintf("failed to parse Vorbis comment header: %v", err),
				Err:     err,
			})
		}

	case "opus":
		file.Format = types.FormatOpus
		// Parse OpusHead identification header (packet 0)
		if err := parseOpusHead(packets[0], file); err != nil {
			return nil, fmt.Errorf("failed to parse OpusHead header: %w", err)
		}

		// Duration first: the comment parser derives the last chapter's end time from it.
		// Calculate duration (Opus always at 48kHz)
		duration, err := calculateDuration(sr, size, 48000)
		if err != nil {
			// Non-fatal - add warning
			file.Warnings = types.AppendWarning(file.Warnings, types.Warning{
				Stage:   types.StageTechnical,
				Message: fmt.Sprintf("failed to calculate duration: %v", err),
				Err:     err,
			})
		} else {
			file.Audio.Duration = duration
		}

		// Parse OpusTags comment header (packet 1)
		if err := parseOpusTags(packets[1], file); err != nil {
			// Non-fatal - add warning
			file.Warnings = types.AppendWarning(file.Warnings, types.Warning{
				Stage:   types.StageMetadata,
				Message: fmt.Sprintf("failed to parse OpusTags header: %v", err),
				Err:     err,
			})
		}

		// Estimate bitrate for Opus (no nominal bitrate in header)
		if file.Audio.Duration > 0 {
			file.Audio.Bitrate = estimateOpusBitrate(size, file.Audio.Duration)
		}

	default:
		return nil, fmt.Errorf("unknown or unsupported Ogg codec: %q", codec)
	}

	// Post-parse fallbacks for audiobook series metadata.
	if file.Tags.Series == "" && file.Tags.Grouping != "" {
		series, part := parsing.ParseGrouping(file.Tags.Grouping)
		if series != "" {
			file.Tags.Series = series
			if file.Tags.SeriesPart == "" && part != "" {
				file.Tags.SeriesPart = part
			}
		}
	}
	if file.Tags.Series != "" && file.Tags.SeriesPart == "" {
		if part := parsing.ExtractSeriesPartFromPath(path); part != "" {
			file.Tags.SeriesPart = part
		}
	}

	return file, nil
}

// maxHeaderBytes bounds how much page data is read while looking for the
// identification and comment headers. Comment packets carrying embedded cover
// art (METADATA_BLOCK_PICTURE) can span many pages.
const maxHeaderBytes = 16 << 20

// readHeaderPackets reads pages from the start of the first logical stream until
// its first two packets (identification and comment headers) are complete, the
// file ends, or maxBytes of page data have been read. A read error on the first
// page is fatal; later errors and hitting the cap are recorded as warnings and
// parsing continues with the packets gathered so far.
func readHeaderPackets(ctx context.Context, sr *binary.SafeReader, size int64, file *types.File, maxBytes int64) ([][]byte, error) {
	var pages []*Page
	var serial uint32
	var read int64
	complete := 0 // packets terminated so far
	offset := int64(0)

	for i := 0; offset < size && complete < 2; i++ {
		if err := binary.Canceled(ctx, i); err != nil {
			return nil, err
		}

		page, nextOffset, err := readPage(sr, offset)
		if err != nil {
			if i == 0 {
				return nil, fmt.Errorf("failed to read first Ogg page: %w", err)
			}
			file.Warnings = types.AppendWarning(file.Warnings, types.Warning{
				Stage:   types.StageMetadata,
				Message: fmt.Sprintf("failed to read Ogg page %d: %v", i, err),
				Err:     err,
				Offset:  offset,
			})
			break
		}
		offset = nextOffset
		if i == 0 {
			serial = page.SerialNumber
		} else if page.SerialNumber != serial {
			continue // another multiplexed stream
		}

		read += int64(len(page.Data))
		if read > maxBytes {
			file.Warnings = types.AppendWarning(file.Warnings, types.Warning{
				Stage:   types.StageMetadata,
				Message: fmt.Sprintf("Ogg comment header exceeds %d bytes; truncated", maxBytes),
			})
			break
		}
		pages = append(pages, page)
		for _, lace := range page.laceValues() {
			if lace < 255 {
				complete++
			}
		}
	}

	if len(pages) == 0 {
		return nil, errors.New("no Ogg pages found")
	}
	return extractPackets(pages), nil
}

// detectOggCodec determines whether this is Vorbis or Opus
// by examining the magic marker in the first packet.
//
// Returns:
//   - "vorbis" for Ogg Vorbis files
//   - "opus" for Ogg Opus files
//   - "unknown" for unrecognized codecs
func detectOggCodec(firstPacket []byte) string {
	// Check for OpusHead (8 bytes)
	if len(firstPacket) >= 8 && string(firstPacket[0:8]) == "OpusHead" {
		return "opus"
	}

	// Check for Vorbis (7 bytes: 0x01 + "vorbis")
	if len(firstPacket) >= 7 && firstPacket[0] == 0x01 && string(firstPacket[1:7]) == codecVorbis {
		return codecVorbis
	}

	return "unknown"
}

// estimateOpusBitrate estimates the bitrate for an Opus file.
//
// Opus files don't have a nominal bitrate field in the header, so we
// estimate it from the file size and duration.
//
// We subtract approximately 5KB for headers and metadata overhead.
func estimateOpusBitrate(fileSize int64, duration time.Duration) int {
	if duration == 0 {
		return 0
	}

	// Estimate audio data size (subtract ~5KB for headers/tags)
	audioSize := fileSize - 5000
	if audioSize < 0 {
		audioSize = fileSize
	}

	// Calculate bitrate: (size in bits) / (duration in seconds)
	seconds := duration.Seconds()
	if seconds == 0 {
		return 0
	}

	bitrate := int((float64(audioSize) * 8) / seconds)
	return bitrate
}

// Duration = granule_position / sample_rate.
func calculateDuration(sr *binary.SafeReader, fileSize int64, sampleRate int) (time.Duration, error) {
	if sampleRate == 0 {
		return 0, errors.New("sample rate is zero")
	}

	// Find last page's granule position
	granule, err := findLastGranulePosition(sr, fileSize)
	if err != nil {
		return 0, err
	}

	// Granule position -1 means "not set"
	if granule < 0 {
		return 0, errors.New("granule position not set")
	}

	// Calculate duration (granule is in samples)
	seconds := float64(granule) / float64(sampleRate)
	return time.Duration(seconds * float64(time.Second)), nil
}

// init registers the Ogg parser for both Vorbis and Opus formats.
func init() {
	p := &parser{}
	registry.Register(types.FormatOgg, p)
	registry.Register(types.FormatOpus, p)
}
