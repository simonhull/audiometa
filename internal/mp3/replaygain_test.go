package mp3

import (
	"bytes"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

func TestParse_ReplayGainFromTXXX(t *testing.T) {
	t.Parallel()
	tag := id3Tag(4,
		id3Frame(4, "TXXX", txxxPayload(3, "replaygain_track_gain", "-7.25 dB")),
		id3Frame(4, "TXXX", txxxPayload(0, "REPLAYGAIN_ALBUM_PEAK", "0.75")),
	)
	data := append(tag, bytes.Repeat(mpegFrame, 4)...)
	f, err := (&parser{}).Parse(t.Context(), bytes.NewReader(data), int64(len(data)), "x.mp3")
	if err != nil {
		t.Fatal(err)
	}
	want := types.ReplayGainInfo{TrackGain: -7.25, AlbumPeak: 0.75}
	if got := f.Audio.ReplayGain; got == nil || *got != want {
		t.Errorf("ReplayGain = %+v, want %+v", got, want)
	}
}
