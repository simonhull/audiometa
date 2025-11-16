package ogg

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

// createMinimalOgg creates a minimal Ogg Vorbis file with identification and comment headers.
func createMinimalOgg(title, artist, album string) []byte {
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

	createPage(0x02, 0, 12345, 0, idHeader.Bytes()) // BOS flag

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

	// Comment count
	binary.Write(commentHeader, binary.LittleEndian, uint32(len(comments)))

	// Write comments
	for _, comment := range comments {
		binary.Write(commentHeader, binary.LittleEndian, uint32(len(comment)))
		commentHeader.WriteString(comment)
	}

	// Framing bit
	commentHeader.WriteByte(0x01)

	createPage(0x00, 0, 12345, 1, commentHeader.Bytes())

	// Page 2: Setup header (empty for test purposes)
	setupHeader := &bytes.Buffer{}
	setupHeader.WriteByte(0x05)       // Packet type: setup
	setupHeader.WriteString("vorbis") // Magic
	setupHeader.WriteByte(0x01)       // Framing

	createPage(0x00, 0, 12345, 2, setupHeader.Bytes())

	// Page 3: Last page with granule position for duration (1 second at 44.1kHz)
	lastPage := &bytes.Buffer{}
	lastPage.Write(make([]byte, 100)) // Dummy audio data

	createPage(0x04, 44100, 12345, 3, lastPage.Bytes()) // EOS flag

	return buf.Bytes()
}

// createMinimalOpus creates a minimal Ogg Opus file with OpusHead and OpusTags headers.
func createMinimalOpus(title, artist, album string) []byte {
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
	data := createMinimalOgg("Test Song", "Test Artist", "Test Album")

	// Write to temp file
	tmpFile, err := os.CreateTemp("", "test*.ogg")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

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
	file, err := p.Parse(f, stat.Size(), tmpFile.Name())
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if file == nil {
		t.Fatal("expected file, got nil")
	}

	// Check format
	if file.Format != types.FormatOgg {
		t.Errorf("expected format Ogg, got %v", file.Format)
	}

	// Check tags
	if file.Tags.Title != "Test Song" {
		t.Errorf("expected title 'Test Song', got '%s'", file.Tags.Title)
	}

	if file.Tags.Artist != "Test Artist" {
		t.Errorf("expected artist 'Test Artist', got '%s'", file.Tags.Artist)
	}

	if file.Tags.Album != "Test Album" {
		t.Errorf("expected album 'Test Album', got '%s'", file.Tags.Album)
	}

	// Check audio info
	if file.Audio.Codec != "Vorbis" {
		t.Errorf("expected codec 'Vorbis', got '%s'", file.Audio.Codec)
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

	if file.Audio.SampleRate != 44100 {
		t.Errorf("expected sample rate 44100, got %d", file.Audio.SampleRate)
	}

	if file.Audio.Channels != 2 {
		t.Errorf("expected 2 channels, got %d", file.Audio.Channels)
	}

	if file.Audio.Bitrate != 128000 {
		t.Errorf("expected bitrate 128000, got %d", file.Audio.Bitrate)
	}

	// Duration should be ~1 second (44100 samples at 44100 Hz)
	expectedDuration := int64(1000000000) // 1 second in nanoseconds
	if file.Audio.Duration.Nanoseconds() < expectedDuration*9/10 ||
		file.Audio.Duration.Nanoseconds() > expectedDuration*11/10 {
		t.Errorf("expected duration ~1s, got %v", file.Audio.Duration)
	}
}

func TestParse_InvalidMagic(t *testing.T) {
	// Create invalid data with wrong magic bytes
	data := []byte("INVALID")

	// Write to temp file
	tmpFile, err := os.CreateTemp("", "test*.ogg")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

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
	_, err = p.Parse(f, stat.Size(), tmpFile.Name())
	if err == nil {
		t.Fatal("expected error for invalid magic, got nil")
	}

	// Should be a CorruptedFileError (invalid Ogg page)
	var corruptedErr *types.CorruptedFileError
	if !errors.As(err, &corruptedErr) {
		t.Errorf("expected CorruptedFileError, got %T: %v", err, err)
	}
}

func TestParse_EmptyTags(t *testing.T) {
	// Create Ogg with no tags
	data := createMinimalOgg("", "", "")

	// Write to temp file
	tmpFile, err := os.CreateTemp("", "test*.ogg")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

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
	file, err := p.Parse(f, stat.Size(), tmpFile.Name())
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
	data := createMinimalOpus("Opus Song", "Opus Artist", "Opus Album")

	// Write to temp file
	tmpFile, err := os.CreateTemp("", "test*.opus")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

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
	file, err := p.Parse(f, stat.Size(), tmpFile.Name())
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if file == nil {
		t.Fatal("expected file, got nil")
	}

	// Check format
	if file.Format != types.FormatOgg {
		t.Errorf("expected format Ogg, got %v", file.Format)
	}

	// Check tags
	if file.Tags.Title != "Opus Song" {
		t.Errorf("expected title 'Opus Song', got '%s'", file.Tags.Title)
	}

	if file.Tags.Artist != "Opus Artist" {
		t.Errorf("expected artist 'Opus Artist', got '%s'", file.Tags.Artist)
	}

	if file.Tags.Album != "Opus Album" {
		t.Errorf("expected album 'Opus Album', got '%s'", file.Tags.Album)
	}

	// Check audio info
	if file.Audio.Codec != "Opus" {
		t.Errorf("expected codec 'Opus', got '%s'", file.Audio.Codec)
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

	// Opus always outputs at 48kHz
	if file.Audio.SampleRate != 48000 {
		t.Errorf("expected sample rate 48000 (Opus output rate), got %d", file.Audio.SampleRate)
	}

	if file.Audio.Channels != 2 {
		t.Errorf("expected 2 channels, got %d", file.Audio.Channels)
	}

	// Duration should be ~1 second (48000 samples at 48kHz)
	expectedDuration := int64(1000000000) // 1 second in nanoseconds
	if file.Audio.Duration.Nanoseconds() < expectedDuration*9/10 ||
		file.Audio.Duration.Nanoseconds() > expectedDuration*11/10 {
		t.Errorf("expected duration ~1s, got %v", file.Audio.Duration)
	}
}

func TestParseOpus_BitrateEstimation(t *testing.T) {
	data := createMinimalOpus("Test", "Test", "Test")

	// Write to temp file
	tmpFile, err := os.CreateTemp("", "test*.opus")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

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
	file, err := p.Parse(f, stat.Size(), tmpFile.Name())
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Bitrate should be estimated (not zero)
	if file.Audio.Bitrate <= 0 {
		t.Errorf("expected bitrate > 0, got %d", file.Audio.Bitrate)
	}

	// For our synthetic file:
	// File size ~= len(data) bytes
	// Duration = 1 second
	// Expected bitrate ~= (fileSize - 5KB overhead) * 8 bits/byte / 1 second
	fileSize := int64(len(data))
	audioSize := fileSize - 5000
	if audioSize < 0 {
		audioSize = fileSize
	}
	expectedBitrate := int((float64(audioSize) * 8) / 1.0)

	// Allow 20% margin
	if file.Audio.Bitrate < expectedBitrate*8/10 ||
		file.Audio.Bitrate > expectedBitrate*12/10 {
		t.Errorf("expected bitrate ~%d, got %d", expectedBitrate, file.Audio.Bitrate)
	}
}

func TestOggParser_BothCodecs(t *testing.T) {
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
			// Write to temp file
			tmpFile, err := os.CreateTemp("", "test*.ogg")
			if err != nil {
				t.Fatal(err)
			}
			defer os.Remove(tmpFile.Name())

			if _, err := tmpFile.Write(tt.data); err != nil {
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
			file, err := p.Parse(f, stat.Size(), tmpFile.Name())
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}

			// Check codec
			if file.Audio.Codec != tt.expectedCodec {
				t.Errorf("expected codec '%s', got '%s'", tt.expectedCodec, file.Audio.Codec)
			}

			// Check sample rate
			if file.Audio.SampleRate != tt.expectedSampleRate {
				t.Errorf("expected sample rate %d, got %d", tt.expectedSampleRate, file.Audio.SampleRate)
			}

			// Both should be Ogg container
			if file.Audio.Container != "Ogg" {
				t.Errorf("expected container 'Ogg', got '%s'", file.Audio.Container)
			}

			// Both should be lossy VBR
			if file.Audio.Lossless {
				t.Error("expected lossless to be false")
			}
			if !file.Audio.VBR {
				t.Error("expected VBR to be true")
			}
		})
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

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
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
		_, err = p.Parse(f, stat.Size(), path)
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

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
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
		file, err := p.Parse(f, stat.Size(), path)
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

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
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
		file, err := p.Parse(f, stat.Size(), path)
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

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
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
		_, err = p.Parse(f, stat.Size(), path)
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

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
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
		file, err := p.Parse(f, stat.Size(), path)
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

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
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
		file, err := p.Parse(f, stat.Size(), path)
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
