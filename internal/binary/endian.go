package binary

import "encoding/binary"

// Endianness represents byte order for multi-byte values.
type Endianness int

const (
	// BigEndian uses big-endian byte order.
	// Used by: MP4/M4A, MP3 ID3v2, most network protocols.
	BigEndian Endianness = iota

	// LittleEndian uses little-endian byte order.
	// Used by: FLAC Vorbis comments, WAV, x86/x64 architectures.
	LittleEndian
)

// ReadLE reads a numeric value of type T at the given offset using little-endian byte order.
//
// This is a convenience wrapper for ReadEndian with LittleEndian.
// Use for formats like FLAC Vorbis comments.
//
// Example:
//
//	length, err := binary.ReadLE[uint32](sr, offset, "vorbis comment length")
func ReadLE[T uint8 | uint16 | uint32 | uint64](sr *SafeReader, off int64, what string) (T, error) {
	return ReadEndian[T](sr, off, what, LittleEndian)
}

// ReadBE reads a numeric value of type T at the given offset using big-endian byte order.
//
// This is a convenience wrapper for ReadEndian with BigEndian.
// Equivalent to Read() but more explicit about byte order.
//
// Example:
//
//	atomSize, err := binary.ReadBE[uint32](sr, offset, "atom size")
func ReadBE[T uint8 | uint16 | uint32 | uint64](sr *SafeReader, off int64, what string) (T, error) {
	return ReadEndian[T](sr, off, what, BigEndian)
}

// ReadEndian reads a numeric value of type T at the given offset with specified byte order.
//
// This is the low-level function used by Read, ReadLE, and ReadBE.
// Most code should use the convenience wrappers instead.
//
// Example:
//
//	value, err := binary.ReadEndian[uint32](sr, offset, "field", binary.LittleEndian)
func ReadEndian[T uint8 | uint16 | uint32 | uint64](sr *SafeReader, off int64, what string, endian Endianness) (T, error) {
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
	}

	buf := make([]byte, size)
	if err := sr.ReadAt(buf, off, what); err != nil {
		return zero, err
	}

	// Convert bytes to value based on endianness
	var val T
	switch any(zero).(type) {
	case uint8:
		val = T(buf[0])
	case uint16:
		if endian == LittleEndian {
			val = T(binary.LittleEndian.Uint16(buf))
		} else {
			val = T(binary.BigEndian.Uint16(buf))
		}
	case uint32:
		if endian == LittleEndian {
			val = T(binary.LittleEndian.Uint32(buf))
		} else {
			val = T(binary.BigEndian.Uint32(buf))
		}
	case uint64:
		if endian == LittleEndian {
			val = T(binary.LittleEndian.Uint64(buf))
		} else {
			val = T(binary.BigEndian.Uint64(buf))
		}
	}

	return val, nil
}
