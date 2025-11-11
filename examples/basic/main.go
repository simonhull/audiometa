// Package main demonstrates basic usage of the audiometa library.
//
// This example shows how to:
//   - Open an audio file
//   - Read standard metadata tags
//   - Access technical audio information
//   - Handle errors gracefully
//
// Usage:
//
//	go run main.go <audio_file>
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/simonhull/audiometa"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: basic <audio_file>")
		fmt.Println("\nExample:")
		fmt.Println("  basic song.flac")
		fmt.Println("  basic audiobook.m4b")
		fmt.Println("  basic track.mp3")
		os.Exit(1)
	}

	path := os.Args[1]

	// Open the audio file
	file, err := audiometa.Open(path)
	if err != nil {
		log.Fatalf("Failed to open %s: %v", path, err)
	}
	defer file.Close()

	// Print file information
	fmt.Printf("File: %s\n", file.Path)
	fmt.Printf("Format: %s\n", file.Format)
	fmt.Printf("Size: %d bytes\n\n", file.Size)

	// Print metadata tags
	fmt.Println("Metadata:")
	fmt.Println("─────────")
	if file.Tags.Title != "" {
		fmt.Printf("Title:       %s\n", file.Tags.Title)
	}
	if file.Tags.Artist != "" {
		fmt.Printf("Artist:      %s\n", file.Tags.Artist)
	}
	if file.Tags.Album != "" {
		fmt.Printf("Album:       %s\n", file.Tags.Album)
	}
	if file.Tags.AlbumArtist != "" {
		fmt.Printf("Album Artist:%s\n", file.Tags.AlbumArtist)
	}
	if file.Tags.Year != 0 {
		fmt.Printf("Year:        %d\n", file.Tags.Year)
	}
	if file.Tags.TrackNumber != 0 {
		if file.Tags.TrackTotal != 0 {
			fmt.Printf("Track:       %d/%d\n", file.Tags.TrackNumber, file.Tags.TrackTotal)
		} else {
			fmt.Printf("Track:       %d\n", file.Tags.TrackNumber)
		}
	}

	// Print genres if present
	if len(file.Tags.Genres) > 0 {
		fmt.Printf("Genre:       %s\n", file.Tags.Genres[0])
		for _, g := range file.Tags.Genres[1:] {
			fmt.Printf("             %s\n", g)
		}
	}

	// Print audiobook-specific tags
	if file.Tags.Narrator != "" {
		fmt.Printf("Narrator:    %s\n", file.Tags.Narrator)
	}
	if file.Tags.Series != "" {
		series := file.Tags.Series
		if file.Tags.SeriesPart != "" {
			series += " #" + file.Tags.SeriesPart
		}
		fmt.Printf("Series:      %s\n", series)
	}

	// Print technical audio information
	fmt.Println("\nAudio Info:")
	fmt.Println("───────────")
	fmt.Printf("Codec:       %s\n", file.Audio.Codec)
	fmt.Printf("Duration:    %s\n", file.Audio.Duration)
	if file.Audio.SampleRate > 0 {
		fmt.Printf("Sample Rate: %d Hz\n", file.Audio.SampleRate)
	}
	if file.Audio.BitDepth > 0 {
		fmt.Printf("Bit Depth:   %d bits\n", file.Audio.BitDepth)
	}
	if file.Audio.Channels > 0 {
		fmt.Printf("Channels:    %d\n", file.Audio.Channels)
	}
	if file.Audio.Bitrate > 0 {
		fmt.Printf("Bitrate:     %d kbps", file.Audio.Bitrate/1000)
		if file.Audio.VBR {
			fmt.Print(" (VBR)")
		}
		fmt.Println()
	}
	if file.Audio.Lossless {
		fmt.Println("Quality:     Lossless")
	}
	if file.Audio.IsHighRes() {
		fmt.Println("             High-Resolution")
	}

	// Check for warnings
	if len(file.Warnings) > 0 {
		fmt.Println("\nWarnings:")
		fmt.Println("─────────")
		for _, w := range file.Warnings {
			fmt.Printf("  • %s\n", w.Message)
		}
	}

	// Check for artwork
	artwork, err := file.ExtractArtwork()
	if err != nil {
		log.Printf("Warning: Failed to extract artwork: %v", err)
	} else if len(artwork) > 0 {
		fmt.Printf("\nArtwork: %d image(s) found\n", len(artwork))
		for i, art := range artwork {
			fmt.Printf("  [%d] %s (%dx%d, %d bytes)\n",
				i+1, art.MIMEType, art.Width, art.Height, len(art.Data))
		}
	}
}
