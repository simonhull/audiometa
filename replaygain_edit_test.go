package audiometa_test

import (
	"bytes"
	"errors"
	"math"
	"testing"

	"github.com/simonhull/audiometa"
)

// checkReplayGain fails t unless f's ReplayGain holds the given values, to
// the precision its tags store: 0.01 dB, or 1/256 dB and no peaks on Opus.
func checkReplayGain(t *testing.T, f *audiometa.File, trackGain, trackPeak, albumGain, albumPeak float64) {
	t.Helper()
	rg := f.Audio.ReplayGain
	if rg == nil {
		t.Fatal("ReplayGain = nil")
	}
	tolerance := 0.005
	if f.Format == audiometa.FormatOpus {
		tolerance, trackPeak, albumPeak = 1.0/512, 0, 0
	}
	if math.Abs(rg.TrackGain-trackGain) > tolerance || math.Abs(rg.AlbumGain-albumGain) > tolerance ||
		math.Abs(rg.TrackPeak-trackPeak) > 1e-6 || math.Abs(rg.AlbumPeak-albumPeak) > 1e-6 {
		t.Errorf("ReplayGain = %+v, want gains %v/%v, peaks %v/%v", *rg, trackGain, albumGain, trackPeak, albumPeak)
	}
}

// rewrite writes f and returns it re-opened.
func rewrite(t *testing.T, path string, f *audiometa.File) *audiometa.File {
	t.Helper()
	var buf bytes.Buffer
	if _, err := f.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return writeAndReopen(t, path, buf.Bytes())
}

// TestReplayGain_KeepsStoredSpelling checks that a ReplayGain tag stored in
// lower case, as some taggers write it, is replaced in place: one TXXX frame
// or freeform item, still in lower case.
func TestReplayGain_KeepsStoredSpelling(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"testdata/sample.mp3", "testdata/sample.m4a", "testdata/sample_id3.wav", "testdata/sample.aiff"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			f := openFile(t, path)
			f.Tags.SetNamed("replaygain_track_gain", "-1.00 dB")
			f = rewrite(t, path, f)
			if err := f.SetTrackReplayGain(-6.5, 0); err != nil {
				t.Fatal(err)
			}
			f = rewrite(t, path, f)
			if got := f.Tags.Get("REPLAYGAIN_TRACK_GAIN"); len(got) != 1 || got[0] != "-6.50 dB" {
				t.Errorf("REPLAYGAIN_TRACK_GAIN = %q, want [-6.50 dB]", got)
			}
			if name := f.Tags.Name("REPLAYGAIN_TRACK_GAIN"); name != "replaygain_track_gain" {
				t.Errorf("stored name = %q, want replaygain_track_gain", name)
			}
		})
	}
}

// TestReplayGain_Opus checks that Opus stores R128 gains and loses the
// REPLAYGAIN_* tags RFC 7845 advises against.
func TestReplayGain_Opus(t *testing.T) {
	t.Parallel()
	const path = "testdata/sample.opus"
	f := openFile(t, path)
	f.Tags.Set("REPLAYGAIN_TRACK_GAIN", "-1.00 dB")
	f.Tags.Set("REPLAYGAIN_ALBUM_PEAK", "0.9")
	f = rewrite(t, path, f)
	if err := f.SetTrackReplayGain(-6.5, 0.9); err != nil {
		t.Fatal(err)
	}
	f = rewrite(t, path, f)
	if got := f.Tags.Get("R128_TRACK_GAIN"); len(got) != 1 || got[0] != "-2944" {
		t.Errorf("R128_TRACK_GAIN = %q, want [-2944]", got)
	}
	for _, key := range []string{"REPLAYGAIN_TRACK_GAIN", "REPLAYGAIN_ALBUM_PEAK"} {
		if got := f.Tags.Get(key); got != nil {
			t.Errorf("%s = %q, want removed", key, got)
		}
	}
	if rg := f.Audio.ReplayGain; rg == nil || rg.TrackGain != -6.5 || rg.TrackPeak != 0 {
		t.Errorf("ReplayGain = %+v, want TrackGain -6.5 and no peak", rg)
	}
}

// TestReplayGain_SetThenRemove checks that RemoveReplayGain removes values a
// previous write stored, and that a peak of 0 removes a stored peak.
func TestReplayGain_SetThenRemove(t *testing.T) {
	t.Parallel()
	for _, tc := range roundTripCorpus {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			f := openFile(t, tc.path)
			if err := errors.Join(f.SetTrackReplayGain(-3, 0.5), f.SetAlbumReplayGain(-4, 0.6)); err != nil {
				t.Fatal(err)
			}
			f = rewrite(t, tc.path, f)
			if err := f.SetTrackReplayGain(-2, 0); err != nil {
				t.Fatal(err)
			}
			f = rewrite(t, tc.path, f)
			checkReplayGain(t, f, -2, 0, -4, 0.6)

			f.RemoveReplayGain()
			f = rewrite(t, tc.path, f)
			if rg := f.Audio.ReplayGain; rg != nil {
				t.Errorf("ReplayGain after RemoveReplayGain = %+v, want nil", rg)
			}
		})
	}
}

// TestReplayGain_Invalid checks that a rejected value fails with
// ErrInvalidReplayGain and leaves the tags as they were.
func TestReplayGain_Invalid(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"testdata/sample.flac", "testdata/sample.opus"} {
		f := openFile(t, path)
		before := f.Tags.Clone()
		for _, err := range []error{
			f.SetTrackReplayGain(math.NaN(), 0),
			f.SetAlbumReplayGain(-1, -0.5),
			f.SetTrackReplayGain(-1, math.Inf(1)),
		} {
			if !errors.Is(err, audiometa.ErrInvalidReplayGain) {
				t.Errorf("%s: err = %v, want ErrInvalidReplayGain", path, err)
			}
		}
		if !f.Tags.Equal(before) {
			t.Errorf("%s: a rejected value changed the tags", path)
		}
	}
	f := openFile(t, "testdata/sample.opus")
	if err := f.SetTrackReplayGain(200, 0); !errors.Is(err, audiometa.ErrInvalidReplayGain) {
		t.Errorf("Opus gain out of R128 range: err = %v, want ErrInvalidReplayGain", err)
	}
}

// TestReplayGain_AudioKeepsStoredValues checks that the edit is pending:
// Audio.ReplayGain reports the stored values until the file is re-opened.
func TestReplayGain_AudioKeepsStoredValues(t *testing.T) {
	t.Parallel()
	f := openFile(t, "testdata/sample.flac")
	before := f.Audio.ReplayGain
	if err := f.SetTrackReplayGain(-6.5, 0.9); err != nil {
		t.Fatal(err)
	}
	if f.Audio.ReplayGain != before {
		t.Errorf("Audio.ReplayGain changed before the write: %+v", f.Audio.ReplayGain)
	}
	if got := f.Tags.GetFirst("REPLAYGAIN_TRACK_GAIN"); got != "-6.50 dB" {
		t.Errorf("Tags REPLAYGAIN_TRACK_GAIN = %q, want -6.50 dB", got)
	}
}
