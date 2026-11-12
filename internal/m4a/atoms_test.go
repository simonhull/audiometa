package m4a

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"slices"
	"testing"
	"time"

	audiobinary "github.com/simonhull/audiometa/internal/binary"
)

// createMockAtom creates a test atom with given type and data.
func createMockAtom(atomType string, data []byte) []byte {
	buf := &bytes.Buffer{}

	// Write size (8 byte header + data length)
	size := uint32(8 + len(data))
	binary.Write(buf, binary.BigEndian, size)

	// Write type
	buf.WriteString(atomType)

	// Write data
	buf.Write(data)

	return buf.Bytes()
}

func TestReadAtomHeader_Success(t *testing.T) {
	data := createMockAtom("moov", []byte{0x01, 0x02, 0x03, 0x04})

	sr := audiobinary.NewSafeReader(bytes.NewReader(data), int64(len(data)), "test.m4b")

	atom, err := readAtomHeader(sr, 0, int64(len(data)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if atom.Size != 12 {
		t.Errorf("expected size 12, got %d", atom.Size)
	}

	if atom.Type != "moov" {
		t.Errorf("expected type 'moov', got %s", atom.Type)
	}

	if atom.Offset != 0 {
		t.Errorf("expected offset 0, got %d", atom.Offset)
	}

	if atom.DataSize() != 4 {
		t.Errorf("expected data size 4, got %d", atom.DataSize())
	}

	if atom.DataOffset() != 8 {
		t.Errorf("expected data offset 8, got %d", atom.DataOffset())
	}
}

func TestReadAtomHeader_Extended(t *testing.T) {
	buf := &bytes.Buffer{}

	// Extended size atom: size=1, then 64-bit size
	binary.Write(buf, binary.BigEndian, uint32(1))
	buf.WriteString("mdat")
	binary.Write(buf, binary.BigEndian, uint64(1000))

	data := buf.Bytes()
	sr := audiobinary.NewSafeReader(bytes.NewReader(data), int64(len(data)), "test.m4b")

	atom, err := readAtomHeader(sr, 0, 1000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if atom.Size != 1000 {
		t.Errorf("expected size 1000, got %d", atom.Size)
	}

	if atom.Type != "mdat" {
		t.Errorf("expected type 'mdat', got %s", atom.Type)
	}

	// Extended header is 16 bytes
	if atom.DataOffset() != 16 {
		t.Errorf("expected data offset 16, got %d", atom.DataOffset())
	}
}

func TestReadAtomHeader_TooSmall(t *testing.T) {
	// File too small to contain atom header
	data := []byte{0x00, 0x00, 0x00}

	sr := audiobinary.NewSafeReader(bytes.NewReader(data), int64(len(data)), "test.m4b")

	_, err := readAtomHeader(sr, 0, int64(len(data)))
	if err == nil {
		t.Fatal("expected error for file too small")
	}
}

func TestReadAtomHeader_InvalidSize(t *testing.T) {
	buf := &bytes.Buffer{}

	// Size too small (less than 8)
	binary.Write(buf, binary.BigEndian, uint32(4))
	buf.WriteString("test")

	data := buf.Bytes()
	sr := audiobinary.NewSafeReader(bytes.NewReader(data), int64(len(data)), "test.m4b")

	_, err := readAtomHeader(sr, 0, int64(len(data)))
	if err == nil {
		t.Fatal("expected error for invalid atom size")
	}
}

func TestFindAtom_Found(t *testing.T) {
	// Create a container with multiple atoms
	atom1 := createMockAtom("free", []byte{0x00, 0x00})
	atom2 := createMockAtom("moov", []byte{0x01, 0x02, 0x03})
	atom3 := createMockAtom("mdat", []byte{0x04, 0x05})

	data := slices.Concat(atom1, atom2, atom3)

	sr := audiobinary.NewSafeReader(bytes.NewReader(data), int64(len(data)), "test.m4b")

	atom, err := findAtom(t.Context(), sr, 0, int64(len(data)), "moov")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if atom.Type != "moov" {
		t.Errorf("expected type 'moov', got %s", atom.Type)
	}

	// moov should be at offset 10 (after first atom)
	if atom.Offset != int64(len(atom1)) {
		t.Errorf("expected offset %d, got %d", len(atom1), atom.Offset)
	}
}

func TestFindAtom_NotFound(t *testing.T) {
	atom1 := createMockAtom("free", []byte{0x00})
	atom2 := createMockAtom("mdat", []byte{0x01})

	data := slices.Concat(atom1, atom2)

	sr := audiobinary.NewSafeReader(bytes.NewReader(data), int64(len(data)), "test.m4b")

	_, err := findAtom(t.Context(), sr, 0, int64(len(data)), "moov")
	if err == nil {
		t.Fatal("expected error when atom not found")
	}
}

func TestFindAtom_Nested(t *testing.T) {
	// Create nested structure: udta contains meta
	metaAtom := createMockAtom("meta", []byte{0x01, 0x02})
	udtaData := createMockAtom("udta", metaAtom)

	sr := audiobinary.NewSafeReader(bytes.NewReader(udtaData), int64(len(udtaData)), "test.m4b")

	// Find the udta atom first
	udtaAtom, err := readAtomHeader(sr, 0, int64(len(udtaData)))
	if err != nil {
		t.Fatalf("failed to read udta: %v", err)
	}

	if udtaAtom.Type != "udta" {
		t.Fatalf("expected type 'udta', got %s", udtaAtom.Type)
	}

	// Now find meta inside udta
	foundMeta, err := findAtom(t.Context(), sr, udtaAtom.DataOffset(), udtaAtom.DataOffset()+udtaAtom.DataSize(), "meta")
	if err != nil {
		t.Fatalf("failed to find nested meta: %v", err)
	}

	if foundMeta.Type != "meta" {
		t.Errorf("expected type 'meta', got %s", foundMeta.Type)
	}
}

func TestReadAtomHeader_ExceedsParent(t *testing.T) {
	ext := func(size uint64) []byte {
		buf := &bytes.Buffer{}
		binary.Write(buf, binary.BigEndian, uint32(1))
		buf.WriteString("free")
		binary.Write(buf, binary.BigEndian, size)
		return buf.Bytes()
	}
	tests := []struct {
		name      string
		data      []byte
		offset    int64
		parentEnd int64
	}{
		{"32-bit size larger than remaining", createMockAtom("free", make([]byte, 8)), 0, 12},
		{"extended size below header", ext(12), 0, 16},
		{"extended size overflows int64", ext(0xFFFFFFFFFFFFFFF0), 0, 16},
		{"offset at parent end", createMockAtom("free", nil), 8, 8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := append(make([]byte, tt.offset), tt.data...)
			sr := audiobinary.NewSafeReader(bytes.NewReader(data), int64(len(data)), "test.m4b")
			if _, err := readAtomHeader(sr, tt.offset, tt.parentEnd); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestReadAtomHeader_ExtendedSizeBeyondInt64(t *testing.T) {
	for _, size := range []uint64{1 << 63, ^uint64(0)} {
		buf := &bytes.Buffer{}
		binary.Write(buf, binary.BigEndian, uint32(1))
		buf.WriteString("mdat")
		binary.Write(buf, binary.BigEndian, size)
		data := buf.Bytes()
		sr := audiobinary.NewSafeReader(bytes.NewReader(data), int64(len(data)), "test.m4b")

		atom, err := readAtomHeader(sr, 0, int64(len(data)))
		if err == nil {
			t.Fatalf("size %#x: got atom %+v, want error", size, atom)
		}
	}
}

func TestDecodeAtomHeader(t *testing.T) {
	ext := func(size uint64) []byte { return binary.BigEndian.AppendUint64(append(u32be(1), "free"...), size) }
	tests := []struct {
		name     string
		b        []byte
		room     int64
		zero     SizeZero
		wantSize int64
		wantHdr  int64
		wantErr  bool
	}{
		{"plain", append(u32be(16), "free"...), 100, SizeZeroInvalid, 16, 8, false},
		{"extended", ext(40), 100, SizeZeroInvalid, 40, 16, false},
		{"zero at top level runs to the end", append(u32be(0), "mdat"...), 100, SizeZeroToEnd, 100, 8, false},
		{"zero inside a container", append(u32be(0), "mdat"...), 100, SizeZeroInvalid, 0, 0, true},
		{"smaller than its header", append(u32be(4), "free"...), 100, SizeZeroInvalid, 0, 0, true},
		{"larger than the room", append(u32be(200), "free"...), 100, SizeZeroInvalid, 0, 0, true},
		{"extended overflow", ext(^uint64(0)), 100, SizeZeroInvalid, 0, 0, true},
		{"truncated header", []byte{0, 0, 0}, 100, SizeZeroInvalid, 0, 0, true},
		{"truncated extended size", append(u32be(1), "free"...), 100, SizeZeroInvalid, 0, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := DecodeAtomHeader(tt.b, tt.room, tt.zero)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if h.Size != tt.wantSize || h.HeaderLen != tt.wantHdr || h.Type != string(tt.b[4:8]) {
				t.Errorf("header = %+v, want size %d, header length %d, type %q", h, tt.wantSize, tt.wantHdr, tt.b[4:8])
			}
		})
	}

	t.Run("size errors carry the declared size", func(t *testing.T) {
		for _, tc := range []struct {
			b    []byte
			want uint64
		}{
			{append(u32be(4), "free"...), 4},
			{ext(^uint64(0)), ^uint64(0)},
		} {
			_, err := DecodeAtomHeader(tc.b, 100, SizeZeroInvalid)
			var sizeErr *AtomSizeError
			if !errors.As(err, &sizeErr) || sizeErr.Size != tc.want {
				t.Errorf("err = %v, want an *AtomSizeError with size %d", err, tc.want)
			}
		}
	})
	t.Run("short input is errAtomHeaderTruncated", func(t *testing.T) {
		if _, err := DecodeAtomHeader([]byte{0, 0, 0}, 100, SizeZeroInvalid); !errors.Is(err, errAtomHeaderTruncated) {
			t.Errorf("err = %v, want errAtomHeaderTruncated", err)
		}
	})
}

func TestParse_TopLevelMoovWithSizeZeroRunsToEnd(t *testing.T) {
	moov := createMockAtom("moov", createMvhdAtom(1000, 10000))
	copy(moov, u32be(0)) // the size field of the last top-level atom, as the format allows

	file := parseBytes(t, append(m4bFtyp(), moov...))

	if file.Audio.Duration != 10*time.Second {
		t.Errorf("duration = %v, want 10s", file.Audio.Duration)
	}
	if len(file.Warnings) != 0 {
		t.Errorf("warnings = %+v, want none", file.Warnings)
	}
}

func TestAtomChildAndPath(t *testing.T) {
	ilst := createMockAtom("ilst", createMockAtom("\xa9nam", nil))
	meta := fullAtom("meta", ilst) // full atom: 4 bytes of version/flags before its children
	moov := createMockAtom("moov", createMockAtom("udta", meta))
	sr := audiobinary.NewSafeReader(bytes.NewReader(moov), int64(len(moov)), "t.m4a")
	root, err := readAtomHeader(sr, 0, int64(len(moov)))
	if err != nil {
		t.Fatal(err)
	}
	got, err := root.path(t.Context(), sr, "udta", "meta", "ilst")
	if err != nil || got.Type != "ilst" {
		t.Fatalf("path = %+v, %v", got, err)
	}
	if _, err := root.child(t.Context(), sr, "trak"); !errors.Is(err, errAtomNotFound) {
		t.Errorf("child(trak) err = %v, want errAtomNotFound", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := root.child(ctx, sr, "udta"); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled child err = %v", err)
	}
}
