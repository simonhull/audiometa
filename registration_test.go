package audiometa

import "testing"

// TestEveryFormatHasAParser fails when a format package's init stops registering its
// parser, which would leave Open unable to read that format.
func TestEveryFormatHasAParser(t *testing.T) {
	t.Parallel()

	for f := FormatFLAC; f <= FormatAIFF; f++ {
		if findParser(f) == nil {
			t.Errorf("no parser registered for %v", f)
		}
	}
}
