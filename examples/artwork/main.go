// Package main demonstrates artwork extraction from audio files.
//
// This example shows how to:
//   - Extract embedded artwork from audio files
//   - Save artwork to disk
//   - Handle multiple artwork images
//   - Detect artwork types (front cover, back cover, etc.)
//
// Usage:
//
//	go run main.go <audio_file> [output_dir]
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/simonhull/audiometa"
	_ "github.com/simonhull/audiometa/internal/flac"
	_ "github.com/simonhull/audiometa/internal/m4a"
	_ "github.com/simonhull/audiometa/internal/mp3"
	_ "github.com/simonhull/audiometa/internal/ogg"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: artwork <audio_file> [output_dir]")
		fmt.Println("\nExtracts and saves embedded artwork from audio files.")
		fmt.Println("\nExamples:")
		fmt.Println("  artwork album.flac")
		fmt.Println("  artwork audiobook.m4b ./covers")
		os.Exit(1)
	}

	audioPath := os.Args[1]

	// Default output directory is current directory
	outputDir := "."
	if len(os.Args) > 2 {
		outputDir = os.Args[2]
	}

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		log.Fatalf("Failed to create output directory: %v", err)
	}

	// Open the audio file
	fmt.Printf("Opening: %s\n", audioPath)
	file, err := audiometa.Open(audioPath)
	if err != nil {
		log.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	// Extract artwork
	fmt.Println("Extracting artwork...")
	artwork, err := file.ExtractArtwork()
	if err != nil {
		log.Fatalf("Failed to extract artwork: %v", err)
	}

	if len(artwork) == 0 {
		fmt.Println("No artwork found in file.")
		return
	}

	fmt.Printf("Found %d artwork image(s)\n\n", len(artwork))

	// Base name for output files (without extension)
	baseName := strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))

	// Save each artwork image
	for i, art := range artwork {
		// Determine file extension from MIME type
		ext := getExtensionFromMIME(art.MIMEType)
		if ext == "" {
			log.Printf("Warning: Unknown MIME type '%s', using .bin", art.MIMEType)
			ext = ".bin"
		}

		// Create descriptive filename
		var filename string
		if len(artwork) == 1 {
			// Single artwork - simple name
			filename = fmt.Sprintf("%s%s", baseName, ext)
		} else {
			// Multiple artworks - include type and index
			typeStr := strings.ToLower(strings.ReplaceAll(art.Type.String(), " ", "_"))
			filename = fmt.Sprintf("%s_%d_%s%s", baseName, i+1, typeStr, ext)
		}

		outputPath := filepath.Join(outputDir, filename)

		// Save artwork
		if err := os.WriteFile(outputPath, art.Data, 0644); err != nil {
			log.Printf("Warning: Failed to save %s: %v", outputPath, err)
			continue
		}

		// Print info
		fmt.Printf("[%d] Saved: %s\n", i+1, outputPath)
		fmt.Printf("    Type:        %s\n", art.Type)
		fmt.Printf("    MIME Type:   %s\n", art.MIMEType)
		if art.Width > 0 && art.Height > 0 {
			fmt.Printf("    Dimensions:  %dx%d\n", art.Width, art.Height)
		}
		fmt.Printf("    Size:        %d bytes (%.1f KB)\n", len(art.Data), float64(len(art.Data))/1024)
		if art.Description != "" {
			fmt.Printf("    Description: %s\n", art.Description)
		}
		fmt.Println()
	}

	fmt.Printf("✓ Successfully extracted %d artwork image(s) to %s\n", len(artwork), outputDir)
}

// getExtensionFromMIME returns the file extension for a MIME type.
func getExtensionFromMIME(mimeType string) string {
	switch strings.ToLower(mimeType) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/bmp":
		return ".bmp"
	case "image/webp":
		return ".webp"
	case "image/tiff":
		return ".tiff"
	default:
		// Try to extract from "image/xxx" pattern
		if strings.HasPrefix(mimeType, "image/") {
			return "." + strings.TrimPrefix(mimeType, "image/")
		}
		return ""
	}
}
