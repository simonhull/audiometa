package vorbis

import (
	"errors"
	"slices"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

func TestEditor_EveryFieldWritable(t *testing.T) {
	t.Parallel()
	for _, format := range []types.Format{types.FormatFLAC, types.FormatOgg, types.FormatOpus} {
		for f := range types.AllFields() {
			v := NewEditor(format, Comments{})
			edits := &types.EditSet{Orig: &types.Tags{}, Final: &types.Tags{Artist: "a", Artists: []string{"a"}}}
			if err := v.ApplyField(types.FieldEdit{Field: f, Values: []string{"1"}}, edits); err != nil || len(v.Entries) == 0 {
				t.Errorf("%s %s: err %v, %d comments", format, f, err, len(v.Entries))
			}
		}
	}
}

func TestEditor_ApplyKey(t *testing.T) {
	t.Parallel()
	v := NewEditor(types.FormatOpus, Comments{Entries: [][]byte{[]byte("ARTIST=A"), []byte("title=Old"), []byte("Artist=B")}})
	if err := v.ApplyKey("TITLE", []string{"New"}); err != nil {
		t.Fatal(err)
	}
	if err := v.ApplyKey("artist", []string{"X", "Y"}); err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(v.Entries))
	for _, e := range v.Entries {
		got = append(got, string(e))
	}
	if want := []string{"ARTIST=X", "ARTIST=Y", "title=New"}; !slices.Equal(got, want) {
		t.Errorf("entries %q, want %q", got, want)
	}
	for _, key := range []string{"A=B", "", "CHAPTER001"} {
		if err := v.ApplyKey(key, []string{"v"}); !errors.Is(err, types.ErrUnsupportedWrite) {
			t.Errorf("key %q: err = %v, want ErrUnsupportedWrite", key, err)
		}
	}
}
