package m4a

import (
	"bytes"
	"slices"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

const (
	picardAlbumID = "MusicBrainz Album Id"
	upperAlbumID  = "MUSICBRAINZ ALBUM ID"
)

// freeformNames lists the names of data's freeform items, in order.
func freeformNames(t *testing.T, data []byte) []string {
	t.Helper()
	var out []string
	for _, it := range moovTreeOf(t, data).child(atomUdta).child(atomMeta).child(atomIlst).children {
		if it.typ != itemFreeform {
			continue
		}
		if n, ok := freeformName(it.payload); ok {
			out = append(out, n)
		}
	}
	return out
}

func TestFreeform_ReadKeepsNameSpelling(t *testing.T) {
	t.Parallel()
	f := parseBytes(t, buildMP4(false, ilstFreeform(picardAlbumID, "id")))
	if got := f.Tags.Get(upperAlbumID); !slices.Equal(got, []string{"id"}) {
		t.Errorf("Get(%q) = %q", upperAlbumID, got)
	}
	if got := f.Tags.Name(upperAlbumID); got != picardAlbumID {
		t.Errorf("Name = %q, want %q", got, picardAlbumID)
	}
}

func TestIlst_FreeformSpelling(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		existing string
		set      func(*types.Tags)
		want     string
	}{
		{"new item takes the caller's spelling", "", func(tg *types.Tags) { tg.SetNamed(picardAlbumID, "new") }, picardAlbumID},
		{"new item set by key is upper-case", "", func(tg *types.Tags) { tg.Set(upperAlbumID, "new") }, upperAlbumID},
		{"upper-case item stays upper-case", upperAlbumID, func(tg *types.Tags) { tg.SetNamed(picardAlbumID, "new") }, upperAlbumID},
		{"Picard item keeps its spelling", picardAlbumID, func(tg *types.Tags) { tg.Set(upperAlbumID, "new") }, picardAlbumID},
	}
	for _, tt := range tests {
		items := [][]byte{ilstText("\xA9nam", "T")}
		if tt.existing != "" {
			items = append(items, ilstFreeform(tt.existing, "old"))
		}
		src := buildMP4(false, items...)
		out := writeTags(t, src, tt.set)
		if got, want := itemOf(t, out, itemFreeform), ilstFreeform(tt.want, "new"); !bytes.Equal(got, want) {
			t.Errorf("%s: freeform %q, want %q", tt.name, got, want)
		}
		if got := parseBytes(t, out).Tags.Name(upperAlbumID); got != tt.want {
			t.Errorf("%s: Name after write = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// Items whose names differ only in case share one raw key: reading gives every
// value and the first item's spelling, and writing the key makes them one item,
// named and placed as the first was.
func TestIlst_FreeformCaseDuplicates(t *testing.T) {
	t.Parallel()
	for _, order := range [][2]string{{upperAlbumID, picardAlbumID}, {picardAlbumID, upperAlbumID}} {
		src := buildMP4(false, ilstFreeform(order[0], "a"), ilstText("\xA9nam", "T"), ilstFreeform(order[1], "b"))
		tags := parseBytes(t, src).Tags
		if got := tags.Get(upperAlbumID); !slices.Equal(got, []string{"a", "b"}) {
			t.Errorf("%q: Get = %q, want [a b]", order, got)
		}
		if got := tags.Name(upperAlbumID); got != order[0] {
			t.Errorf("%q: Name = %q, want the first", order, got)
		}

		out := writeTags(t, src, func(tg *types.Tags) { tg.Set("\xA9nam", "U") })
		if got := freeformNames(t, out); !slices.Equal(got, order[:]) {
			t.Errorf("%q: an edit elsewhere changed the items to %q", order, got)
		}

		out = writeTags(t, src, func(tg *types.Tags) { tg.SetNamed(picardAlbumID, "c") })
		items := ilstItems(t, out)
		if len(items) != 2 || !bytes.Equal(items[0], ilstFreeform(order[0], "c")) {
			t.Errorf("%q: items %q, want the merged item first, named %q", order, items, order[0])
		}
	}
}
