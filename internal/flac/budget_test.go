package flac

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/simonhull/audiometa/internal/testbudget"
	"github.com/simonhull/audiometa/internal/types"
)

func TestParse_CommentsAreCapped(t *testing.T) {
	comments := slices.Repeat([]string{"GENRE=a"}, 1_000_000)
	data := simpleFLAC(0, comments...)
	var f *types.File
	testbudget.Check(t, testbudget.For(testbudget.Read, types.FormatFLAC), len(data), "Parse", func() {
		var err error
		f, err = (&parser{}).Parse(context.Background(), bytes.NewReader(data), int64(len(data)), "x.flac")
		if err != nil {
			t.Fatal(err)
		}
	})
	if got := len(f.Tags.Genres); got != types.MaxTagValues {
		t.Errorf("Genres = %d, want %d", got, types.MaxTagValues)
	}
	if !slices.ContainsFunc(f.Warnings, func(w types.Warning) bool {
		return w.Stage == types.StageMetadata && strings.Contains(w.Message, "tag values")
	}) {
		t.Errorf("warnings = %+v, want a tag values warning", f.Warnings)
	}
}
