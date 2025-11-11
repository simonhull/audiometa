package parsing

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ExtractSeriesPartFromText attempts to extract series position from text fields
// Handles patterns like: "Book 2", "Part 2", "#2", "Vol. 2", "2 -"
// Supports fractional positions (0.5), zero-based (Book 0), and large numbers (100+)
func ExtractSeriesPartFromText(text string) string {
	if text == "" {
		return ""
	}

	// Pattern list in priority order
	// Note: Removed written number support (one, two, three...) - too brittle
	// Focus on reliable digit parsing instead
	patterns := []string{
		`(?i)book\s+(\d+(?:\.\d+)?)`,           // "Book 2", "Book 0.5", "Book 100"
		`(?i)part\s+(\d+(?:\.\d+)?)`,           // "Part 2", "Part 1.5"
		`(?i)vol(?:ume)?\.?\s+(\d+(?:\.\d+)?)`, // "Vol 2", "Volume 2.5"
		`#(\d+(?:\.\d+)?)`,                     // "#2", "#0.5", "#100"
		`^(\d+(?:\.\d+)?)\s*[-–—:]`,            // "2 -", "0.5 -", "100:", "2 –", "2 —"
		`[-–—:]\s*book\s+(\d+(?:\.\d+)?)`,      // "- Book 2"
		`\((\d+(?:\.\d+)?)\)`,                  // "(2)", "(1.5)", "(0)"
		`^(\d+(?:\.\d+)?)$`,                    // "3", "0.5", "0", "150" (standalone number)
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		if matches := re.FindStringSubmatch(text); len(matches) > 1 {
			part := matches[1]
			return normalizeSeriesPart(part)
		}
	}

	return ""
}

// normalizeSeriesPart normalizes the series part string
// - Removes leading zeros from whole numbers: "01" -> "1"
// - Normalizes decimals: "01.5" -> "1.5", "00.5" -> "0.5"
// - Handles book 0: "0" -> "0"
func normalizeSeriesPart(part string) string {
	if part == "" {
		return ""
	}

	// If it contains a decimal point, parse as float to normalize
	if strings.Contains(part, ".") {
		if num, err := strconv.ParseFloat(part, 64); err == nil {
			// Format to remove leading zeros but keep decimal precision
			// Use %g to avoid trailing zeros (1.5 not 1.50)
			return strconv.FormatFloat(num, 'f', -1, 64)
		}
		// Fallback: return as-is if parsing fails
		return part
	}

	// Parse as integer to remove leading zeros
	// This handles "01" -> "1", "001" -> "1", but keeps "0" as "0"
	if num, err := strconv.Atoi(part); err == nil {
		return fmt.Sprintf("%d", num)
	}

	// Fallback: return as-is if parsing fails
	return part
}

// IsLikelySeriesPosition determines if track number represents series position
// versus chapter/file position using improved heuristics
func IsLikelySeriesPosition(trackNum, trackTotal int) bool {
	// Invalid input
	if trackNum == 0 || trackTotal == 0 {
		return false
	}

	// Invalid track number (out of bounds)
	if trackNum > trackTotal {
		return false
	}

	// Special case: Track 1/1 is highly ambiguous
	// Most M4B files are single-file audiobooks with track 1/1, but this doesn't
	// indicate series position. Prefer to fall through to other methods (title, album, path)
	if trackNum == 1 && trackTotal == 1 {
		return false
	}

	// Heuristic 1: Very small totals (≤ 10) are likely series
	// Examples: 3-book trilogy, 5-book series
	if trackTotal <= 10 {
		return true
	}

	// Heuristic 2: Medium totals (11-30) could be either
	// Use ratio: if track number is low relative to total, likely chapters
	// Examples:
	// - Book 2 of 15 = likely series (ratio: 0.13)
	// - Chapter 2 of 25 = likely chapters (ratio: 0.08)
	if trackTotal <= 30 {
		ratio := float64(trackNum) / float64(trackTotal)
		// If we're past 1/3 of the total, more likely to be a series
		return ratio > 0.33 || trackNum == 1
	}

	// Heuristic 3: Large totals (31-100) - could be large series
	// Examples: Discworld (41 books), Horus Heresy (54+ books)
	// Use distribution heuristic: chapters are usually evenly distributed
	// while series books can have any position
	if trackTotal <= 100 {
		// If it's near the beginning or end, more likely chapters
		// If it's a middle position, check ratio
		if trackNum <= 3 || trackNum >= trackTotal-3 {
			// Near edges - ambiguous, default to false (safer)
			return false
		}
		// Middle positions with large totals are likely chapters
		return false
	}

	// Heuristic 4: Very large totals (> 100) are almost certainly chapters
	// No audiobook series has 100+ books in a single M4B with track metadata
	// (They would be separate files)
	return false
}

// ExtractSeriesPartFromPath attempts to extract series position from the file path
// by examining the immediate parent directory name
// Example: "/audiobooks/Author/Series/2 - North or Be Eaten/file.m4b" → "2"
func ExtractSeriesPartFromPath(path string) string {
	if path == "" {
		return ""
	}

	// Get the directory containing the file
	dir := filepath.Dir(path)

	// Extract the last component of the directory path (the immediate parent)
	dirName := filepath.Base(dir)

	// Extract series part from directory name using text patterns
	return ExtractSeriesPartFromText(dirName)
}
