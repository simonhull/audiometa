package types

import (
	"slices"
	"testing"
)

func TestTags_NameOfParsedKey(t *testing.T) {
	t.Parallel()
	var tags Tags
	tags.AddRawNamed("MusicBrainz Album Id", "a")
	tags.AddRawNamed("MUSICBRAINZ ALBUM ID", "b")
	tags.AddRaw("TIT2", "Title")

	if got := tags.Get("MUSICBRAINZ ALBUM ID"); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("Get = %q, want both values under the upper-cased key", got)
	}
	tests := map[string]string{
		"MUSICBRAINZ ALBUM ID": "MusicBrainz Album Id", // the first spelling wins
		"TIT2":                 "TIT2",
		"ABSENT":               "ABSENT",
	}
	for key, want := range tests {
		if got := tags.Name(key); got != want {
			t.Errorf("Name(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestTags_SetNamed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		before func(*Tags)
		want   string
	}{
		{"new tag takes the spelling", func(*Tags) {}, "MusicBrainz Album Id"},
		{"upper-case tag keeps its spelling", func(tg *Tags) { tg.AddRaw("MUSICBRAINZ ALBUM ID", "x") }, "MUSICBRAINZ ALBUM ID"},
		{"other spelling is kept", func(tg *Tags) { tg.AddRawNamed("Musicbrainz Album ID", "x") }, "Musicbrainz Album ID"},
		{"deleted tag is new again", func(tg *Tags) {
			tg.AddRawNamed("MUSICBRAINZ Album Id", "x")
			tg.Delete("MUSICBRAINZ ALBUM ID")
		}, "MusicBrainz Album Id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var tags Tags
			tt.before(&tags)
			tags.SetNamed("MusicBrainz Album Id", "id")
			if got := tags.Get("MUSICBRAINZ ALBUM ID"); !slices.Equal(got, []string{"id"}) {
				t.Errorf("Get = %q, want [id]", got)
			}
			if got := tags.Name("MUSICBRAINZ ALBUM ID"); got != tt.want {
				t.Errorf("Name = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTags_RemovingATagForgetsItsName(t *testing.T) {
	t.Parallel()
	for name, remove := range map[string]func(*Tags){
		"Set":      func(tg *Tags) { tg.Set("NARRATOR") },
		"SetNamed": func(tg *Tags) { tg.SetNamed("Narrator") },
		"Delete":   func(tg *Tags) { tg.Delete("NARRATOR") },
	} {
		var tags Tags
		tags.SetNamed("Narrator", "x")
		remove(&tags)
		tags.Set("NARRATOR", "y")
		if got := tags.Name("NARRATOR"); got != "NARRATOR" {
			t.Errorf("%s: Name after removing and setting again = %q, want NARRATOR", name, got)
		}
	}
}

func TestTags_NamesCloneMergeEqual(t *testing.T) {
	t.Parallel()
	var a Tags
	a.SetNamed("MusicBrainz Album Id", "id")

	c := a.Clone()
	if !c.Equal(&a) || c.Name("MUSICBRAINZ ALBUM ID") != "MusicBrainz Album Id" {
		t.Errorf("Clone lost the name: %q", c.Name("MUSICBRAINZ ALBUM ID"))
	}
	c.Delete("MUSICBRAINZ ALBUM ID")
	if a.Name("MUSICBRAINZ ALBUM ID") != "MusicBrainz Album Id" {
		t.Error("Delete on a clone changed the original's name")
	}

	var upper Tags
	upper.Set("MUSICBRAINZ ALBUM ID", "id")
	if upper.Equal(&a) {
		t.Error("Equal ignores how a name is spelled")
	}

	upper.Merge(&a)
	if got := upper.Name("MUSICBRAINZ ALBUM ID"); got != "MUSICBRAINZ ALBUM ID" {
		t.Errorf("Merge into a tag that has the key: Name = %q, want its own spelling kept", got)
	}
	var empty Tags
	empty.Merge(&a)
	if got := empty.Name("MUSICBRAINZ ALBUM ID"); got != "MusicBrainz Album Id" {
		t.Errorf("Merge into a tag without the key: Name = %q, want the merged tag's spelling", got)
	}
}
