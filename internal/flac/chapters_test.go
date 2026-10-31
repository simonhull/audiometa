package flac

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"
	"time"

	"github.com/simonhull/audiometa/internal/types"
)

type testIndex struct {
	offset uint64
	number byte
}

type testTrack struct {
	offset  uint64
	number  byte
	indices []testIndex
}

func cueSheetPayload(isCD bool, tracks []testTrack) []byte {
	b := &bytes.Buffer{}
	b.Write(make([]byte, 128))                       // media catalog number
	_ = binary.Write(b, binary.BigEndian, uint64(0)) // lead-in
	var flags byte
	if isCD {
		flags = 0x80
	}
	b.WriteByte(flags)
	b.Write(make([]byte, 258)) // reserved
	b.WriteByte(byte(len(tracks)))
	for _, t := range tracks {
		_ = binary.Write(b, binary.BigEndian, t.offset)
		b.WriteByte(t.number)
		b.Write(make([]byte, 12)) // ISRC
		b.WriteByte(0x00)         // audio, no pre-emphasis
		b.Write(make([]byte, 13)) // reserved
		b.WriteByte(byte(len(t.indices)))
		for _, ix := range t.indices {
			_ = binary.Write(b, binary.BigEndian, ix.offset)
			b.WriteByte(ix.number)
			b.Write(make([]byte, 3))
		}
	}
	return b.Bytes()
}

const (
	testRate    = 44100
	testSamples = testRate * 600 // 600 s
)

func parseTestFLAC(t *testing.T, data []byte) *types.File {
	t.Helper()
	file, err := (&parser{}).Parse(context.Background(), bytes.NewReader(data), int64(len(data)), "test.flac")
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func sec(n int) time.Duration { return time.Duration(n) * time.Second }

func checkChapters(t *testing.T, got, want []types.Chapter) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d chapters %+v, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("chapter %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

var chapterComments = []string{
	"CHAPTER001=00:00:00.000", "CHAPTER001NAME=Opening",
	"CHAPTER002=00:05:00.000", "CHAPTER002NAME=Middle",
}

func cdCue() []byte {
	return cueSheetPayload(true, []testTrack{
		{0, 1, []testIndex{{0, 1}}},
		{2646000, 2, []testIndex{{0, 0}, {88200, 1}}}, // 2 s pregap
		{26460000, 170, nil},
	})
}

func TestParse_CueSheetCD(t *testing.T) {
	data := buildFLAC(
		flacBlock(false, 0, streamInfoPayload(testRate, testSamples)),
		flacBlock(true, 5, cdCue()),
	)
	file := parseTestFLAC(t, data)
	checkChapters(t, file.Chapters, []types.Chapter{
		{Index: 1, Title: "Track 01", StartTime: 0, EndTime: sec(62), Source: types.ChapterSourceCueSheet},
		{Index: 2, Title: "Track 02", StartTime: sec(62), EndTime: sec(600), Source: types.ChapterSourceCueSheet},
	})
	if len(file.Warnings) != 0 {
		t.Errorf("warnings: %+v", file.Warnings)
	}
}

func TestParse_CueSheetNonCDLeadOut(t *testing.T) {
	cue := cueSheetPayload(false, []testTrack{
		{0, 1, []testIndex{{0, 1}}},
		{4410000, 2, []testIndex{{0, 1}}},
		{26460000, 255, nil},
	})
	data := buildFLAC(
		flacBlock(false, 0, streamInfoPayload(testRate, testSamples)),
		flacBlock(true, 5, cue),
	)
	file := parseTestFLAC(t, data)
	checkChapters(t, file.Chapters, []types.Chapter{
		{Index: 1, Title: "Track 01", StartTime: 0, EndTime: sec(100), Source: types.ChapterSourceCueSheet},
		{Index: 2, Title: "Track 02", StartTime: sec(100), EndTime: sec(600), Source: types.ChapterSourceCueSheet},
	})
}

func TestCuesheetToChapters_NoLeadOut(t *testing.T) {
	cs := &CueSheet{Tracks: []CueTrack{
		{Offset: 0, Number: 1, IsAudio: true},
		{Offset: 441000, Number: 2, IsAudio: true},
	}}
	got := cuesheetToChapters(cs, testRate, 600*time.Second)
	if got[1].EndTime != sec(600) {
		t.Errorf("last end = %v, want 600s", got[1].EndTime)
	}
	if got[0].EndTime != sec(10) {
		t.Errorf("first end = %v, want 10s", got[0].EndTime)
	}
	got = cuesheetToChapters(cs, testRate, 0)
	if got[1].EndTime != 0 {
		t.Errorf("last end = %v, want 0", got[1].EndTime)
	}
}

func TestParse_FLACChapterComments(t *testing.T) {
	data := buildFLAC(
		flacBlock(false, 0, streamInfoPayload(testRate, testSamples)),
		flacBlock(true, 4, vorbisCommentPayload(chapterComments...)),
	)
	file := parseTestFLAC(t, data)
	checkChapters(t, file.Chapters, []types.Chapter{
		{Index: 1, Title: "Opening", StartTime: 0, EndTime: 5 * time.Minute, Source: types.ChapterSourceVorbisComment},
		{Index: 2, Title: "Middle", StartTime: 5 * time.Minute, EndTime: sec(600), Source: types.ChapterSourceVorbisComment},
	})
}

func TestParse_ChapterCommentsWinOverCueSheet(t *testing.T) {
	data := buildFLAC(
		flacBlock(false, 0, streamInfoPayload(testRate, testSamples)),
		flacBlock(false, 4, vorbisCommentPayload(chapterComments...)),
		flacBlock(true, 5, cdCue()),
	)
	file := parseTestFLAC(t, data)
	if len(file.Chapters) != 2 || file.Chapters[0].Title != "Opening" || file.Chapters[1].Title != "Middle" {
		t.Errorf("chapters = %+v", file.Chapters)
	}
}
