package audiometa_test

import (
	"bytes"
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

// The audio digest identifies a recording across renames and retags.
func ExampleFile_AudioDigest() {
	file, err := audiometa.Open("testdata/sample.flac")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	sum, err := file.AudioDigest()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%x\n", sum)
	// Output:
	// f36e19b9a2c276157124aab7ab0b52e90025e3f92e6c2bc4f4da8988ef12fdff
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
	defer func() { _ = os.RemoveAll(dir) }()
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

// DetectFormat reads only the header, so it works on any io.ReaderAt without
// parsing the file.
func ExampleDetectFormat() {
	f, err := os.Open("testdata/sample.opus")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		log.Fatal(err)
	}

	format, err := audiometa.DetectFormat(f, info.Size(), f.Name())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(format)
	// Output:
	// Opus
}

// WithIgnoreWarnings discards warnings. The FLAC file here is cut off just
// after its STREAMINFO block, which normally records one warning.
func ExampleWithIgnoreWarnings() {
	path := copyToTemp("testdata/sample.flac")
	defer func() { _ = os.RemoveAll(filepath.Dir(path)) }()
	if err := os.Truncate(path, 43); err != nil {
		log.Fatal(err)
	}

	for _, opts := range [][]audiometa.Option{nil, {audiometa.WithIgnoreWarnings()}} {
		file, err := audiometa.Open(path, opts...)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("warnings:", len(file.Warnings))
		file.Close()
	}
	// Output:
	// warnings: 1
	// warnings: 0
}

// WithArtworkPreload reads the artwork during Open, so the first
// ExtractArtwork call returns the cached images.
func ExampleWithArtworkPreload() {
	file, err := audiometa.Open("testdata/sample.mp3", audiometa.WithArtworkPreload())
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	artwork, err := file.ExtractArtwork()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(artwork), len(file.Warnings))
	// Output:
	// 1 0
}

// ExtractArtworkContext stops early when the context is done.
func ExampleFile_ExtractArtworkContext() {
	file, err := audiometa.Open("testdata/sample.mp3")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = file.ExtractArtworkContext(ctx)
	fmt.Println(errors.Is(err, context.Canceled))

	artwork, err := file.ExtractArtworkContext(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(artwork))
	// Output:
	// true
	// 1
}

// AudioDigestContext bounds how long hashing the audio can take. It returns
// the same digest as AudioDigest.
func ExampleFile_AudioDigestContext() {
	file, err := audiometa.Open("testdata/sample.flac")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sum, err := file.AudioDigestContext(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%x\n", sum[:4])
	// Output:
	// f36e19b9
}

// GetFirst returns the first value of a key, or "" when the key is absent.
func ExampleTags_GetFirst() {
	file, err := audiometa.Open("testdata/sample.flac")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	fmt.Printf("%q %q\n", file.Tags.GetFirst("TITLE"), file.Tags.GetFirst("NO SUCH KEY"))
	// Output:
	// "Test Title" ""
}

// GetBest tries several keys and returns the first non-empty value, which
// helps when one field lives under different keys in different formats.
func ExampleTags_GetBest() {
	for _, path := range []string{"testdata/sample.flac", "testdata/sample.mp3"} {
		file, err := audiometa.Open(path)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(file.Tags.GetBest("TITLE", "TIT2"))
		file.Close()
	}
	// Output:
	// Test Title
	// Test Title
}

// A CorruptedFileError means the container structure is invalid. Here a WAV
// file ends right after its RIFF header, so it has no fmt chunk.
func ExampleCorruptedFileError() {
	data, err := os.ReadFile("testdata/sample.wav")
	if err != nil {
		log.Fatal(err)
	}
	dir, err := os.MkdirTemp("", "audiometa-example")
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	path := filepath.Join(dir, "headless.wav")
	if err := os.WriteFile(path, data[:12], 0o600); err != nil {
		log.Fatal(err)
	}

	_, err = audiometa.Open(path)
	if corrupt, ok := errors.AsType[*audiometa.CorruptedFileError](err); ok {
		fmt.Println(corrupt.Reason, "at offset", corrupt.Offset)
	}
	// Output:
	// WAV file without a fmt chunk at offset 12
}

// An OutOfBoundsError means a required read runs past the end of the file.
// Here an Ogg file is cut off in the middle of its first page.
func ExampleOutOfBoundsError() {
	data, err := os.ReadFile("testdata/sample.ogg")
	if err != nil {
		log.Fatal(err)
	}
	dir, err := os.MkdirTemp("", "audiometa-example")
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	path := filepath.Join(dir, "cut.ogg")
	if err := os.WriteFile(path, data[:30], 0o600); err != nil {
		log.Fatal(err)
	}

	_, err = audiometa.Open(path)
	if oob, ok := errors.AsType[*audiometa.OutOfBoundsError](err); ok {
		fmt.Printf("%s: %d bytes at offset %d, file size %d\n", oob.What, oob.Length, oob.Offset, oob.Size)
	}
	// Output:
	// page data: 30 bytes at offset 28, file size 30
}

// The version depends on how the binary was built, so GetVersion and
// GetVersionInfo have no checked output.
func ExampleGetVersion() { //nolint:testableexamples // the version depends on how the binary was built
	fmt.Println(audiometa.GetVersion())
}

// GetVersionInfo adds the commit, build time and Go version when the build
// recorded them.
func ExampleGetVersionInfo() { //nolint:testableexamples // the build details differ per build
	info := audiometa.GetVersionInfo()
	fmt.Println(info.Version, info.GitCommit, info.BuildTime, info.GoVersion)
}

// List the tag containers a file stores, and which of them its Tags were read
// from. sample_v24.mp3 has an ID3v2.4 tag with a title, so the ID3v1 trailer
// behind it is stored but not used.
func ExampleFile_TagSources() {
	file, err := audiometa.Open("testdata/sample_v24.mp3")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	for _, s := range file.TagSources {
		fmt.Println(s.Kind, s.Version, s.Used)
	}
	// Output:
	// id3v2 4 true
	// id3v1 0 false
}

// A WAV file can hold an ID3 chunk and native LIST/INFO text; both are listed,
// in the order the reader consults them. Their kinds print as stable strings.
func ExampleTagSource() {
	file, err := audiometa.Open("testdata/sample_id3.wav")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	for _, s := range file.TagSources {
		if s.Kind == audiometa.TagID3v2 {
			fmt.Printf("ID3v2.%d, used: %t\n", s.Version, s.Used)
		} else {
			fmt.Printf("%s, used: %t\n", s.Kind, s.Used)
		}
	}
	// Output:
	// ID3v2.3, used: true
	// riff-info, used: true
}

// Ask whether a format has a tag writer before offering to edit it.
func ExampleCanWrite() {
	fmt.Println(audiometa.CanWrite(audiometa.FormatFLAC))
	fmt.Println(audiometa.CanWrite(audiometa.FormatOpus))
	// Output:
	// true
	// true
}

// Ask whether Write would accept this file, before the user has edited it.
// A nil result covers the file, not the edit that follows.
func ExampleFile_Writable() {
	file, err := audiometa.Open("testdata/sample.m4a")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	fmt.Println(file.Writable())
	// Output: <nil>
}

// Writable names the reason a file is refused. This copy of an M4A file is
// marked as encrypted.
func ExampleFile_WritableContext() {
	data, err := os.ReadFile("testdata/sample.m4a")
	if err != nil {
		log.Fatal(err)
	}
	dir, err := os.MkdirTemp("", "audiometa-example")
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	path := filepath.Join(dir, "protected.m4a")
	if err := os.WriteFile(path, bytes.ReplaceAll(data, []byte("mp4a"), []byte("enca")), 0o600); err != nil {
		log.Fatal(err)
	}

	file, err := audiometa.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	err = file.WritableContext(context.Background())
	fmt.Println(errors.Is(err, audiometa.ErrDRMProtected))
	// Output: true
}
