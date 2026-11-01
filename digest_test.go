package audiometa

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/simonhull/audiometa/internal/registry"
	"github.com/simonhull/audiometa/internal/types"
)

// Expected digests were computed by an independent parser of each container,
// not by audiometa. Files that differ only in their tags share a digest.
var sampleDigests = map[string]string{
	"testdata/sample.mp3":           "767927277cbbbe44e151ffb9079508cde531f932b8d14c550112896a2d779ea1",
	"testdata/sample_v24.mp3":       "767927277cbbbe44e151ffb9079508cde531f932b8d14c550112896a2d779ea1",
	"testdata/sample.flac":          "f36e19b9a2c276157124aab7ab0b52e90025e3f92e6c2bc4f4da8988ef12fdff",
	"testdata/sample.m4a":           "dafb65662f5c593fafd18ac02c10fe9244e826d5e64ab6e99a552dc821c41be5",
	"testdata/sample_faststart.m4a": "dafb65662f5c593fafd18ac02c10fe9244e826d5e64ab6e99a552dc821c41be5",
	"testdata/sample.m4b":           "32c09dba8ffe6553de1858ac36334245f3e0691e607f0245f1d881ed7c0a1b4f",
	"testdata/sample.ogg":           "86c0ebbc7226d63535820ddb54ad2dc768f163862a54b5f0e905a5f720081df0",
	"testdata/sample.opus":          "e835192e34f8a97dc5d65811a107f2334821ddd51d2fde45179caad9a8e2ca88",
}

func TestAudioDigest_KnownValues(t *testing.T) {
	t.Parallel()
	for path, want := range sampleDigests {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			sum, err := openSample(t, path).AudioDigest()
			if err != nil {
				t.Fatal(err)
			}
			if got := hex.EncodeToString(sum[:]); got != want {
				t.Errorf("digest = %s, want %s", got, want)
			}
		})
	}
}

func TestAudioDigest_StructuralErrors(t *testing.T) {
	t.Parallel()
	_, data := copySample(t, "testdata/sample.flac")
	tests := []struct {
		name string
		data []byte
	}{
		{"metadata cut short", data[:len(data)/2]},
		{"no audio data", data[:flacAudioStart(t, data)]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := writeTemp(t, "cut.flac", tt.data)
			_, err := openSample(t, path).AudioDigest()
			if _, ok := errors.AsType[*CorruptedFileError](err); !ok {
				t.Fatalf("err = %v, want a *CorruptedFileError", err)
			}
			if msg := err.Error(); !strings.HasPrefix(msg, "audio digest: "+path+": ") {
				t.Errorf("err = %q, want it to start with %q", msg, "audio digest: "+path+": ")
			}
		})
	}
}

// flacAudioStart returns the offset just past the last FLAC metadata block.
func flacAudioStart(t *testing.T, data []byte) int {
	t.Helper()
	off := 4
	for {
		if off+4 > len(data) {
			t.Fatal("no last metadata block")
		}
		header := data[off]
		size := int(data[off+1])<<16 | int(data[off+2])<<8 | int(data[off+3])
		off += 4 + size
		if header&0x80 != 0 {
			return off
		}
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

// The sample_retagged fixtures are stream copies with replaced, ~2 KB longer
// tags, so every page after the comment header moves and is renumbered:
//
//	ffmpeg -i testdata/sample.opus -map 0:a -c copy -map_metadata -1 \
//	  -metadata title="$(printf 'Retagged so every page moves %.0s' {1..70})" \
//	  -metadata artist="Someone Else" testdata/sample_retagged.opus
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
	for _, path := range []string{
		"testdata/sample.mp3", "testdata/sample.flac", "testdata/sample_faststart.m4a",
		"testdata/sample.ogg", "testdata/sample.opus",
	} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			dst, data := copySample(t, path)
			before := digestFile(t, dst)

			data[lastAudioByte(data)] ^= 0xFF
			if err := os.WriteFile(dst, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if after := digestFile(t, dst); after == before {
				t.Error("changing an audio byte did not change the digest")
			}
		})
	}
}

// lastAudioByte returns the offset of the last audio byte in a sample whose
// audio runs to the end of the file, apart from an MP3's ID3v1 trailer.
func lastAudioByte(data []byte) int {
	const id3v1Size = 128
	end := len(data)
	if end >= id3v1Size && string(data[end-id3v1Size:end-id3v1Size+3]) == "TAG" {
		end -= id3v1Size
	}
	return end - 1
}
