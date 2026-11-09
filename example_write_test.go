package audiometa_test

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

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
