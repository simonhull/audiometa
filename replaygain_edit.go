package audiometa

import "github.com/simonhull/audiometa/internal/replaygain"

// SetTrackReplayGain sets the file's ReplayGain track gain and peak. gain is
// in dB relative to the ReplayGain 2 reference level (-18 LUFS), the scale
// [ReplayGainInfo] reports; peak is the linear sample peak, where 1.0 is full
// scale, or 0 when it is unknown, which removes a stored track peak.
//
// SetTrackReplayGain edits f.Tags in the file format's own scheme:
//   - Opus: R128_TRACK_GAIN, a Q7.8 integer in dB relative to EBU R128
//     (-23 LUFS) and to the header's output gain, as RFC 7845 defines it.
//     Opus has no peak tags, so peak is not stored, and the REPLAYGAIN_* tags
//     RFC 7845 advises against are removed.
//   - every other format: REPLAYGAIN_TRACK_GAIN ("-6.50 dB") and
//     REPLAYGAIN_TRACK_PEAK ("0.988525"), as Vorbis comments, ID3v2 TXXX
//     frames or M4A freeform items. A tag the file already holds keeps its
//     stored spelling; a new one is written in upper case.
//
// Values are rounded to what the tags store: 0.01 dB (1/256 dB on Opus) and
// six decimals for the peak.
//
// Like any edit of f.Tags, the change is pending until [File.WriteTo] or
// [Write], and f.Audio.ReplayGain keeps reporting the values stored in the
// file until it is opened again. SetTrackReplayGain is not safe for use
// concurrently with other reads or edits of f.Tags.
//
// Measuring loudness is the caller's job: audiometa reads and writes the
// values, it never computes them.
//
// # Errors
//
// SetTrackReplayGain returns an error wrapping [ErrInvalidReplayGain], and
// changes nothing, when gain or peak is NaN or infinite, peak is negative,
// or, on Opus, the gain is outside the range an R128 tag can store (about
// -123 to 133 dB on the ReplayGain scale).
func (f *File) SetTrackReplayGain(gain, peak float64) error {
	return replaygain.SetTrack(&f.Tags, f.Format, gain, peak)
}

// SetAlbumReplayGain sets the file's ReplayGain album gain and peak:
// REPLAYGAIN_ALBUM_GAIN and REPLAYGAIN_ALBUM_PEAK, or R128_ALBUM_GAIN on
// Opus. It works exactly as [File.SetTrackReplayGain] does, and returns the
// same errors.
func (f *File) SetAlbumReplayGain(gain, peak float64) error {
	return replaygain.SetAlbum(&f.Tags, f.Format, gain, peak)
}

// RemoveReplayGain removes every ReplayGain tag from f.Tags: the
// REPLAYGAIN_TRACK_GAIN, REPLAYGAIN_TRACK_PEAK, REPLAYGAIN_ALBUM_GAIN and
// REPLAYGAIN_ALBUM_PEAK tags, and R128_TRACK_GAIN and R128_ALBUM_GAIN. An
// Opus header's output gain is left as it is.
//
// The change is pending until [File.WriteTo] or [Write], as for
// [File.SetTrackReplayGain].
func (f *File) RemoveReplayGain() {
	replaygain.Remove(&f.Tags)
}
