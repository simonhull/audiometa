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
// Ogg Vorbis and Opus support chapters via special comments:
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
	// Map to collect chapter data by chapter number
	type chapterData struct {
		number    int
		timestamp string
		title     string
	}

	chaptersMap := make(map[int]*chapterData)

	// Scan all comments for CHAPTERxxx tags
	for _, comment := range comments {
		// Find '=' separator
		eq := strings.IndexByte(comment, '=')
		if eq < 0 {
			continue
		}

		key := strings.ToUpper(strings.TrimSpace(comment[:eq]))
		value := strings.TrimSpace(comment[eq+1:])

		if !strings.HasPrefix(key, "CHAPTER") {
			continue
		}

		if strings.HasSuffix(key, "NAME") {
			// CHAPTERxxxNAME - extract chapter number
			numStr := strings.TrimSuffix(strings.TrimPrefix(key, "CHAPTER"), "NAME")
			num, err := strconv.Atoi(numStr)
			if err != nil {
				continue // Invalid chapter number
			}

			if chaptersMap[num] == nil {
				chaptersMap[num] = &chapterData{number: num}
			}
			chaptersMap[num].title = value

		} else {
			// CHAPTERxxx - extract chapter number and timestamp
			numStr := strings.TrimPrefix(key, "CHAPTER")
			num, err := strconv.Atoi(numStr)
			if err != nil {
				continue // Invalid chapter number
			}

			if chaptersMap[num] == nil {
				chaptersMap[num] = &chapterData{number: num}
			}
			chaptersMap[num].timestamp = value
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
	chapters := make([]types.Chapter, len(chapterList))
	for i, chap := range chapterList {
		startTime, err := parseChapterTimestamp(chap.timestamp)
		if err != nil {
			// Invalid timestamp - skip this chapter
			continue
		}

		// Calculate end time
		var endTime time.Duration
		if i < len(chapterList)-1 {
			// End at start of next chapter
			endTime, _ = parseChapterTimestamp(chapterList[i+1].timestamp)
		} else if fileDuration > 0 {
			// Last chapter: use file duration
			endTime = fileDuration
		}
		// If no duration and last chapter, endTime stays 0

		title := chap.title
		if title == "" {
			title = fmt.Sprintf("Chapter %d", chap.number)
		}

		chapters[i] = types.Chapter{
			Index:     i + 1,
			Title:     title,
			StartTime: startTime,
			EndTime:   endTime,
		}
	}

	return chapters
}

// parseChapterTimestamp parses chapter timestamps in various formats:
//   - HH:MM:SS.mmm (hours:minutes:seconds.milliseconds)
//   - MM:SS.mmm (minutes:seconds.milliseconds)
//   - SS.mmm (seconds.milliseconds)
//
// Returns the duration or an error if the format is invalid.
func parseChapterTimestamp(ts string) (time.Duration, error) {
	// Split by ':'
	parts := strings.Split(ts, ":")

	var hours, minutes int
	var seconds float64
	var err error

	switch len(parts) {
	case 3:
		// HH:MM:SS.mmm
		hours, err = strconv.Atoi(parts[0])
		if err != nil {
			return 0, fmt.Errorf("invalid hours in timestamp: %s", ts)
		}
		minutes, err = strconv.Atoi(parts[1])
		if err != nil {
			return 0, fmt.Errorf("invalid minutes in timestamp: %s", ts)
		}
		seconds, err = strconv.ParseFloat(parts[2], 64)
		if err != nil {
			return 0, fmt.Errorf("invalid seconds in timestamp: %s", ts)
		}

	case 2:
		// MM:SS.mmm
		minutes, err = strconv.Atoi(parts[0])
		if err != nil {
			return 0, fmt.Errorf("invalid minutes in timestamp: %s", ts)
		}
		seconds, err = strconv.ParseFloat(parts[1], 64)
		if err != nil {
			return 0, fmt.Errorf("invalid seconds in timestamp: %s", ts)
		}

	case 1:
		// SS.mmm
		seconds, err = strconv.ParseFloat(parts[0], 64)
		if err != nil {
			return 0, fmt.Errorf("invalid seconds in timestamp: %s", ts)
		}

	default:
		return 0, fmt.Errorf("invalid timestamp format: %s", ts)
	}

	// Validate ranges
	if hours < 0 || minutes < 0 || minutes >= 60 || seconds < 0 || seconds >= 60 {
		return 0, fmt.Errorf("timestamp values out of range: %s", ts)
	}

	// Convert to duration
	totalSeconds := float64(hours*3600+minutes*60) + seconds
	return time.Duration(totalSeconds * float64(time.Second)), nil
}
