package vorbis

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/simonhull/audiometa/internal/types"
)

// ParseChapters extracts chapters from Vorbis CHAPTER comments.
//
// FLAC, Ogg Vorbis and Opus support chapters via special comments:
//
//	CHAPTERxxx=HH:MM:SS.mmm
//	CHAPTERxxxNAME=Title
//
// Where xxx is a zero-padded chapter number (e.g., 001, 002, 010, 100).
//
// Example:
//
//	CHAPTER001=00:00:00.000
//	CHAPTER001NAME=Introduction
//	CHAPTER002=00:05:23.500
//	CHAPTER002NAME=Chapter 1: The Beginning
func ParseChapters(comments []string, fileDuration time.Duration) []types.Chapter {
	// Collect chapter data by chapter number.
	chaptersMap := make(map[int]*chapterData)
	for _, comment := range comments {
		num, isName, value, ok := chapterTag(comment)
		if !ok {
			continue
		}
		chap := chaptersMap[num]
		if chap == nil {
			chap = &chapterData{number: num}
			chaptersMap[num] = chap
		}
		if isName {
			chap.title = value
		} else {
			chap.timestamp = value
		}
	}

	if len(chaptersMap) == 0 {
		return nil
	}

	// Convert map to sorted slice
	var chapterList []chapterData
	for _, chap := range chaptersMap {
		if chap.timestamp != "" { // Must have timestamp
			chapterList = append(chapterList, *chap)
		}
	}

	if len(chapterList) == 0 {
		return nil
	}

	// Sort by chapter number
	slices.SortFunc(chapterList, func(a, b chapterData) int {
		return cmp.Compare(a.number, b.number)
	})

	// Convert to types.Chapter
	chapters := make([]types.Chapter, 0, len(chapterList))
	for _, chap := range chapterList {
		startTime, err := parseChapterTimestamp(chap.timestamp)
		if err != nil {
			continue // Invalid timestamp: drop the chapter entirely
		}
		title := chap.title
		if title == "" {
			title = fmt.Sprintf("Chapter %d", chap.number)
		}
		chapters = append(chapters, types.Chapter{
			Index:     len(chapters) + 1,
			Title:     title,
			StartTime: startTime,
			Source:    types.ChapterSourceVorbisComment,
		})
	}
	if len(chapters) == 0 {
		return nil
	}

	// End each chapter at the next one's start; the last at the file duration (0 if unknown).
	types.FillChapterEndTimes(chapters, fileDuration)
	return chapters
}

// chapterData is what the comments say about one chapter number.
type chapterData struct {
	timestamp string
	title     string
	number    int
}

// chapterTag reads a CHAPTERxxx (a start time) or CHAPTERxxxNAME (a title) comment. It
// reports ok false for any other comment and for a chapter number that is not an integer.
func chapterTag(comment string) (int, bool, string, bool) {
	before, after, found := strings.Cut(comment, "=")
	if !found {
		return 0, false, "", false
	}

	// Most comments are not chapters: reject them before upper-casing.
	key := strings.TrimSpace(before)
	if len(key) < len("CHAPTER") || !strings.EqualFold(key[:len("CHAPTER")], "CHAPTER") {
		return 0, false, "", false
	}
	key = strings.ToUpper(key)

	numStr := strings.TrimPrefix(key, "CHAPTER")
	isName := strings.HasSuffix(key, "NAME")
	if isName {
		numStr = strings.TrimSuffix(numStr, "NAME")
	}
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, false, "", false // Invalid chapter number
	}
	return num, isName, strings.TrimSpace(after), true
}

// maxChapterHours caps the hours of a chapter time so the sum cannot overflow
// a [time.Duration] (about 11 years).
const maxChapterHours = 100_000

// parseChapterTimestamp parses a chapter time with integer arithmetic only.
// The accepted forms are:
//   - HH:MM:SS[.fff] (hours, minutes, seconds)
//   - MM:SS[.fff] (minutes, seconds)
//   - SS[.fff] (seconds)
//
// Every component is ASCII digits only: no sign, exponent, hex, NaN or Inf.
// Hours run from 0 to 100000; minutes and seconds must be below 60. The
// fraction may have any number of digits; those beyond nine (nanoseconds) are
// truncated. The integer part of the seconds needs at least one digit.
func parseChapterTimestamp(ts string) (time.Duration, error) {
	parts := strings.Split(ts, ":")
	if len(parts) > 3 {
		return 0, fmt.Errorf("invalid timestamp format: %s", ts)
	}

	whole, frac, _ := strings.Cut(parts[len(parts)-1], ".")
	seconds, ok := chapterDigits(whole)
	if !ok || seconds >= 60 || !isASCIIDigits(frac) {
		return 0, fmt.Errorf("invalid seconds in timestamp: %s", ts)
	}
	total := time.Duration(seconds) * time.Second
	if len(frac) > 9 {
		frac = frac[:9]
	}
	if frac != "" {
		nanos, _ := chapterDigits(frac)
		for range 9 - len(frac) {
			nanos *= 10
		}
		total += time.Duration(nanos)
	}

	if len(parts) >= 2 {
		minutes, ok := chapterDigits(parts[len(parts)-2])
		if !ok || minutes >= 60 {
			return 0, fmt.Errorf("invalid minutes in timestamp: %s", ts)
		}
		total += time.Duration(minutes) * time.Minute
	}
	if len(parts) == 3 {
		hours, ok := chapterDigits(parts[0])
		if !ok || hours > maxChapterHours {
			return 0, fmt.Errorf("invalid hours in timestamp: %s", ts)
		}
		total += time.Duration(hours) * time.Hour
	}
	return total, nil
}

// chapterDigits parses 1 to 9 ASCII digits.
func chapterDigits(s string) (int, bool) {
	if s == "" || len(s) > 9 || !isASCIIDigits(s) {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

// isASCIIDigits reports whether s holds only the digits 0-9 (true for "").
func isASCIIDigits(s string) bool {
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
