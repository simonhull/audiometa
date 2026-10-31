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
