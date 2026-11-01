package audiometa

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/simonhull/audiometa/internal/registry"
	"github.com/simonhull/audiometa/internal/types"
)

func TestAudioDigest_EveryFormat(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		"testdata/sample.mp3", "testdata/sample.flac", "testdata/sample.m4a",
		"testdata/sample.m4b", "testdata/sample.ogg", "testdata/sample.opus",
	} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			sum, err := openSample(t, path).AudioDigest()
			if err != nil {
				t.Fatal(err)
			}
			if sum == ([sha256.Size]byte{}) {
				t.Error("digest is all zeros")
			}
		})
	}
}

func TestAudioDigest_Canceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := openSample(t, "testdata/sample.flac").AudioDigestContext(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestAudioDigest_AfterClose(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		"testdata/sample.mp3", "testdata/sample.flac", "testdata/sample.m4a",
		"testdata/sample.m4b", "testdata/sample.ogg", "testdata/sample.opus",
	} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			f, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := f.AudioDigest(); !errors.Is(err, os.ErrClosed) {
				t.Errorf("err = %v, want os.ErrClosed", err)
			}
		})
	}
}

func TestAudioDigest_ConcurrentCalls(t *testing.T) {
	t.Parallel()
	f := openSample(t, "testdata/sample.m4a")
	want, err := f.AudioDigest()
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if got, err := f.AudioDigest(); err != nil || got != want {
				t.Errorf("concurrent AudioDigest = %x, %v; want %x", got, err, want)
			}
		})
	}
	wg.Wait()
}

// TestAudioDigest_NoDigester swaps a package variable, so it must not run in parallel.
func TestAudioDigest_NoDigester(t *testing.T) { //nolint:paralleltest // swaps lookupDigester
	f := openSample(t, "testdata/sample.flac")
	orig := lookupDigester
	lookupDigester = func(types.Format) registry.AudioDigester { return nil }
	t.Cleanup(func() { lookupDigester = orig })

	_, err := f.AudioDigest()
	if _, ok := errors.AsType[*UnsupportedFormatError](err); !ok {
		t.Errorf("err = %v, want *UnsupportedFormatError", err)
	}
}
