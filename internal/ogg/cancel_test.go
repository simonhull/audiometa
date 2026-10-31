package ogg

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"github.com/simonhull/audiometa/internal/testctx"
)

func TestParse_CancelInsidePageLoop(t *testing.T) {
	for _, name := range []string{"sample.ogg", "sample.opus"} {
		data, err := os.ReadFile("../../testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		_, err = (&parser{}).Parse(testctx.CancelAfter(1), bytes.NewReader(data), int64(len(data)), name)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("%s: want context.Canceled, got %v", name, err)
		}
	}
}
