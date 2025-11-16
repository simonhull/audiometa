package audiometa

import (
	"io"

	"github.com/simonhull/audiometa/internal/types"
)

// Format is an alias to types.Format for backwards compatibility.
// Re-exporting from internal/types to maintain public API.
type Format = types.Format

// Re-export all format constants.
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

// DetectFormat is a wrapper around types.DetectFormat.
// Maintains the public API while delegating to internal implementation.
func DetectFormat(r io.ReaderAt, size int64, path string) (Format, error) {
	return types.DetectFormat(r, size, path)
}
