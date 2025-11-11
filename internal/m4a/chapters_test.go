package m4a

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	audiobinary "github.com/simonhull/audiometa/internal/binary"
)

// createChplAtom creates a chapter list atom
func createChplAtom(chapters []struct {
	time  int64
	title string
}) []byte {
	buf := &bytes.Buffer{}

	// chpl structure:
	// [1 byte]  version
	// [3 bytes] flags
	// [4 bytes] reserved
	// [1 byte]  chapter count

	buf.WriteByte(1)                               // version
	buf.Write([]byte{0x00, 0x00, 0x00})            // flags
	binary.Write(buf, binary.BigEndian, uint32(0)) // reserved
	buf.WriteByte(byte(len(chapters)))             // count (1 byte)

	// For each chapter:
	// [8 bytes] start time (100-nanosecond units)
	// [1 byte]  title length
	// [N bytes] title (UTF-8)
	for _, ch := range chapters {
		binary.Write(buf, binary.BigEndian, uint64(ch.time)) // start time
		buf.WriteByte(byte(len(ch.title)))                   // title length
		buf.WriteString(ch.title)                            // title
	}

	return createMockAtom("chpl", buf.Bytes())
}

func TestParseChapters_Success(t *testing.T) {
	// Create chapters with start times and titles
	chapterData := []struct {
		time  int64
		title string
	}{
		{0, "Chapter 1"},
		{600_000_000, "Chapter 2"},   // 60 seconds * 10^7 (100ns units)
		{1_200_000_000, "Chapter 3"}, // 120 seconds
	}

	chpl := createChplAtom(chapterData)
	udta := createMockAtom("udta", chpl)
	moov := createMockAtom("moov", udta)

	sr := audiobinary.NewSafeReader(bytes.NewReader(moov), int64(len(moov)), "test.m4b")
	moovAtom, _ := readAtomHeader(sr, 0)

	// File duration: 180 seconds
	fileDuration := 180 * time.Second

	chapters, err := parseChapters(sr, moovAtom, fileDuration)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(chapters) != 3 {
		t.Fatalf("expected 3 chapters, got %d", len(chapters))
	}

	// Verify chapter 1
	if chapters[0].Index != 1 {
		t.Errorf("chapter 0: expected index 1, got %d", chapters[0].Index)
	}
	if chapters[0].Title != "Chapter 1" {
		t.Errorf("chapter 0: expected title 'Chapter 1', got '%s'", chapters[0].Title)
	}
	if chapters[0].StartTime != 0 {
		t.Errorf("chapter 0: expected start 0, got %v", chapters[0].StartTime)
	}
	if chapters[0].EndTime != 60*time.Second {
		t.Errorf("chapter 0: expected end 60s, got %v", chapters[0].EndTime)
	}

	// Verify chapter 2
	if chapters[1].StartTime != 60*time.Second {
		t.Errorf("chapter 1: expected start 60s, got %v", chapters[1].StartTime)
	}
	if chapters[1].EndTime != 120*time.Second {
		t.Errorf("chapter 1: expected end 120s, got %v", chapters[1].EndTime)
	}

	// Verify chapter 3 (last chapter ends at file duration)
	if chapters[2].StartTime != 120*time.Second {
		t.Errorf("chapter 2: expected start 120s, got %v", chapters[2].StartTime)
	}
	if chapters[2].EndTime != 180*time.Second {
		t.Errorf("chapter 2: expected end 180s (file duration), got %v", chapters[2].EndTime)
	}
}

func TestParseChapters_EmptyTitles(t *testing.T) {
	chapterData := []struct {
		time  int64
		title string
	}{
		{0, ""},
		{500_000_000, ""},
	}

	chpl := createChplAtom(chapterData)
	udta := createMockAtom("udta", chpl)
	moov := createMockAtom("moov", udta)

	sr := audiobinary.NewSafeReader(bytes.NewReader(moov), int64(len(moov)), "test.m4b")
	moovAtom, _ := readAtomHeader(sr, 0)

	chapters, err := parseChapters(sr, moovAtom, 100*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(chapters) != 2 {
		t.Fatalf("expected 2 chapters, got %d", len(chapters))
	}

	// Should have empty titles
	if chapters[0].Title != "" {
		t.Errorf("expected empty title, got '%s'", chapters[0].Title)
	}
}

func TestParseChapters_SingleChapter(t *testing.T) {
	chapterData := []struct {
		time  int64
		title string
	}{
		{0, "The Book"},
	}

	chpl := createChplAtom(chapterData)
	udta := createMockAtom("udta", chpl)
	moov := createMockAtom("moov", udta)

	sr := audiobinary.NewSafeReader(bytes.NewReader(moov), int64(len(moov)), "test.m4b")
	moovAtom, _ := readAtomHeader(sr, 0)

	chapters, err := parseChapters(sr, moovAtom, 3600*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(chapters) != 1 {
		t.Fatalf("expected 1 chapter, got %d", len(chapters))
	}

	// Single chapter should span entire duration
	if chapters[0].StartTime != 0 {
		t.Errorf("expected start 0, got %v", chapters[0].StartTime)
	}
	if chapters[0].EndTime != 3600*time.Second {
		t.Errorf("expected end 3600s, got %v", chapters[0].EndTime)
	}
}

func TestParseChapters_NoChpl(t *testing.T) {
	// moov with udta but no chpl
	udta := createMockAtom("udta", []byte{})
	moov := createMockAtom("moov", udta)

	sr := audiobinary.NewSafeReader(bytes.NewReader(moov), int64(len(moov)), "test.m4b")
	moovAtom, _ := readAtomHeader(sr, 0)

	chapters, err := parseChapters(sr, moovAtom, 100*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should return empty slice, not error
	if len(chapters) != 0 {
		t.Errorf("expected 0 chapters, got %d", len(chapters))
	}
}

func TestParseChapters_NoUdta(t *testing.T) {
	// moov without udta
	moov := createMockAtom("moov", []byte{})

	sr := audiobinary.NewSafeReader(bytes.NewReader(moov), int64(len(moov)), "test.m4b")
	moovAtom, _ := readAtomHeader(sr, 0)

	chapters, err := parseChapters(sr, moovAtom, 100*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should return empty slice, not error
	if len(chapters) != 0 {
		t.Errorf("expected 0 chapters, got %d", len(chapters))
	}
}
