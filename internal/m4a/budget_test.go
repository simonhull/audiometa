package m4a

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/simonhull/audiometa/internal/testbudget"
	"github.com/simonhull/audiometa/internal/types"
)

func TestParse_RepeatedItemsAreLinear(t *testing.T) {
	const n = 5000
	var items [][]byte
	for range n {
		items = append(items, ilstFreeform("X", "v"), ilstText("\xa9nam", "t"))
	}
	data := buildMP4(false, items...)
	var f *types.File
	parse := func() {
		var err error
		f, err = (&parser{}).Parse(context.Background(), bytes.NewReader(data), int64(len(data)), "x.m4a")
		if err != nil {
			t.Fatal(err)
		}
	}
	testbudget.Check(t, testbudget.For(testbudget.Read, types.FormatM4A), len(data), "Parse", parse)
	if a := testbudget.Allocated(parse); a > 64<<20 {
		t.Errorf("Parse allocated %d bytes for a %d-byte input, want under 64 MiB", a, len(data))
	}
	if got := len(f.Tags.Get("X")); got != n {
		t.Errorf("X has %d values, want %d", got, n)
	}
	if f.Tags.Title != "t" {
		t.Errorf("Title = %q, want t", f.Tags.Title)
	}
}

func TestParse_ItemValuesAreCapped(t *testing.T) {
	values := slices.Repeat([]string{"a"}, 70_000)
	data := buildMP4(false, ilstText("\xa9gen", values...))
	f, err := (&parser{}).Parse(context.Background(), bytes.NewReader(data), int64(len(data)), "x.m4a")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(f.Tags.Genres); got != types.MaxTagValues {
		t.Errorf("Genres = %d, want %d", got, types.MaxTagValues)
	}
	if !slices.ContainsFunc(f.Warnings, func(w types.Warning) bool {
		return w.Stage == types.StageMetadata && strings.Contains(w.Message, "tag values")
	}) {
		t.Errorf("warnings = %+v, want a tag values warning", f.Warnings)
	}
}
