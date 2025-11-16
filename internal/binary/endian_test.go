package binary

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestReadLE(t *testing.T) {
	// Create test data with known little-endian values
	buf := &bytes.Buffer{}

	// uint16: 0x0201 (little-endian) = 513 (decimal)
	binary.Write(buf, binary.LittleEndian, uint16(513))

	// uint32: 0x04030201 (little-endian) = 67305985 (decimal)
	binary.Write(buf, binary.LittleEndian, uint32(67305985))

	// uint64: 0x0807060504030201 (little-endian)
	binary.Write(buf, binary.LittleEndian, uint64(578437695752307201))

	data := buf.Bytes()
	sr := NewSafeReader(bytes.NewReader(data), int64(len(data)), "test.flac")

	tests := []struct {
		readFunc func() (uint64, error)
		name     string
		offset   int64
		want     uint64
	}{
		{
			name:   "uint16 little-endian",
			offset: 0,
			want:   513,
			readFunc: func() (uint64, error) {
				val, err := ReadLE[uint16](sr, 0, "uint16")
				return uint64(val), err
			},
		},
		{
			name:   "uint32 little-endian",
			offset: 2,
			want:   67305985,
			readFunc: func() (uint64, error) {
				val, err := ReadLE[uint32](sr, 2, "uint32")
				return uint64(val), err
			},
		},
		{
			name:   "uint64 little-endian",
			offset: 6,
			want:   578437695752307201,
			readFunc: func() (uint64, error) {
				val, err := ReadLE[uint64](sr, 6, "uint64")
				return uint64(val), err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.readFunc()
			if err != nil {
				t.Fatalf("ReadLE failed: %v", err)
			}
			if got != tt.want {
				t.Errorf("ReadLE() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestReadBE(t *testing.T) {
	// Create test data with known big-endian values
	buf := &bytes.Buffer{}

	// uint16: 0x0102 (big-endian) = 258 (decimal)
	binary.Write(buf, binary.BigEndian, uint16(258))

	// uint32: 0x01020304 (big-endian) = 16909060 (decimal)
	binary.Write(buf, binary.BigEndian, uint32(16909060))

	// uint64: 0x0102030405060708 (big-endian)
	binary.Write(buf, binary.BigEndian, uint64(72623859790382856))

	data := buf.Bytes()
	sr := NewSafeReader(bytes.NewReader(data), int64(len(data)), "test.m4a")

	tests := []struct {
		readFunc func() (uint64, error)
		name     string
		offset   int64
		want     uint64
	}{
		{
			name:   "uint16 big-endian",
			offset: 0,
			want:   258,
			readFunc: func() (uint64, error) {
				val, err := ReadBE[uint16](sr, 0, "uint16")
				return uint64(val), err
			},
		},
		{
			name:   "uint32 big-endian",
			offset: 2,
			want:   16909060,
			readFunc: func() (uint64, error) {
				val, err := ReadBE[uint32](sr, 2, "uint32")
				return uint64(val), err
			},
		},
		{
			name:   "uint64 big-endian",
			offset: 6,
			want:   72623859790382856,
			readFunc: func() (uint64, error) {
				val, err := ReadBE[uint64](sr, 6, "uint64")
				return uint64(val), err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.readFunc()
			if err != nil {
				t.Fatalf("ReadBE failed: %v", err)
			}
			if got != tt.want {
				t.Errorf("ReadBE() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestReadEndian(t *testing.T) {
	// Create test data
	data := []byte{0x01, 0x02, 0x03, 0x04}
	sr := NewSafeReader(bytes.NewReader(data), int64(len(data)), "test")

	t.Run("uint32 big-endian", func(t *testing.T) {
		val, err := ReadEndian[uint32](sr, 0, "test", BigEndian)
		if err != nil {
			t.Fatalf("ReadEndian failed: %v", err)
		}
		// 0x01020304 = 16909060
		if val != 16909060 {
			t.Errorf("ReadEndian(BigEndian) = %d, want 16909060", val)
		}
	})

	t.Run("uint32 little-endian", func(t *testing.T) {
		val, err := ReadEndian[uint32](sr, 0, "test", LittleEndian)
		if err != nil {
			t.Fatalf("ReadEndian failed: %v", err)
		}
		// 0x04030201 = 67305985
		if val != 67305985 {
			t.Errorf("ReadEndian(LittleEndian) = %d, want 67305985", val)
		}
	})
}

func TestEndianness_RealWorldExample(t *testing.T) {
	// Simulate FLAC Vorbis comment length (little-endian)
	// and MP4 atom size (big-endian) side by side
	buf := &bytes.Buffer{}

	// FLAC Vorbis comment vendor length: 26 bytes (little-endian)
	binary.Write(buf, binary.LittleEndian, uint32(26))

	// MP4 atom size: 1000 bytes (big-endian)
	binary.Write(buf, binary.BigEndian, uint32(1000))

	data := buf.Bytes()
	sr := NewSafeReader(bytes.NewReader(data), int64(len(data)), "test")

	t.Run("FLAC vendor length (LE)", func(t *testing.T) {
		length, err := ReadLE[uint32](sr, 0, "vendor length")
		if err != nil {
			t.Fatalf("ReadLE failed: %v", err)
		}
		if length != 26 {
			t.Errorf("FLAC vendor length = %d, want 26", length)
		}
	})

	t.Run("MP4 atom size (BE)", func(t *testing.T) {
		size, err := ReadBE[uint32](sr, 4, "atom size")
		if err != nil {
			t.Fatalf("ReadBE failed: %v", err)
		}
		if size != 1000 {
			t.Errorf("MP4 atom size = %d, want 1000", size)
		}
	})
}

func TestReadEndian_Uint8(t *testing.T) {
	// Uint8 should be the same regardless of endianness
	data := []byte{0x42}
	sr := NewSafeReader(bytes.NewReader(data), int64(len(data)), "test")

	beByte, err := ReadBE[uint8](sr, 0, "byte")
	if err != nil {
		t.Fatalf("ReadBE uint8 failed: %v", err)
	}

	leByte, err := ReadLE[uint8](sr, 0, "byte")
	if err != nil {
		t.Fatalf("ReadLE uint8 failed: %v", err)
	}

	if beByte != 0x42 || leByte != 0x42 {
		t.Errorf("uint8 values should be 0x42, got BE=%d, LE=%d", beByte, leByte)
	}

	if beByte != leByte {
		t.Errorf("uint8 should be same for both endianness, got BE=%d, LE=%d", beByte, leByte)
	}
}

// Benchmark endianness reads.
func BenchmarkReadLE_Uint32(b *testing.B) {
	data := []byte{0x01, 0x02, 0x03, 0x04}
	sr := NewSafeReader(bytes.NewReader(data), int64(len(data)), "bench")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ReadLE[uint32](sr, 0, "uint32")
	}
}

func BenchmarkReadBE_Uint32(b *testing.B) {
	data := []byte{0x01, 0x02, 0x03, 0x04}
	sr := NewSafeReader(bytes.NewReader(data), int64(len(data)), "bench")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ReadBE[uint32](sr, 0, "uint32")
	}
}

func BenchmarkRead_Uint32_Legacy(b *testing.B) {
	data := []byte{0x01, 0x02, 0x03, 0x04}
	sr := NewSafeReader(bytes.NewReader(data), int64(len(data)), "bench")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Read[uint32](sr, 0, "uint32")
	}
}
