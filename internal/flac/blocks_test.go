package flac

import (
	"strings"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

func TestParse_UnterminatedChainWarns(t *testing.T) {
	streamInfo := flacBlock(false, blockTypeStreamInfo, streamInfoPayload(44100))
	tests := []struct {
		name string
		data []byte
	}{
		{"chain ends at end of file", buildFLAC(streamInfo)},
		{"stray bytes after the chain", buildFLAC(streamInfo, []byte{0, 0})},
		{"block runs past end of file", buildFLAC(streamInfo, flacBlock(false, blockTypePadding, make([]byte, 100))[:50])},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := parseTestFLAC(t, tt.data)
			var warned bool
			for _, w := range file.Warnings {
				warned = warned || w.Stage == types.StageMetadata && strings.Contains(w.Message, "metadata block chain")
			}
			if !warned {
				t.Errorf("warnings = %+v, want an unterminated-chain warning", file.Warnings)
			}
			if file.Audio.Bitrate != 0 {
				t.Errorf("Bitrate = %d, want 0 when the audio start is unknown", file.Audio.Bitrate)
			}
		})
	}
}
