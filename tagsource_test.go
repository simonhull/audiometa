package audiometa_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/simonhull/audiometa"
)

// tagSourcesOf returns the sources f reports.
func tagSourcesOf(t *testing.T, path string) []audiometa.TagSource {
	t.Helper()
	return openFile(t, path).TagSources
}

func src(kind audiometa.TagKind, version int, used bool) audiometa.TagSource {
	return audiometa.TagSource{Kind: kind, Version: version, Used: used}
}

func TestTagSource_Corpus(t *testing.T) {
	t.Parallel()
	vorbis := []audiometa.TagSource{src(audiometa.TagVorbisComment, 0, true)}
	tests := []struct {
		path string
		want []audiometa.TagSource
	}{
		{"testdata/sample.mp3", []audiometa.TagSource{src(audiometa.TagID3v2, 3, true)}},
		{"testdata/sample_v24.mp3", []audiometa.TagSource{src(audiometa.TagID3v2, 4, true), src(audiometa.TagID3v1, 0, false)}},
		{"testdata/sample_mpeg2.mp3", []audiometa.TagSource{src(audiometa.TagID3v2, 4, true)}},
		{"testdata/sample.flac", vorbis},
		{"testdata/sample_upper.flac", vorbis},
		{"testdata/sample.ogg", vorbis},
		{"testdata/sample.opus", vorbis},
		{"testdata/sample.m4a", []audiometa.TagSource{src(audiometa.TagMP4, 0, true)}},
		{"testdata/sample.m4b", []audiometa.TagSource{src(audiometa.TagMP4, 0, true)}},
		{"testdata/sample_id3.wav", []audiometa.TagSource{src(audiometa.TagID3v2, 3, true), src(audiometa.TagRIFFInfo, 0, true)}},
		{"testdata/sample.wav", []audiometa.TagSource{src(audiometa.TagRIFFInfo, 0, true)}},
		{"testdata/sample.aiff", []audiometa.TagSource{src(audiometa.TagID3v2, 4, true), src(audiometa.TagAIFFText, 0, true)}},
		{"testdata/sample.aifc", []audiometa.TagSource{src(audiometa.TagAIFFText, 0, true)}},
	}
	for _, tt := range tests {
		t.Run(filepath.Base(tt.path), func(t *testing.T) {
			t.Parallel()
			if got := tagSourcesOf(t, tt.path); !slices.Equal(got, tt.want) {
				t.Errorf("TagSources = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// tempFileOf writes the parts of data into a temp dir under name and returns its path.
func tempFileOf(t *testing.T, name string, data ...[]byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, bytes.Join(data, nil), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func id3v1Block(title, artist string) []byte {
	b := make([]byte, 128)
	copy(b, "TAG")
	copy(b[3:], title)
	copy(b[33:], artist)
	return b
}

// apeBlock is an empty APEv2 tag: a footer only, with no items.
func apeBlock() []byte {
	b := make([]byte, 32)
	copy(b, "APETAGEX")
	binary.LittleEndian.PutUint32(b[8:], 2000)
	binary.LittleEndian.PutUint32(b[12:], 32)
	return b
}

func lyrics3Block() []byte {
	return fmt.Appendf(nil, "LYRICSBEGIN%06dLYRICS200", len("LYRICSBEGIN"))
}

// id3v2FooterBlock is an empty ID3v2.4 tag with a footer, appended after audio.
func id3v2FooterBlock() []byte {
	return slices.Concat([]byte{'I', 'D', '3', 4, 0, 0x10, 0, 0, 0, 0}, []byte{'3', 'D', 'I', 4, 0, 0x10, 0, 0, 0, 0})
}

// stripID3v2 returns the MP3 data without its leading ID3v2 tag.
func stripID3v2(data []byte) []byte {
	n := 10 + (int(data[6])<<21 | int(data[7])<<14 | int(data[8])<<7 | int(data[9]))
	return data[n:]
}

func TestTagSource_MP3Trailers(t *testing.T) {
	t.Parallel()
	sample := readTestdata(t, "testdata/sample.mp3")
	v2 := src(audiometa.TagID3v2, 3, true)
	tests := []struct {
		name  string
		data  [][]byte
		want  []audiometa.TagSource
		title string
	}{
		{"untagged", [][]byte{stripID3v2(sample)}, nil, ""},
		{"ID3v1 only is used", [][]byte{stripID3v2(sample), id3v1Block("V1 Title", "V1 Artist")},
			[]audiometa.TagSource{src(audiometa.TagID3v1, 0, true)}, "V1 Title"},
		{"empty ID3v1 only supplies nothing", [][]byte{stripID3v2(sample), id3v1Block("", "")},
			[]audiometa.TagSource{src(audiometa.TagID3v1, 0, false)}, ""},
		{"ID3v1 behind ID3v2 with a title is not used", [][]byte{sample, id3v1Block("V1 Title", "V1 Artist")},
			[]audiometa.TagSource{v2, src(audiometa.TagID3v1, 0, false)}, "Test Title"},
		{"APEv2 is listed, unread", [][]byte{sample, apeBlock()},
			[]audiometa.TagSource{v2, src(audiometa.TagAPEv2, 0, false)}, "Test Title"},
		{"Lyrics3 is listed, unread", [][]byte{sample, lyrics3Block()},
			[]audiometa.TagSource{v2, src(audiometa.TagLyrics3, 0, false)}, "Test Title"},
		{"ID3v2 with footer after the audio is listed, unread", [][]byte{sample, id3v2FooterBlock()},
			[]audiometa.TagSource{v2, src(audiometa.TagID3v2, 4, false)}, "Test Title"},
		{"ID3v1 followed by APEv2 is not the last block, so unused", [][]byte{stripID3v2(sample), id3v1Block("V1 Title", "V1 Artist"), apeBlock()},
			[]audiometa.TagSource{src(audiometa.TagID3v1, 0, false), src(audiometa.TagAPEv2, 0, false)}, ""},
		{"ID3v1 behind APEv2 is the last block, so used", [][]byte{stripID3v2(sample), apeBlock(), id3v1Block("V1 Title", "V1 Artist")},
			[]audiometa.TagSource{src(audiometa.TagID3v1, 0, true), src(audiometa.TagAPEv2, 0, false)}, "V1 Title"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := openFile(t, tempFileOf(t, "t.mp3", tt.data...))
			if !slices.Equal(f.TagSources, tt.want) {
				t.Errorf("TagSources = %+v, want %+v", f.TagSources, tt.want)
			}
			if f.Tags.Title != tt.title {
				t.Errorf("Title = %q, want %q", f.Tags.Title, tt.title)
			}
		})
	}
}

func TestTagSource_MP3UnsupportedID3Version(t *testing.T) {
	t.Parallel()
	data := bytes.Clone(readTestdata(t, "testdata/sample.mp3"))
	data[3] = 5
	f := openFile(t, tempFileOf(t, "v5.mp3", data))
	want := []audiometa.TagSource{src(audiometa.TagID3v2, 5, false)}
	if !slices.Equal(f.TagSources, want) {
		t.Errorf("TagSources = %+v, want %+v", f.TagSources, want)
	}
}

func TestTagSource_FLACBehindID3v2(t *testing.T) {
	t.Parallel()
	prefix := readTestdata(t, "testdata/sample.mp3")
	prefix = prefix[:len(prefix)-len(stripID3v2(prefix))]
	flac := readTestdata(t, "testdata/sample.flac")
	f := openFile(t, tempFileOf(t, "prefixed.flac", prefix, flac, apeBlock()))
	want := []audiometa.TagSource{
		src(audiometa.TagVorbisComment, 0, true),
		src(audiometa.TagID3v2, 3, false),
		src(audiometa.TagAPEv2, 0, false),
	}
	if !slices.Equal(f.TagSources, want) {
		t.Errorf("TagSources = %+v, want %+v", f.TagSources, want)
	}
}

func TestTagSource_AfterWrite(t *testing.T) {
	t.Parallel()
	p := tempFileOf(t, "w.mp3", readTestdata(t, "testdata/sample_v24.mp3"))
	if err := audiometa.Write(p, setTitle("Edited")); err != nil {
		t.Fatal(err)
	}
	want := []audiometa.TagSource{src(audiometa.TagID3v2, 4, true), src(audiometa.TagID3v1, 0, false)}
	if got := tagSourcesOf(t, p); !slices.Equal(got, want) {
		t.Errorf("TagSources after Write = %+v, want %+v", got, want)
	}
}
