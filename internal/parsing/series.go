// Package parsing provides utilities for parsing and extracting metadata from text.
package parsing

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ExtractSeriesPartFromText extracts series part numbers from text strings.
// Supports fractional positions (0.5), zero-based (Book 0), and large numbers (100+).
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

// - Handles book 0: "0" -> "0".
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

// ParseGrouping extracts series name and part from a grouping tag.
//
// Handles common audiobook grouping formats:
//   - "Series Name #5" → ("Series Name", "5")
//   - "Series Name, Book 5" → ("Series Name", "5")
//   - "Series Name - Book 5" → ("Series Name", "5")
//   - "Series Name, Part 3" → ("Series Name", "3")
//   - "Series Name" → ("Series Name", "")
//
// Returns empty strings if the input is empty.
func ParseGrouping(grouping string) (series, part string) {
	grouping = strings.TrimSpace(grouping)
	if grouping == "" {
		return "", ""
	}

	// Patterns that capture series name and part number
	// Order matters: more specific patterns first
	patterns := []struct {
		re          *regexp.Regexp
		seriesGroup int
		partGroup   int
	}{
		// "Series Name #5" or "Series Name #5.5"
		{regexp.MustCompile(`^(.+?)\s*#(\d+(?:\.\d+)?)\s*$`), 1, 2},
		// "Series Name, Book 5" or "Series Name - Book 5"
		{regexp.MustCompile(`(?i)^(.+?)\s*[,\-–—]\s*book\s+(\d+(?:\.\d+)?)\s*$`), 1, 2},
		// "Series Name, Part 5" or "Series Name - Part 5"
		{regexp.MustCompile(`(?i)^(.+?)\s*[,\-–—]\s*part\s+(\d+(?:\.\d+)?)\s*$`), 1, 2},
		// "Series Name, Vol 5" or "Series Name - Volume 5"
		{regexp.MustCompile(`(?i)^(.+?)\s*[,\-–—]\s*vol(?:ume)?\.?\s+(\d+(?:\.\d+)?)\s*$`), 1, 2},
	}

	for _, p := range patterns {
		if matches := p.re.FindStringSubmatch(grouping); matches != nil {
			series = strings.TrimSpace(matches[p.seriesGroup])
			part = normalizeSeriesPart(matches[p.partGroup])
			return series, part
		}
	}

	// No part number found - treat entire string as series name
	return grouping, ""
}

// ExtractSeriesPartFromPath extracts series part numbers from file paths.
// Example: "/audiobooks/Author/Series/2 - North or Be Eaten/file.m4b" → "2".
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
