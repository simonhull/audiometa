package ogg

import (
	"fmt"
	"io"
	"time"

	"github.com/simonhull/audiometa/internal/binary"
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
func (p *parser) Parse(r io.ReaderAt, size int64, path string) (*types.File, error) { //nolint:gocyclo // Multiple codec types and error handling require branching
	// Create safe reader
	sr := binary.NewSafeReader(r, size, path)

	// Verify Ogg magic bytes ("OggS")
	magic := make([]byte, 4)
	if err := sr.ReadAt(magic, 0, "Ogg magic bytes"); err != nil {
		return nil, fmt.Errorf("read Ogg magic: %w", err)
	}
	if string(magic) != "OggS" {
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

	// Read first few pages (Vorbis headers are always in the first pages)
	var pages []*Page
	offset := int64(0)

	// Read up to 3 pages (identification, comment, setup headers)
	for i := 0; i < 3 && offset < size; i++ {
		page, nextOffset, err := readPage(sr, offset)
		if err != nil {
			if i == 0 {
				// First page failed - this is fatal
				return nil, fmt.Errorf("failed to read first Ogg page: %w", err)
			}
			// Subsequent pages - add warning and stop
			file.Warnings = append(file.Warnings, types.Warning{
				Stage:   "metadata",
				Message: fmt.Sprintf("failed to read Ogg page %d: %v", i, err),
				Offset:  offset,
			})
			break
		}
		pages = append(pages, page)
		offset = nextOffset
	}

	if len(pages) == 0 {
		return nil, fmt.Errorf("no Ogg pages found")
	}

	// Extract packets from pages
	packets := extractPackets(pages)

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

		// Parse Vorbis comment header (packet 1)
		if err := parseVorbisComment(packets[1], file); err != nil {
			// Non-fatal - add warning
			file.Warnings = append(file.Warnings, types.Warning{
				Stage:   "metadata",
				Message: fmt.Sprintf("failed to parse Vorbis comment header: %v", err),
			})
		}

		// Calculate duration from last page's granule position
		if file.Audio.SampleRate > 0 {
			duration, err := calculateDuration(sr, size, file.Audio.SampleRate)
			if err != nil {
				// Non-fatal - add warning
				file.Warnings = append(file.Warnings, types.Warning{
					Stage:   "technical",
					Message: fmt.Sprintf("failed to calculate duration: %v", err),
				})
			} else {
				file.Audio.Duration = duration
			}
		}

	case "opus":
		file.Format = types.FormatOpus
		// Parse OpusHead identification header (packet 0)
		if err := parseOpusHead(packets[0], file); err != nil {
			return nil, fmt.Errorf("failed to parse OpusHead header: %w", err)
		}

		// Parse OpusTags comment header (packet 1)
		if err := parseOpusTags(packets[1], file); err != nil {
			// Non-fatal - add warning
			file.Warnings = append(file.Warnings, types.Warning{
				Stage:   "metadata",
				Message: fmt.Sprintf("failed to parse OpusTags header: %v", err),
			})
		}

		// Calculate duration (Opus always at 48kHz)
		duration, err := calculateDuration(sr, size, 48000)
		if err != nil {
			// Non-fatal - add warning
			file.Warnings = append(file.Warnings, types.Warning{
				Stage:   "technical",
				Message: fmt.Sprintf("failed to calculate duration: %v", err),
			})
		} else {
			file.Audio.Duration = duration
		}

		// Estimate bitrate for Opus (no nominal bitrate in header)
		if file.Audio.Duration > 0 {
			file.Audio.Bitrate = estimateOpusBitrate(size, file.Audio.Duration)
		}

	default:
		return nil, fmt.Errorf("unknown or unsupported Ogg codec: %q", codec)
	}

	return file, nil
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
		return 0, fmt.Errorf("sample rate is zero")
	}

	// Find last page's granule position
	granule, err := findLastGranulePosition(sr, fileSize)
	if err != nil {
		return 0, err
	}

	// Granule position -1 means "not set"
	if granule < 0 {
		return 0, fmt.Errorf("granule position not set")
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
