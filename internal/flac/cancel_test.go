package flac

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"github.com/simonhull/audiometa/internal/testctx"
)

func TestParse_CancelInsideBlockLoop(t *testing.T) {
	data, err := os.ReadFile("../../testdata/sample.flac")
	if err != nil {
		t.Fatal(err)
	}
	_, err = (&parser{}).Parse(testctx.CancelAfter(1), bytes.NewReader(data), int64(len(data)), "x.flac")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}

	_, err = (&parser{}).ExtractArtwork(testctx.CancelAfter(1), bytes.NewReader(data), int64(len(data)), "x.flac")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ExtractArtwork: want context.Canceled, got %v", err)
	}
}
