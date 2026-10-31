package audiometa

import (
	"io"

	"github.com/simonhull/audiometa/internal/types"
)

// Format identifies an audio file format. File.Format reports the format
// [Open] detected, and its String method gives a display name such as
// "FLAC" or "Ogg Vorbis".
type Format = types.Format

// Formats detected by [DetectFormat]. WAV and AIFF are detected but cannot be
// parsed.
const (
	FormatUnknown = types.FormatUnknown
	FormatFLAC    = types.FormatFLAC
	FormatMP3     = types.FormatMP3
	FormatM4A     = types.FormatM4A
	FormatM4B     = types.FormatM4B
	FormatOgg     = types.FormatOgg
	FormatOpus    = types.FormatOpus
	FormatWAV     = types.FormatWAV
	FormatAIFF    = types.FormatAIFF
)

// DetectFormat identifies the format of the size-byte file readable through r
// from its signature bytes. It reads only the header and does not validate the
// rest of the file. path is used only in error messages.
//
// MP4 files are classified by their ftyp brands: an M4B brand gives
// [FormatM4B], other audio brands give [FormatM4A].
//
// It returns an [*UnsupportedFormatError] when the header cannot be read or
// matches no known format.
func DetectFormat(r io.ReaderAt, size int64, path string) (Format, error) {
	return types.DetectFormat(r, size, path)
}
