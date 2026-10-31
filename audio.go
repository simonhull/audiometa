package audiometa

import (
	"github.com/simonhull/audiometa/internal/types"
)

// AudioInfo describes a file's audio stream: Duration, SampleRate (Hz),
// BitDepth (0 for lossy codecs), Channels, Bitrate (bits per second), whether
// it is Lossless or VBR, the Codec and its profile, and ReplayGain values
// when the file has them.
//
// Its String method gives a one-line summary such as
// "FLAC 44.1kHz 16-bit stereo lossless". Every field and method is documented
// on the aliased type, [types.AudioInfo].
type AudioInfo = types.AudioInfo

// ReplayGainInfo holds ReplayGain loudness normalization values: track and
// album gain in dB and track and album peak amplitude (1.0 is full scale).
// AudioInfo.ReplayGain is nil when the file has no ReplayGain tags.
type ReplayGainInfo = types.ReplayGainInfo
