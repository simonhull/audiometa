package flac

import (
	"fmt"
	"io"
	"time"

	"github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/registry"
	"github.com/simonhull/audiometa/internal/types"
	"github.com/simonhull/audiometa/internal/vorbis"
)

// Metadata block types
const (
	blockTypeStreamInfo    = 0
	blockTypePadding       = 1
	blockTypeApplication   = 2
	blockTypeSeekTable     = 3
	blockTypeVorbisComment = 4
	blockTypeCueSheet      = 5
	blockTypePicture       = 6
)

// parser implements the audiometa.FormatParser interface for FLAC files
type parser struct{}

// Parse parses a FLAC file and extracts metadata
func (p *parser) Parse(r io.ReaderAt, size int64, path string) (*types.File, error) {
	// Create safe reader
	sr := binary.NewSafeReader(r, size, path)

	// Verify FLAC magic bytes ("fLaC")
	magic := make([]byte, 4)
	if err := sr.ReadAt(magic, 0, "FLAC magic bytes"); err != nil {
		return nil, fmt.Errorf("read FLAC magic: %w", err)
	}
	if string(magic) != "fLaC" {
		return nil, &types.CorruptedFileError{
			Path:   path,
			Offset: 0,
			Reason: "invalid FLAC magic bytes",
		}
	}

	// Initialize file
	file := &types.File{
		Path:   path,
		Format: types.FormatFLAC,
		Size:   size,
		Tags:   types.Tags{},
		Audio:  types.AudioInfo{},
	}

	// Parse metadata blocks
	offset := int64(4) // After "fLaC"
	for {
		if offset >= size {
			break
		}

		// Read metadata block header (4 bytes)
		header, err := binary.Read[uint32](sr, offset, "metadata block header")
		if err != nil {
			file.Warnings = append(file.Warnings, types.Warning{
				Stage:   "metadata",
				Message: fmt.Sprintf("failed to read metadata block header at offset %d: %v", offset, err),
				Offset:  offset,
			})
			break
		}

		// Parse header
		isLast := (header >> 31) == 1
		blockType := uint8((header >> 24) & 0x7F)
		blockLength := int64(header & 0x00FFFFFF)

		offset += 4 // Move past header

		// Parse block based on type
		switch blockType {
		case blockTypeStreamInfo:
			if err := parseStreamInfo(sr, offset, blockLength, file); err != nil {
				file.Warnings = append(file.Warnings, types.Warning{
					Stage:   "metadata",
					Message: fmt.Sprintf("failed to parse STREAMINFO: %v", err),
					Offset:  offset,
				})
			}

		case blockTypeVorbisComment:
			if err := parseVorbisComment(sr, offset, blockLength, file); err != nil {
				file.Warnings = append(file.Warnings, types.Warning{
					Stage:   "metadata",
					Message: fmt.Sprintf("failed to parse Vorbis comments: %v", err),
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
			if err := parseCueSheet(sr, offset, uint32(blockLength), file); err != nil {
				file.Warnings = append(file.Warnings, types.Warning{
					Stage:   "chapters",
					Message: fmt.Sprintf("failed to parse CUESHEET: %v", err),
					Offset:  offset,
				})
			}

		default:
			// Unknown block type - skip it
		}

		// Move to next block
		offset += blockLength

		// If this was the last metadata block, we're done
		if isLast {
			break
		}
	}

	// Set container and codec
	file.Audio.Container = "FLAC"
	file.Audio.Codec = "FLAC"
	file.Audio.Lossless = true

	return file, nil
}

// ExtractArtwork extracts embedded artwork from FLAC files
func (p *parser) ExtractArtwork(r io.ReaderAt, size int64, path string) ([]types.Artwork, error) {
	sr := binary.NewSafeReader(r, size, path)

	var artwork []types.Artwork

	// Skip FLAC magic
	offset := int64(4)

	// Scan for PICTURE blocks
	for offset < size {
		// Read metadata block header
		header, err := binary.Read[uint32](sr, offset, "metadata block header")
		if err != nil {
			break
		}

		isLast := (header >> 31) == 1
		blockType := uint8((header >> 24) & 0x7F)
		blockLength := int64(header & 0x00FFFFFF)

		offset += 4

		// If this is a PICTURE block, parse it
		if blockType == blockTypePicture {
			pic, err := parsePicture(sr, offset, blockLength)
			if err != nil {
				// Skip this picture but continue
				offset += blockLength
				continue
			}
			artwork = append(artwork, pic)
		}

		offset += blockLength

		if isLast {
			break
		}
	}

	return artwork, nil
}

// parseStreamInfo extracts audio info from STREAMINFO block
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

	sampleRate := (packed >> 44) & 0xFFFFF // Top 20 bits
	channels := ((packed >> 41) & 0x7) + 1  // Next 3 bits, stored as (channels - 1)
	bitsPerSample := ((packed >> 36) & 0x1F) + 1 // Next 5 bits, stored as (bits - 1)
	totalSamples := packed & 0xFFFFFFFFF // Bottom 36 bits

	// Calculate duration
	if sampleRate > 0 {
		durationSeconds := float64(totalSamples) / float64(sampleRate)
		file.Audio.Duration = time.Duration(durationSeconds * float64(time.Second))
	}

	// Set audio properties
	file.Audio.SampleRate = int(sampleRate)
	file.Audio.Channels = int(channels)
	file.Audio.BitDepth = int(bitsPerSample)

	// Calculate approximate bitrate (FLAC is variable bitrate)
	// Use file size and duration for a rough estimate
	if file.Audio.Duration > 0 {
		durationSeconds := file.Audio.Duration.Seconds()
		bitsPerSecond := (float64(file.Size) * 8) / durationSeconds
		file.Audio.Bitrate = int(bitsPerSecond)
	}

	return nil
}

// parseVorbisComment extracts tags from VORBIS_COMMENT block
func parseVorbisComment(sr *binary.SafeReader, offset, blockLength int64, file *types.File) error {
	currentOffset := offset

	// Read vendor string length (32-bit little-endian)
	vendorLength, err := binary.ReadLE[uint32](sr, currentOffset, "vendor string length")
	if err != nil {
		return err
	}
	currentOffset += 4

	// Skip vendor string
	currentOffset += int64(vendorLength)

	// Read number of comments (32-bit little-endian)
	numComments, err := binary.ReadLE[uint32](sr, currentOffset, "number of comments")
	if err != nil {
		return err
	}
	currentOffset += 4

	// Parse each comment
	for i := uint32(0); i < numComments; i++ {
		// Read comment length (32-bit little-endian)
		commentLength, err := binary.ReadLE[uint32](sr, currentOffset, "comment length")
		if err != nil {
			return fmt.Errorf("read comment %d length: %w", i, err)
		}
		currentOffset += 4

		// Read comment string (UTF-8)
		commentData := make([]byte, commentLength)
		if err := sr.ReadAt(commentData, currentOffset, fmt.Sprintf("comment %d", i)); err != nil {
			return fmt.Errorf("read comment %d: %w", i, err)
		}
		currentOffset += int64(commentLength)

		// Parse "KEY=VALUE" format
		comment := string(commentData)
		if err := vorbis.ParseComment(comment, &file.Tags); err != nil {
			// Non-fatal - add warning and continue
			file.Warnings = append(file.Warnings, types.Warning{
				Stage:   "metadata",
				Message: fmt.Sprintf("invalid Vorbis comment: %s", err),
			})
		}
	}

	return nil
}

// parsePicture extracts artwork from PICTURE block
func parsePicture(sr *binary.SafeReader, offset, blockLength int64) (types.Artwork, error) {
	currentOffset := offset

	// Read picture type (32-bit big-endian)
	pictureType, err := binary.Read[uint32](sr, currentOffset, "picture type")
	if err != nil {
		return types.Artwork{}, err
	}
	currentOffset += 4

	// Read MIME type length (32-bit big-endian)
	mimeLength, err := binary.Read[uint32](sr, currentOffset, "MIME type length")
	if err != nil {
		return types.Artwork{}, err
	}
	currentOffset += 4

	// Read MIME type string
	mimeData := make([]byte, mimeLength)
	if err := sr.ReadAt(mimeData, currentOffset, "MIME type"); err != nil {
		return types.Artwork{}, err
	}
	mimeType := string(mimeData)
	currentOffset += int64(mimeLength)

	// Read description length (32-bit big-endian)
	descLength, err := binary.Read[uint32](sr, currentOffset, "description length")
	if err != nil {
		return types.Artwork{}, err
	}
	currentOffset += 4

	// Read description string (UTF-8)
	descData := make([]byte, descLength)
	if descLength > 0 {
		if err := sr.ReadAt(descData, currentOffset, "description"); err != nil {
			return types.Artwork{}, err
		}
	}
	description := string(descData)
	currentOffset += int64(descLength)

	// Read width, height, color depth, indexed colors (4 × 32-bit big-endian)
	width, err := binary.Read[uint32](sr, currentOffset, "width")
	if err != nil {
		return types.Artwork{}, err
	}
	currentOffset += 4

	height, err := binary.Read[uint32](sr, currentOffset, "height")
	if err != nil {
		return types.Artwork{}, err
	}
	currentOffset += 4

	// Skip color depth and indexed colors (not used)
	currentOffset += 8

	// Read picture data length (32-bit big-endian)
	dataLength, err := binary.Read[uint32](sr, currentOffset, "picture data length")
	if err != nil {
		return types.Artwork{}, err
	}
	currentOffset += 4

	// Read picture data
	pictureData := make([]byte, dataLength)
	if err := sr.ReadAt(pictureData, currentOffset, "picture data"); err != nil {
		return types.Artwork{}, err
	}

	// Map FLAC picture type to types.ArtworkType
	var artType types.ArtworkType
	switch pictureType {
	case 3:
		artType = types.ArtworkFrontCover
	case 4:
		artType = types.ArtworkBackCover
	default:
		artType = types.ArtworkOther
	}

	return types.Artwork{
		Data:        pictureData,
		MIMEType:    mimeType,
		Type:        artType,
		Description: description,
		Width:       int(width),
		Height:      int(height),
	}, nil
}

// init registers the FLAC parser
func init() {
	registry.Register(types.FormatFLAC, &parser{})
}
