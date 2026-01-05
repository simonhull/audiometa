package audiometa

import (
	"github.com/simonhull/audiometa/internal/types"
)

// OutOfBoundsError is an alias to types.OutOfBoundsError for backwards compatibility.
// Re-exporting from internal/types to maintain public API.
type OutOfBoundsError = types.OutOfBoundsError

// UnsupportedFormatError is an alias to types.UnsupportedFormatError for backwards compatibility.
// Re-exporting from internal/types to maintain public API.
type UnsupportedFormatError = types.UnsupportedFormatError

// CorruptedFileError is an alias to types.CorruptedFileError for backwards compatibility.
// Re-exporting from internal/types to maintain public API.
type CorruptedFileError = types.CorruptedFileError

// UnsupportedWriteError is an alias to types.UnsupportedWriteError for backwards compatibility.
// Re-exporting from internal/types to maintain public API.
type UnsupportedWriteError = types.UnsupportedWriteError

// Warning is an alias to types.Warning for backwards compatibility.
// Re-exporting from internal/types to maintain public API.
type Warning = types.Warning
