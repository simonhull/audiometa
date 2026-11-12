package audiometa

import (
	"bytes"
	"encoding/hex"
	"os"
	"slices"
	"strings"
	"testing"
)

// id3PrefixedFLAC writes sample.flac behind sample.mp3's ID3v2.3 tag, as
// some taggers leave FLAC files, and returns the path and the tag.
func id3PrefixedFLAC(t *testing.T) (path string, prefix []byte) { //nolint:nonamedreturns // the names document the results
	t.Helper()
	mp3, err := os.ReadFile("testdata/sample.mp3")
	if err != nil {
		t.Fatal(err)
	}
	prefix = mp3[:10+(int(mp3[6])<<21|int(mp3[7])<<14|int(mp3[8])<<7|int(mp3[9]))]
	flac, err := os.ReadFile("testdata/sample.flac")
	if err != nil {
		t.Fatal(err)
	}
	return writeTemp(t, "prefixed.flac", slices.Concat(prefix, flac)), prefix
}

func TestOpen_ID3PrefixedFLAC(t *testing.T) {
	path, _ := id3PrefixedFLAC(t)
	f := openSample(t, path)
	if f.Format != FormatFLAC || f.Tags.Title != "Test Title" || f.Audio.SampleRate != 44100 {
		t.Fatalf("Format %v, Title %q, SampleRate %d; want the FLAC stream", f.Format, f.Tags.Title, f.Audio.SampleRate)
	}
	if len(f.Warnings) != 1 || !strings.Contains(f.Warnings[0].Message, "non-standard ID3v2 tag") {
		t.Errorf("warnings = %v, want one about the ID3v2 tag", f.Warnings)
	}
	sum, err := f.AudioDigest()
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(sum[:]); got != sampleDigests["testdata/sample.flac"] {
		t.Errorf("AudioDigest = %s, want sample.flac's", got)
	}
	if art, err := f.ExtractArtwork(); err != nil || len(art) != 1 {
		t.Errorf("ExtractArtwork = %d pictures, %v; want sample.flac's one", len(art), err)
	}
}

func TestWrite_ID3PrefixedFLACKeepsPrefix(t *testing.T) {
	for name, opts := range map[string][]WriteOption{"atomic": nil, "in place": {WithInPlace()}} {
		t.Run(name, func(t *testing.T) {
			path, prefix := id3PrefixedFLAC(t)
			if err := Write(path, func(f *File) error { f.Tags.Title = "New"; return nil }, opts...); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(data, slices.Concat(prefix, []byte("fLaC"))) {
				t.Error("the ID3v2 tag in front of the stream was not kept byte for byte")
			}
			f := openSample(t, path)
			if f.Format != FormatFLAC || f.Tags.Title != "New" {
				t.Errorf("Format %v, Title %q; want FLAC, New", f.Format, f.Tags.Title)
			}
			sum, err := f.AudioDigest()
			if err != nil {
				t.Fatal(err)
			}
			if got := hex.EncodeToString(sum[:]); got != sampleDigests["testdata/sample.flac"] {
				t.Errorf("AudioDigest = %s after the write, want sample.flac's", got)
			}
		})
	}
}
