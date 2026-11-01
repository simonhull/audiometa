package audiometa

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
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

func digestFile(t *testing.T, path string) [sha256.Size]byte {
	t.Helper()
	sum, err := openSample(t, path).AudioDigest()
	if err != nil {
		t.Fatalf("AudioDigest(%s): %v", path, err)
	}
	return sum
}

func TestAudioDigest_UnchangedByWrite(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		"testdata/sample.mp3", "testdata/sample_v24.mp3", "testdata/sample.flac",
		"testdata/sample.m4a", "testdata/sample.m4b", "testdata/sample_faststart.m4a",
	} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			dst, _ := copySample(t, path)
			before := digestFile(t, dst)
			err := Write(dst, func(f *File) error {
				f.Tags.Title = "A completely different title that is much longer than before"
				f.Tags.Set("KITSUNE_TEST", "value")
				f.RemoveAllArtwork()
				return nil
			})
			if err != nil {
				t.Fatalf("Write: %v", err)
			}
			if after := digestFile(t, dst); after != before {
				t.Errorf("digest changed after a metadata write: %x -> %x", before, after)
			}
		})
	}
}

func TestAudioDigest_UnchangedByOggRetag(t *testing.T) {
	t.Parallel()
	for _, ext := range []string{"ogg", "opus"} {
		t.Run(ext, func(t *testing.T) {
			t.Parallel()
			orig := digestFile(t, "testdata/sample."+ext)
			if retagged := digestFile(t, "testdata/sample_retagged."+ext); retagged != orig {
				t.Errorf("retagged %s digest %x, want %x", ext, retagged, orig)
			}
		})
	}
}

func TestAudioDigest_ChangesWithAudio(t *testing.T) {
	t.Parallel()
	dst, _ := copySample(t, "testdata/sample.flac")
	before := digestFile(t, dst)

	f, err := os.OpenFile(dst, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	// The last byte of a FLAC file is inside its final audio frame.
	last := make([]byte, 1)
	if _, err := f.ReadAt(last, info.Size()-1); err != nil && !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	last[0] ^= 0xFF
	if _, err := f.WriteAt(last, info.Size()-1); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	if after := digestFile(t, dst); after == before {
		t.Error("changing an audio byte did not change the digest")
	}
}
