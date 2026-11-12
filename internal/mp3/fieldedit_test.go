package mp3

import (
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

// TestFieldEditSeesKeyEdits clears Language in the same write that sets the lower-precedence
// raw key LANG on a file holding LANGUAGE. The field edit sees the key edit, so it removes
// LANG too, and Language does not come back from it on the next read.
func TestFieldEditSeesKeyEdits(t *testing.T) {
	src := file(id3Tag(4, id3Frame(4, "TXXX", txxxPayload(3, "LANGUAGE", "eng"))))
	edits := editsFor(t, src, nil)
	edits.Keys = []types.KeyEdit{{Key: "LANG", Values: []string{"fre"}}}
	edits.Fields = []types.FieldEdit{{Field: types.FieldLanguage}}
	edits.Final.Language = ""

	out := writeBytes(t, src, edits)
	tags := parseBytes(t, out).Tags
	if tags.Language != "" || tags.Get("LANG") != nil || tags.Get("LANGUAGE") != nil {
		t.Errorf("Language = %q, LANG = %q, LANGUAGE = %q; want all empty", tags.Language, tags.Get("LANG"), tags.Get("LANGUAGE"))
	}
}
