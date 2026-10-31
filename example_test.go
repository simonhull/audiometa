package audiometa_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/simonhull/audiometa"
)

// Open any supported file; the format is detected from its contents.
func ExampleOpen() {
	file, err := audiometa.Open("testdata/sample.flac")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	fmt.Println("Format:  ", file.Format)
	fmt.Println("Title:   ", file.Tags.Title)
	fmt.Println("Artist:  ", file.Tags.Artist)
	fmt.Println("Album:   ", file.Tags.Album)
	fmt.Println("Duration:", file.Audio.Duration)
	fmt.Println("Audio:   ", file.Audio)
	// Output:
	// Format:   FLAC
	// Title:    Test Title
	// Artist:   Test Artist
	// Album:    Test Album
	// Duration: 2s
	// Audio:    FLAC 44.1kHz 16-bit stereo lossless
}

// Bound the parse with a timeout. A context that is already done stops
// OpenContext before it touches the file.
func ExampleOpenContext() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	file, err := audiometa.OpenContext(ctx, "testdata/sample.opus")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	fmt.Println(file.Format, file.Audio.Duration)

	canceled, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	_, err = audiometa.OpenContext(canceled, "testdata/sample.opus")
	fmt.Println(errors.Is(err, context.Canceled))
	// Output:
	// Opus 2.0065s
	// true
}

// Artwork is read on demand and cached by the File.
func ExampleFile_ExtractArtwork() {
	file, err := audiometa.Open("testdata/sample.mp3")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	artwork, err := file.ExtractArtwork()
	if err != nil {
		log.Fatal(err)
	}
	for _, art := range artwork {
		fmt.Println(art.Type, art.MIMEType, art.Width, art.Height, len(art.Data))
		fmt.Println(art)
	}
	// Output:
	// Front cover image/jpeg 16 16 224
	// Front cover (16x16 JPEG, 224B)
}

// Images over the size limit are dropped with a warning.
func ExampleWithMaxArtworkSize() {
	file, err := audiometa.Open("testdata/sample.mp3", audiometa.WithMaxArtworkSize(100))
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	artwork, err := file.ExtractArtwork()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("images:", len(artwork))
	for _, w := range file.Warnings {
		fmt.Println(w)
	}
	// Output:
	// images: 0
	// artwork: artwork (image/jpeg, 224 bytes) exceeds max size 100; skipped
}

// Each chapter records the container structure it was read from, so the same
// loop works for audiobooks in any format.
func Example_chapters() {
	for _, path := range []string{"testdata/sample.m4b", "testdata/sample.mp3"} {
		file, err := audiometa.Open(path)
		if err != nil {
			log.Fatal(err)
		}
		for _, ch := range file.Chapters {
			fmt.Printf("[%d] %v-%v %s (%s)\n", ch.Index, ch.StartTime, ch.EndTime, ch.Title, ch.Source)
		}
		file.Close()
	}
	// Output:
	// [1] 0s-1s Chapter One (quicktime)
	// [2] 1s-2s Chapter Two (quicktime)
	// [1] 0s-1s Chapter One (id3_chap)
	// [2] 1s-2s Chapter Two (id3_chap)
}

// OpenMany parses files in parallel. One failure does not stop the others:
// its slot is nil and its error is joined into the returned error.
func ExampleOpenMany() {
	paths := []string{
		"testdata/sample.flac",
		"testdata/missing.mp3",
		"testdata/sample.ogg",
	}
	files, err := audiometa.OpenMany(context.Background(), paths...)
	defer func() {
		for _, f := range files {
			if f != nil {
				f.Close()
			}
		}
	}()

	for i, f := range files {
		if f == nil {
			fmt.Println(paths[i], "failed")
			continue
		}
		fmt.Println(paths[i], f.Format)
	}
	fmt.Println("missing file reported:", errors.Is(err, fs.ErrNotExist))
	// Output:
	// testdata/sample.flac FLAC
	// testdata/missing.mp3 failed
	// testdata/sample.ogg Ogg Vorbis
	// missing file reported: true
}

// Strict parsing turns a warning into an error. Here a FLAC file is cut off
// just after its STREAMINFO block: by default Open succeeds with a warning;
// with WithStrictParsing it fails, and errors.AsType (or errors.As) finds the
// typed cause.
func ExampleWithStrictParsing() {
	data, err := os.ReadFile("testdata/sample.flac")
	if err != nil {
		log.Fatal(err)
	}
	dir, err := os.MkdirTemp("", "audiometa-example")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	truncated := filepath.Join(dir, "truncated.flac")
	if err := os.WriteFile(truncated, data[:43], 0o600); err != nil {
		log.Fatal(err)
	}

	file, err := audiometa.Open(truncated)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("lenient:", len(file.Warnings), "warning at stage", file.Warnings[0].Stage)
	file.Close()

	_, err = audiometa.Open(truncated, audiometa.WithStrictParsing())
	if oob, ok := errors.AsType[*audiometa.OutOfBoundsError](err); ok {
		fmt.Printf("strict: reading %s: %d bytes at offset %d, file size %d\n",
			oob.What, oob.Length, oob.Offset, oob.Size)
	}
	// Output:
	// lenient: 1 warning at stage metadata
	// strict: reading metadata block header: 4 bytes at offset 42, file size 43
}

// Files that are not audio, or are in a format without a parser, fail with
// an UnsupportedFormatError.
func ExampleUnsupportedFormatError() {
	_, err := audiometa.Open("go.mod")

	if unsupported, ok := errors.AsType[*audiometa.UnsupportedFormatError](err); ok {
		fmt.Println(unsupported.Path, "-", unsupported.Reason)
	}
	// Output:
	// go.mod - unsupported file format
}

// Every tag is also available under its native key. Vorbis comment keys are
// upper-cased; MP3 uses ID3v2 frame IDs and M4A uses atom names.
func ExampleTags_Get() {
	flac, err := audiometa.Open("testdata/sample.flac")
	if err != nil {
		log.Fatal(err)
	}
	defer flac.Close()
	fmt.Println(flac.Tags.Get("ENCODER"))

	mp3, err := audiometa.Open("testdata/sample.mp3")
	if err != nil {
		log.Fatal(err)
	}
	defer mp3.Close()
	fmt.Println(mp3.Tags.Get("TIT2"), mp3.Tags.GetFirst("TYER"))

	m4a, err := audiometa.Open("testdata/sample.m4a")
	if err != nil {
		log.Fatal(err)
	}
	defer m4a.Close()
	fmt.Println(m4a.Tags.Get("\xa9nam"))
	// Output:
	// [Lavf63.1.102]
	// [Test Title] 2021
	// [Test Title]
}

// All iterates over every raw tag. Map order is random, so sort the keys for
// stable output.
func ExampleTags_All() {
	file, err := audiometa.Open("testdata/sample.ogg")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	raw := maps.Collect(file.Tags.All())
	for _, key := range slices.Sorted(maps.Keys(raw)) {
		fmt.Println(key, raw[key])
	}
	// Output:
	// ALBUM [Test Album]
	// ARTIST [Test Artist]
	// ENCODER [Lavc63.1.102 libvorbis]
	// TITLE [Test Title]
}
