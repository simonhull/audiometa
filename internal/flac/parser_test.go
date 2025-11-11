package flac

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"testing"

	"github.com/simonhull/audiometa"
)

// createMinimalFLAC creates a minimal FLAC file with STREAMINFO and VORBIS_COMMENT blocks
func createMinimalFLAC(title, artist, album string) []byte {
	buf := &bytes.Buffer{}

	// 1. FLAC magic bytes
	buf.WriteString("fLaC")

	// 2. STREAMINFO block (block type 0, not last)
	// Header: [is_last(1) | block_type(7)] [length(24)]
	// Block type 0, not last: 0x00
	buf.WriteByte(0x00)
	// Length: 34 bytes (24-bit big-endian)
	buf.WriteByte(0x00)
	buf.WriteByte(0x00)
	buf.WriteByte(0x22) // 34 in hex

	// STREAMINFO data (34 bytes)
	// Min block size: 4096 (16-bit)
	binary.Write(buf, binary.BigEndian, uint16(4096))
	// Max block size: 4096 (16-bit)
	binary.Write(buf, binary.BigEndian, uint16(4096))
	// Min frame size: 0 (24-bit)
	buf.WriteByte(0x00)
	buf.WriteByte(0x00)
	buf.WriteByte(0x00)
	// Max frame size: 0 (24-bit)
	buf.WriteByte(0x00)
	buf.WriteByte(0x00)
	buf.WriteByte(0x00)

	// Packed data: sample rate (20 bits), channels (3 bits), bits per sample (5 bits), total samples (36 bits)
	// Sample rate: 44100 Hz = 0xAC44
	// Channels: 2 (stored as 1) = 0b001
	// Bits per sample: 16 (stored as 15) = 0b01111
	// Total samples: 44100 (1 second) = 0xAC44
	// Pack into 64 bits: [sample_rate(20)] [channels-1(3)] [bits-1(5)] [total_samples(36)]
	sampleRate := uint64(44100)
	channels := uint64(1) // 2 channels - 1
	bitsPerSample := uint64(15) // 16 bits - 1
	totalSamples := uint64(44100) // 1 second at 44.1kHz

	packed := (sampleRate << 44) | (channels << 41) | (bitsPerSample << 36) | totalSamples
	binary.Write(buf, binary.BigEndian, packed)

	// MD5 signature (128 bits = 16 bytes) - all zeros
	buf.Write(make([]byte, 16))

	// 3. VORBIS_COMMENT block (block type 4, last)
	// Header: [is_last(1) | block_type(7)] [length(24)]
	// Block type 4, last: 0x84
	buf.WriteByte(0x84)

	// Build Vorbis comment data
	commentData := &bytes.Buffer{}

	// Vendor string (little-endian length + string)
	vendor := "audiometa"
	binary.Write(commentData, binary.LittleEndian, uint32(len(vendor)))
	commentData.WriteString(vendor)

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

	// Number of comments (little-endian)
	binary.Write(commentData, binary.LittleEndian, uint32(len(comments)))

	// Write each comment
	for _, comment := range comments {
		binary.Write(commentData, binary.LittleEndian, uint32(len(comment)))
		commentData.WriteString(comment)
	}

	// Write block length (24-bit big-endian)
	commentLen := commentData.Len()
	buf.WriteByte(byte((commentLen >> 16) & 0xFF))
	buf.WriteByte(byte((commentLen >> 8) & 0xFF))
	buf.WriteByte(byte(commentLen & 0xFF))

	// Write comment data
	buf.Write(commentData.Bytes())

	return buf.Bytes()
}

func TestParse_Success(t *testing.T) {
	data := createMinimalFLAC("Test Song", "Test Artist", "Test Album")

	// Write to temp file
	tmpFile, err := os.CreateTemp("", "test*.flac")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(data); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	// Parse the file
	file, err := audiometa.Open(tmpFile.Name())
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	defer file.Close()

	if file == nil {
		t.Fatal("expected file, got nil")
	}

	// Check format
	if file.Format != audiometa.FormatFLAC {
		t.Errorf("expected format FLAC, got %v", file.Format)
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
	if file.Audio.Codec != "FLAC" {
		t.Errorf("expected codec 'FLAC', got '%s'", file.Audio.Codec)
	}

	if file.Audio.Container != "FLAC" {
		t.Errorf("expected container 'FLAC', got '%s'", file.Audio.Container)
	}

	if !file.Audio.Lossless {
		t.Error("expected lossless to be true")
	}

	if file.Audio.SampleRate != 44100 {
		t.Errorf("expected sample rate 44100, got %d", file.Audio.SampleRate)
	}

	if file.Audio.Channels != 2 {
		t.Errorf("expected 2 channels, got %d", file.Audio.Channels)
	}

	if file.Audio.BitDepth != 16 {
		t.Errorf("expected 16-bit depth, got %d", file.Audio.BitDepth)
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
	tmpFile, err := os.CreateTemp("", "test*.flac")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(data); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	// Parse should fail
	_, err = audiometa.Open(tmpFile.Name())
	if err == nil {
		t.Fatal("expected error for invalid magic, got nil")
	}

	// Should be an UnsupportedFormatError (format detection fails first)
	var unsupportedErr *audiometa.UnsupportedFormatError
	if !errors.As(err, &unsupportedErr) {
		t.Errorf("expected UnsupportedFormatError, got %T: %v", err, err)
	}
}

func TestExtractArtwork_NoPictures(t *testing.T) {
	// Create FLAC without PICTURE blocks
	data := createMinimalFLAC("Test", "Artist", "Album")

	// Write to temp file
	tmpFile, err := os.CreateTemp("", "test*.flac")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(data); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	// Open file
	file, err := audiometa.Open(tmpFile.Name())
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer file.Close()

	// Extract artwork should return empty slice
	artwork, err := file.ExtractArtwork()
	if err != nil {
		t.Fatalf("ExtractArtwork failed: %v", err)
	}

	if len(artwork) != 0 {
		t.Errorf("expected no artwork, got %d images", len(artwork))
	}
}

func TestParse_EmptyTags(t *testing.T) {
	// Create FLAC with no tags
	data := createMinimalFLAC("", "", "")

	// Write to temp file
	tmpFile, err := os.CreateTemp("", "test*.flac")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(data); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	// Parse should succeed
	file, err := audiometa.Open(tmpFile.Name())
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	defer file.Close()

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

// Benchmarks

func BenchmarkParseFLAC(b *testing.B) {
	data := createMinimalFLAC("Benchmark Song", "Benchmark Artist", "Benchmark Album")

	tmpFile, err := os.CreateTemp(b.TempDir(), "bench*.flac")
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
		file, err := audiometa.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		file.Close()
	}
}

func BenchmarkParseStreamInfo(b *testing.B) {
	data := createMinimalFLAC("Test", "Artist", "Album")

	tmpFile, err := os.CreateTemp(b.TempDir(), "bench*.flac")
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
		file, err := audiometa.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		// Access audio info to ensure STREAMINFO was parsed
		_ = file.Audio.SampleRate
		_ = file.Audio.Channels
		_ = file.Audio.BitDepth
		_ = file.Audio.Duration
		file.Close()
	}
}

func BenchmarkParseVorbisComment(b *testing.B) {
	data := createMinimalFLAC("Long Title For Testing", "Artist Name Here", "Album Name Goes Here")

	tmpFile, err := os.CreateTemp(b.TempDir(), "bench*.flac")
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
		file, err := audiometa.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		// Access tags to ensure Vorbis comments were parsed
		_ = file.Tags.Title
		_ = file.Tags.Artist
		_ = file.Tags.Album
		file.Close()
	}
}

func BenchmarkExtractArtwork(b *testing.B) {
	data := createMinimalFLAC("Test", "Artist", "Album")

	tmpFile, err := os.CreateTemp(b.TempDir(), "bench*.flac")
	if err != nil {
		b.Fatal(err)
	}
	defer tmpFile.Close()

	if _, err := tmpFile.Write(data); err != nil {
		b.Fatal(err)
	}

	path := tmpFile.Name()

	file, err := audiometa.Open(path)
	if err != nil {
		b.Fatal(err)
	}
	defer file.Close()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := file.ExtractArtwork()
		if err != nil {
			b.Fatal(err)
		}
	}
}
