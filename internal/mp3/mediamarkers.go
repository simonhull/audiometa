package mp3

import (
	"encoding/xml"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/simonhull/audiometa/internal/types"
)

// mediaMarkersDescription is the TXXX description OverDrive/Libby use for chapter markers.
const mediaMarkersDescription = "overdrive mediamarkers" // compared lower-cased

// maxMediaMarkers bounds work on hostile input.
const maxMediaMarkers = 10_000

type mediaMarkersXML struct {
	XMLName xml.Name `xml:"Markers"`
	Markers []struct {
		Name string `xml:"Name"`
		Time string `xml:"Time"`
	} `xml:"Marker"`
}

// parseMediaMarkers converts an OverDrive MediaMarkers XML document into
// chapters with file-relative start times. EndTime of each chapter is the next
// chapter's start; the last chapter's EndTime is left 0 for the caller to fill.
// Any malformed marker fails the whole document.
func parseMediaMarkers(doc string) ([]types.Chapter, error) {
	var parsed mediaMarkersXML
	if err := xml.Unmarshal([]byte(strings.TrimRight(doc, "\x00")), &parsed); err != nil {
		return nil, fmt.Errorf("parse MediaMarkers XML: %w", err)
	}
	if len(parsed.Markers) > maxMediaMarkers {
		return nil, fmt.Errorf("too many markers (%d, max %d)", len(parsed.Markers), maxMediaMarkers)
	}
	if len(parsed.Markers) == 0 {
		return nil, nil
	}

	chapters := make([]types.Chapter, len(parsed.Markers))
	for i, m := range parsed.Markers {
		start, err := parseMarkerTime(m.Time)
		if err != nil {
			return nil, err
		}
		if i > 0 && start < chapters[i-1].StartTime {
			return nil, fmt.Errorf("marker %d time %q precedes the previous marker", i+1, m.Time)
		}
		title := strings.TrimSpace(m.Name)
		if title == "" {
			title = fmt.Sprintf("Chapter %d", i+1)
		}
		chapters[i] = types.Chapter{
			Title:     title,
			Index:     i + 1,
			StartTime: start,
			Source:    types.ChapterSourceOverDrive,
		}
	}
	types.FillChapterEndTimes(chapters, 0)
	return chapters, nil
}

// parseMarkerTime parses "M:SS[.f]" or "H:MM:SS[.f]" (1-3 fraction digits)
// using integer arithmetic only. Minutes (or hours) may exceed 59.
func parseMarkerTime(s string) (time.Duration, error) {
	bad := func() (time.Duration, error) { return 0, fmt.Errorf("invalid marker time %q", s) }

	parts := strings.Split(strings.TrimSpace(s), ":")
	var big, mins int // leading component, and minutes for the 3-part form
	var secField string
	var err error
	switch len(parts) {
	case 2:
		if big, err = parseDigits(parts[0], 0); err != nil {
			return bad()
		}
		secField = parts[1]
	case 3:
		if big, err = parseDigits(parts[0], 0); err != nil {
			return bad()
		}
		if mins, err = parseDigits(parts[1], 2); err != nil || mins > 59 {
			return bad()
		}
		secField = parts[2]
	default:
		return bad()
	}

	secStr, frac, hasFrac := strings.Cut(secField, ".")
	secs, err := parseDigits(secStr, 2)
	if err != nil || secs > 59 {
		return bad()
	}
	var millis int
	if hasFrac {
		if len(frac) < 1 || len(frac) > 3 {
			return bad()
		}
		if millis, err = parseDigits(frac, len(frac)); err != nil {
			return bad()
		}
		for range 3 - len(frac) {
			millis *= 10
		}
	}

	total := time.Duration(secs)*time.Second + time.Duration(millis)*time.Millisecond
	if len(parts) == 2 {
		total += time.Duration(big) * time.Minute
	} else {
		total += time.Duration(big)*time.Hour + time.Duration(mins)*time.Minute
	}
	return total, nil
}

var errNotDigits = errors.New("not a digit string")

// parseDigits parses a non-empty string of ASCII digits; width > 0 requires
// exactly that many digits. Values that overflow a time-safe range are rejected.
func parseDigits(s string, width int) (int, error) {
	if s == "" || (width > 0 && len(s) != width) || len(s) > 9 {
		return 0, errNotDigits
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errNotDigits
		}
	}
	return strconv.Atoi(s)
}
