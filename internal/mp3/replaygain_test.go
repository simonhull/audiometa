package mp3

import (
	"bytes"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

func TestParse_ReplayGainFromTXXX(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		tag  []byte
	}{
		{"ID3v2.4", id3Tag(4,
			id3Frame(4, "TXXX", txxxPayload(3, "replaygain_track_gain", "-7.25 dB")),
			id3Frame(4, "TXXX", txxxPayload(0, "REPLAYGAIN_ALBUM_PEAK", "0.75")),
		)},
		{"ID3v2.2", id3Tag(2,
			id3v22Frame("TXX", txxxPayload(0, "replaygain_track_gain", "-7.25 dB")),
			id3v22Frame("TXX", txxxPayload(0, "replaygain_album_peak", "0.75")),
		)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data := append(tt.tag, bytes.Repeat(mpegFrame, 4)...)
			f, err := (&parser{}).Parse(t.Context(), bytes.NewReader(data), int64(len(data)), "x.mp3")
			if err != nil {
				t.Fatal(err)
			}
			want := types.ReplayGainInfo{TrackGain: -7.25, AlbumPeak: 0.75}
			if got := f.Audio.ReplayGain; got == nil || *got != want {
				t.Errorf("ReplayGain = %+v, want %+v", got, want)
			}
		})
	}
}
