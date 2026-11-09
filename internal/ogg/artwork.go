package ogg

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"strings"

	"github.com/simonhull/audiometa/internal/picture"
	"github.com/simonhull/audiometa/internal/types"
)

// ExtractArtwork extracts embedded artwork from Ogg Vorbis/Opus files.
//
// Ogg files can embed artwork via the METADATA_BLOCK_PICTURE Vorbis comment,
// which contains a base64-encoded FLAC picture block.
//
// It reads only the header packets and decodes only the comment packet: the
// duration, the technical headers and the rest of the file are left alone.
func (p *parser) ExtractArtwork(ctx context.Context, r io.ReaderAt, size int64, path string) ([]types.Artwork, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var file types.File // scratch: receives the comment header's tags
	h, err := readHeaderPackets(ctx, r, size, path, &file, maxHeaderBytes)
	if err != nil {
		return nil, fmt.Errorf("parse file: %w", err)
	}
	if len(h.packets) < 2 {
		return nil, fmt.Errorf("parse file: not enough packets found (need at least 2, got %d)", len(h.packets))
	}
	// A comment header that fails to decode keeps the tags read before the
	// failure, as in Parse, where it is only a warning.
	switch codec := detectOggCodec(h.packets[0]); codec {
	case codecVorbis:
		_ = parseVorbisComment(ctx, h.packets[1], &file)
	case codecOpus:
		_ = parseOpusTags(ctx, h.packets[1], &file)
	default:
		return nil, fmt.Errorf("parse file: unknown or unsupported Ogg codec: %q", codec)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}

	var artwork []types.Artwork

	// Look for METADATA_BLOCK_PICTURE tags
	for key, values := range file.Tags.All() {
		if !strings.EqualFold(key, "METADATA_BLOCK_PICTURE") {
			continue
		}

		for _, value := range values {
			pic, err := parseMetadataBlockPicture(value)
			if err != nil {
				// Skip invalid pictures but continue
				continue
			}
			artwork = append(artwork, pic)
		}
	}

	return artwork, nil
}

// parseMetadataBlockPicture decodes a METADATA_BLOCK_PICTURE value: the base64 encoding of a
// FLAC PICTURE block, decoded by [picture.Decode].
func parseMetadataBlockPicture(base64Value string) (types.Artwork, error) {
	data, err := base64.StdEncoding.DecodeString(base64Value)
	if err != nil {
		return types.Artwork{}, fmt.Errorf("invalid base64: %w", err)
	}
	return picture.Decode(data)
}
