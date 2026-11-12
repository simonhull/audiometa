package registry

import (
	"context"
	"io"

	"github.com/simonhull/audiometa/internal/types"
)

// AudioDigester computes the digest that identifies a file's audio.
type AudioDigester interface {
	// AudioDigest returns the SHA-256 of src's audio payload, leaving out its
	// tags. MP3: after the ID3v2 tag, before any trailing ID3v1, APEv2,
	// Lyrics3v2 or footer-terminated ID3v2 tags. FLAC: after the last metadata
	// block, before any such trailing tags. M4A: the concatenated payloads of
	// every mdat atom. Ogg: the packet bytes of the first logical stream after
	// its header packets. WAV: the data chunk. AIFF: the SSND chunk's samples.
	AudioDigest(ctx context.Context, src io.ReaderAt, size int64) ([]byte, error)
}

// TagWriter is implemented by format packages that can serialize edited metadata.
// Implementations must be deterministic: the same src and edits produce the same bytes.
type TagWriter interface {
	// WriteFile streams a complete new file to w: src (size bytes) with edits applied.
	// It never modifies src. It returns bytes written. Errors wrap the types.Err* sentinels
	// where one applies; structural problems return *types.CorruptedFileError.
	WriteFile(ctx context.Context, w io.Writer, src io.ReaderAt, size int64, edits *types.EditSet) (int64, error)

	// PreservedDigest returns the SHA-256 of every byte a metadata write must
	// copy unchanged. It covers the AudioDigest bytes plus anything else the
	// writer passes through verbatim, such as trailing APEv2 or Lyrics3 tags.
	PreservedDigest(ctx context.Context, src io.ReaderAt, size int64) ([]byte, error)

	AudioDigester
}

// InPlaceWriter is optionally implemented by TagWriters that can apply edits by
// overwriting one region without changing the file size.
//
// An InPlaceWriter should also implement [PreservedRanger]. The write path
// then verifies an in-place patch by structure, without reading the audio.
// Without it, the patch is verified by digesting the whole preserved content
// before and after, which reads the entire file twice.
type InPlaceWriter interface {
	// InPlacePatch returns the bytes to write over the file and where. A nil Patch
	// and a nil error mean the edits do not fit; the caller then falls back to
	// WriteFile.
	InPlacePatch(ctx context.Context, src io.ReaderAt, size int64, edits *types.EditSet) (*Patch, error)
}

// Patch is an in-place edit: Data is written over the file at offset Off.
type Patch struct {
	Off  int64
	Data []byte
}

// Range is a half-open byte range [Off, End) of a file.
type Range struct{ Off, End int64 }

// Overlaps reports whether r and o share at least one byte. An empty range
// overlaps nothing.
func (r Range) Overlaps(o Range) bool {
	return r.Off < r.End && o.Off < o.End && r.Off < o.End && o.Off < r.End
}

// PreservedRanger is optionally implemented by InPlaceWriters. PreservedRanges
// returns, in file order, the byte ranges PreservedDigest hashes, computed
// from src's structure alone. PreservedDigest must be the hash of exactly
// these ranges, so the two cannot drift apart: an in-place patch is verified
// by checking that it lies outside these ranges and that the ranges computed
// on the patched file are unchanged, and a writer whose ranges understate
// what it preserves weakens that check.
type PreservedRanger interface {
	PreservedRanges(ctx context.Context, src io.ReaderAt, size int64) ([]Range, error)
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

// digesters holds AudioDigesters for formats without a TagWriter; guarded by mu.
var digesters = make(map[types.Format]AudioDigester)

// RegisterDigester registers d for a format that has no TagWriter. Called
// from format packages' init(). A registered digester takes precedence over
// the format's TagWriter in GetDigester.
func RegisterDigester(format types.Format, d AudioDigester) {
	mu.Lock()
	defer mu.Unlock()
	digesters[format] = d
}

// GetDigester returns the digester for format: the one registered with
// RegisterDigester, otherwise the format's TagWriter, otherwise nil.
func GetDigester(format types.Format) AudioDigester {
	mu.RLock()
	defer mu.RUnlock()
	if d, ok := digesters[format]; ok {
		return d
	}
	if w, ok := writers[format]; ok {
		return w
	}
	return nil
}

// unregisterDigester removes the digester for format. Used by tests.
func unregisterDigester(format types.Format) {
	mu.Lock()
	defer mu.Unlock()
	delete(digesters, format)
}
