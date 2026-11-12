package audiometa_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/simonhull/audiometa"
)

// writeBytes applies edit to the file at path and returns what WriteTo writes.
func writeBytes(t *testing.T, path string, edit func(f *audiometa.File)) []byte {
	t.Helper()
	f := openFile(t, path)
	edit(f)
	var buf bytes.Buffer
	if _, err := f.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return buf.Bytes()
}

// chunkIDs lists every chunk ID of an IFF file, in order.
func chunkIDs(t *testing.T, data []byte) []string {
	t.Helper()
	ids := make([]string, 0, len(iffChunks(t, data)))
	for _, c := range iffChunks(t, data) {
		ids = append(ids, c.id)
	}
	return ids
}

func TestWriteIFF_NewID3ChunkGoesLast(t *testing.T) {
	t.Parallel()
	out := writeBytes(t, "testdata/sample.wav", func(f *audiometa.File) { f.Tags.Title = "New" })
	if got, want := chunkIDs(t, out), []string{"fmt ", "LIST", "data", "id3 "}; !slices.Equal(got, want) {
		t.Errorf("chunks = %v, want %v", got, want)
	}
	if size := int(binary.LittleEndian.Uint32(out[4:])); size != len(out)-8 {
		t.Errorf("RIFF size = %d, want %d", size, len(out)-8)
	}
	f := writeAndReopen(t, "testdata/sample.wav", out)
	if f.Tags.Title != "New" || f.Tags.GetFirst("TIT2") != "New" || f.Tags.GetFirst("INAM") != "New" {
		t.Errorf("Title %q, TIT2 %q, INAM %q", f.Tags.Title, f.Tags.Get("TIT2"), f.Tags.Get("INAM"))
	}
	if id3 := iffChunks(t, out)[3]; string(out[id3.start:id3.start+4]) != "ID3\x04" {
		t.Errorf("new ID3 chunk starts %q, want an ID3v2.4 tag", out[id3.start:id3.start+4])
	}
}

func TestWriteIFF_NativeKeysStayNative(t *testing.T) {
	t.Parallel()
	out := writeBytes(t, "testdata/sample.wav", func(f *audiometa.File) {
		f.Tags.Set("ICOP", "(C) Me")
		f.Tags.Delete("ISFT")
	})
	if got, want := chunkIDs(t, out), []string{"fmt ", "LIST", "data"}; !slices.Equal(got, want) {
		t.Errorf("chunks = %v, want %v: native keys must not make an ID3 chunk", got, want)
	}
	f := writeAndReopen(t, "testdata/sample.wav", out)
	if f.Tags.GetFirst("ICOP") != "(C) Me" || f.Tags.Get("ISFT") != nil {
		t.Errorf("ICOP %q, ISFT %q", f.Tags.Get("ICOP"), f.Tags.Get("ISFT"))
	}
}

func TestWriteIFF_MirrorsFollowTheID3Tag(t *testing.T) {
	t.Parallel()
	out := writeBytes(t, "testdata/sample.wav", func(f *audiometa.File) {
		f.Tags.Comment = ""
		f.Tags.Year = 1999
		f.Tags.Artists, f.Tags.Artist = []string{"A", "B"}, "A"
		f.Tags.TrackNumber = 7
	})
	f := writeAndReopen(t, "testdata/sample.wav", out)
	tags := &f.Tags
	if tags.Comment != "" || tags.Get("ICMT") != nil {
		t.Errorf("Comment %q, ICMT %q: a cleared comment must leave no native one", tags.Comment, tags.Get("ICMT"))
	}
	if tags.GetFirst("ICRD") != "1999" || !slices.Equal(tags.Get("IART"), []string{"A", "B"}) || tags.GetFirst("IPRT") != "7" {
		t.Errorf("ICRD %q, IART %q, IPRT %q", tags.Get("ICRD"), tags.Get("IART"), tags.Get("IPRT"))
	}
	if tags.Get("ITRK") != nil {
		t.Errorf("ITRK %q: the track mirrors into the key the file has", tags.Get("ITRK"))
	}
}

func TestWriteIFF_NoNativeTextIsMade(t *testing.T) {
	t.Parallel()
	src := readTestdata(t, "testdata/sample_float.wav")
	stripped := writeBytes(t, "testdata/sample_float.wav", func(f *audiometa.File) { f.Tags.Delete("ISFT") })
	if slices.Contains(chunkIDs(t, stripped), "LIST") {
		t.Fatal("deleting the last INFO key kept the LIST chunk")
	}
	path := tempFile(t, "bare.wav", stripped)
	out := writeBytes(t, path, func(f *audiometa.File) { f.Tags.Title = "Only ID3" })
	if got, want := chunkIDs(t, out), []string{"fmt ", "fact", "data", "id3 "}; !slices.Equal(got, want) {
		t.Errorf("chunks = %v, want %v", got, want)
	}
	if iffAudioDigest(t, out) != iffAudioDigest(t, src) {
		t.Error("audio changed")
	}
}

func TestWriteIFF_AIFFText(t *testing.T) {
	t.Parallel()
	out := writeBytes(t, "testdata/sample.aiff", func(f *audiometa.File) {
		f.Tags.Title = "Renamed"
		f.Tags.Comment = "Two"
		f.Tags.Set("AUTH", "Author")
	})
	if got, want := chunkIDs(t, out), []string{"NAME", "(c) ", "ANNO", "AUTH", "COMM", "SSND", "ID3 "}; !slices.Equal(got, want) {
		t.Errorf("chunks = %v, want %v", got, want)
	}
	if size := int(binary.BigEndian.Uint32(out[4:])); size != len(out)-8 {
		t.Errorf("FORM size = %d, want %d", size, len(out)-8)
	}
	f := writeAndReopen(t, "testdata/sample.aiff", out)
	if f.Tags.GetFirst("NAME") != "Renamed" || f.Tags.GetFirst("ANNO") != "Two" || f.Tags.Comment != "Two" {
		t.Errorf("NAME %q, ANNO %q, Comment %q", f.Tags.Get("NAME"), f.Tags.Get("ANNO"), f.Tags.Comment)
	}
	if f.Tags.Artist != "Test Artist" || f.Tags.GetFirst("AUTH") != "Author" {
		t.Errorf("Artist %q (ID3 wins), AUTH %q", f.Tags.Artist, f.Tags.Get("AUTH"))
	}
}

func TestWriteIFF_RF64KeepsItsSizes(t *testing.T) {
	t.Parallel()
	out := writeBytes(t, "testdata/sample_rf64.wav", func(f *audiometa.File) { f.Tags.Title = "Sixty-four" })
	if string(out[:4]) != "RF64" || binary.LittleEndian.Uint32(out[4:]) != 0xFFFFFFFF {
		t.Fatalf("header %q %x", out[:4], out[4:8])
	}
	if riff := binary.LittleEndian.Uint64(out[20:]); int(riff) != len(out)-8 {
		t.Errorf("ds64 RIFF size = %d, want %d", riff, len(out)-8)
	}
	if got := writeAndReopen(t, "testdata/sample_rf64.wav", out).Tags.Title; got != "Sixty-four" {
		t.Errorf("Title = %q", got)
	}
}

func TestWriteIFF_Refusals(t *testing.T) {
	t.Parallel()
	src := readTestdata(t, "testdata/sample.wav")
	cut := tempFile(t, "cut.wav", src[:len(src)-100])
	f := openFile(t, cut)
	f.Tags.Title = "x"
	if _, err := f.WriteTo(&bytes.Buffer{}); !errors.As(err, new(*audiometa.CorruptedFileError)) {
		t.Errorf("cut-short data chunk: err = %v, want a CorruptedFileError", err)
	}

	f = openFile(t, "testdata/sample.wav")
	f.Tags.Set("ICOP", "a\x00b")
	if _, err := f.WriteTo(&bytes.Buffer{}); !errors.Is(err, audiometa.ErrUnsupportedWrite) {
		t.Errorf("NUL in INFO: err = %v, want ErrUnsupportedWrite", err)
	}
}

func TestWriteIFF_OddLastChunkGetsItsPad(t *testing.T) {
	t.Parallel()
	// A data chunk of odd size, last in the file, without the pad byte
	// sloppy writers leave out.
	src := readTestdata(t, "testdata/sample.wav")
	chunks := iffChunks(t, src)
	data := chunks[len(chunks)-1]
	odd := slices.Clone(src[:data.end-1])
	binary.LittleEndian.PutUint32(odd[data.start-4:], uint32(data.end-data.start-1))
	binary.LittleEndian.PutUint32(odd[4:], uint32(len(odd)-8))
	path := tempFile(t, "odd.wav", odd)

	out := writeBytes(t, path, func(f *audiometa.File) { f.Tags.Title = "Padded" })
	if got := chunkIDs(t, out); !slices.Equal(got, []string{"fmt ", "LIST", "data", "id3 "}) {
		t.Errorf("chunks = %v", got)
	}
	if iffAudioDigest(t, out) != iffAudioDigest(t, odd) {
		t.Error("audio changed")
	}
}

// tempFile writes data to name in a temp dir and returns its path.
func tempFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
