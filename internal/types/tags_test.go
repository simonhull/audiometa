package types

import (
	"slices"
	"strings"
	"testing"
)

func TestTags_All(t *testing.T) {
	tags := &Tags{}
	tags.Set("TITLE", "Test Song")
	tags.Set("ARTIST", "Test Artist")
	tags.Set("GENRE", "Rock", "Alternative")

	count := 0
	found := make(map[string]bool)
	for key, values := range tags.All() {
		count++
		found[key] = true
		switch key {
		case "TITLE":
			if len(values) != 1 || values[0] != "Test Song" {
				t.Errorf("TITLE values = %v, want [Test Song]", values)
			}
		case "ARTIST":
			if len(values) != 1 || values[0] != "Test Artist" {
				t.Errorf("ARTIST values = %v, want [Test Artist]", values)
			}
		case "GENRE":
			if len(values) != 2 || values[0] != "Rock" || values[1] != "Alternative" {
				t.Errorf("GENRE values = %v, want [Rock Alternative]", values)
			}
		}
	}

	if count != 3 {
		t.Errorf("All() yielded %d tags, want 3", count)
	}
}

func TestTags_All_NilRaw(t *testing.T) {
	tags := &Tags{}

	count := 0
	for range tags.All() {
		count++
	}

	if count != 0 {
		t.Errorf("All() on nil raw yielded %d tags, want 0", count)
	}
}

func TestTags_Get(t *testing.T) {
	tags := &Tags{}
	tags.Set("ARTIST", "Test Artist")
	tags.Set("GENRE", "Rock", "Pop")

	tests := []struct {
		key  string
		want []string
	}{
		{"ARTIST", []string{"Test Artist"}},
		{"GENRE", []string{"Rock", "Pop"}},
		{"NONEXISTENT", nil},
	}

	for _, tc := range tests {
		got := tags.Get(tc.key)
		if !slices.Equal(got, tc.want) {
			t.Errorf("Get(%q) = %v, want %v", tc.key, got, tc.want)
		}
	}
}

func TestTags_Get_ReturnsClone(t *testing.T) {
	tags := &Tags{}
	tags.Set("GENRE", "Rock", "Pop")

	got := tags.Get("GENRE")
	got[0] = "Modified"

	original := tags.Get("GENRE")
	if original[0] != "Rock" {
		t.Error("Get() should return a clone, but modification affected original")
	}
}

func TestTags_Get_NilRaw(t *testing.T) {
	tags := &Tags{}
	got := tags.Get("ANYTHING")
	if got != nil {
		t.Errorf("Get() on nil raw = %v, want nil", got)
	}
}

func TestTags_GetFirst(t *testing.T) {
	tags := &Tags{}
	tags.Set("ARTIST", "Main Artist")
	tags.Set("GENRE", "Rock", "Pop")

	tests := []struct {
		key  string
		want string
	}{
		{"ARTIST", "Main Artist"},
		{"GENRE", "Rock"},
		{"NONEXISTENT", ""},
	}

	for _, tc := range tests {
		got := tags.GetFirst(tc.key)
		if got != tc.want {
			t.Errorf("GetFirst(%q) = %q, want %q", tc.key, got, tc.want)
		}
	}
}

func TestTags_GetBest(t *testing.T) {
	tags := &Tags{}
	tags.Set("TPE1", "ID3 Artist")
	tags.Set("ARTIST", "Vorbis Artist")

	tests := []struct {
		name       string
		candidates []string
		want       string
	}{
		{"first match", []string{"ARTIST", "TPE1"}, "Vorbis Artist"},
		{"second match", []string{"©ART", "TPE1"}, "ID3 Artist"},
		{"no match", []string{"©ART", "PERFORMER"}, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tags.GetBest(tc.candidates...)
			if got != tc.want {
				t.Errorf("GetBest(%v) = %q, want %q", tc.candidates, got, tc.want)
			}
		})
	}
}

func TestTags_Set(t *testing.T) {
	tags := &Tags{}

	// Set single value
	tags.Set("TITLE", "Test Song")
	if got := tags.GetFirst("TITLE"); got != "Test Song" {
		t.Errorf("after Set, GetFirst(TITLE) = %q, want %q", got, "Test Song")
	}

	// Set multiple values
	tags.Set("GENRE", "Rock", "Pop", "Alternative")
	if got := tags.Get("GENRE"); len(got) != 3 {
		t.Errorf("after Set with 3 values, Get(GENRE) = %v, want 3 values", got)
	}

	// Remove by setting empty
	tags.Set("TITLE")
	if got := tags.Get("TITLE"); got != nil {
		t.Errorf("after Set(), Get(TITLE) = %v, want nil", got)
	}
}

func TestTags_Set_ClonesValues(t *testing.T) {
	tags := &Tags{}
	values := []string{"Rock", "Pop"}
	tags.Set("GENRE", values...)

	values[0] = "Modified"

	got := tags.GetFirst("GENRE")
	if got != "Rock" {
		t.Error("Set() should clone values, but modification affected stored value")
	}
}

func TestTags_Merge(t *testing.T) {
	t.Run("merge into empty", func(t *testing.T) {
		tags := &Tags{}
		other := &Tags{
			Title:  "Other Title",
			Artist: "Other Artist",
			Year:   2024,
		}

		tags.Merge(other)

		if tags.Title != "Other Title" {
			t.Errorf("Title = %q, want %q", tags.Title, "Other Title")
		}
		if tags.Artist != "Other Artist" {
			t.Errorf("Artist = %q, want %q", tags.Artist, "Other Artist")
		}
		if tags.Year != 2024 {
			t.Errorf("Year = %d, want %d", tags.Year, 2024)
		}
	})

	t.Run("non-empty wins", func(t *testing.T) {
		tags := &Tags{
			Title: "Original Title",
			Year:  0,
		}
		other := &Tags{
			Title: "Other Title",
			Year:  2024,
		}

		tags.Merge(other)

		if tags.Title != "Original Title" {
			t.Errorf("Title = %q, want %q (original should be kept)", tags.Title, "Original Title")
		}
		if tags.Year != 2024 {
			t.Errorf("Year = %d, want %d (should be filled from other)", tags.Year, 2024)
		}
	})

	t.Run("merge slices unique", func(t *testing.T) {
		tags := &Tags{
			Genres: []string{"Rock"},
		}
		other := &Tags{
			Genres: []string{"Rock", "Pop"},
		}

		tags.Merge(other)

		if len(tags.Genres) != 2 {
			t.Errorf("Genres = %v, want 2 unique genres", tags.Genres)
		}
	})

	t.Run("merge nil", func(t *testing.T) {
		tags := &Tags{Title: "Test"}
		tags.Merge(nil)

		if tags.Title != "Test" {
			t.Error("Merge(nil) should not modify tags")
		}
	})

	t.Run("merge raw tags", func(t *testing.T) {
		tags := &Tags{}
		tags.Set("CUSTOM", "Original")

		other := &Tags{}
		other.Set("CUSTOM", "Other")
		other.Set("NEW", "Value")

		tags.Merge(other)

		if got := tags.GetFirst("CUSTOM"); got != "Other" {
			t.Errorf("CUSTOM = %q, want %q (should be overwritten)", got, "Other")
		}
		if got := tags.GetFirst("NEW"); got != "Value" {
			t.Errorf("NEW = %q, want %q", got, "Value")
		}
	})
}

func TestTags_Clone(t *testing.T) {
	original := &Tags{
		Title:       "Test Title",
		Artist:      "Test Artist",
		Year:        2024,
		TrackNumber: 5,
		Genres:      []string{"Rock", "Pop"},
	}
	original.Set("CUSTOM", "Value")

	clone := original.Clone()

	// Verify values match
	if clone.Title != original.Title {
		t.Errorf("clone.Title = %q, want %q", clone.Title, original.Title)
	}
	if clone.Year != original.Year {
		t.Errorf("clone.Year = %d, want %d", clone.Year, original.Year)
	}
	if !slices.Equal(clone.Genres, original.Genres) {
		t.Errorf("clone.Genres = %v, want %v", clone.Genres, original.Genres)
	}
	if clone.GetFirst("CUSTOM") != "Value" {
		t.Errorf("clone.GetFirst(CUSTOM) = %q, want %q", clone.GetFirst("CUSTOM"), "Value")
	}

	// Verify deep copy (modify clone shouldn't affect original)
	clone.Title = "Modified"
	clone.Genres[0] = "Modified"
	clone.Set("CUSTOM", "Modified")

	if original.Title != "Test Title" {
		t.Error("modifying clone.Title affected original")
	}
	if original.Genres[0] != "Rock" {
		t.Error("modifying clone.Genres affected original")
	}
	if original.GetFirst("CUSTOM") != "Value" {
		t.Error("modifying clone raw tags affected original")
	}
}

func TestTags_Clone_Nil(t *testing.T) {
	var tags *Tags
	clone := tags.Clone()
	if clone != nil {
		t.Error("Clone() of nil should return nil")
	}
}

func TestTags_Equal(t *testing.T) {
	tests := []struct {
		name string
		a    *Tags
		b    *Tags
		want bool
	}{
		{
			name: "both nil",
			a:    nil,
			b:    nil,
			want: true,
		},
		{
			name: "one nil",
			a:    &Tags{},
			b:    nil,
			want: false,
		},
		{
			name: "equal empty",
			a:    &Tags{},
			b:    &Tags{},
			want: true,
		},
		{
			name: "equal with values",
			a:    &Tags{Title: "Test", Year: 2024, Genres: []string{"Rock"}},
			b:    &Tags{Title: "Test", Year: 2024, Genres: []string{"Rock"}},
			want: true,
		},
		{
			name: "different title",
			a:    &Tags{Title: "Test1"},
			b:    &Tags{Title: "Test2"},
			want: false,
		},
		{
			name: "different year",
			a:    &Tags{Year: 2023},
			b:    &Tags{Year: 2024},
			want: false,
		},
		{
			name: "different genres",
			a:    &Tags{Genres: []string{"Rock"}},
			b:    &Tags{Genres: []string{"Pop"}},
			want: false,
		},
		{
			name: "different genre count",
			a:    &Tags{Genres: []string{"Rock"}},
			b:    &Tags{Genres: []string{"Rock", "Pop"}},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.a.Equal(tc.b)
			if got != tc.want {
				t.Errorf("Equal() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTags_Equal_RawTags(t *testing.T) {
	a := &Tags{}
	a.Set("CUSTOM", "Value")

	b := &Tags{}
	b.Set("CUSTOM", "Value")

	if !a.Equal(b) {
		t.Error("Tags with equal raw tags should be equal")
	}

	b.Set("CUSTOM", "Different")
	if a.Equal(b) {
		t.Error("Tags with different raw tag values should not be equal")
	}
}

func TestTags_Filter(t *testing.T) {
	tags := &Tags{}
	tags.Set("MUSICBRAINZ_TRACKID", "abc123")
	tags.Set("MUSICBRAINZ_ALBUMID", "def456")
	tags.Set("TITLE", "Test Song")
	tags.Set("ARTIST", "Test Artist")

	count := 0
	for key := range tags.Filter(func(k string) bool {
		return strings.HasPrefix(k, "MUSICBRAINZ")
	}) {
		count++
		if !strings.HasPrefix(key, "MUSICBRAINZ") {
			t.Errorf("Filter yielded non-matching key: %s", key)
		}
	}

	if count != 2 {
		t.Errorf("Filter yielded %d MusicBrainz tags, want 2", count)
	}
}

func TestTags_Filter_NilRaw(t *testing.T) {
	tags := &Tags{}

	count := 0
	for range tags.Filter(func(k string) bool { return true }) {
		count++
	}

	if count != 0 {
		t.Errorf("Filter on nil raw yielded %d tags, want 0", count)
	}
}

func TestMergeUnique(t *testing.T) {
	tests := []struct {
		name string
		a    []string
		b    []string
		want []string
	}{
		{
			name: "no overlap",
			a:    []string{"Rock"},
			b:    []string{"Pop"},
			want: []string{"Rock", "Pop"},
		},
		{
			name: "case insensitive duplicate",
			a:    []string{"Rock"},
			b:    []string{"rock", "Pop"},
			want: []string{"Rock", "Pop"},
		},
		{
			name: "empty b",
			a:    []string{"Rock"},
			b:    nil,
			want: []string{"Rock"},
		},
		{
			name: "empty a",
			a:    nil,
			b:    []string{"Rock"},
			want: []string{"Rock"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeUnique(tc.a, tc.b)
			if !slices.Equal(got, tc.want) {
				t.Errorf("mergeUnique(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}
