package audiometa_test

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"

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

// ExampleWrite sets the title and the front cover of an audiobook and
// replaces the file atomically.
func ExampleWrite() {
	cover, err := os.ReadFile("cover.jpg")
	if err != nil {
		log.Fatal(err)
	}

	err = audiometa.Write("book.mp3", func(f *audiometa.File) error {
		f.Tags.Title = "New Title"
		f.Tags.Delete("TCOP") // any tag can be removed by its native key
		return f.ReplaceArtwork(audiometa.Artwork{Data: cover, Type: audiometa.ArtworkFrontCover})
	})
	if err != nil {
		log.Fatal(err)
	}
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
