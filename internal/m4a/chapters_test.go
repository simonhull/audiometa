package m4a

import (
	"bytes"
	"context"
	"encoding/binary"
	"slices"
	"testing"
	"time"

	audiobinary "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// createChplAtom creates a chapter list atom.
func createChplAtom(chapters []struct {
	time  int64
	title string
}) []byte {
	return createChplAtomVersion(1, chapters)
}

// createChplAtomVersion creates a chpl atom; the 4 reserved bytes are written only when version != 0.
func createChplAtomVersion(version byte, chapters []struct {
	time  int64
	title string
}) []byte {
	buf := &bytes.Buffer{}

	// chpl structure:
	// [1 byte]  version
	// [3 bytes] flags
	// [4 bytes] reserved
	// [1 byte]  chapter count

	buf.WriteByte(version)              // version
	buf.Write([]byte{0x00, 0x00, 0x00}) // flags
	if version != 0 {
		binary.Write(buf, binary.BigEndian, uint32(0)) // reserved
	}
	buf.WriteByte(byte(len(chapters))) // count (1 byte)

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
	moovAtom, _ := readAtomHeader(sr, 0, int64(len(moov)))

	// File duration: 180 seconds
	fileDuration := 180 * time.Second

	chapters, err := parseChapters(context.Background(), sr, moovAtom, fileDuration)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(chapters) != 3 {
		t.Fatalf("expected 3 chapters, got %d", len(chapters))
	}

	for i, c := range chapters {
		if c.Source != types.ChapterSourceNero {
			t.Errorf("chapter %d: source = %q, want %q", i, c.Source, types.ChapterSourceNero)
		}
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
	moovAtom, _ := readAtomHeader(sr, 0, int64(len(moov)))

	chapters, err := parseChapters(context.Background(), sr, moovAtom, 100*time.Second)
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
	moovAtom, _ := readAtomHeader(sr, 0, int64(len(moov)))

	chapters, err := parseChapters(context.Background(), sr, moovAtom, 3600*time.Second)
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
	moovAtom, _ := readAtomHeader(sr, 0, int64(len(moov)))

	chapters, err := parseChapters(context.Background(), sr, moovAtom, 100*time.Second)
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
	moovAtom, _ := readAtomHeader(sr, 0, int64(len(moov)))

	chapters, err := parseChapters(context.Background(), sr, moovAtom, 100*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should return empty slice, not error
	if len(chapters) != 0 {
		t.Errorf("expected 0 chapters, got %d", len(chapters))
	}
}

func u32be(vs ...uint32) []byte {
	var b []byte
	for _, v := range vs {
		b = binary.BigEndian.AppendUint32(b, v)
	}
	return b
}

func fullAtom(typ string, body []byte) []byte {
	return createMockAtom(typ, append(u32be(0), body...))
}

func textSample(s string) []byte {
	return append(binary.BigEndian.AppendUint16(nil, uint16(len(s))), s...)
}

func sttsAtom(entries ...[2]uint32) []byte {
	body := u32be(uint32(len(entries)))
	for _, e := range entries {
		body = append(body, u32be(e[0], e[1])...)
	}
	return fullAtom("stts", body)
}

func stszAtom(defaultSize, count uint32, sizes ...uint32) []byte {
	return fullAtom("stsz", u32be(append([]uint32{defaultSize, count}, sizes...)...))
}

func stscAtom(entries ...[3]uint32) []byte {
	body := u32be(uint32(len(entries)))
	for _, e := range entries {
		body = append(body, u32be(e[0], e[1], e[2])...)
	}
	return fullAtom("stsc", body)
}

func stcoAtom(offsets ...uint32) []byte {
	return fullAtom("stco", u32be(append([]uint32{uint32(len(offsets))}, offsets...)...))
}

func tkhdAtom(id uint32) []byte {
	body := make([]byte, 80)
	binary.BigEndian.PutUint32(body[8:], id)
	return fullAtom("tkhd", body)
}

// buildQTChapterFile returns a file laid out as [mdat(payload)][moov] and the moov offset.
// Payload byte k is at absolute file offset 8+k.
func buildQTChapterFile(timescale uint32, payload []byte, stblChildren ...[]byte) ([]byte, int64) {
	mdat := createMockAtom("mdat", payload)
	trak1 := createMockAtom("trak", append(tkhdAtom(1), createMockAtom("tref", createMockAtom("chap", u32be(2)))...))
	stbl := createMockAtom("stbl", bytes.Join(stblChildren, nil))
	minf := createMockAtom("minf", stbl)
	mdhd := fullAtom("mdhd", u32be(0, 0, timescale, 0, 0))
	mdia := createMockAtom("mdia", append(mdhd, minf...))
	trak2 := createMockAtom("trak", append(tkhdAtom(2), mdia...))
	moov := createMockAtom("moov", append(trak1, trak2...))
	return append(mdat, moov...), int64(len(mdat))
}

func parseQTChapters(t *testing.T, data []byte, moovOffset int64, dur time.Duration) []types.Chapter {
	t.Helper()
	sr := audiobinary.NewSafeReader(bytes.NewReader(data), int64(len(data)), "test.m4b")
	moov, err := readAtomHeader(sr, moovOffset, int64(len(data)))
	if err != nil {
		t.Fatalf("readAtomHeader: %v", err)
	}
	chapters, err := parseChapters(context.Background(), sr, moov, dur)
	if err != nil {
		t.Fatalf("parseChapters: %v", err)
	}
	return chapters
}

func checkChapters(t *testing.T, got []types.Chapter, titles []string, starts []time.Duration) {
	t.Helper()
	if len(got) != len(titles) {
		t.Fatalf("got %d chapters, want %d: %+v", len(got), len(titles), got)
	}
	for i, ch := range got {
		if ch.Title != titles[i] {
			t.Errorf("chapter %d title = %q, want %q", i, ch.Title, titles[i])
		}
		if ch.StartTime != starts[i] {
			t.Errorf("chapter %d start = %v, want %v", i, ch.StartTime, starts[i])
		}
	}
}

func TestQTChapters_OneSamplePerChunk(t *testing.T) {
	payload := slices.Concat(textSample("Intro"), textSample("Middle"), textSample("End"))
	data, moovOff := buildQTChapterFile(1000, payload,
		sttsAtom([2]uint32{3, 1000}),
		stszAtom(0, 3, 7, 8, 5),
		stscAtom([3]uint32{1, 1, 1}),
		stcoAtom(8, 15, 23),
	)
	got := parseQTChapters(t, data, moovOff, 10*time.Second)
	checkChapters(t, got, []string{"Intro", "Middle", "End"}, []time.Duration{0, time.Second, 2 * time.Second})
	wantEnds := []time.Duration{time.Second, 2 * time.Second, 10 * time.Second}
	for i, ch := range got {
		if ch.EndTime != wantEnds[i] {
			t.Errorf("chapter %d end = %v, want %v", i, ch.EndTime, wantEnds[i])
		}
	}
}

func TestQTChapters_StszDefaultSize(t *testing.T) {
	payload := slices.Concat(textSample("Ch 1"), textSample("Ch 2"), textSample("Ch 3"))
	data, moovOff := buildQTChapterFile(1000, payload,
		sttsAtom([2]uint32{3, 1000}),
		stszAtom(6, 3),
		stscAtom([3]uint32{1, 3, 1}),
		stcoAtom(8),
	)
	got := parseQTChapters(t, data, moovOff, 10*time.Second)
	checkChapters(t, got, []string{"Ch 1", "Ch 2", "Ch 3"}, []time.Duration{0, time.Second, 2 * time.Second})
}

func TestQTChapters_MultipleSamplesPerChunk(t *testing.T) {
	payload := slices.Concat(textSample("A"), textSample("Bb"), make([]byte, 9), textSample("Ccc"), textSample("Dddd"))
	data, moovOff := buildQTChapterFile(1000, payload,
		sttsAtom([2]uint32{4, 1000}),
		stszAtom(0, 4, 3, 4, 5, 6),
		stscAtom([3]uint32{1, 2, 1}),
		stcoAtom(8, 24),
	)
	got := parseQTChapters(t, data, moovOff, 10*time.Second)
	checkChapters(t, got, []string{"A", "Bb", "Ccc", "Dddd"},
		[]time.Duration{0, time.Second, 2 * time.Second, 3 * time.Second})
}

func TestComputeSampleOffsets(t *testing.T) {
	tests := []struct {
		name   string
		sizes  []uint32
		chunks []uint64
		stsc   []stscEntry
		want   []uint64
	}{
		{"no stsc single chunk", []uint32{3, 4, 5}, []uint64{100}, nil, []uint64{100, 103, 107}},
		{"no stsc one per chunk", []uint32{3, 4, 5}, []uint64{100, 200, 300}, nil, []uint64{100, 200, 300}},
		{"two per chunk", []uint32{3, 4, 5, 6}, []uint64{100, 200}, []stscEntry{{1, 2}}, []uint64{100, 103, 200, 205}},
		{"stsc runs", []uint32{1, 1, 1, 1}, []uint64{100, 200, 300}, []stscEntry{{1, 2}, {2, 1}}, []uint64{100, 101, 200, 300}},
		{"truncated to sample count", []uint32{2, 2}, []uint64{100}, []stscEntry{{1, 5}}, []uint64{100, 102}},
		{"tables describe fewer samples", []uint32{2, 2, 2}, []uint64{100}, []stscEntry{{1, 1}}, []uint64{100}},
		{"huge samples per chunk", []uint32{1}, []uint64{0}, []stscEntry{{1, 4294967295}}, []uint64{0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := computeSampleOffsets(tt.sizes, tt.chunks, tt.stsc); !slices.Equal(got, tt.want) {
				t.Errorf("computeSampleOffsets() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseChapters_ChplVersion0(t *testing.T) {
	chpl := createChplAtomVersion(0, []struct {
		time  int64
		title string
	}{{0, "One"}, {600_000_000, "Two"}})
	moov := createMockAtom("moov", createMockAtom("udta", chpl))
	got := parseQTChapters(t, moov, 0, 5*time.Minute)
	checkChapters(t, got, []string{"One", "Two"}, []time.Duration{0, 60 * time.Second})
}

func TestQTChapters_HugeSttsCountIsBounded(t *testing.T) {
	data, moovOff := buildQTChapterFile(1000, textSample("Only"),
		sttsAtom([2]uint32{0xFFFFFFFF, 1000}),
		stszAtom(0, 1, 6),
		stcoAtom(8),
	)
	chapters := parseQTChapters(t, data, moovOff, time.Hour)
	if len(chapters) > 1 {
		t.Errorf("got %d chapters, want at most 1", len(chapters))
	}
}

func TestParseChapterTimings_CapsAccumulation(t *testing.T) {
	stts := sttsAtom([2]uint32{0xFFFFFFFF, 1})
	stbl := createMockAtom("stbl", stts)
	sr := audiobinary.NewSafeReader(bytes.NewReader(stbl), int64(len(stbl)), "t.m4b")
	atom, err := readAtomHeader(sr, 0, int64(len(stbl)))
	if err != nil {
		t.Fatal(err)
	}
	times, err := parseChapterTimings(context.Background(), sr, atom, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(times) != maxChapterSamples {
		t.Errorf("len = %d, want %d", len(times), maxChapterSamples)
	}
}

func TestParseChunkOffsets_CountPastFile(t *testing.T) {
	for _, typ := range []string{"stco", "co64"} {
		stbl := createMockAtom("stbl", fullAtom(typ, u32be(0xFFFFFFFF)))
		sr := audiobinary.NewSafeReader(bytes.NewReader(stbl), int64(len(stbl)), "t.m4b")
		atom, err := readAtomHeader(sr, 0, int64(len(stbl)))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := parseChunkOffsets(context.Background(), sr, atom); err == nil {
			t.Errorf("%s: expected error for count past end of file", typ)
		}
	}
}

func TestParseChapters_ChplHostileStartTime(t *testing.T) {
	chpl := createChplAtom([]struct {
		time  int64
		title string
	}{{0, "One"}, {-1, "Overflows"}}) // 0xFFFF... as uint64 * 100 would wrap
	moov := createMockAtom("moov", createMockAtom("udta", chpl))
	got := parseQTChapters(t, moov, 0, 5*time.Minute)
	for _, ch := range got {
		if ch.StartTime < 0 {
			t.Errorf("chapter %q has negative start %v", ch.Title, ch.StartTime)
		}
	}
	if len(got) != 1 || got[0].Title != "One" {
		t.Errorf("got %d chapters, want only the valid first one", len(got))
	}
}

func TestParseChapterTimings_HugeAccumulationDoesNotWrap(t *testing.T) {
	stts := sttsAtom([2]uint32{3, 0xFFFFFFFF})
	stbl := createMockAtom("stbl", stts)
	sr := audiobinary.NewSafeReader(bytes.NewReader(stbl), int64(len(stbl)), "t.m4b")
	atom, err := readAtomHeader(sr, 0, int64(len(stbl)))
	if err != nil {
		t.Fatal(err)
	}
	times, err := parseChapterTimings(context.Background(), sr, atom, 1)
	if err != nil {
		t.Fatal(err)
	}
	for i, d := range times {
		if d < 0 {
			t.Errorf("times[%d] = %v, want non-negative", i, d)
		}
	}
}
