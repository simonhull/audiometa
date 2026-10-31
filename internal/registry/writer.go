package registry

import (
	"context"
	"io"

	"github.com/simonhull/audiometa/internal/types"
)

// TagWriter is implemented by format packages that can serialize edited metadata.
// Implementations must be deterministic: the same src and edits produce the same bytes.
type TagWriter interface {
	// WriteFile streams a complete new file to w: src (size bytes) with edits applied.
	// It never modifies src. It returns bytes written. Errors wrap the types.Err* sentinels
	// where one applies; structural problems return *types.CorruptedFileError.
	WriteFile(ctx context.Context, w io.Writer, src io.ReaderAt, size int64, edits *types.EditSet) (int64, error)

	// AudioDigest returns the SHA-256 of src's audio payload: the bytes no metadata
	// write may change (MP3: after the ID3v2 tag, before any ID3v1 trailer; FLAC: after
	// the last metadata block; M4A: the concatenated payloads of every mdat atom).
	AudioDigest(ctx context.Context, src io.ReaderAt, size int64) ([]byte, error)
}

// InPlaceWriter is optionally implemented by TagWriters that can apply edits by
// overwriting one region without changing the file size.
type InPlaceWriter interface {
	// InPlacePatch returns the bytes to write at off. ok is false when the edits do
	// not fit (the caller then falls back to WriteFile); that is not an error.
	InPlacePatch(ctx context.Context, src io.ReaderAt, size int64, edits *types.EditSet) (off int64, patch []byte, ok bool, err error)
}

// writers holds the registered TagWriters; guarded by mu.
var writers = make(map[types.Format]TagWriter)

// RegisterWriter registers w for format, replacing any writer already
// registered for it. Called from format packages' init().
func RegisterWriter(format types.Format, w TagWriter) {
	mu.Lock()
	defer mu.Unlock()
	writers[format] = w
}

// GetWriter returns the writer for format, or nil.
func GetWriter(format types.Format) TagWriter {
	mu.RLock()
	defer mu.RUnlock()
	return writers[format]
}

// unregisterWriter removes the writer for format. Used by tests.
func unregisterWriter(format types.Format) {
	mu.Lock()
	defer mu.Unlock()
	delete(writers, format)
}
