package mp3

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"github.com/simonhull/audiometa/internal/testctx"
)

func TestParse_CancelInsideLoops(t *testing.T) {
	data, err := os.ReadFile("../../testdata/sample.mp3")
	if err != nil {
		t.Fatal(err)
	}
	// Err #1 is the entry check; #2 is the first loop iteration.
	for n := 1; n <= 2; n++ {
		_, err := (&parser{}).Parse(testctx.CancelAfter(n), bytes.NewReader(data), int64(len(data)), "x.mp3")
		if !errors.Is(err, context.Canceled) {
			t.Errorf("CancelAfter(%d): want context.Canceled, got %v", n, err)
		}
	}
	if _, err := (&parser{}).ExtractArtwork(testctx.CancelAfter(1), bytes.NewReader(data), int64(len(data)), "x.mp3"); err != nil && !errors.Is(err, context.Canceled) {
		t.Errorf("ExtractArtwork: %v", err)
	}
}
