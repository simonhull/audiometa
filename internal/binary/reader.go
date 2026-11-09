// Package binary provides bounds-checked binary reading primitives.
//
// [SafeReader] wraps an [io.ReaderAt] with the file size, rejects reads past
// the end with an [*OutOfBoundsError], buffers small reads, and offers generic
// fixed-width integer reads. [SafeReader.CheckLen] and [MaxAllocBytes] bound
// allocations driven by length fields, and [Canceled] gives parse loops a
// cheap periodic context check.
package binary

import (
	"errors"
	"fmt"
	"io"
)

// OutOfBoundsError is returned when attempting to read beyond file bounds.
type OutOfBoundsError struct {
	Path   string
	What   string
	Offset int64
	Length int
	Size   int64
}

// Error implements the error interface.
func (e *OutOfBoundsError) Error() string {
	if e.Offset < 0 || e.Offset >= e.Size {
		return fmt.Sprintf("%s: offset %d out of bounds (file size: %d) while reading %s",
			e.Path, e.Offset, e.Size, e.What)
	}
	return fmt.Sprintf("%s: read of %d bytes at offset %d would exceed file size %d while reading %s",
		e.Path, e.Length, e.Offset, e.Size, e.What)
}

// windowSize is the read-through cache size. Small reads are served from one
// cached window of the file; reads larger than windowSize/4 go straight to the
// underlying reader and leave the window untouched.
const windowSize = 16 << 10

// HeadSize is how much of a file's start [Prefetched] is meant to hold. It
// matches the SafeReader window so a parser's first window, filled at offset 0,
// is served from it.
const HeadSize = windowSize

// Prefetched is an [io.ReaderAt] that serves reads inside one prefetched region
// from memory and passes every other read to R.
type Prefetched struct {
	R   io.ReaderAt
	Off int64  // file offset of Buf[0]
	Buf []byte // the prefetched bytes
}

// ReadAt implements [io.ReaderAt]. A read entirely inside the prefetched region
// is copied from it and returns len(b), nil; any other read, including one
// that straddles the region's end, is delegated to R unchanged.
func (p *Prefetched) ReadAt(b []byte, off int64) (int, error) {
	if off >= p.Off && off-p.Off+int64(len(b)) <= int64(len(p.Buf)) {
		return copy(b, p.Buf[off-p.Off:]), nil
	}
	return p.R.ReadAt(b, off)
}

// SafeReader wraps io.ReaderAt with bounds checking and helpful error messages.
//
// A SafeReader caches one window of the file to turn many small reads into one
// underlying ReadAt. It is not safe for concurrent use; create one per parse.
type SafeReader struct {
	r       io.ReaderAt
	path    string
	size    int64
	win     []byte  // allocated on first small read
	winOff  int64   // file offset of win[0]
	winLen  int     // valid bytes in win (0 = empty)
	scratch [8]byte // backing store for Read/ReadEndian, avoids per-call allocation
}

// NewSafeReader creates a new SafeReader.
func NewSafeReader(r io.ReaderAt, size int64, path string) *SafeReader {
	return &SafeReader{
		r:    r,
		size: size,
		path: path,
	}
}

// Path returns the file path associated with this reader.
func (sr *SafeReader) Path() string {
	return sr.path
}

// Size returns the size of the data the reader covers.
func (sr *SafeReader) Size() int64 { return sr.size }

// ReadAt reads bytes at the given offset with context for error messages.
// A zero-length read anywhere in [0, size] succeeds without reading.
func (sr *SafeReader) ReadAt(b []byte, off int64, what string) error {
	if len(b) == 0 {
		if off < 0 || off > sr.size {
			return &OutOfBoundsError{Path: sr.path, What: what, Offset: off, Length: 0, Size: sr.size}
		}
		return nil
	}

	// Check bounds
	if off < 0 || off >= sr.size || off+int64(len(b)) > sr.size {
		return &OutOfBoundsError{Path: sr.path, What: what, Offset: off, Length: len(b), Size: sr.size}
	}

	if len(b) <= windowSize/4 {
		if sr.winLen > 0 && off >= sr.winOff && off+int64(len(b)) <= sr.winOff+int64(sr.winLen) {
			copy(b, sr.win[off-sr.winOff:])
			return nil
		}
		if sr.fill(off) {
			copy(b, sr.win[:len(b)])
			return nil
		}
		// fill failed: fall through to the direct read so errors are reported exactly as before
	}

	n, err := sr.r.ReadAt(b, off)
	if err != nil && err != io.EOF {
		return fmt.Errorf("%s: failed to read %s at offset %d: %w", sr.path, what, off, err)
	}

	if n < len(b) {
		return fmt.Errorf("%s: short read for %s at offset %d: got %d bytes, expected %d",
			sr.path, what, off, n, len(b))
	}

	return nil
}

// fill loads the window starting at off. It reports false (and leaves the window
// empty) on any error or short read, so the caller can fall back to a direct read.
func (sr *SafeReader) fill(off int64) bool {
	if sr.win == nil {
		sr.win = make([]byte, windowSize)
	}
	n := int64(windowSize)
	if rest := sr.size - off; rest < n {
		n = rest
	}
	got, err := sr.r.ReadAt(sr.win[:n], off)
	if (err != nil && !errors.Is(err, io.EOF)) || int64(got) < n {
		sr.winLen = 0
		return false
	}
	sr.winOff, sr.winLen = off, got
	return true
}

// MaxAllocBytes is the absolute ceiling on any single length/count-driven
// allocation, regardless of file size. Real audio metadata blocks are far
// smaller; this rejects crafted files that declare implausible sizes.
const MaxAllocBytes = 256 << 20 // 256 MiB

// CheckLen validates that count elements of elemSize bytes each, starting at
// off, fit within the file and within MaxAllocBytes. It allocates nothing.
// Call it immediately before make([]T, count) when count came from file data.
func (sr *SafeReader) CheckLen(off int64, count, elemSize int, what string) error {
	if count < 0 || elemSize <= 0 {
		return fmt.Errorf("%s: invalid %s length %d", sr.path, what, count)
	}
	total := int64(count) * int64(elemSize)
	if total > MaxAllocBytes {
		return fmt.Errorf("%s: %s length %d exceeds max %d", sr.path, what, total, int64(MaxAllocBytes))
	}
	if off < 0 || off > sr.size || total > sr.size-off {
		return fmt.Errorf("%s: %s length %d at offset %d exceeds file size %d", sr.path, what, total, off, sr.size)
	}
	return nil
}

// Integer is the set of fixed-width unsigned integer types that SafeReader can read.
type Integer interface {
	uint8 | uint16 | uint32 | uint64
}

// Read reads a big-endian value of type T from the given offset.
// It is the default for MP4/ID3v2/FLAC-header fields; use ReadLE for little-endian data.
//
// Example:
//
//	size, err := sr.Read[uint32](offset, "atom size")
func (sr *SafeReader) Read[T Integer](off int64, what string) (T, error) {
	return sr.ReadEndian[T](off, what, BigEndian)
}
