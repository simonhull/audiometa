// Package replaygain derives a file's ReplayGain values from its raw tags.
//
// Parsers record tags under upper-cased keys (REPLAYGAIN_TRACK_GAIN from a
// Vorbis comment, an MP3 TXXX frame or an M4A freeform item alike), so one
// reader serves every format.
package replaygain

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/simonhull/audiometa/internal/types"
)

// r128Offset converts a gain relative to EBU R128's -23 LUFS (Opus R128_*
// tags) to one relative to ReplayGain 2's -18 LUFS.
const r128Offset = 5.0

var (
	errNotNumber    = errors.New("not a number")
	errNotFinite    = errors.New("not a finite number")
	errNegativePeak = errors.New("peak is negative")
)

// field maps one raw tag to one ReplayGainInfo value.
type field struct {
	key   string
	parse func(string) (float64, error)
	dst   func(*types.ReplayGainInfo) *float64
}

func trackGain(rg *types.ReplayGainInfo) *float64 { return &rg.TrackGain }
func trackPeak(rg *types.ReplayGainInfo) *float64 { return &rg.TrackPeak }
func albumGain(rg *types.ReplayGainInfo) *float64 { return &rg.AlbumGain }
func albumPeak(rg *types.ReplayGainInfo) *float64 { return &rg.AlbumPeak }

// replayGainFields are the REPLAYGAIN_* tags every format may carry.
var replayGainFields = []field{
	{"REPLAYGAIN_TRACK_GAIN", parseGain, trackGain},
	{"REPLAYGAIN_TRACK_PEAK", parsePeak, trackPeak},
	{"REPLAYGAIN_ALBUM_GAIN", parseGain, albumGain},
	{"REPLAYGAIN_ALBUM_PEAK", parsePeak, albumPeak},
}

// r128Fields are the Opus gain tags of RFC 7845 section 5.2.1.
var r128Fields = []field{
	{"R128_TRACK_GAIN", parseR128, trackGain},
	{"R128_ALBUM_GAIN", parseR128, albumGain},
}

// maxQuoted bounds how much of an invalid value a warning quotes.
const maxQuoted = 64

// Apply sets f.Audio.ReplayGain from f's raw tags, leaving it nil when no
// tag holds a valid value, and appends a StageMetadata warning for each
// value it cannot parse. The first value of a repeated key wins.
//
// Opus files take their gains from R128_TRACK_GAIN and R128_ALBUM_GAIN,
// converted to the ReplayGain reference, with peaks left unknown. Other
// files, and Opus files with no valid R128 value, use the REPLAYGAIN_* tags.
func Apply(f *types.File) {
	if f.Format == types.FormatOpus {
		if rg := read(f, r128Fields); rg != nil {
			f.Audio.ReplayGain = rg
			return
		}
	}
	f.Audio.ReplayGain = read(f, replayGainFields)
}

// read returns the values of fields found in f's raw tags, or nil when none
// is valid, warning about each value it cannot parse.
func read(f *types.File, fields []field) *types.ReplayGainInfo {
	var (
		rg    types.ReplayGainInfo
		found bool
	)
	for _, fd := range fields {
		raw := f.Tags.GetFirst(fd.key)
		if raw == "" {
			continue
		}
		v, err := fd.parse(raw)
		if err != nil {
			f.Warnings = types.AppendWarning(f.Warnings, types.Warning{
				Stage:   types.StageMetadata,
				Message: fmt.Sprintf("ignoring %s %q: %v", fd.key, truncate(raw), err),
				Err:     err,
			})
			continue
		}
		*fd.dst(&rg) = v
		found = true
	}
	if !found {
		return nil
	}
	return &rg
}

// truncate shortens s to maxQuoted bytes for a warning message.
func truncate(s string) string {
	if len(s) <= maxQuoted {
		return s
	}
	return s[:maxQuoted] + "..."
}

// parseGain parses a gain such as "-6.50 dB", "+2.1dB" or "-6.5".
func parseGain(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if n := len(s); n >= 2 && strings.EqualFold(s[n-2:], "dB") {
		s = strings.TrimSpace(s[:n-2])
	}
	return parseFinite(s)
}

// parsePeak parses a linear peak amplitude such as "0.988127".
func parsePeak(s string) (float64, error) {
	v, err := parseFinite(strings.TrimSpace(s))
	if err != nil {
		return 0, err
	}
	if v < 0 {
		return 0, errNegativePeak
	}
	return v, nil
}

// parseR128 parses an RFC 7845 gain (a Q7.8 integer in dB relative to
// -23 LUFS) and returns it in dB relative to ReplayGain's -18 LUFS.
func parseR128(s string) (float64, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 16)
	if err != nil {
		return 0, errNotNumber
	}
	return float64(n)/256 + r128Offset, nil
}

// parseFinite parses s as a float64 and rejects NaN and infinities,
// including values too large for a float64.
func parseFinite(s string) (float64, error) {
	v, err := strconv.ParseFloat(s, 64)
	switch {
	case errors.Is(err, strconv.ErrRange):
		return 0, errNotFinite
	case err != nil:
		return 0, errNotNumber
	case math.IsNaN(v) || math.IsInf(v, 0):
		return 0, errNotFinite
	}
	return v, nil
}
