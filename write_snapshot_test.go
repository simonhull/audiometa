package audiometa

// These tests use the real writers. Other tests in this package swap
// lookupWriter, so none of them may call t.Parallel.

import (
	"bytes"
	"fmt"
	"testing"
)

const sampleMP3 = "testdata/sample.mp3"

// fieldMutations change the exported fields that describe the file. None of
// them may change what Write, WriteTo, AudioDigest or ExtractArtwork read.
var fieldMutations = []struct {
	name   string
	mutate func(f *File)
}{
	{"Size halved", func(f *File) { f.Size /= 2 }},
	{"Size zero", func(f *File) { f.Size = 0 }},
	{"Size grown", func(f *File) { f.Size += 1 << 20 }},
	{"Format changed", func(f *File) { f.Format = FormatFLAC }},
	{"Path changed", func(f *File) { f.Path = "elsewhere.flac" }},
}

func TestWrite_IgnoresMutatedFileFields(t *testing.T) {
	for _, inPlace := range []bool{false, true} {
		for _, m := range fieldMutations {
			t.Run(fmt.Sprintf("%s/inPlace=%v", m.name, inPlace), func(t *testing.T) {
				path, _ := copySample(t, sampleMP3)
				want := digestFile(t, path)
				var opts []WriteOption
				if inPlace {
					opts = append(opts, WithInPlace())
				}
				err := Write(path, func(f *File) error {
					f.Tags.Title = "Snapshot"
					m.mutate(f)
					return nil
				}, opts...)
				if err != nil {
					t.Fatalf("Write: %v", err)
				}
				if got := openSample(t, path).Tags.Title; got != "Snapshot" {
					t.Errorf("Title = %q, want Snapshot", got)
				}
				if got := digestFile(t, path); got != want {
					t.Error("audio digest changed: the write used the mutated field")
				}
			})
		}
	}
}

func TestWriteTo_IgnoresMutatedFileFields(t *testing.T) {
	for _, m := range fieldMutations {
		t.Run(m.name, func(t *testing.T) {
			path, src := copySample(t, sampleMP3)
			want := digestFile(t, path)

			f := openSample(t, path)
			m.mutate(f)
			var verbatim bytes.Buffer
			if _, err := f.WriteTo(&verbatim); err != nil {
				t.Fatalf("WriteTo without edits: %v", err)
			}
			if !bytes.Equal(verbatim.Bytes(), src) {
				t.Errorf("WriteTo without edits wrote %d bytes, want the %d source bytes", verbatim.Len(), len(src))
			}

			f.Tags.Title = "Snapshot"
			var edited bytes.Buffer
			if _, err := f.WriteTo(&edited); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			out := writeTemp(t, "out.mp3", edited.Bytes())
			if got := openSample(t, out).Tags.Title; got != "Snapshot" {
				t.Errorf("Title = %q, want Snapshot", got)
			}
			if got := digestFile(t, out); got != want {
				t.Error("audio digest changed: WriteTo used the mutated field")
			}
		})
	}
}

func TestReads_IgnoreMutatedFileFields(t *testing.T) {
	for _, m := range fieldMutations {
		t.Run(m.name, func(t *testing.T) {
			path, _ := copySample(t, sampleMP3)
			want := digestFile(t, path)
			f := openSample(t, path)
			m.mutate(f)
			if got, err := f.AudioDigest(); err != nil || got != want {
				t.Errorf("AudioDigest = %x, %v; want the unmutated digest", got[:4], err)
			}
			if art, err := f.ExtractArtwork(); err != nil || len(art) != 1 {
				t.Errorf("ExtractArtwork = %d images, %v; want 1, nil", len(art), err)
			}
		})
	}
}
