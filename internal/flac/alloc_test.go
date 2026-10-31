package flac

import (
	"encoding/binary"
	"testing"
)

func TestParse_OversizedLengthsDoNotAllocate(t *testing.T) {
	t.Run("vorbis comment length past block", func(t *testing.T) {
		payload := binary.LittleEndian.AppendUint32(nil, 0) // empty vendor
		payload = binary.LittleEndian.AppendUint32(payload, 1)
		payload = binary.LittleEndian.AppendUint32(payload, 0xFFFFFFFF) // comment length
		data := buildFLAC(
			flacBlock(false, blockTypeStreamInfo, streamInfoPayload(44100, 44100)),
			flacBlock(true, blockTypeVorbisComment, payload),
		)
		file := parseTestFLAC(t, data)
		if len(file.Warnings) == 0 {
			t.Error("expected a warning for the oversized comment")
		}
	})

	t.Run("picture lengths past file", func(t *testing.T) {
		for _, field := range []int{4, 8, 28} { // MIME length, description length, picture data length (MIME length 0)
			payload := make([]byte, 64)
			binary.BigEndian.PutUint32(payload[0:], 3)
			binary.BigEndian.PutUint32(payload[field:], 0xFFFFFFFF)
			data := buildFLAC(
				flacBlock(false, blockTypeStreamInfo, streamInfoPayload(44100, 44100)),
				flacBlock(true, blockTypePicture, payload),
			)
			if art := extractTestArtwork(t, data); len(art) != 0 {
				t.Errorf("field %d: got %d artworks, want 0", field, len(art))
			}
		}
	})
}
