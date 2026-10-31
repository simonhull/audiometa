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

// ExampleFile_ReplaceArtwork replaces the front cover. The MIME type and
// dimensions are detected from the image data.
func ExampleFile_ReplaceArtwork() {
	cover, err := os.ReadFile("cover.png")
	if err != nil {
		log.Fatal(err)
	}

	err = audiometa.Write("song.flac", func(f *audiometa.File) error {
		return f.ReplaceArtwork(audiometa.Artwork{
			Data: cover,
			Type: audiometa.ArtworkFrontCover,
		})
	})
	if errors.Is(err, audiometa.ErrInvalidArtwork) {
		log.Fatal("cover.png is not a usable image")
	}
	if err != nil {
		log.Fatal(err)
	}
}
