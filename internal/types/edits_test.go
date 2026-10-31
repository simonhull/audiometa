package types

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestDiff(t *testing.T) {
	mp3Orig := func() *Tags {
		o := boundTags(FormatMP3)
		o.Set("TIT2", "Orig")
		o.Set("TPE1", "Artist")
		o.Set("TCOM", "Composer")
		o.Set("TXXX-ONLY", "keep")
		return o
	}
	vorbisOrig := func() *Tags {
		o := boundTags(FormatFLAC)
		o.Set("TITLE", "Orig")
		o.Set("DATE", "2000-01-01")
		return o
	}

	tests := []struct {
		name     string
		format   Format
		orig     func() *Tags
		mutate   func(cur *Tags)
		wantKeys []KeyEdit
		wantFlds []FieldEdit
		conflict bool
	}{
		{"no change", FormatMP3, mp3Orig, func(*Tags) {}, nil, nil, false},
		{"raw set", FormatMP3, mp3Orig, func(c *Tags) { c.Set("TIT2", "x") },
			[]KeyEdit{{"TIT2", []string{"x"}}}, nil, false},
		{"typed set", FormatMP3, mp3Orig, func(c *Tags) { c.Title = "x" },
			nil, []FieldEdit{{FieldTitle, []string{"x"}}}, false},
		{"raw and typed disagree", FormatMP3, mp3Orig, func(c *Tags) { c.Set("TIT2", "x"); c.Title = "y" },
			nil, nil, true},
		{"raw and typed agree", FormatMP3, mp3Orig, func(c *Tags) { c.Set("TIT2", "x"); c.Title = "x" },
			[]KeyEdit{{"TIT2", []string{"x"}}}, nil, false},
		{"delete key", FormatMP3, mp3Orig, func(c *Tags) { c.Delete("TXXX-ONLY") },
			[]KeyEdit{{"TXXX-ONLY", nil}}, nil, false},
		{"delete bound key", FormatMP3, mp3Orig, func(c *Tags) { c.Delete("TIT2") },
			[]KeyEdit{{"TIT2", nil}}, nil, false},
		{"clear typed field", FormatMP3, mp3Orig, func(c *Tags) { c.Title = "" },
			nil, []FieldEdit{{FieldTitle, nil}}, false},
		{"artist vs artists", FormatMP3, mp3Orig, func(c *Tags) { c.Artist = "A"; c.Artists = []string{"B"} },
			nil, nil, true},
		{"artist and artists agree", FormatMP3, mp3Orig, func(c *Tags) { c.Artist = "A"; c.Artists = []string{"A", "B"} },
			nil, []FieldEdit{{FieldArtist, []string{"A"}}, {FieldArtists, []string{"A", "B"}}}, false},
		{"year vs date", FormatFLAC, vorbisOrig, func(c *Tags) { c.Year = 1999; c.Date = "2001-01-01" },
			nil, nil, true},
		{"year and date agree", FormatFLAC, vorbisOrig, func(c *Tags) { c.Year = 1999; c.Date = "1999-02-03" },
			nil, []FieldEdit{{FieldYear, []string{"1999"}}, {FieldDate, []string{"1999-02-03"}}}, false},
		{"narrator only, no inference", FormatMP3, mp3Orig, func(c *Tags) { c.Narrator = "N" },
			nil, []FieldEdit{{FieldNarrator, []string{"N"}}}, false},
		{"unbound cur raw set", FormatMP3, mp3Orig, func(c *Tags) { BindFormat(c, FormatUnknown); c.Set("TIT2", "x") },
			nil, nil, true}, // raw says x, typed Title still Orig: ambiguous
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			orig := tc.orig()
			cur := orig.Clone()
			tc.mutate(cur)
			keys, fields, err := Diff(orig, cur, tc.format)
			if tc.conflict {
				if !errors.Is(err, ErrConflictingEdits) {
					t.Fatalf("err = %v, want ErrConflictingEdits", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Diff: %v", err)
			}
			if !slices.EqualFunc(keys, tc.wantKeys, func(a, b KeyEdit) bool {
				return a.Key == b.Key && slices.Equal(a.Values, b.Values) && (a.Values == nil) == (b.Values == nil)
			}) {
				t.Errorf("keys = %+v, want %+v", keys, tc.wantKeys)
			}
			if !slices.EqualFunc(fields, tc.wantFlds, func(a, b FieldEdit) bool {
				return a.Field == b.Field && slices.Equal(a.Values, b.Values)
			}) {
				t.Errorf("fields = %+v, want %+v", fields, tc.wantFlds)
			}
		})
	}
}

func TestDiff_KeysSorted(t *testing.T) {
	for range 20 {
		orig := boundTags(FormatFLAC)
		cur := orig.Clone()
		for i := range 30 {
			cur.Set(fmt.Sprintf("K%02d", 29-i), "v")
		}
		keys, _, err := Diff(orig, cur, FormatFLAC)
		if err != nil {
			t.Fatal(err)
		}
		if len(keys) != 30 || !slices.IsSortedFunc(keys, func(a, b KeyEdit) int { return strings.Compare(a.Key, b.Key) }) {
			t.Fatalf("keys not sorted: %v", keys)
		}
	}
}

func TestEditSet_Empty(t *testing.T) {
	if !(&EditSet{}).Empty() {
		t.Error("zero EditSet must be empty")
	}
	if (&EditSet{Artwork: []ArtworkOp{{Kind: ArtworkOpRemoveAll}}}).Empty() {
		t.Error("EditSet with an artwork op is not empty")
	}
}
