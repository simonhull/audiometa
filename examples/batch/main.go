// Package main demonstrates batch processing of audio files.
//
// This example shows how to:
//   - Process multiple files concurrently using OpenMany
//   - Handle errors gracefully
//   - Report progress and statistics
//   - Use context for cancellation
//
// Usage:
//
//	go run main.go <audio_files...>
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/simonhull/audiometa"
	_ "github.com/simonhull/audiometa/internal/flac"
	_ "github.com/simonhull/audiometa/internal/m4a"
	_ "github.com/simonhull/audiometa/internal/mp3"
	_ "github.com/simonhull/audiometa/internal/ogg"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: batch <audio_files...>")
		fmt.Println("\nExample:")
		fmt.Println("  batch *.flac")
		fmt.Println("  batch ~/Music/**/*.mp3")
		os.Exit(1)
	}

	paths := os.Args[1:]

	fmt.Printf("Processing %d files...\n\n", len(paths))

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start timer
	start := time.Now()

	// Open all files concurrently
	files, err := audiometa.OpenMany(ctx, paths...)
	if err != nil {
		log.Fatalf("Failed to process files: %v", err)
	}

	// Ensure all files are closed when done
	defer func() {
		for _, f := range files {
			f.Close()
		}
	}()

	elapsed := time.Since(start)

	// Print results
	fmt.Println("Results:")
	fmt.Println("════════════════════════════════════════════════════════")

	var (
		totalDuration time.Duration
		totalSize     int64
		formatCounts  = make(map[audiometa.Format]int)
		withArtwork   int
		withWarnings  int
	)

	for i, file := range files {
		fmt.Printf("\n[%d] %s\n", i+1, file.Path)
		fmt.Printf("    Format:   %s\n", file.Format)
		fmt.Printf("    Tags:     %s - %s\n", file.Tags.Artist, file.Tags.Title)
		fmt.Printf("    Audio:    %s\n", file.Audio)
		fmt.Printf("    Duration: %s\n", file.Audio.Duration)

		// Check for artwork
		artwork, err := file.ExtractArtwork()
		if err == nil && len(artwork) > 0 {
			fmt.Printf("    Artwork:  %d image(s)\n", len(artwork))
			withArtwork++
		}

		// Check for warnings
		if len(file.Warnings) > 0 {
			fmt.Printf("    Warnings: %d issue(s)\n", len(file.Warnings))
			for _, w := range file.Warnings {
				fmt.Printf("      - %s\n", w.Message)
			}
			withWarnings++
		}

		// Accumulate statistics
		totalDuration += file.Audio.Duration
		totalSize += file.Size
		formatCounts[file.Format]++
	}

	// Print statistics
	fmt.Println("\n════════════════════════════════════════════════════════")
	fmt.Println("Statistics:")
	fmt.Println("────────────────────────────────────────────────────────")
	fmt.Printf("Files Processed:   %d\n", len(files))
	fmt.Printf("Total Duration:    %s\n", totalDuration)
	fmt.Printf("Total Size:        %.2f MB\n", float64(totalSize)/(1024*1024))
	fmt.Printf("With Artwork:      %d (%.0f%%)\n", withArtwork, float64(withArtwork)/float64(len(files))*100)
	if withWarnings > 0 {
		fmt.Printf("With Warnings:     %d (%.0f%%)\n", withWarnings, float64(withWarnings)/float64(len(files))*100)
	}

	// Format breakdown
	if len(formatCounts) > 0 {
		fmt.Println("\nFormat Breakdown:")
		for format, count := range formatCounts {
			fmt.Printf("  %s: %d (%.0f%%)\n", format, count, float64(count)/float64(len(files))*100)
		}
	}

	// Performance metrics
	fmt.Println("\nPerformance:")
	fmt.Println("────────────────────────────────────────────────────────")
	fmt.Printf("Total Time:        %s\n", elapsed)
	fmt.Printf("Average per File:  %s\n", elapsed/time.Duration(len(files)))
	fmt.Printf("Files per Second:  %.1f\n", float64(len(files))/elapsed.Seconds())
}
