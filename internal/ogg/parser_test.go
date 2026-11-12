package ogg

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/simonhull/audiometa/internal/types"
)

// createMinimalOgg creates a minimal Ogg Vorbis file with identification and comment headers.
func createMinimalOgg(title, artist, album string, extra ...string) []byte {
	buf := &bytes.Buffer{}

	// Helper to create Ogg page
	const serial uint32 = 12345
	createPage := func(headerType byte, granule int64, sequence uint32, data []byte) {
		// OggS magic
		buf.WriteString("OggS")

		// Version
		buf.WriteByte(0x00)

		// Header type flags
		buf.WriteByte(headerType)

		// Granule position (64-bit LE)
		binary.Write(buf, binary.LittleEndian, uint64(granule))

		// Serial number (32-bit LE)
		binary.Write(buf, binary.LittleEndian, serial)

		// Sequence number (32-bit LE)
		binary.Write(buf, binary.LittleEndian, sequence)

		// Checksum (32-bit LE) - use 0 for simplicity
		binary.Write(buf, binary.LittleEndian, uint32(0))

		// Segment table
		segments := []byte{}
		remaining := len(data)
		for remaining > 0 {
			if remaining >= 255 {
				segments = append(segments, 255)
				remaining -= 255
			} else {
				segments = append(segments, byte(remaining))
				remaining = 0
			}
		}

		// Segment count
		buf.WriteByte(byte(len(segments)))

		// Segment table
		buf.Write(segments)

		// Data
		buf.Write(data)
	}

	// Page 0: Identification header (BOS = Beginning of Stream)
	idHeader := &bytes.Buffer{}
	idHeader.WriteByte(0x01)                                    // Packet type: identification
	idHeader.WriteString("vorbis")                              // Magic
	binary.Write(idHeader, binary.LittleEndian, uint32(0))      // Vorbis version
	idHeader.WriteByte(2)                                       // Channels (stereo)
	binary.Write(idHeader, binary.LittleEndian, uint32(44100))  // Sample rate
	binary.Write(idHeader, binary.LittleEndian, uint32(0))      // Bitrate maximum
	binary.Write(idHeader, binary.LittleEndian, uint32(128000)) // Bitrate nominal
	binary.Write(idHeader, binary.LittleEndian, uint32(0))      // Bitrate minimum
	idHeader.WriteByte(0xB8)                                    // Blocksize info
	idHeader.WriteByte(0x01)                                    // Framing flag

	createPage(0x02, 0, 0, idHeader.Bytes()) // BOS flag

	// Page 1: Comment header
	commentHeader := &bytes.Buffer{}
	commentHeader.WriteByte(0x03)       // Packet type: comment
	commentHeader.WriteString("vorbis") // Magic

	// Vendor string
	vendor := "audiometa"
	binary.Write(commentHeader, binary.LittleEndian, uint32(len(vendor)))
	commentHeader.WriteString(vendor)

	// Build comments
	var comments []string
	if title != "" {
		comments = append(comments, "TITLE="+title)
	}
	if artist != "" {
		comments = append(comments, "ARTIST="+artist)
	}
	if album != "" {
		comments = append(comments, "ALBUM="+album)
	}
	comments = append(comments, extra...)

	// Comment count
	binary.Write(commentHeader, binary.LittleEndian, uint32(len(comments)))

	// Write comments
	for _, comment := range comments {
		binary.Write(commentHeader, binary.LittleEndian, uint32(len(comment)))
		commentHeader.WriteString(comment)
	}

	// Framing bit
	commentHeader.WriteByte(0x01)

	createPage(0x00, 0, 1, commentHeader.Bytes())

	// Page 2: Setup header (empty for test purposes)
	setupHeader := &bytes.Buffer{}
	setupHeader.WriteByte(0x05)       // Packet type: setup
	setupHeader.WriteString("vorbis") // Magic
	setupHeader.WriteByte(0x01)       // Framing

	createPage(0x00, 0, 2, setupHeader.Bytes())

	// Page 3: Last page with granule position for duration (1 second at 44.1kHz)
	lastPage := &bytes.Buffer{}
	lastPage.Write(make([]byte, 100)) // Dummy audio data

	createPage(0x04, 44100, 3, lastPage.Bytes()) // EOS flag

	return buf.Bytes()
}

// createMinimalOpus creates a minimal Ogg Opus file with OpusHead and OpusTags headers.
func createMinimalOpus(title, artist, album string, extra ...string) []byte {
	buf := &bytes.Buffer{}

	// Helper to create Ogg page
	createPage := func(headerType byte, granule int64, serial uint32, sequence uint32, data []byte) {
		// OggS magic
		buf.WriteString("OggS")

		// Version
		buf.WriteByte(0x00)

		// Header type flags
		buf.WriteByte(headerType)

		// Granule position (64-bit LE)
		binary.Write(buf, binary.LittleEndian, uint64(granule))

		// Serial number (32-bit LE)
		binary.Write(buf, binary.LittleEndian, serial)

		// Sequence number (32-bit LE)
		binary.Write(buf, binary.LittleEndian, sequence)

		// Checksum (32-bit LE) - use 0 for simplicity
		binary.Write(buf, binary.LittleEndian, uint32(0))

		// Segment table
		segments := []byte{}
		remaining := len(data)
		for remaining > 0 {
			if remaining >= 255 {
				segments = append(segments, 255)
				remaining -= 255
			} else {
				segments = append(segments, byte(remaining))
				remaining = 0
			}
		}

		// Segment count
		buf.WriteByte(byte(len(segments)))

		// Segment table
		buf.Write(segments)

		// Data
		buf.Write(data)
	}

	// Page 0: OpusHead identification header (BOS = Beginning of Stream)
	idHeader := &bytes.Buffer{}
	idHeader.WriteString("OpusHead")                           // Magic (8 bytes)
	idHeader.WriteByte(1)                                      // Version
	idHeader.WriteByte(2)                                      // Channels (stereo)
	binary.Write(idHeader, binary.LittleEndian, uint16(312))   // Pre-skip
	binary.Write(idHeader, binary.LittleEndian, uint32(48000)) // Input sample rate
	binary.Write(idHeader, binary.LittleEndian, int16(0))      // Output gain
	idHeader.WriteByte(0)                                      // Channel mapping family

	createPage(0x02, 0, 54321, 0, idHeader.Bytes()) // BOS flag

	// Page 1: OpusTags comment header
	commentHeader := &bytes.Buffer{}
	commentHeader.WriteString("OpusTags") // Magic (8 bytes)

	// Vendor string
	vendor := "audiometa"
	binary.Write(commentHeader, binary.LittleEndian, uint32(len(vendor)))
	commentHeader.WriteString(vendor)

	// Build comments
	var comments []string
	if title != "" {
		comments = append(comments, "TITLE="+title)
	}
	if artist != "" {
		comments = append(comments, "ARTIST="+artist)
	}
	if album != "" {
		comments = append(comments, "ALBUM="+album)
	}
	comments = append(comments, extra...)

	// Comment count
	binary.Write(commentHeader, binary.LittleEndian, uint32(len(comments)))

	// Write comments
	for _, comment := range comments {
		binary.Write(commentHeader, binary.LittleEndian, uint32(len(comment)))
		commentHeader.WriteString(comment)
	}

	createPage(0x00, 0, 54321, 1, commentHeader.Bytes())

	// Page 2: Last page with granule position for duration (1 second at 48kHz)
	lastPage := &bytes.Buffer{}
	lastPage.Write(make([]byte, 100)) // Dummy audio data

	createPage(0x04, 48000, 54321, 2, lastPage.Bytes()) // EOS flag

	return buf.Bytes()
}

func TestParse_Success(t *testing.T) {
	t.Parallel()
	data := createMinimalOgg("Test Song", "Test Artist", "Test Album")

	// Write to temp file
	tmpFile, err := os.CreateTemp(t.TempDir(), "test*.ogg")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	// Open file for parsing
	f, err := os.Open(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}

	// Parse the file
	p := &parser{}
	file, err := p.Parse(context.Background(), f, stat.Size(), tmpFile.Name())
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if file == nil {
		t.Fatal("expected file, got nil")
	}

	checks := []struct {
		name      string
		got, want any
	}{
		{"file.Format", file.Format, types.FormatOgg},
		{"file.Tags.Title", file.Tags.Title, "Test Song"},
		{"file.Tags.Artist", file.Tags.Artist, "Test Artist"},
		{"file.Tags.Album", file.Tags.Album, "Test Album"},
		{"file.Audio.Codec", file.Audio.Codec, "Vorbis"},
		{"file.Audio.Container", file.Audio.Container, "Ogg"},
		{"file.Audio.Lossless", file.Audio.Lossless, false},
		{"file.Audio.VBR", file.Audio.VBR, true},
		{"file.Audio.SampleRate", file.Audio.SampleRate, 44100},
		{"file.Audio.Channels", file.Audio.Channels, 2},
		{"file.Audio.Bitrate", file.Audio.Bitrate, 128000},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}

	// Duration should be ~1 second (44100 samples at 44100 Hz)
	expectedDuration := int64(1000000000) // 1 second in nanoseconds
	if file.Audio.Duration.Nanoseconds() < expectedDuration*9/10 ||
		file.Audio.Duration.Nanoseconds() > expectedDuration*11/10 {
		t.Errorf("expected duration ~1s, got %v", file.Audio.Duration)
	}
}

func TestParse_InvalidMagic(t *testing.T) {
	t.Parallel()
	// Create invalid data with wrong magic bytes
	data := []byte("INVALID")

	// Write to temp file
	tmpFile, err := os.CreateTemp(t.TempDir(), "test*.ogg")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	// Open file for parsing
	f, err := os.Open(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}

	// Parse should fail
	p := &parser{}
	_, err = p.Parse(context.Background(), f, stat.Size(), tmpFile.Name())
	if err == nil {
		t.Fatal("expected error for invalid magic, got nil")
	}

	// Should be a CorruptedFileError (invalid Ogg page)
	if _, ok := errors.AsType[*types.CorruptedFileError](err); !ok {
		t.Errorf("expected CorruptedFileError, got %T: %v", err, err)
	}
}

func TestParse_EmptyTags(t *testing.T) {
	t.Parallel()
	// Create Ogg with no tags
	data := createMinimalOgg("", "", "")

	// Write to temp file
	tmpFile, err := os.CreateTemp(t.TempDir(), "test*.ogg")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	// Open file for parsing
	f, err := os.Open(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}

	// Parse should succeed
	p := &parser{}
	file, err := p.Parse(context.Background(), f, stat.Size(), tmpFile.Name())
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Tags should be empty
	if file.Tags.Title != "" {
		t.Errorf("expected empty title, got '%s'", file.Tags.Title)
	}

	if file.Tags.Artist != "" {
		t.Errorf("expected empty artist, got '%s'", file.Tags.Artist)
	}

	if file.Tags.Album != "" {
		t.Errorf("expected empty album, got '%s'", file.Tags.Album)
	}
}

func TestParseOpus_Success(t *testing.T) {
	t.Parallel()
	data := createMinimalOpus("Opus Song", "Opus Artist", "Opus Album")

	// Write to temp file
	tmpFile, err := os.CreateTemp(t.TempDir(), "test*.opus")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	// Open file for parsing
	f, err := os.Open(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}

	// Parse the file
	p := &parser{}
	file, err := p.Parse(context.Background(), f, stat.Size(), tmpFile.Name())
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if file == nil {
		t.Fatal("expected file, got nil")
	}

	checks := []struct {
		name      string
		got, want any
	}{
		{"file.Format", file.Format, types.FormatOpus},
		{"file.Tags.Title", file.Tags.Title, "Opus Song"},
		{"file.Tags.Artist", file.Tags.Artist, "Opus Artist"},
		{"file.Tags.Album", file.Tags.Album, "Opus Album"},
		{"file.Audio.Codec", file.Audio.Codec, "Opus"},
		{"file.Audio.Container", file.Audio.Container, "Ogg"},
		{"file.Audio.Lossless", file.Audio.Lossless, false},
		{"file.Audio.VBR", file.Audio.VBR, true},
		{"file.Audio.SampleRate", file.Audio.SampleRate, 48000},
		{"file.Audio.Channels", file.Audio.Channels, 2},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}

	// Duration should be ~1 second (48000 samples at 48kHz)
	expectedDuration := int64(1000000000) // 1 second in nanoseconds
	if file.Audio.Duration.Nanoseconds() < expectedDuration*9/10 ||
		file.Audio.Duration.Nanoseconds() > expectedDuration*11/10 {
		t.Errorf("expected duration ~1s, got %v", file.Audio.Duration)
	}
}

func TestParseOpus_BitrateEstimation(t *testing.T) {
	t.Parallel()
	data := createMinimalOpus("Test", "Test", "Test")

	// Write to temp file
	tmpFile, err := os.CreateTemp(t.TempDir(), "test*.opus")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	// Open file for parsing
	f, err := os.Open(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}

	// Parse the file
	p := &parser{}
	file, err := p.Parse(context.Background(), f, stat.Size(), tmpFile.Name())
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// createMinimalOpus ends with one EOS page of 100 audio bytes plus its
	// 28-byte header, at granule 48000: 128 bytes in one second.
	if file.Audio.Bitrate != 1024 {
		t.Errorf("Bitrate = %d, want 1024", file.Audio.Bitrate)
	}
}

func TestOggParser_BothCodecs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name               string
		expectedCodec      string
		data               []byte
		expectedSampleRate int
	}{
		{
			name:               "Vorbis",
			data:               createMinimalOgg("Test", "Test", "Test"),
			expectedCodec:      "Vorbis",
			expectedSampleRate: 44100,
		},
		{
			name:               "Opus",
			data:               createMinimalOpus("Test", "Test", "Test"),
			expectedCodec:      "Opus",
			expectedSampleRate: 48000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			checkCodec(t, tt.data, tt.expectedCodec, tt.expectedSampleRate)
		})
	}
}

// checkCodec parses data from a file and checks the codec, the sample rate and what every Ogg
// codec shares: the Ogg container and lossy variable-bitrate audio.
func checkCodec(t *testing.T, data []byte, codec string, sampleRate int) {
	t.Helper()
	tmpFile, err := os.CreateTemp(t.TempDir(), "test*.ogg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tmpFile.Write(data); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	f, err := os.Open(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}

	file, err := (&parser{}).Parse(context.Background(), f, stat.Size(), tmpFile.Name())
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if file.Audio.Codec != codec {
		t.Errorf("expected codec '%s', got '%s'", codec, file.Audio.Codec)
	}
	if file.Audio.SampleRate != sampleRate {
		t.Errorf("expected sample rate %d, got %d", sampleRate, file.Audio.SampleRate)
	}
	if file.Audio.Container != "Ogg" {
		t.Errorf("expected container 'Ogg', got '%s'", file.Audio.Container)
	}
	if file.Audio.Lossless {
		t.Error("expected lossless to be false")
	}
	if !file.Audio.VBR {
		t.Error("expected VBR to be true")
	}
}

// Benchmarks

func BenchmarkParseOgg(b *testing.B) {
	data := createMinimalOgg("Benchmark Song", "Benchmark Artist", "Benchmark Album")

	tmpFile, err := os.CreateTemp(b.TempDir(), "bench*.ogg")
	if err != nil {
		b.Fatal(err)
	}
	defer tmpFile.Close()

	if _, err := tmpFile.Write(data); err != nil {
		b.Fatal(err)
	}

	path := tmpFile.Name()

	b.ReportAllocs()

	for b.Loop() {
		f, err := os.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		stat, err := f.Stat()
		if err != nil {
			f.Close()
			b.Fatal(err)
		}
		p := &parser{}
		_, err = p.Parse(context.Background(), f, stat.Size(), path)
		if err != nil {
			f.Close()
			b.Fatal(err)
		}
		f.Close()
	}
}

func BenchmarkParseVorbisIdentification(b *testing.B) {
	data := createMinimalOgg("Test", "Artist", "Album")

	tmpFile, err := os.CreateTemp(b.TempDir(), "bench*.ogg")
	if err != nil {
		b.Fatal(err)
	}
	defer tmpFile.Close()

	if _, err := tmpFile.Write(data); err != nil {
		b.Fatal(err)
	}

	path := tmpFile.Name()

	b.ReportAllocs()

	for b.Loop() {
		f, err := os.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		stat, err := f.Stat()
		if err != nil {
			f.Close()
			b.Fatal(err)
		}
		p := &parser{}
		file, err := p.Parse(context.Background(), f, stat.Size(), path)
		if err != nil {
			f.Close()
			b.Fatal(err)
		}
		// Access audio info to ensure identification header was parsed
		_ = file.Audio.SampleRate
		_ = file.Audio.Channels
		_ = file.Audio.Bitrate
		f.Close()
	}
}

func BenchmarkParseVorbisComment(b *testing.B) {
	data := createMinimalOgg("Long Title For Testing", "Artist Name Here", "Album Name Goes Here")

	tmpFile, err := os.CreateTemp(b.TempDir(), "bench*.ogg")
	if err != nil {
		b.Fatal(err)
	}
	defer tmpFile.Close()

	if _, err := tmpFile.Write(data); err != nil {
		b.Fatal(err)
	}

	path := tmpFile.Name()

	b.ReportAllocs()

	for b.Loop() {
		f, err := os.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		stat, err := f.Stat()
		if err != nil {
			f.Close()
			b.Fatal(err)
		}
		p := &parser{}
		file, err := p.Parse(context.Background(), f, stat.Size(), path)
		if err != nil {
			f.Close()
			b.Fatal(err)
		}
		// Access tags to ensure Vorbis comments were parsed
		_ = file.Tags.Title
		_ = file.Tags.Artist
		_ = file.Tags.Album
		f.Close()
	}
}

func BenchmarkParseOpus(b *testing.B) {
	data := createMinimalOpus("Benchmark Song", "Benchmark Artist", "Benchmark Album")

	tmpFile, err := os.CreateTemp(b.TempDir(), "bench*.opus")
	if err != nil {
		b.Fatal(err)
	}
	defer tmpFile.Close()

	if _, err := tmpFile.Write(data); err != nil {
		b.Fatal(err)
	}

	path := tmpFile.Name()

	b.ReportAllocs()

	for b.Loop() {
		f, err := os.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		stat, err := f.Stat()
		if err != nil {
			f.Close()
			b.Fatal(err)
		}
		p := &parser{}
		_, err = p.Parse(context.Background(), f, stat.Size(), path)
		if err != nil {
			f.Close()
			b.Fatal(err)
		}
		f.Close()
	}
}

func BenchmarkParseOpusHead(b *testing.B) {
	data := createMinimalOpus("Test", "Artist", "Album")

	tmpFile, err := os.CreateTemp(b.TempDir(), "bench*.opus")
	if err != nil {
		b.Fatal(err)
	}
	defer tmpFile.Close()

	if _, err := tmpFile.Write(data); err != nil {
		b.Fatal(err)
	}

	path := tmpFile.Name()

	b.ReportAllocs()

	for b.Loop() {
		f, err := os.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		stat, err := f.Stat()
		if err != nil {
			f.Close()
			b.Fatal(err)
		}
		p := &parser{}
		file, err := p.Parse(context.Background(), f, stat.Size(), path)
		if err != nil {
			f.Close()
			b.Fatal(err)
		}
		// Access audio info to ensure OpusHead was parsed
		_ = file.Audio.SampleRate
		_ = file.Audio.Channels
		_ = file.Audio.Bitrate
		f.Close()
	}
}

func BenchmarkParseOpusTags(b *testing.B) {
	data := createMinimalOpus("Long Title For Testing", "Artist Name Here", "Album Name Goes Here")

	tmpFile, err := os.CreateTemp(b.TempDir(), "bench*.opus")
	if err != nil {
		b.Fatal(err)
	}
	defer tmpFile.Close()

	if _, err := tmpFile.Write(data); err != nil {
		b.Fatal(err)
	}

	path := tmpFile.Name()

	b.ReportAllocs()

	for b.Loop() {
		f, err := os.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		stat, err := f.Stat()
		if err != nil {
			f.Close()
			b.Fatal(err)
		}
		p := &parser{}
		file, err := p.Parse(context.Background(), f, stat.Size(), path)
		if err != nil {
			f.Close()
			b.Fatal(err)
		}
		// Access tags to ensure OpusTags were parsed
		_ = file.Tags.Title
		_ = file.Tags.Artist
		_ = file.Tags.Album
		f.Close()
	}
}

func TestParse_ChapterEndTimeUsesDuration(t *testing.T) {
	t.Parallel()
	extra := []string{"CHAPTER001=00:00:00.000", "CHAPTER001NAME=Intro", "CHAPTER002=00:00:00.500"}
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"vorbis", createMinimalOgg("T", "A", "B", extra...)},
		{"opus", createMinimalOpus("T", "A", "B", extra...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			file, err := (&parser{}).Parse(context.Background(), bytes.NewReader(tc.data), int64(len(tc.data)), "test.ogg")
			if err != nil {
				t.Fatal(err)
			}
			if file.Audio.Duration <= 0 {
				t.Fatalf("duration = %v, want > 0", file.Audio.Duration)
			}
			if len(file.Chapters) != 2 {
				t.Fatalf("got %d chapters, want 2", len(file.Chapters))
			}
			if got := file.Chapters[0].EndTime; got != 500*time.Millisecond {
				t.Errorf("chapter 1 end = %v, want 500ms", got)
			}
			if got := file.Chapters[1].EndTime; got != file.Audio.Duration {
				t.Errorf("chapter 2 end = %v, want duration %v", got, file.Audio.Duration)
			}
		})
	}
}

func TestParseOpus_InformationalHeaderFieldsDoNotWarn(t *testing.T) {
	t.Parallel()
	data := createMinimalOpus("T", "A", "B")
	// Patch OpusHead: input sample rate 44100, output gain 256 (+1 dB).
	i := bytes.Index(data, []byte("OpusHead"))
	if i < 0 {
		t.Fatal("OpusHead not found")
	}
	binary.LittleEndian.PutUint32(data[i+12:], 44100)
	binary.LittleEndian.PutUint16(data[i+16:], 256)

	file, err := (&parser{}).Parse(context.Background(), bytes.NewReader(data), int64(len(data)), "test.opus")
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Warnings) != 0 {
		t.Errorf("unexpected warnings: %+v", file.Warnings)
	}
	if file.Audio.SampleRate != 48000 {
		t.Errorf("SampleRate = %d, want 48000", file.Audio.SampleRate)
	}
}

func TestParse_ReplayGain(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data []byte
		want types.ReplayGainInfo
	}{
		{
			"vorbis", createMinimalOgg("T", "A", "B", "replaygain_track_gain=-6.50 dB", "REPLAYGAIN_TRACK_PEAK=0.5"),
			types.ReplayGainInfo{TrackGain: -6.5, TrackPeak: 0.5},
		},
		{
			"opus R128", createMinimalOpus("T", "A", "B", "R128_TRACK_GAIN=-1280", "REPLAYGAIN_TRACK_GAIN=-3 dB"),
			types.ReplayGainInfo{TrackGain: 0},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f, err := (&parser{}).Parse(t.Context(), bytes.NewReader(tt.data), int64(len(tt.data)), "x")
			if err != nil {
				t.Fatal(err)
			}
			if len(f.Warnings) != 0 {
				t.Errorf("warnings = %v, want none", f.Warnings)
			}
			if got := f.Audio.ReplayGain; got == nil || *got != tt.want {
				t.Errorf("ReplayGain = %+v, want %+v", got, tt.want)
			}
		})
	}
}
