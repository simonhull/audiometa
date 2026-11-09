package types

import (
	"strings"
	"testing"
)

func TestFile_TakeValue(t *testing.T) {
	var f File
	for i := range MaxTagValues {
		if !f.TakeValue() {
			t.Fatalf("value %d refused", i)
		}
	}
	for range 3 {
		if f.TakeValue() {
			t.Fatal("value past MaxTagValues admitted")
		}
	}
	if len(f.Warnings) != 1 || f.Warnings[0].Stage != StageMetadata || !strings.Contains(f.Warnings[0].Message, "tag values") {
		t.Errorf("warnings = %+v, want one StageMetadata warning about tag values", f.Warnings)
	}
}
