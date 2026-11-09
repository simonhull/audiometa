package m4a

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	audiobinary "github.com/simonhull/audiometa/internal/binary"
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

func TestParseChunkOffsets_BoundedByAtomAndSamples(t *testing.T) {
	co64 := append(u32be(1<<20), make([]byte, 16)...) // two 8-byte entries
	for _, tc := range []struct{ typ, body string }{
		{"stco", string(u32be(1<<20, 100, 200))},
		{"co64", string(co64)},
	} {
		stbl := createMockAtom("stbl", fullAtom(tc.typ, []byte(tc.body)))
		// Zeros after the atom let a table that claims 1<<20 entries fit in the file.
		data := append(stbl, make([]byte, 9<<20)...)
		sr := audiobinary.NewSafeReader(bytes.NewReader(data), int64(len(data)), "t.m4b")
		atom, err := readAtomHeader(sr, 0, int64(len(stbl)))
		if err != nil {
			t.Fatal(err)
		}
		var offsets []uint64
		alloc := testbudget.Allocated(func() {
			offsets, err = parseChunkOffsets(context.Background(), sr, atom, 2)
		})
		if err != nil {
			t.Fatalf("%s: %v", tc.typ, err)
		}
		if len(offsets) != 2 {
			t.Errorf("%s: %d offsets, want 2", tc.typ, len(offsets))
		}
		if alloc > 1<<20 {
			t.Errorf("%s: allocated %d bytes, want under 1 MiB", tc.typ, alloc)
		}
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
