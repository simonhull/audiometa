// Package binary provides type-safe binary reading primitives with bounds checking
package binary

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// SafeReader wraps io.ReaderAt with bounds checking and helpful error messages.
type SafeReader struct {
	r    io.ReaderAt
	path string
	size int64
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

	buf := make([]byte, size)
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

// Reader provides sequential reading with automatic offset tracking.
type Reader struct {
	*SafeReader
	offset int64
}

// NewReader creates a new Reader starting at the given offset.
func NewReader(sr *SafeReader, offset int64) *Reader {
	return &Reader{
		SafeReader: sr,
		offset:     offset,
	}
}

// ReadValue reads a numeric value and advances the offset.
func ReadValue[T uint8 | uint16 | uint32 | uint64](r *Reader, what string) (T, error) {
	val, err := Read[T](r.SafeReader, r.offset, what)
	if err != nil {
		var zero T
		return zero, err
	}

	// Advance offset based on type size
	var size int64
	var zero T
	switch any(zero).(type) {
	case uint8:
		size = 1
	case uint16:
		size = 2
	case uint32:
		size = 4
	case uint64:
		size = 8
	}

	r.offset += size
	return val, nil
}

// ReadString reads a string of the given length and advances the offset.
func (r *Reader) ReadString(length int, what string) (string, error) {
	if err := r.CheckLen(r.offset, length, 1, what); err != nil {
		return "", err
	}
	buf := make([]byte, length)
	if err := r.ReadAt(buf, r.offset, what); err != nil {
		return "", err
	}

	r.offset += int64(length)
	return string(buf), nil
}

// Skip advances the offset by n bytes.
func (r *Reader) Skip(n int64) {
	r.offset += n
}

// Offset returns the current offset.
func (r *Reader) Offset() int64 {
	return r.offset
}

// ChainReader allows chaining multiple reads with deferred error checking.
// This avoids repetitive "if err != nil" checks.
type ChainReader struct {
	*Reader
	err error
}

// NewChainReader creates a new ChainReader.
func NewChainReader(r *Reader) *ChainReader {
	return &ChainReader{Reader: r}
}

// ReadChained reads a value with deferred error checking.
// If a previous read failed, returns zero value without attempting read.
func ReadChained[T uint8 | uint16 | uint32 | uint64](cr *ChainReader, what string) T {
	if cr.err != nil {
		var zero T
		return zero
	}

	val, err := ReadValue[T](cr.Reader, what)
	if err != nil {
		cr.err = err
		var zero T
		return zero
	}

	return val
}

// String reads a string, accumulating any error.
func (cr *ChainReader) String(length int, what string) string {
	if cr.err != nil {
		return ""
	}

	val, err := cr.ReadString(length, what)
	if err != nil {
		cr.err = err
		return ""
	}

	return val
}

// Error returns the accumulated error, if any.
func (cr *ChainReader) Error() error {
	return cr.err
}
