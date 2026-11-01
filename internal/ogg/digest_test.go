package ogg

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"slices"
	"testing"

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
	got, err := digester{}.AudioDigest(context.Background(), bytes.NewReader(data), int64(len(data)))
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

func TestAudioDigest_ChangesWithAudio(t *testing.T) {
	a := buildOgg(pageOf(1, opusHead), pageOf(1, commentHeader(10)), pageOf(1, audio1, audio2))
	b := buildOgg(pageOf(1, opusHead), pageOf(1, commentHeader(10)), pageOf(1, audio1, audio3))
	if bytes.Equal(digestOf(t, a), digestOf(t, b)) {
		t.Error("streams with different audio have the same digest")
	}
}

func TestAudioDigest_Errors(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"unknown codec", buildOgg(pageOf(1, []byte("not a codec header")), pageOf(1, audio1))},
		{"ends inside the header packets", buildOgg(pageOf(1, vorbisID), pageOf(1, commentHeader(10)))},
		{"not an Ogg stream", []byte("RIFF....WAVEfmt ")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := digester{}.AudioDigest(context.Background(), bytes.NewReader(tt.data), int64(len(tt.data)))
			if err == nil {
				t.Fatal("AudioDigest succeeded, want an error")
			}
		})
	}
}

func TestAudioDigest_StructuralErrorsAreCorruptedFileErrors(t *testing.T) {
	data := buildOgg(pageOf(1, vorbisID), pageOf(1, commentHeader(10)))
	_, err := digester{}.AudioDigest(context.Background(), bytes.NewReader(data), int64(len(data)))
	if _, ok := errors.AsType[*types.CorruptedFileError](err); !ok {
		t.Errorf("err = %v, want *types.CorruptedFileError", err)
	}
}

func TestAudioDigest_Canceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	data := buildOgg(pageOf(1, opusHead), pageOf(1, commentHeader(10)), pageOf(1, audio1))
	if _, err := (digester{}).AudioDigest(ctx, bytes.NewReader(data), int64(len(data))); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}
