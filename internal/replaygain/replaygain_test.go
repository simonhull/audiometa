package replaygain

import (
	"errors"
	"strings"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

// fileWith returns a file of format whose raw tags hold kv pairs in order.
func fileWith(format types.Format, kv ...string) *types.File {
	f := &types.File{Format: format}
	for i := 0; i+1 < len(kv); i += 2 {
		f.Tags.AddRaw(kv[i], kv[i+1])
	}
	return f
}

func TestApply(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		format types.Format
		kv     []string
		want   *types.ReplayGainInfo
	}{
		{"none", types.FormatFLAC, []string{"TITLE", "x"}, nil},
		{"all four", types.FormatFLAC, []string{
			"REPLAYGAIN_TRACK_GAIN", "-6.50 dB", "REPLAYGAIN_TRACK_PEAK", "0.988127",
			"REPLAYGAIN_ALBUM_GAIN", "-8.23 dB", "REPLAYGAIN_ALBUM_PEAK", "1.000000",
		}, &types.ReplayGainInfo{TrackGain: -6.5, TrackPeak: 0.988127, AlbumGain: -8.23, AlbumPeak: 1}},
		{"no unit", types.FormatMP3, []string{"REPLAYGAIN_TRACK_GAIN", "-6.5"}, &types.ReplayGainInfo{TrackGain: -6.5}},
		{"unit without space, any case", types.FormatM4A, []string{"REPLAYGAIN_TRACK_GAIN", "+2.10db"}, &types.ReplayGainInfo{TrackGain: 2.1}},
		{"padded", types.FormatMP3, []string{"REPLAYGAIN_TRACK_GAIN", "  -1.00 dB  "}, &types.ReplayGainInfo{TrackGain: -1}},
		{"first value wins", types.FormatFLAC, []string{"REPLAYGAIN_TRACK_GAIN", "-1 dB", "REPLAYGAIN_TRACK_GAIN", "-9 dB"}, &types.ReplayGainInfo{TrackGain: -1}},
		{"empty value ignored", types.FormatFLAC, []string{"REPLAYGAIN_TRACK_GAIN", ""}, nil},
		{"opus R128", types.FormatOpus, []string{"R128_TRACK_GAIN", "-1280", "R128_ALBUM_GAIN", "256"}, &types.ReplayGainInfo{TrackGain: 0, AlbumGain: 6}},
		{"opus R128 beats REPLAYGAIN", types.FormatOpus, []string{
			"REPLAYGAIN_TRACK_GAIN", "-3 dB", "REPLAYGAIN_TRACK_PEAK", "0.9", "R128_TRACK_GAIN", "512",
		}, &types.ReplayGainInfo{TrackGain: 7}},
		{"opus without R128 uses REPLAYGAIN", types.FormatOpus, []string{"REPLAYGAIN_TRACK_GAIN", "-3 dB"}, &types.ReplayGainInfo{TrackGain: -3}},
		{"R128 ignored outside opus", types.FormatOgg, []string{"R128_TRACK_GAIN", "512"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := fileWith(tt.format, tt.kv...)
			Apply(f)
			if len(f.Warnings) != 0 {
				t.Errorf("warnings = %v, want none", f.Warnings)
			}
			switch got := f.Audio.ReplayGain; {
			case got == nil && tt.want == nil:
			case got == nil || tt.want == nil || *got != *tt.want:
				t.Errorf("ReplayGain = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestApply_InvalidValuesWarn(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		format  types.Format
		key     string
		value   string
		wantErr error
	}{
		{"not a number", types.FormatFLAC, "REPLAYGAIN_TRACK_GAIN", "loud", errNotNumber},
		{"unit only", types.FormatFLAC, "REPLAYGAIN_TRACK_GAIN", "dB", errNotNumber},
		{"NaN", types.FormatMP3, "REPLAYGAIN_ALBUM_GAIN", "NaN", errNotFinite},
		{"Inf", types.FormatMP3, "REPLAYGAIN_TRACK_PEAK", "+Inf", errNotFinite},
		{"overflow", types.FormatMP3, "REPLAYGAIN_TRACK_GAIN", "1e999", errNotFinite},
		{"negative peak", types.FormatM4A, "REPLAYGAIN_ALBUM_PEAK", "-0.5", errNegativePeak},
		{"R128 fraction", types.FormatOpus, "R128_TRACK_GAIN", "-6.5", errNotNumber},
		{"R128 out of range", types.FormatOpus, "R128_ALBUM_GAIN", "40000", errNotNumber},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := fileWith(tt.format, tt.key, tt.value)
			Apply(f)
			if f.Audio.ReplayGain != nil {
				t.Errorf("ReplayGain = %+v, want nil", f.Audio.ReplayGain)
			}
			if len(f.Warnings) != 1 {
				t.Fatalf("warnings = %v, want 1", f.Warnings)
			}
			w := f.Warnings[0]
			if w.Stage != types.StageMetadata || !errors.Is(w.Err, tt.wantErr) {
				t.Errorf("warning = %+v, want stage %q and %v", w, types.StageMetadata, tt.wantErr)
			}
		})
	}
}

func TestApply_InvalidFieldKeepsValidOnes(t *testing.T) {
	t.Parallel()
	f := fileWith(types.FormatFLAC, "REPLAYGAIN_TRACK_GAIN", "-2 dB", "REPLAYGAIN_TRACK_PEAK", "huge")
	Apply(f)
	if got := f.Audio.ReplayGain; got == nil || *got != (types.ReplayGainInfo{TrackGain: -2}) {
		t.Errorf("ReplayGain = %+v, want TrackGain -2 only", got)
	}
	if len(f.Warnings) != 1 {
		t.Errorf("warnings = %v, want 1", f.Warnings)
	}
}

func TestApply_OpusFallsBackWhenR128IsInvalid(t *testing.T) {
	t.Parallel()
	f := fileWith(types.FormatOpus, "R128_TRACK_GAIN", "-6.5", "REPLAYGAIN_TRACK_GAIN", "-3 dB", "REPLAYGAIN_TRACK_PEAK", "0.9")
	Apply(f)
	want := types.ReplayGainInfo{TrackGain: -3, TrackPeak: 0.9}
	if got := f.Audio.ReplayGain; got == nil || *got != want {
		t.Errorf("ReplayGain = %+v, want %+v", got, want)
	}
	if len(f.Warnings) != 1 || !errors.Is(f.Warnings[0].Err, errNotNumber) {
		t.Errorf("warnings = %v, want one for R128_TRACK_GAIN", f.Warnings)
	}
}

func TestApply_WarningQuotesBoundedValue(t *testing.T) {
	t.Parallel()
	f := fileWith(types.FormatMP3, "REPLAYGAIN_TRACK_GAIN", strings.Repeat("x", 1<<20))
	Apply(f)
	if len(f.Warnings) != 1 {
		t.Fatalf("warnings = %d, want 1", len(f.Warnings))
	}
	if n := len(f.Warnings[0].Message); n > 200 {
		t.Errorf("warning message is %d bytes, want at most 200", n)
	}
}
