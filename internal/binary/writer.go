// Package binary provides type-safe binary writing primitives with offset tracking.
package binary

import (
	"encoding/binary"
	"io"
)

// SafeWriter wraps io.Writer with position tracking.
type SafeWriter struct {
	w      io.Writer
	offset int64
}

// NewSafeWriter creates a new SafeWriter.
func NewSafeWriter(w io.Writer) *SafeWriter {
	return &SafeWriter{
		w:      w,
		offset: 0,
	}
}

// Offset returns the current position (number of bytes written).
func (sw *SafeWriter) Offset() int64 {
	return sw.offset
}

// WriteBytes writes raw bytes to the underlying writer.
func (sw *SafeWriter) WriteBytes(b []byte) error {
	n, err := sw.w.Write(b)
	sw.offset += int64(n)
	return err
}

// WriteString writes a string as bytes to the underlying writer.
func (sw *SafeWriter) WriteString(s string) error {
	return sw.WriteBytes([]byte(s))
}

// Write writes a value of type T in big-endian byte order.
// T must be uint8, uint16, uint32, or uint64.
func Write[T uint8 | uint16 | uint32 | uint64](sw *SafeWriter, val T) error {
	var buf []byte

	// Determine size and encode based on type
	var zero T
	switch any(zero).(type) {
	case uint8:
		buf = []byte{byte(val)}
	case uint16:
		buf = make([]byte, 2)
		binary.BigEndian.PutUint16(buf, uint16(val))
	case uint32:
		buf = make([]byte, 4)
		binary.BigEndian.PutUint32(buf, uint32(val))
	case uint64:
		buf = make([]byte, 8)
		binary.BigEndian.PutUint64(buf, uint64(val))
	}

	return sw.WriteBytes(buf)
}

// WriteLE writes a value of type T in little-endian byte order.
// T must be uint8, uint16, uint32, or uint64.
func WriteLE[T uint8 | uint16 | uint32 | uint64](sw *SafeWriter, val T) error {
	var buf []byte

	// Determine size and encode based on type
	var zero T
	switch any(zero).(type) {
	case uint8:
		buf = []byte{byte(val)}
	case uint16:
		buf = make([]byte, 2)
		binary.LittleEndian.PutUint16(buf, uint16(val))
	case uint32:
		buf = make([]byte, 4)
		binary.LittleEndian.PutUint32(buf, uint32(val))
	case uint64:
		buf = make([]byte, 8)
		binary.LittleEndian.PutUint64(buf, uint64(val))
	}

	return sw.WriteBytes(buf)
}
