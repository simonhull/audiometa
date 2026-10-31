package m4a

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	audiobinary "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/testctx"
)

func TestParse_CancelInsideAtomWalk(t *testing.T) {
	for _, name := range []string{"sample.m4b", "sample.m4a"} {
		data, err := os.ReadFile("../../testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		for n := 1; n <= 4; n++ {
			_, err = (&parser{}).Parse(testctx.CancelAfter(n), bytes.NewReader(data), int64(len(data)), name)
			if !errors.Is(err, context.Canceled) {
				t.Errorf("%s CancelAfter(%d): want context.Canceled, got %v", name, n, err)
			}
		}
	}
}

func TestParseChapters_CancelInsideTables(t *testing.T) {
	payload := append(textSample("A"), textSample("B")...)
	data, moovOff := buildQTChapterFile(1000, payload,
		sttsAtom([2]uint32{2, 1000}),
		stszAtom(0, 2, 3, 3),
		stcoAtom(8),
	)
	sr := audiobinary.NewSafeReader(bytes.NewReader(data), int64(len(data)), "t.m4b")
	moov, err := readAtomHeader(sr, moovOff, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	// Every Err call from the first on must surface as cancellation, wherever it lands.
	for n := range 8 {
		_, err := parseChapters(testctx.CancelAfter(n), sr, moov, time.Minute)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("CancelAfter(%d): want context.Canceled, got %v", n, err)
		}
	}
}
