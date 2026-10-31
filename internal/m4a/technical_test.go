package m4a

import (
	"bytes"
	"encoding/binary"
	"math"
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
	moovAtom, _ := readAtomHeader(sr, 0, int64(len(moov)))

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
	moovAtom, _ := readAtomHeader(sr, 0, int64(len(moov)))

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
	moovAtom, _ := readAtomHeader(sr, 0, int64(len(moov)))

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

func TestMvhdDuration(t *testing.T) {
	tests := []struct {
		name      string
		duration  uint64
		timescale uint32
		want      time.Duration
	}{
		{"70 hour book at 44100", 44100 * 3600 * 70, 44100, 70 * time.Hour},
		{"one minute", 2646000, 44100, 60 * time.Second},
		{"one second", 1000, 1000, time.Second},
		{"rounds down", 1, 3, 333333333 * time.Nanosecond},
		{"zero timescale", 12345, 0, 0},
		{"huge duration clamps", math.MaxUint64, 1, time.Duration(math.MaxInt64)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mvhdDuration(tt.duration, tt.timescale); got != tt.want {
				t.Errorf("mvhdDuration(%d, %d) = %v, want %v", tt.duration, tt.timescale, got, tt.want)
			}
		})
	}
}

func sampleEntryV01(version uint16, channels uint16, rate uint32) []byte {
	b := binary.BigEndian.AppendUint16(nil, version) // version
	b = append(b, 0, 0)                              // revision
	b = append(b, 0, 0, 0, 0)                        // vendor
	b = binary.BigEndian.AppendUint16(b, channels)
	b = append(b, 0, 16, 0, 0, 0, 0)               // sample size, compression id, packet size
	b = binary.BigEndian.AppendUint32(b, rate<<16) // 16.16 rate
	return append(b, make([]byte, 16)...)          // v1 extension fields (ignored)
}

func sampleEntryV2(rate float64, channels uint32) []byte {
	b := binary.BigEndian.AppendUint16(nil, 2) // version
	b = append(b, 0, 0, 0, 0, 0, 0)            // revision + vendor
	b = binary.BigEndian.AppendUint16(b, 3)    // legacy channels constant
	b = append(b, 0, 16, 0xFF, 0xFE, 0, 0)     // legacy sample size, compression id, packet size
	b = binary.BigEndian.AppendUint32(b, 0x00010000)
	b = binary.BigEndian.AppendUint32(b, 72) // sizeOfStructOnly
	b = binary.BigEndian.AppendUint64(b, math.Float64bits(rate))
	b = binary.BigEndian.AppendUint32(b, channels)
	return append(b, make([]byte, 20)...)
}

func stsdFor(format string, fromVersion []byte) []byte {
	entry := make([]byte, 4, 64) // size, patched below
	entry = append(entry, []byte(format)...)
	entry = append(entry, make([]byte, 8)...)
	entry = append(entry, fromVersion...)
	binary.BigEndian.PutUint32(entry[0:4], uint32(len(entry)))
	return fullAtom("stsd", append(u32be(1), entry...))
}

func trakWith(handler string, stsd []byte) []byte {
	var mdiaChildren []byte
	if handler != "" {
		hdlr := fullAtom("hdlr", append(append(u32be(0), handler...), make([]byte, 12)...))
		mdiaChildren = append(mdiaChildren, hdlr...)
	}
	stbl := createMockAtom("stbl", stsd)
	minf := createMockAtom("minf", stbl)
	mdiaChildren = append(mdiaChildren, minf...)
	return createMockAtom("trak", createMockAtom("mdia", mdiaChildren))
}

func parseMoovTech(t *testing.T, children ...[]byte) *types.File {
	t.Helper()
	moovBody := createMvhdAtom(0, 1000, 10000)
	for _, c := range children {
		moovBody = append(moovBody, c...)
	}
	moov := createMockAtom("moov", moovBody)
	sr := audiobinary.NewSafeReader(bytes.NewReader(moov), int64(len(moov)), "test.m4b")
	moovAtom, err := readAtomHeader(sr, 0, int64(len(moov)))
	if err != nil {
		t.Fatal(err)
	}
	file := &types.File{}
	if err := parseTechnicalInfo(sr, moovAtom, file); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestParseTechnicalInfo_SelectsSoundTrack(t *testing.T) {
	text := trakWith("text", stsdFor("text", sampleEntryV01(0, 1, 1000)))
	audio := trakWith("soun", stsdFor("mp4a", sampleEntryV01(0, 2, 44100)))
	file := parseMoovTech(t, text, audio)
	if file.Audio.Channels != 2 || file.Audio.SampleRate != 44100 || file.Audio.Codec != "mp4a" {
		t.Errorf("channels %d rate %d codec %q", file.Audio.Channels, file.Audio.SampleRate, file.Audio.Codec)
	}
}

func TestParseTechnicalInfo_FallsBackToFirstTrakWithoutHdlr(t *testing.T) {
	only := trakWith("", stsdFor("mp4a", sampleEntryV01(0, 1, 22050)))
	file := parseMoovTech(t, only)
	if file.Audio.Channels != 1 || file.Audio.SampleRate != 22050 {
		t.Errorf("channels %d rate %d", file.Audio.Channels, file.Audio.SampleRate)
	}
}

func TestParseTechnicalInfo_SoundEntryVersions(t *testing.T) {
	tests := []struct {
		name         string
		entry        []byte
		wantRate     int
		wantChannels int
	}{
		{"v0", sampleEntryV01(0, 2, 48000), 48000, 2},
		{"v1 matches v0", sampleEntryV01(1, 2, 48000), 48000, 2},
		{"v2", sampleEntryV2(48000.0, 2), 48000, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := parseMoovTech(t, trakWith("soun", stsdFor("mp4a", tt.entry)))
			if file.Audio.SampleRate != tt.wantRate || file.Audio.Channels != tt.wantChannels {
				t.Errorf("rate %d channels %d, want %d/%d", file.Audio.SampleRate, file.Audio.Channels, tt.wantRate, tt.wantChannels)
			}
		})
	}
}

func TestParseTechnicalInfo_UnknownSoundEntryVersionLeavesUnset(t *testing.T) {
	file := parseMoovTech(t, trakWith("soun", stsdFor("mp4a", sampleEntryV01(7, 2, 48000))))
	if file.Audio.SampleRate != 0 || file.Audio.Channels != 0 {
		t.Errorf("rate %d channels %d, want unset", file.Audio.SampleRate, file.Audio.Channels)
	}
}
