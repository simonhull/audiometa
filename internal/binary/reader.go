// Package binary provides type-safe binary reading primitives with bounds checking
package binary

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// windowSize is the read-through cache size. Small reads are served from one
// cached window of the file; reads larger than windowSize/4 go straight to the
// underlying reader and leave the window untouched.
const windowSize = 16 << 10

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

// ReadAt reads bytes at the given offset with context for error messages.
func (sr *SafeReader) ReadAt(b []byte, off int64, what string) error {
	// Check bounds
	if off < 0 || off >= sr.size {
		return fmt.Errorf("%s: offset %d out of bounds (file size: %d) while reading %s",
			sr.path, off, sr.size, what)
	}

	if off+int64(len(b)) > sr.size {
		return fmt.Errorf("%s: read of %d bytes at offset %d would exceed file size %d while reading %s",
			sr.path, len(b), off, sr.size, what)
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

// Read reads a value of type T from the given offset.
// T must be uint8, uint16, uint32, or uint64.
func Read[T uint8 | uint16 | uint32 | uint64](sr *SafeReader, off int64, what string) (T, error) {
	var zero T
	var size int

	// Determine size based on type
	switch any(zero).(type) {
	case uint8:
		size = 1
	case uint16:
		size = 2
	case uint32:
		size = 4
	case uint64:
		size = 8
	default:
		return zero, errors.New("unsupported type for Read")
	}

	buf := sr.scratch[:size]
	if err := sr.ReadAt(buf, off, what); err != nil {
		return zero, err
	}

	// Convert bytes to value (big-endian, MP4 standard)
	var val T
	switch any(zero).(type) {
	case uint8:
		val = T(buf[0])
	case uint16:
		val = T(binary.BigEndian.Uint16(buf))
	case uint32:
		val = T(binary.BigEndian.Uint32(buf))
	case uint64:
		val = T(binary.BigEndian.Uint64(buf))
	}

	return val, nil
}
