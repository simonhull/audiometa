// Package parsing holds metadata heuristics shared by the format parsers:
// series name and part extraction from grouping tags, titles and paths, and
// the narrator fallback.
package parsing

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// seriesPartPatterns lists the series-part patterns in priority order.
// Note: written numbers (one, two, three...) are deliberately unsupported -
// too brittle; digit parsing is reliable.
var seriesPartPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)book\s+(\d+(?:\.\d+)?)`),           // "Book 2", "Book 0.5", "Book 100"
	regexp.MustCompile(`(?i)part\s+(\d+(?:\.\d+)?)`),           // "Part 2", "Part 1.5"
	regexp.MustCompile(`(?i)vol(?:ume)?\.?\s+(\d+(?:\.\d+)?)`), // "Vol 2", "Volume 2.5"
	regexp.MustCompile(`#(\d+(?:\.\d+)?)`),                     // "#2", "#0.5", "#100"
	regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*[-–—:]`),            // "2 -", "0.5 -", "100:", "2 –", "2 —"
	regexp.MustCompile(`[-–—:]\s*book\s+(\d+(?:\.\d+)?)`),      // "- Book 2"
	regexp.MustCompile(`\((\d+(?:\.\d+)?)\)`),                  // "(2)", "(1.5)", "(0)"
	regexp.MustCompile(`\[(\d+(?:\.\d+)?)\]`),                  // "[2]", "[01]", "[1.5]" (bracket notation)
	regexp.MustCompile(`^(\d+(?:\.\d+)?)$`),                    // "3", "0.5", "0", "150" (standalone number)
}

// ExtractSeriesPartFromText extracts series part numbers from text strings.
// Supports fractional positions (0.5), zero-based (Book 0), and large numbers (100+).
func ExtractSeriesPartFromText(text string) string {
	if text == "" {
		return ""
	}

	for _, re := range seriesPartPatterns {
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
		return strconv.Itoa(num)
	}

	// Fallback: return as-is if parsing fails
	return part
}

// groupingPatterns capture series name and part number. Order matters: more
// specific patterns first.
var groupingPatterns = []struct {
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

	for _, p := range groupingPatterns {
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
