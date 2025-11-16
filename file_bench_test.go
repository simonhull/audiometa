package audiometa_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"testing"

	"github.com/simonhull/audiometa"
	_ "github.com/simonhull/audiometa/internal/flac" // Register FLAC parser
	_ "github.com/simonhull/audiometa/internal/m4a"  // Register M4A/M4B parser
	_ "github.com/simonhull/audiometa/internal/mp3"  // Register MP3 parser
	_ "github.com/simonhull/audiometa/internal/ogg"  // Register Ogg Vorbis parser
	"github.com/simonhull/audiometa/internal/types"
)

// createBenchmarkM4B creates a minimal but valid M4B file for benchmarking.
func createBenchmarkM4B(b *testing.B) string {
	b.Helper()

	buf := &bytes.Buffer{}

	// ftyp atom
	ftypBuf := &bytes.Buffer{}
	ftypBuf.WriteString("M4B ")
	binary.Write(ftypBuf, binary.BigEndian, uint32(0))
	ftypBuf.WriteString("M4B ")

	ftypSize := uint32(8 + ftypBuf.Len())
	binary.Write(buf, binary.BigEndian, ftypSize)
	buf.WriteString("ftyp")
	buf.Write(ftypBuf.Bytes())

	// moov atom (empty but valid)
	binary.Write(buf, binary.BigEndian, uint32(8))
	buf.WriteString("moov")

	tmpFile, err := os.CreateTemp(b.TempDir(), "bench*.m4b")
	if err != nil {
		b.Fatal(err)
	}
	defer tmpFile.Close()

	if _, err := tmpFile.Write(buf.Bytes()); err != nil {
		b.Fatal(err)
	}

	return tmpFile.Name()
}

// BenchmarkOpen measures the performance of opening a single audio file.
func BenchmarkOpen(b *testing.B) {
	path := createBenchmarkM4B(b)

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

// BenchmarkOpenContext measures the performance with context support.
func BenchmarkOpenContext(b *testing.B) {
	path := createBenchmarkM4B(b)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		file, err := audiometa.OpenContext(ctx, path)
		if err != nil {
			b.Fatal(err)
		}
		file.Close()
	}
}

// BenchmarkOpenMany measures concurrent file opening performance.
func BenchmarkOpenMany(b *testing.B) {
	// Create multiple test files
	paths := make([]string, 10)
	for i := range paths {
		paths[i] = createBenchmarkM4B(b)
	}

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		files, err := audiometa.OpenMany(ctx, paths...)
		if err != nil {
			b.Fatal(err)
		}
		for _, f := range files {
			f.Close()
		}
	}
}

// BenchmarkOpenManyParallel measures OpenMany scalability.
func BenchmarkOpenManyParallel(b *testing.B) {
	// Create test files in different sizes
	for _, n := range []int{1, 5, 10, 20, 50} {
		b.Run(string(rune('0'+n/10))+string(rune('0'+n%10))+"_files", func(b *testing.B) {
			paths := make([]string, n)
			for i := range paths {
				paths[i] = createBenchmarkM4B(b)
			}

			ctx := context.Background()

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				files, err := audiometa.OpenMany(ctx, paths...)
				if err != nil {
					b.Fatal(err)
				}
				for _, f := range files {
					f.Close()
				}
			}
		})
	}
}

// BenchmarkDetectFormat measures format detection performance.
func BenchmarkDetectFormat(b *testing.B) {
	buf := &bytes.Buffer{}

	// Create minimal M4B header
	ftypBuf := &bytes.Buffer{}
	ftypBuf.WriteString("M4B ")
	binary.Write(ftypBuf, binary.BigEndian, uint32(0))
	ftypBuf.WriteString("M4B ")

	ftypSize := uint32(8 + ftypBuf.Len())
	binary.Write(buf, binary.BigEndian, ftypSize)
	buf.WriteString("ftyp")
	buf.Write(ftypBuf.Bytes())

	data := buf.Bytes()
	reader := bytes.NewReader(data)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := audiometa.DetectFormat(reader, int64(len(data)), "test.m4b")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkTagAccess measures tag access performance.
func BenchmarkTagAccess(b *testing.B) {
	path := createBenchmarkM4B(b)
	file, err := audiometa.Open(path)
	if err != nil {
		b.Fatal(err)
	}
	defer file.Close()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = file.Tags.Title
		_ = file.Tags.Artist
		_ = file.Tags.Album
		_ = file.Audio.Duration
		_ = file.Audio.Bitrate
	}
}

// BenchmarkFileAllocation measures the overhead of File struct allocation.
func BenchmarkFileAllocation(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = &audiometa.File{
			File: types.File{
				Path:   "/test/path.m4b",
				Format: audiometa.FormatM4B,
				Size:   1024,
				Tags: audiometa.Tags{
					Title:  "Test Title",
					Artist: "Test Artist",
					Album:  "Test Album",
				},
				Audio: audiometa.AudioInfo{
					Duration:   3600000000000, // 1 hour in nanoseconds
					Bitrate:    128000,
					SampleRate: 44100,
					Channels:   2,
				},
			},
		}
	}
}
