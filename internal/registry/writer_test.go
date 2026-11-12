package registry

import (
	"context"
	"io"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

type stubWriter struct{}

func (stubWriter) WriteFile(context.Context, io.Writer, io.ReaderAt, int64, *types.EditSet) (int64, error) {
	return 0, nil
}

func (stubWriter) AudioDigest(context.Context, io.ReaderAt, int64) ([]byte, error) {
	return nil, nil
}

func (stubWriter) PreservedDigest(context.Context, io.ReaderAt, int64) ([]byte, error) {
	return nil, nil
}

func TestRegisterWriter_GetWriter(t *testing.T) {
	t.Cleanup(func() { unregisterWriter(types.FormatWAV) })

	w := stubWriter{}
	RegisterWriter(types.FormatWAV, w)
	if got := GetWriter(types.FormatWAV); got != w {
		t.Errorf("GetWriter(WAV) = %v, want the registered stub", got)
	}
	if got := GetWriter(types.FormatAIFF); got != nil {
		t.Errorf("GetWriter(AIFF) = %v, want nil", got)
	}
}

type stubDigester struct{}

func (stubDigester) AudioDigest(context.Context, io.ReaderAt, int64) ([]byte, error) {
	return nil, nil
}

func TestGetDigester(t *testing.T) {
	t.Cleanup(func() {
		unregisterWriter(types.FormatWAV)
		unregisterDigester(types.FormatAIFF)
	})

	w := stubWriter{}
	RegisterWriter(types.FormatWAV, w)
	d := stubDigester{}
	RegisterDigester(types.FormatAIFF, d)

	if got := GetDigester(types.FormatWAV); got != w {
		t.Errorf("GetDigester(WAV) = %v, want the registered writer", got)
	}
	if got := GetDigester(types.FormatAIFF); got != d {
		t.Errorf("GetDigester(AIFF) = %v, want the registered digester", got)
	}
	if got := GetDigester(types.FormatUnknown); got != nil {
		t.Errorf("GetDigester(Unknown) = %v, want nil", got)
	}
}

func TestRange_Overlaps(t *testing.T) {
	t.Parallel()
	r := Range{Off: 10, End: 20}
	for _, tc := range []struct {
		o    Range
		want bool
	}{
		{Range{0, 10}, false},
		{Range{20, 30}, false},
		{Range{0, 11}, true},
		{Range{19, 30}, true},
		{Range{12, 14}, true},
		{Range{0, 30}, true},
		{Range{15, 15}, false},
		{Range{30, 30}, false},
	} {
		if got := r.Overlaps(tc.o); got != tc.want || tc.o.Overlaps(r) != tc.want {
			t.Errorf("%v.Overlaps(%v) = %v, want %v", r, tc.o, got, tc.want)
		}
	}
}
