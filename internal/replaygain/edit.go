package replaygain

import (
	"fmt"
	"math"
	"strconv"

	"github.com/simonhull/audiometa/internal/types"
)

// scope names the keys of one ReplayGain scope, track or album.
type scope struct {
	gain, peak, r128 string
}

var (
	track = scope{KeyTrackGain, KeyTrackPeak, KeyR128TrackGain}
	album = scope{KeyAlbumGain, KeyAlbumPeak, KeyR128AlbumGain}
)

// q78Steps is the number of Q7.8 steps in one dB.
const q78Steps = 256

// SetTrack sets the track gain (dB relative to ReplayGain 2's -18 LUFS) and
// peak (linear; 0 when unknown) in t, in format's scheme. See [set].
func SetTrack(t *types.Tags, format types.Format, gain, peak float64) error {
	return set(t, format, track, gain, peak)
}

// SetAlbum is SetTrack for the album gain and peak.
func SetAlbum(t *types.Tags, format types.Format, gain, peak float64) error {
	return set(t, format, album, gain, peak)
}

// Remove deletes every ReplayGain key from t, R128 keys included.
func Remove(t *types.Tags) {
	for _, key := range []string{KeyTrackGain, KeyTrackPeak, KeyAlbumGain, KeyAlbumPeak, KeyR128TrackGain, KeyR128AlbumGain} {
		t.Set(key)
	}
}

// set writes one scope's gain and peak. Opus stores the gain alone, as a
// Q7.8 R128 tag, and drops every REPLAYGAIN_* tag (RFC 7845 section 5.2.1
// advises against them); other formats store REPLAYGAIN_* tags, removing a
// stored peak when peak is 0. A rejected value leaves t unchanged and
// returns an error wrapping [types.ErrInvalidReplayGain].
func set(t *types.Tags, format types.Format, s scope, gain, peak float64) error {
	if math.IsNaN(gain) || math.IsInf(gain, 0) {
		return fmt.Errorf("%w: gain %v is not a finite number", types.ErrInvalidReplayGain, gain)
	}
	if math.IsNaN(peak) || math.IsInf(peak, 0) || peak < 0 {
		return fmt.Errorf("%w: peak %v is not a finite, non-negative number", types.ErrInvalidReplayGain, peak)
	}
	if format == types.FormatOpus {
		q, err := formatR128(gain)
		if err != nil {
			return err
		}
		for _, key := range []string{KeyTrackGain, KeyTrackPeak, KeyAlbumGain, KeyAlbumPeak} {
			t.Set(key)
		}
		t.Set(s.r128, q)
		return nil
	}
	t.Set(s.gain, formatGain(gain))
	if peak == 0 {
		t.Set(s.peak)
	} else {
		t.Set(s.peak, formatPeak(peak))
	}
	return nil
}

// formatGain formats a gain the way ReplayGain taggers write it: "-6.50 dB".
func formatGain(gain float64) string {
	s := strconv.FormatFloat(gain, 'f', 2, 64)
	if s == "-0.00" {
		s = "0.00"
	}
	return s + " dB"
}

// formatPeak formats a linear peak with six decimals: "0.988525".
func formatPeak(peak float64) string {
	return strconv.FormatFloat(peak, 'f', 6, 64)
}

// formatR128 converts a gain on the ReplayGain scale to an RFC 7845 R128
// gain: a Q7.8 integer in dB relative to EBU R128, rounded half away from
// zero. It returns an error wrapping [types.ErrInvalidReplayGain] when the
// result does not fit 16 bits.
func formatR128(gain float64) (string, error) {
	q := math.Round((gain - r128Offset) * q78Steps)
	if q < math.MinInt16 || q > math.MaxInt16 {
		return "", fmt.Errorf("%w: gain %v dB is outside the range an Opus R128 tag can store", types.ErrInvalidReplayGain, gain)
	}
	return strconv.Itoa(int(q)), nil
}
