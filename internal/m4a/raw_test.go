package m4a

import (
	"slices"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

func TestIlstItemRecordsRawAndArtists(t *testing.T) {
	t.Parallel()
	f := runIlst(t, ilstText("\xA9ART", "Someone"))
	types.DeriveFields(&f.Tags, types.FormatM4A)
	if got := f.Tags.Get("\xA9ART"); !slices.Equal(got, []string{"Someone"}) {
		t.Errorf("raw = %q", got)
	}
	if !slices.Equal(f.Tags.Artists, []string{"Someone"}) || f.Tags.Artist != "Someone" {
		t.Errorf("Artist=%q Artists=%q", f.Tags.Artist, f.Tags.Artists)
	}
}

func TestFreeformReadsEveryValue(t *testing.T) {
	t.Parallel()
	src := buildMP4(false, ilstFreeform("Label", "A", "", "B", "C"))
	if got := parseBytes(t, src).Tags.Get("LABEL"); !slices.Equal(got, []string{"A", "B", "C"}) {
		t.Errorf("LABEL = %q", got)
	}
}

func TestFreeformFirstValueFillsTheField(t *testing.T) {
	t.Parallel()
	tags := parseBytes(t, buildMP4(false, ilstFreeform("Narrator", "A", "B"))).Tags
	if tags.Narrator != "A" {
		t.Errorf("Narrator = %q, want A", tags.Narrator)
	}
	if got := tags.Get("NARRATOR"); !slices.Equal(got, []string{"A", "B"}) {
		t.Errorf("raw = %q", got)
	}
}

func TestFreeformRepeatedItemsAndValuesAccumulate(t *testing.T) {
	t.Parallel()
	src := buildMP4(false, ilstFreeform("Label", "A", "B"), ilstFreeform("label", "C"))
	if got := parseBytes(t, src).Tags.Get("LABEL"); !slices.Equal(got, []string{"A", "B", "C"}) {
		t.Errorf("LABEL = %q", got)
	}
}

func TestFreeformMultiValueWriteRoundTrip(t *testing.T) {
	t.Parallel()
	src := buildMP4(false, ilstText("\xA9nam", "T"), ilstFreeform("Label", "A", "B"))

	out := writeTags(t, src, func(tg *types.Tags) { tg.Title = "New" })
	if got := parseBytes(t, out).Tags.Get("LABEL"); !slices.Equal(got, []string{"A", "B"}) {
		t.Errorf("an untouched item reads %q", got)
	}

	out = writeTags(t, src, func(tg *types.Tags) { tg.Set("LABEL", "B", "C", "D") })
	if got := parseBytes(t, out).Tags.Get("LABEL"); !slices.Equal(got, []string{"B", "C", "D"}) {
		t.Errorf("a rewritten item reads %q", got)
	}
}
