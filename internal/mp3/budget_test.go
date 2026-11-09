package mp3

import (
	"slices"
	"strings"
	"testing"

	"github.com/simonhull/audiometa/internal/testbudget"
	"github.com/simonhull/audiometa/internal/types"
)

// hasTagValuesWarning reports whether ws holds the value-cap warning.
func hasTagValuesWarning(ws []types.Warning) bool {
	return slices.ContainsFunc(ws, func(w types.Warning) bool {
		return w.Stage == types.StageMetadata && strings.Contains(w.Message, "tag values")
	})
}

func TestParse_MultiValueFrameIsCapped(t *testing.T) {
	data := file(id3Tag(3, id3Frame(3, "TPE1", latin1Text(strings.Repeat("a\x00", 1<<21)))))
	var f *types.File
	testbudget.Check(t, testbudget.For(testbudget.Read, types.FormatMP3), len(data), "Parse", func() {
		f = parseBytes(t, data)
	})
	if got := len(f.Tags.Get("TPE1")); got != types.MaxTagValues {
		t.Errorf("TPE1 raw values = %d, want %d", got, types.MaxTagValues)
	}
	if got := len(f.Tags.Artists); got != types.MaxTagValues {
		t.Errorf("Artists = %d, want %d", got, types.MaxTagValues)
	}
	if !hasTagValuesWarning(f.Warnings) {
		t.Errorf("warnings = %+v, want a tag values warning", f.Warnings)
	}
}

func TestParse_TXXXValuesAreCapped(t *testing.T) {
	payload := append(latin1Text("X\x00"), strings.Repeat("a\x00", 1<<17)...)
	data := file(id3Tag(3, id3Frame(3, "TXXX", payload)))
	f := parseBytes(t, data)
	if got := len(f.Tags.Get("X")); got != types.MaxTagValues {
		t.Errorf("X values = %d, want %d", got, types.MaxTagValues)
	}
	if !hasTagValuesWarning(f.Warnings) {
		t.Errorf("warnings = %+v, want a tag values warning", f.Warnings)
	}
}
