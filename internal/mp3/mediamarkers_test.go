package mp3

import (
	"strings"
	"testing"
	"time"

	"github.com/simonhull/audiometa/internal/types"
)

func TestParseMarkerTime(t *testing.T) {
	ok := []struct {
		in   string
		want time.Duration
	}{
		{"0:00.000", 0},
		{"15:51.000", 15*time.Minute + 51*time.Second},
		{"75:30.250", 75*time.Minute + 30*time.Second + 250*time.Millisecond},
		{"1:02:03.456", time.Hour + 2*time.Minute + 3*time.Second + 456*time.Millisecond},
		{"1:02:03", time.Hour + 2*time.Minute + 3*time.Second},
		{"0:05.5", 5500 * time.Millisecond},
		{"0:05.05", 5050 * time.Millisecond},
	}
	for _, tt := range ok {
		got, err := parseMarkerTime(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("parseMarkerTime(%q) = %v, %v; want %v", tt.in, got, err, tt.want)
		}
	}
	for _, in := range []string{"", "5.000", "1:2:3:4", "0:60.000", "1:60:00.000", "0:5.000", "-1:00.000", "0:05.0001", "a:bc", "0:05.", "0: 5.000"} {
		if got, err := parseMarkerTime(in); err == nil {
			t.Errorf("parseMarkerTime(%q) = %v, want error", in, got)
		}
	}
}

const twoMarkers = `<Markers>
  <Marker><Name>Chapter 1</Name><Time>0:00.000</Time></Marker>
  <Marker><Name> Chapter 2 </Name><Time>15:51.000</Time></Marker>
</Markers>`

func TestParseMediaMarkers(t *testing.T) {
	chapters, err := parseMediaMarkers(twoMarkers)
	if err != nil {
		t.Fatal(err)
	}
	if len(chapters) != 2 {
		t.Fatalf("got %d chapters", len(chapters))
	}
	want := []types.Chapter{
		{Title: "Chapter 1", Index: 1, StartTime: 0, EndTime: 15*time.Minute + 51*time.Second, Source: types.ChapterSourceOverDrive},
		{Title: "Chapter 2", Index: 2, StartTime: 15*time.Minute + 51*time.Second, EndTime: 0, Source: types.ChapterSourceOverDrive},
	}
	for i := range want {
		if chapters[i] != want[i] {
			t.Errorf("chapter %d = %+v, want %+v", i, chapters[i], want[i])
		}
	}
}

func TestParseMediaMarkersEdgeCases(t *testing.T) {
	if ch, err := parseMediaMarkers("<Markers/>"); ch != nil || err != nil {
		t.Errorf("empty = %v, %v", ch, err)
	}
	if ch, err := parseMediaMarkers(twoMarkers + "\x00\x00"); err != nil || len(ch) != 2 {
		t.Errorf("trailing NUL = %v, %v", ch, err)
	}
	if ch, err := parseMediaMarkers("<Markers><Marker><Name></Name><Time>0:01.000</Time></Marker></Markers>"); err != nil || ch[0].Title != "Chapter 1" {
		t.Errorf("empty name = %v, %v", ch, err)
	}

	many := "<Markers>" + strings.Repeat("<Marker><Name>x</Name><Time>0:00.000</Time></Marker>", maxMediaMarkers+1) + "</Markers>"
	for name, doc := range map[string]string{
		"malformed XML": "<Markers><Marker>",
		"wrong root":    "<Other/>",
		"non-monotonic": `<Markers><Marker><Name>a</Name><Time>1:00.000</Time></Marker><Marker><Name>b</Name><Time>0:30.000</Time></Marker></Markers>`,
		"bad time":      `<Markers><Marker><Name>a</Name><Time>0:00.000</Time></Marker><Marker><Name>b</Name><Time>soon</Time></Marker></Markers>`,
		"too many":      many,
	} {
		if ch, err := parseMediaMarkers(doc); err == nil || ch != nil {
			t.Errorf("%s: got %v, %v; want nil chapters and an error", name, ch, err)
		}
	}
}

func TestParseTXXXMediaMarkers(t *testing.T) {
	latin := func(desc string) []byte {
		return append(append([]byte{0}, desc+"\x00"...), twoMarkers...)
	}
	utf16 := func(desc string) []byte {
		b := []byte{1, 0xFF, 0xFE}
		for _, r := range desc {
			b = append(b, byte(r), 0)
		}
		b = append(b, 0, 0, 0xFF, 0xFE)
		for _, r := range twoMarkers {
			b = append(b, byte(r), 0)
		}
		return b
	}
	tests := map[string][]byte{
		"latin1":     latin("OverDrive MediaMarkers"),
		"lower case": latin("overdrive mediamarkers"),
		"utf16":      utf16("OverDrive MediaMarkers"),
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			var f types.File
			parseTXXXFrame(ID3v2Frame{ID: "TXXX", Data: data}, &f)
			if len(f.Chapters) != 2 || f.Chapters[1].Title != "Chapter 2" {
				t.Errorf("chapters = %+v (warnings %v)", f.Chapters, f.Warnings)
			}
		})
	}

	var f types.File
	bad := append(append([]byte{0}, "OverDrive MediaMarkers\x00"...), "<Markers>"...)
	parseTXXXFrame(ID3v2Frame{ID: "TXXX", Data: bad}, &f)
	if len(f.Chapters) != 0 || len(f.Warnings) != 1 || f.Warnings[0].Stage != types.StageChapters {
		t.Errorf("chapters %v warnings %v", f.Chapters, f.Warnings)
	}
}

func mediaMarkersFrame(version byte, doc string) []byte {
	return id3Frame(version, "TXXX", append(append([]byte{0}, "OverDrive MediaMarkers\x00"...), doc...))
}

func TestParse_OverDriveMediaMarkers(t *testing.T) {
	// 1 s of CBR audio at 128 kbps = 16000 bytes.
	// Second chapter starts inside the file, at 0.5 s.
	doc := strings.Replace(twoMarkers, "15:51.000", "0:00.500", 1)
	tag := id3Tag(3, mediaMarkersFrame(3, doc))
	f := parseBytes(t, append(tag, audioBytes(16000)...))
	if len(f.Chapters) != 2 {
		t.Fatalf("chapters = %+v, warnings %v", f.Chapters, f.Warnings)
	}
	if f.Chapters[0].Source != types.ChapterSourceOverDrive {
		t.Errorf("source = %q", f.Chapters[0].Source)
	}
	if f.Audio.Duration <= f.Chapters[1].StartTime {
		t.Fatalf("duration %v not beyond last start %v", f.Audio.Duration, f.Chapters[1].StartTime)
	}
	if f.Chapters[1].EndTime != f.Audio.Duration {
		t.Errorf("last end = %v, want duration %v", f.Chapters[1].EndTime, f.Audio.Duration)
	}
}

func TestParse_OverDriveLastChapterBeyondDuration(t *testing.T) {
	tag := id3Tag(3, mediaMarkersFrame(3, twoMarkers)) // second chapter starts at 15:51, far past the audio
	f := parseBytes(t, append(tag, audioBytes(16000)...))
	if len(f.Chapters) != 2 || f.Chapters[1].EndTime != 0 {
		t.Errorf("chapters = %+v", f.Chapters)
	}
}

func TestParse_CHAPBeatsMediaMarkers(t *testing.T) {
	chap := id3Frame(3, "CHAP", chapFrameBody("ch1", 0, 1000))
	for _, frames := range [][][]byte{
		{mediaMarkersFrame(3, twoMarkers), chap},
		{chap, mediaMarkersFrame(3, twoMarkers)},
	} {
		f := parseBytes(t, append(id3Tag(3, frames...), mpegFrame...))
		if len(f.Chapters) != 1 || f.Chapters[0].Source != types.ChapterSourceID3CHAP {
			t.Errorf("chapters = %+v", f.Chapters)
		}
	}
}
