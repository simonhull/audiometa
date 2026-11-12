package audiometa_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/simonhull/audiometa"
)

// pngWithPadding returns a PNG signature and IHDR chunk followed by zero
// padding up to size bytes, enough for the artwork sniffer to accept it.
func pngWithPadding(t *testing.T, size int) []byte {
	t.Helper()
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], 1) // width
	binary.BigEndian.PutUint32(ihdr[4:], 1) // height
	ihdr[8], ihdr[9] = 8, 2                 // 8-bit RGB

	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	_ = binary.Write(&b, binary.BigEndian, uint32(len(ihdr)))
	chunk := append([]byte("IHDR"), ihdr...)
	b.Write(chunk)
	_ = binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	if b.Len() > size {
		t.Fatalf("size %d is smaller than the PNG header", size)
	}
	b.Write(make([]byte, size-b.Len()))
	return b.Bytes()
}

// TestPublicErrors checks the write error contract through the public API
// alone.
func TestPublicErrors(t *testing.T) {
	t.Parallel()
	t.Run("ErrTagTooLarge via WriteTo", func(t *testing.T) {
		t.Parallel()
		f, err := audiometa.Open("testdata/sample.flac")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()

		// A FLAC metadata block length is 24 bits: 16 MiB - 1 at most.
		big := pngWithPadding(t, 1<<24+1)
		if err := f.AddArtwork(audiometa.Artwork{Data: big, Type: audiometa.ArtworkFrontCover}); err != nil {
			t.Fatalf("AddArtwork: %v", err)
		}
		_, err = f.WriteTo(io.Discard)
		if !errors.Is(err, audiometa.ErrTagTooLarge) {
			t.Fatalf("WriteTo error = %v, want ErrTagTooLarge", err)
		}
	})

	t.Run("ErrDRMProtected via WriteTo", func(t *testing.T) {
		t.Parallel()
		data, err := os.ReadFile("testdata/sample.m4a")
		if err != nil {
			t.Fatal(err)
		}
		// Rename the audio sample entry from mp4a to enca (encrypted audio).
		i := bytes.Index(data, []byte("mp4a"))
		if i < 4 {
			t.Fatal("no mp4a sample entry in the fixture")
		}
		if size := binary.BigEndian.Uint32(data[i-4:]); size < 28 || int(size) > len(data)-i+4 {
			t.Fatalf("mp4a at %d has implausible box size %d", i, size)
		}
		copy(data[i:], "enca")
		path := filepath.Join(t.TempDir(), "encrypted.m4a")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}

		f, err := audiometa.Open(path)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer f.Close()
		f.Tags.Title = "x"
		_, err = f.WriteTo(io.Discard)
		if !errors.Is(err, audiometa.ErrDRMProtected) {
			t.Fatalf("WriteTo error = %v, want ErrDRMProtected", err)
		}
	})

	t.Run("Warning unwraps its cause", func(t *testing.T) {
		t.Parallel()
		// Warning is not an error itself (it has no Error method), but its
		// Unwrap method lets callers reach the cause with errors.Is.
		w := audiometa.Warning{Stage: audiometa.StageMetadata, Message: "m", Err: io.ErrUnexpectedEOF}
		if !errors.Is(w.Unwrap(), io.ErrUnexpectedEOF) {
			t.Error("errors.Is(w.Unwrap(), cause) = false, want true")
		}
		if errors.Is(audiometa.Warning{Message: "m"}.Unwrap(), io.ErrUnexpectedEOF) {
			t.Error("errors.Is(Unwrap of a Warning without cause, io.ErrUnexpectedEOF) = true, want false")
		}
	})
}
