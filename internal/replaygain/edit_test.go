package replaygain

import (
	"errors"
	"math"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

func TestFormatGain(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		gain float64
		want string
	}{
		{-6.5, "-6.50 dB"},
		{2.1, "2.10 dB"},
		{0, "0.00 dB"},
		{-0.001, "0.00 dB"},
		{math.Copysign(0, -1), "0.00 dB"},
		{-7.886, "-7.89 dB"},
	} {
		if got := formatGain(tt.gain); got != tt.want {
			t.Errorf("formatGain(%v) = %q, want %q", tt.gain, got, tt.want)
		}
	}
}

func TestFormatPeak(t *testing.T) {
	t.Parallel()
	if got := formatPeak(0.98852549); got != "0.988525" {
		t.Errorf("formatPeak = %q, want 0.988525", got)
	}
	if got := formatPeak(1.2); got != "1.200000" {
		t.Errorf("formatPeak = %q, want 1.200000", got)
	}
}

func TestFormatR128(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		gain float64
		want string
		ok   bool
	}{
		{-6.5, "-2944", true}, // (-6.5 - 5) * 256
		{5, "0", true},
		{5 + 1.0/512, "1", true}, // half a step rounds away from zero
		{5 - 1.0/512, "-1", true},
		{5 + 32767.0/256, "32767", true},
		{5 + 32768.0/256, "", false},
		{5 - 128, "-32768", true},
		{5 - 128 - 1.0/256, "", false},
	} {
		got, err := formatR128(tt.gain)
		if tt.ok && (err != nil || got != tt.want) {
			t.Errorf("formatR128(%v) = %q, %v, want %q", tt.gain, got, err, tt.want)
		}
		if !tt.ok && !errors.Is(err, types.ErrInvalidReplayGain) {
			t.Errorf("formatR128(%v) err = %v, want ErrInvalidReplayGain", tt.gain, err)
		}
	}
}

// tagsWith returns tags holding kv pairs, as a parser would have read them.
func tagsWith(kv ...string) *types.Tags {
	var tg types.Tags
	for i := 0; i+1 < len(kv); i += 2 {
		tg.AddRaw(kv[i], kv[i+1])
	}
	return &tg
}

func TestSet(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		format types.Format
		before []string
		set    func(*types.Tags, types.Format) error
		want   map[string]string // raw key → first value; "" means absent
	}{
		{
			"track", types.FormatFLAC, nil,
			func(tg *types.Tags, f types.Format) error { return SetTrack(tg, f, -6.5, 0.988525) },
			map[string]string{KeyTrackGain: "-6.50 dB", KeyTrackPeak: "0.988525", KeyAlbumGain: ""},
		},
		{
			"album", types.FormatMP3, nil,
			func(tg *types.Tags, f types.Format) error { return SetAlbum(tg, f, 1.25, 1) },
			map[string]string{KeyAlbumGain: "1.25 dB", KeyAlbumPeak: "1.000000", KeyTrackGain: ""},
		},
		{
			"zero gain is written", types.FormatM4A, nil,
			func(tg *types.Tags, f types.Format) error { return SetTrack(tg, f, 0, 0.5) },
			map[string]string{KeyTrackGain: "0.00 dB", KeyTrackPeak: "0.500000"},
		},
		{
			"unknown peak removes the stored one", types.FormatOgg,
			[]string{KeyTrackGain, "-1.00 dB", KeyTrackPeak, "0.9"},
			func(tg *types.Tags, f types.Format) error { return SetTrack(tg, f, -2, 0) },
			map[string]string{KeyTrackGain: "-2.00 dB", KeyTrackPeak: ""},
		},
		{
			"opus writes R128 and drops REPLAYGAIN", types.FormatOpus,
			[]string{KeyTrackGain, "-1.00 dB", KeyTrackPeak, "0.9", KeyAlbumGain, "-3.00 dB", KeyAlbumPeak, "1.0"},
			func(tg *types.Tags, f types.Format) error { return SetTrack(tg, f, -6.5, 0.9) },
			map[string]string{
				KeyR128TrackGain: "-2944", KeyR128AlbumGain: "",
				KeyTrackGain: "", KeyTrackPeak: "", KeyAlbumGain: "", KeyAlbumPeak: "",
			},
		},
		{
			"opus album", types.FormatOpus,
			[]string{KeyR128TrackGain, "-100"},
			func(tg *types.Tags, f types.Format) error { return SetAlbum(tg, f, 6, 0) },
			map[string]string{KeyR128TrackGain: "-100", KeyR128AlbumGain: "256"},
		},
		{
			"remove", types.FormatOpus,
			[]string{KeyTrackGain, "-1 dB", KeyAlbumPeak, "1", KeyR128TrackGain, "5", KeyR128AlbumGain, "6", "TITLE", "t"},
			func(tg *types.Tags, _ types.Format) error { Remove(tg); return nil },
			map[string]string{
				KeyTrackGain: "", KeyAlbumPeak: "", KeyR128TrackGain: "", KeyR128AlbumGain: "", "TITLE": "t",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tg := tagsWith(tt.before...)
			if err := tt.set(tg, tt.format); err != nil {
				t.Fatal(err)
			}
			for key, want := range tt.want {
				if got := tg.GetFirst(key); got != want {
					t.Errorf("%s = %q, want %q", key, got, want)
				}
			}
		})
	}
}

func TestSet_Invalid(t *testing.T) {
	t.Parallel()
	nan, inf := math.NaN(), math.Inf(1)
	for _, tt := range []struct {
		name       string
		format     types.Format
		gain, peak float64
	}{
		{"NaN gain", types.FormatFLAC, nan, 1},
		{"infinite gain", types.FormatMP3, -inf, 1},
		{"NaN peak", types.FormatM4A, -1, nan},
		{"infinite peak", types.FormatFLAC, -1, inf},
		{"negative peak", types.FormatFLAC, -1, -0.5},
		{"NaN gain on opus", types.FormatOpus, nan, 0},
		{"negative peak on opus", types.FormatOpus, -1, -0.5},
		{"opus gain out of range", types.FormatOpus, 200, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tg := tagsWith(KeyTrackGain, "-1.00 dB")
			before := tg.Clone()
			if err := SetTrack(tg, tt.format, tt.gain, tt.peak); !errors.Is(err, types.ErrInvalidReplayGain) {
				t.Errorf("SetTrack err = %v, want ErrInvalidReplayGain", err)
			}
			if err := SetAlbum(tg, tt.format, tt.gain, tt.peak); !errors.Is(err, types.ErrInvalidReplayGain) {
				t.Errorf("SetAlbum err = %v, want ErrInvalidReplayGain", err)
			}
			if !tg.Equal(before) {
				t.Errorf("tags changed by a rejected value: %v", tg.All())
			}
		})
	}
}
