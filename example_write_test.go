package audiometa_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/simonhull/audiometa"
)

// ExampleFile_WriteTo shows the error a format without a writer returns.
// WriteTo streams the edited file to any io.Writer; the original is never
// modified.
func ExampleFile_WriteTo() {
	f, err := audiometa.Open("testdata/sample.ogg")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	f.Tags.Title = "New Title"
	_, err = f.WriteTo(io.Discard)
	fmt.Println(errors.Is(err, audiometa.ErrUnsupportedWrite))
	// Output: true
}

// ExampleWrite sets the title of an MP3 file and replaces the file
// atomically. It edits a copy so the sample stays unchanged.
func ExampleWrite() {
	data, err := os.ReadFile("testdata/sample.mp3")
	if err != nil {
		log.Fatal(err)
	}
	dir, err := os.MkdirTemp("", "audiometa-example")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "book.mp3")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		log.Fatal(err)
	}

	err = audiometa.Write(path, func(f *audiometa.File) error {
		f.Tags.Title = "New Title"
		f.Tags.Delete("TCOP") // any tag can be removed by its native key
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	f, err := audiometa.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	fmt.Println(f.Tags.Title)
	// Output: New Title
}

// ExampleFile_ReplaceArtwork replaces the front cover of a FLAC file. The
// MIME type and dimensions are detected from the image data. The cover comes
// from another test file and the edit applies to a copy.
func ExampleFile_ReplaceArtwork() {
	src, err := audiometa.Open("testdata/sample.mp3")
	if err != nil {
		log.Fatal(err)
	}
	defer src.Close()
	covers, err := src.ExtractArtwork()
	if err != nil || len(covers) == 0 {
		log.Fatal("no cover to copy: ", err)
	}

	path := copyToTemp("testdata/sample.flac")
	defer os.RemoveAll(filepath.Dir(path))

	err = audiometa.Write(path, func(f *audiometa.File) error {
		return f.ReplaceArtwork(audiometa.Artwork{
			Data: covers[0].Data,
			Type: audiometa.ArtworkFrontCover,
		})
	})
	if errors.Is(err, audiometa.ErrInvalidArtwork) {
		log.Fatal("the cover is not a usable image")
	}
	if err != nil {
		log.Fatal(err)
	}

	f, err := audiometa.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	art, err := f.ExtractArtwork()
	if err != nil {
		log.Fatal(err)
	}
	// The file's existing "Other" picture is kept: only front covers are replaced.
	for _, a := range art {
		fmt.Println(a.Type, a.MIMEType, a.Width, a.Height)
	}
	// Output:
	// Other image/jpeg 16 16
	// Front cover image/jpeg 16 16
}

// ExampleTags_SetNamed writes MusicBrainz IDs to an MP3 the way MusicBrainz
// Picard names them: a TXXX frame spelled "MusicBrainz Album Id", found by its
// upper-cased raw key, and the recording ID in a UFID frame.
func ExampleTags_SetNamed() {
	data, err := os.ReadFile("testdata/sample.mp3")
	if err != nil {
		log.Fatal(err)
	}
	dir, err := os.MkdirTemp("", "audiometa-example")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "song.mp3")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		log.Fatal(err)
	}

	err = audiometa.Write(path, func(f *audiometa.File) error {
		f.Tags.SetNamed("MusicBrainz Album Id", "f3c0fd5e-ba3e-4a2c-8fd3-6e4a6f0c1b2d")
		f.Tags.Set("UFID:http://musicbrainz.org", "8f3471b5-7e6a-48da-86a9-c1c07a0f47ae")
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	f, err := audiometa.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	fmt.Println(f.Tags.Name("MUSICBRAINZ ALBUM ID"), f.Tags.GetFirst("MUSICBRAINZ ALBUM ID"))
	fmt.Println(f.Tags.GetFirst("UFID:http://musicbrainz.org"))
	// Output:
	// MusicBrainz Album Id f3c0fd5e-ba3e-4a2c-8fd3-6e4a6f0c1b2d
	// 8f3471b5-7e6a-48da-86a9-c1c07a0f47ae
}

// ExampleWithID3v1 removes an MP3's ID3v1 trailer while editing its title.
// Without the option the trailer is copied unchanged; ID3v1Update rewrites it
// from the edited tags instead.
func ExampleWithID3v1() {
	path := copyToTemp("testdata/sample_v24.mp3")
	defer os.RemoveAll(filepath.Dir(path))
	before, err := os.Stat(path)
	if err != nil {
		log.Fatal(err)
	}

	err = audiometa.Write(path, func(f *audiometa.File) error {
		f.Tags.Title = "New Title"
		return nil
	}, audiometa.WithID3v1(audiometa.ID3v1Remove))
	if err != nil {
		log.Fatal(err)
	}

	after, err := os.Stat(path)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(before.Size() - after.Size())
	// Output: 128
}

// ExampleWriteContext bounds the write with a timeout. A write that is
// canceled leaves the original file untouched.
func ExampleWriteContext() {
	path := copyToTemp("testdata/sample.flac")
	defer os.RemoveAll(filepath.Dir(path))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := audiometa.WriteContext(ctx, path, func(f *audiometa.File) error {
		f.Tags.Title = "New Title"
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	canceled, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	err = audiometa.WriteContext(canceled, path, func(f *audiometa.File) error {
		f.Tags.Title = "Never Written"
		return nil
	})
	fmt.Println(errors.Is(err, context.Canceled))

	f, err := audiometa.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	fmt.Println(f.Tags.Title)
	// Output:
	// true
	// New Title
}

// ExampleWithInPlace patches the tag in place when the edit fits the space
// already reserved for it, so the file keeps its identity (os.SameFile)
// instead of being replaced by a rewritten copy.
func ExampleWithInPlace() {
	path := copyToTemp("testdata/sample.flac")
	defer os.RemoveAll(filepath.Dir(path))
	before, err := os.Stat(path)
	if err != nil {
		log.Fatal(err)
	}

	err = audiometa.Write(path, func(f *audiometa.File) error {
		f.Tags.Title = "Shorter"
		return nil
	}, audiometa.WithInPlace())
	if err != nil {
		log.Fatal(err)
	}

	after, err := os.Stat(path)
	if err != nil {
		log.Fatal(err)
	}
	f, err := audiometa.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	fmt.Println(f.Tags.Title, os.SameFile(before, after))
	// Output:
	// Shorter true
}

// ExampleWithPreserveModTime keeps the file's modification time, which
// Write otherwise sets to the time of the write.
func ExampleWithPreserveModTime() {
	path := copyToTemp("testdata/sample.mp3")
	defer os.RemoveAll(filepath.Dir(path))
	old := time.Date(2020, time.January, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(path, old, old); err != nil {
		log.Fatal(err)
	}

	err := audiometa.Write(path, func(f *audiometa.File) error {
		f.Tags.Title = "New Title"
		return nil
	}, audiometa.WithPreserveModTime())
	if err != nil {
		log.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(info.ModTime().Equal(old))
	// Output:
	// true
}

// ExampleFile_WriteToContext streams the edited file to any io.Writer and
// stops early when the context is done. The original file is not modified.
func ExampleFile_WriteToContext() {
	f, err := audiometa.Open("testdata/sample.flac")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	f.Tags.Title = "New Title"

	var buf bytes.Buffer
	n, err := f.WriteToContext(context.Background(), &buf)
	fmt.Println(n == int64(buf.Len()), n > 0, err)
	// Output:
	// true true <nil>
}

// ExampleFile_AddArtwork adds a picture after the existing ones. The edit
// is pending until Write or WriteTo.
func ExampleFile_AddArtwork() {
	src, err := audiometa.Open("testdata/sample.mp3")
	if err != nil {
		log.Fatal(err)
	}
	defer src.Close()
	covers, err := src.ExtractArtwork()
	if err != nil || len(covers) == 0 {
		log.Fatal("no image to copy: ", err)
	}

	path := copyToTemp("testdata/sample.flac")
	defer os.RemoveAll(filepath.Dir(path))
	err = audiometa.Write(path, func(f *audiometa.File) error {
		return f.AddArtwork(audiometa.Artwork{
			Data:        covers[0].Data,
			Type:        audiometa.ArtworkBackCover,
			Description: "back",
		})
	})
	if err != nil {
		log.Fatal(err)
	}

	f, err := audiometa.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	art, err := f.ExtractArtwork()
	if err != nil {
		log.Fatal(err)
	}
	for _, a := range art {
		fmt.Printf("%s %s %q\n", a.Type, a.MIMEType, a.Description)
	}
	// Output:
	// Other image/jpeg ""
	// Back cover image/jpeg "back"
}

// ExampleFile_RemoveArtwork removes every picture of one type and keeps the
// others.
func ExampleFile_RemoveArtwork() {
	path := copyToTemp("testdata/sample.flac")
	defer os.RemoveAll(filepath.Dir(path))

	err := audiometa.Write(path, func(f *audiometa.File) error {
		f.RemoveArtwork(audiometa.ArtworkFrontCover) // not in the file: not an error
		f.RemoveArtwork(audiometa.ArtworkOther)
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	f, err := audiometa.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	art, err := f.ExtractArtwork()
	fmt.Println(len(art), err)
	// Output:
	// 0 <nil>
}

// ExampleFile_RemoveAllArtwork removes every picture, whatever its type.
func ExampleFile_RemoveAllArtwork() {
	path := copyToTemp("testdata/sample.mp3")
	defer os.RemoveAll(filepath.Dir(path))

	err := audiometa.Write(path, func(f *audiometa.File) error {
		f.RemoveAllArtwork()
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	f, err := audiometa.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	art, err := f.ExtractArtwork()
	fmt.Println(len(art), err)
	// Output:
	// 0 <nil>
}

// ExampleTags_Set sets a raw tag under its native key. On a File from Open
// the typed field follows. Changing a field and its own key to different
// values is a conflict that WriteTo refuses.
func ExampleTags_Set() {
	f, err := audiometa.Open("testdata/sample.mp3")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	f.Tags.Set("TIT2", "From the Raw Key")
	fmt.Println(f.Tags.Title)

	f.Tags.Title = "From the Field"
	_, err = f.WriteTo(io.Discard)
	fmt.Println(errors.Is(err, audiometa.ErrConflictingEdits))
	// Output:
	// From the Raw Key
	// true
}

// ExampleTags_Delete removes tags by their native keys. The typed field is
// cleared with them.
func ExampleTags_Delete() {
	path := copyToTemp("testdata/sample.flac")
	defer os.RemoveAll(filepath.Dir(path))

	err := audiometa.Write(path, func(f *audiometa.File) error {
		f.Tags.Delete("TITLE", "ENCODER")
		fmt.Printf("%q\n", f.Tags.Title)
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	f, err := audiometa.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	fmt.Printf("%q %q %q\n", f.Tags.Title, f.Tags.GetFirst("ENCODER"), f.Tags.Artist)
	// Output:
	// ""
	// "" "" "Test Artist"
}

// copyToTemp copies a testdata file into a new temporary directory and
// returns the copy's path. The caller removes the directory.
func copyToTemp(src string) string {
	data, err := os.ReadFile(src)
	if err != nil {
		log.Fatal(err)
	}
	dir, err := os.MkdirTemp("", "audiometa-example")
	if err != nil {
		log.Fatal(err)
	}
	path := filepath.Join(dir, filepath.Base(src))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		log.Fatal(err)
	}
	return path
}
