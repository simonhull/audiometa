package m4a

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	audiobinary "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// createMvhdAtom creates a movie header atom with duration.
func createMvhdAtom(version byte, timescale, duration uint32) []byte {
	buf := &bytes.Buffer{}

	// mvhd data
	buf.WriteByte(version)              // version
	buf.Write([]byte{0x00, 0x00, 0x00}) // flags

	if version == 1 {
		// 64-bit version (not implementing for now)
		panic("64-bit mvhd not implemented in test")
	} else {
		// 32-bit version
		binary.Write(buf, binary.BigEndian, uint32(0)) // creation time
		binary.Write(buf, binary.BigEndian, uint32(0)) // modification time
		binary.Write(buf, binary.BigEndian, timescale)
		binary.Write(buf, binary.BigEndian, duration)
	}

	return createMockAtom("mvhd", buf.Bytes())
}

func TestParseTechnicalInfo_Duration(t *testing.T) {
	// Create moov with mvhd
	// Duration: 10 seconds at 1000 Hz timescale = 10000 units
	mvhdAtom := createMvhdAtom(0, 1000, 10000)
	moovData := mvhdAtom
	moov := createMockAtom("moov", moovData)

	sr := audiobinary.NewSafeReader(bytes.NewReader(moov), int64(len(moov)), "test.m4b")
	moovAtom, _ := readAtomHeader(sr, 0)

	file := &types.File{}
	err := parseTechnicalInfo(sr, moovAtom, file)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := 10 * time.Second
	if file.Audio.Duration != expected {
		t.Errorf("expected duration %v, got %v", expected, file.Audio.Duration)
	}
}

func TestParseTechnicalInfo_NonStandardTimescale(t *testing.T) {
	// Duration: 5.5 seconds at 44100 Hz timescale = 242550 units
	mvhdAtom := createMvhdAtom(0, 44100, 242550)
	moovData := mvhdAtom
	moov := createMockAtom("moov", moovData)

	sr := audiobinary.NewSafeReader(bytes.NewReader(moov), int64(len(moov)), "test.m4b")
	moovAtom, _ := readAtomHeader(sr, 0)

	file := &types.File{}
	err := parseTechnicalInfo(sr, moovAtom, file)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := time.Duration(5.5 * float64(time.Second))
	// Allow small rounding error
	diff := file.Audio.Duration - expected
	if diff < 0 {
		diff = -diff
	}
	if diff > time.Millisecond {
		t.Errorf("expected duration ~%v, got %v", expected, file.Audio.Duration)
	}
}

func TestParseTechnicalInfo_NoMvhd(t *testing.T) {
	// moov with no mvhd
	moov := createMockAtom("moov", []byte{})

	sr := audiobinary.NewSafeReader(bytes.NewReader(moov), int64(len(moov)), "test.m4b")
	moovAtom, _ := readAtomHeader(sr, 0)

	file := &types.File{}
	err := parseTechnicalInfo(sr, moovAtom, file)

	// Should not error, just leave duration as 0
	if err != nil {
		t.Errorf("expected no error for missing mvhd, got %v", err)
	}

	if file.Audio.Duration != 0 {
		t.Errorf("expected duration 0 for missing mvhd, got %v", file.Audio.Duration)
	}
}
