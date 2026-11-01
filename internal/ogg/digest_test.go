package ogg

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/simonhull/audiometa/internal/registry"
	"github.com/simonhull/audiometa/internal/types"
)

// Packets shared by the synthetic streams below.
var (
	vorbisID = append([]byte("\x01vorbis"), make([]byte, 23)...)
	opusHead = append([]byte("OpusHead"), make([]byte, 11)...)
	setup    = bytes.Repeat([]byte{5}, 40)
	audio1   = bytes.Repeat([]byte{0xA1}, 300)
	audio2   = bytes.Repeat([]byte{0xA2}, 17)
	audio3   = bytes.Repeat([]byte{0xA3}, 255)
)

// commentHeader returns a comment header packet of n bytes.
func commentHeader(n int) []byte { return bytes.Repeat([]byte{3}, n) }

// pageOf builds a page on serial carrying whole packets, each ending on it.
func pageOf(serial uint32, pkts ...[]byte) pageSpec {
	p := pageSpec{serial: serial}
	for _, pk := range pkts {
		p.lacing = append(p.lacing, lacingFor(len(pk))...)
		p.data = append(p.data, pk...)
	}
	return p
}

// wantDigest is the SHA-256 of the concatenated audio packets.
func wantDigest(pkts ...[]byte) []byte {
	sum := sha256.Sum256(slices.Concat(pkts...))
	return sum[:]
}

func digestOf(t *testing.T, data []byte) []byte {
	t.Helper()
	got, err := digester{}.AudioDigest(t.Context(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("AudioDigest: %v", err)
	}
	return got
}

func TestAudioDigest_HashesOnlyAudioPackets(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"vorbis, audio on fresh pages", buildOgg(
			pageOf(1, vorbisID),
			pageOf(1, commentHeader(10), setup),
			pageOf(1, audio1, audio2),
			pageOf(1, audio3),
		)},
		{"vorbis, bigger comment and different pagination", buildOgg(
			pageOf(1, vorbisID),
			pageOf(1, commentHeader(600)),
			pageOf(1, setup),
			pageOf(1, audio1),
			pageOf(1, audio2, audio3),
		)},
		{"vorbis, audio starts mid-page", buildOgg(
			pageOf(1, vorbisID),
			pageOf(1, commentHeader(10)),
			pageOf(1, setup, audio1),
			pageOf(1, audio2, audio3),
		)},
		{"opus, two header packets", buildOgg(
			pageOf(1, opusHead),
			pageOf(1, commentHeader(10)),
			pageOf(1, audio1, audio2, audio3),
		)},
		{"other logical streams are ignored", buildOgg(
			pageOf(1, vorbisID),
			pageOf(2, bytes.Repeat([]byte{9}, 50)),
			pageOf(1, commentHeader(10), setup),
			pageOf(1, audio1),
			pageOf(2, bytes.Repeat([]byte{8}, 50)),
			pageOf(1, audio2, audio3),
		)},
	}
	want := wantDigest(audio1, audio2, audio3)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := digestOf(t, tt.data); !bytes.Equal(got, want) {
				t.Errorf("digest = %x, want %x", got, want)
			}
		})
	}
}

func TestAudioDigest_PacketsSpanningPages(t *testing.T) {
	big := bytes.Repeat([]byte{0xB1}, 70_000) // more than one page's 255 segments
	tests := []struct {
		name string
		data []byte
		want []byte
	}{
		{"audio packet continued across pages", buildOgg(slices.Concat(
			[]pageSpec{pageOf(1, opusHead), pageOf(1, commentHeader(10))},
			splitAcrossPages(big, 0, 1),
			[]pageSpec{pageOf(1, audio1)},
		)...), wantDigest(big, audio1)},
		{"header packet continued across pages", buildOgg(slices.Concat(
			[]pageSpec{pageOf(1, opusHead)},
			splitAcrossPages(commentHeader(70_000), 0, 1),
			[]pageSpec{pageOf(1, audio1, audio2)},
		)...), wantDigest(audio1, audio2)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := digestOf(t, tt.data); !bytes.Equal(got, tt.want) {
				t.Errorf("digest = %x, want %x", got, tt.want)
			}
		})
	}
}

// lastPage marks p as the last page of its logical stream.
func lastPage(p pageSpec) pageSpec {
	p.headerType |= pageEOS
	return p
}

func TestAudioDigest_StreamEnd(t *testing.T) {
	clean := buildOgg(pageOf(1, opusHead), pageOf(1, commentHeader(10)), lastPage(pageOf(1, audio1, audio2)))
	tests := []struct {
		name string
		data []byte
	}{
		{"chained link after the last page is ignored", slices.Concat(clean, buildOgg(
			pageOf(1, opusHead), pageOf(1, commentHeader(10)), pageOf(1, audio3),
		))},
		{"appended tag after the audio is ignored", slices.Concat(clean, append([]byte("TAG"), make([]byte, 125)...))},
		{"truncated trailing page is ignored", slices.Concat(clean, buildOgg(pageOf(1, audio3))[:40])},
	}
	want := wantDigest(audio1, audio2)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := digestOf(t, tt.data); !bytes.Equal(got, want) {
				t.Errorf("digest = %x, want %x", got, want)
			}
		})
	}
}

func TestAudioDigest_ChangesWithAudio(t *testing.T) {
	a := buildOgg(pageOf(1, opusHead), pageOf(1, commentHeader(10)), pageOf(1, audio1, audio2))
	b := buildOgg(pageOf(1, opusHead), pageOf(1, commentHeader(10)), pageOf(1, audio1, audio3))
	if bytes.Equal(digestOf(t, a), digestOf(t, b)) {
		t.Error("streams with different audio have the same digest")
	}
}

func TestAudioDigest_Errors(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		wantCorrupt bool
	}{
		{"empty input", nil, true},
		{"unknown codec", buildOgg(pageOf(1, []byte("not a codec header")), pageOf(1, audio1)), true},
		{"ends inside the header packets", buildOgg(pageOf(1, vorbisID), pageOf(1, commentHeader(10))), true},
		{"not an Ogg stream", []byte("RIFF....WAVEfmt "), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := digester{}.AudioDigest(t.Context(), bytes.NewReader(tt.data), int64(len(tt.data)))
			if err == nil {
				t.Fatal("AudioDigest succeeded, want an error")
			}
			if _, ok := errors.AsType[*types.CorruptedFileError](err); ok != tt.wantCorrupt {
				t.Errorf("err = %v; CorruptedFileError = %v, want %v", err, ok, tt.wantCorrupt)
			}
		})
	}
}

// failingReader serves data, then fails every read at or past failAt.
type failingReader struct {
	data   []byte
	failAt int64
}

var errDisk = errors.New("disk error")

func (r failingReader) ReadAt(p []byte, off int64) (int, error) {
	if off+int64(len(p)) > r.failAt {
		return 0, errDisk
	}
	return bytes.NewReader(r.data).ReadAt(p, off)
}

func TestAudioDigest_ReadErrorsPassThrough(t *testing.T) {
	data := buildOgg(pageOf(1, opusHead), pageOf(1, commentHeader(10)), pageOf(1, audio1))
	src := failingReader{data: data, failAt: 10}
	_, err := digester{}.AudioDigest(t.Context(), src, int64(len(data)))
	if !errors.Is(err, errDisk) {
		t.Errorf("err = %v, want it to wrap the read error", err)
	}
}

func TestAudioDigest_Canceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	data := buildOgg(pageOf(1, opusHead), pageOf(1, commentHeader(10)), pageOf(1, audio1))
	if _, err := (digester{}).AudioDigest(ctx, bytes.NewReader(data), int64(len(data))); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestAudioDigest_SampleFiles(t *testing.T) {
	for _, name := range []string{"../../testdata/sample.ogg", "../../testdata/sample.opus"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			first := digestOf(t, data)
			if len(first) != sha256.Size {
				t.Fatalf("digest length = %d, want %d", len(first), sha256.Size)
			}
			if !bytes.Equal(digestOf(t, data), first) {
				t.Error("digest is not deterministic")
			}

			// The last byte of an Ogg file is audio payload; readPage does
			// not verify CRCs, so the flipped file still parses.
			flipped := slices.Clone(data)
			flipped[len(flipped)-1] ^= 0xFF
			if bytes.Equal(digestOf(t, flipped), first) {
				t.Error("changing an audio byte did not change the digest")
			}
		})
	}
}

func TestDigesterRegistered(t *testing.T) {
	for _, f := range []types.Format{types.FormatOgg, types.FormatOpus} {
		if registry.GetDigester(f) == nil {
			t.Errorf("no digester registered for %s", f)
		}
	}
}
