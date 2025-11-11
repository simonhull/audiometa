// Package main demonstrates chapter extraction from audio files.
//
// This example shows how to:
//   - Extract chapters from audio files (all formats)
//   - Display chapter information (index, title, timestamps)
//   - Handle files without chapters gracefully
//   - Calculate chapter durations
//
// Supported formats:
//   - MP3: ID3v2 CHAP frames
//   - M4A/M4B: QuickTime chapter tracks, Nero CHPL format
//   - FLAC: CUESHEET metadata block
//   - Ogg Vorbis/Opus: CHAPTER Vorbis comments
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
		fmt.Println("Usage: chapters <audio_file>")
		fmt.Println("\nExtracts and displays chapter markers from audio files.")
		fmt.Println("\nSupported formats:")
		fmt.Println("  MP3:        ID3v2 CHAP frames")
		fmt.Println("  M4A/M4B:    QuickTime chapter tracks, Nero CHPL")
		fmt.Println("  FLAC:       CUESHEET metadata block")
		fmt.Println("  Ogg/Opus:   CHAPTER Vorbis comments")
		fmt.Println("\nExamples:")
		fmt.Println("  chapters audiobook.mp3")
		fmt.Println("  chapters audiobook.m4b")
		fmt.Println("  chapters album.flac")
		fmt.Println("  chapters podcast.ogg")
		os.Exit(1)
	}

	audioPath := os.Args[1]

	// Open the audio file
	fmt.Printf("Opening: %s\n", audioPath)
	file, err := audiometa.Open(audioPath)
	if err != nil {
		log.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	// Display file information
	fmt.Printf("Format:   %s\n", file.Format)
	fmt.Printf("Duration: %s\n\n", file.Audio.Duration)

	// Check for chapters
	if len(file.Chapters) == 0 {
		fmt.Println("No chapters found in this file.")
		return
	}

	fmt.Printf("Found %d chapter(s):\n", len(file.Chapters))
	fmt.Println("════════════════════════════════════════════════════════════════════")

	// Display each chapter
	for _, chapter := range file.Chapters {
		// Calculate chapter duration
		duration := chapter.EndTime - chapter.StartTime

		fmt.Printf("\n[%d] %s\n", chapter.Index, chapter.Title)
		fmt.Printf("    Start:    %s\n", formatDuration(chapter.StartTime))
		fmt.Printf("    End:      %s\n", formatDuration(chapter.EndTime))
		fmt.Printf("    Duration: %s\n", formatDuration(duration))
	}

	fmt.Println("\n════════════════════════════════════════════════════════════════════")
	fmt.Printf("Total: %d chapters\n", len(file.Chapters))

	// Check for warnings
	if len(file.Warnings) > 0 {
		fmt.Println("\nWarnings:")
		for _, w := range file.Warnings {
			fmt.Printf("  • %s\n", w.Message)
		}
	}
}

// formatDuration formats a duration in a human-readable format
func formatDuration(d fmt.Stringer) string {
	return d.String()
}
