package iff

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/testctx"
	"github.com/simonhull/audiometa/internal/types"
)

// corpus reads a file of the root testdata corpus.
func corpus(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// parse parses data as format and fails the test on an error.
func parse(t *testing.T, format types.Format, data []byte) *types.File {
	t.Helper()
	f, err := (&parser{format: format}).Parse(t.Context(), bytes.NewReader(data), int64(len(data)), "test")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return f
}

func TestParse_Corpus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		format    types.Format
		audio     types.AudioInfo
		title     string
		artist    string
		comment   string
		track     int
		year      int
		raw       map[string][]string
		container string
	}{
		{
			name: "sample.wav", format: types.FormatWAV,
			audio: types.AudioInfo{Codec: codecPCM, Container: "WAV", SampleRate: 8000, BitDepth: 16, Channels: 1, Bitrate: 128000, Lossless: true, Duration: 250 * time.Millisecond},
			title: "Test Title", artist: "Test Artist", comment: "A comment", track: 3, year: 2021,
			raw: map[string][]string{"INAM": {"Test Title"}, "IPRT": {"3"}, "IGNR": {"Rock"}, "ISFT": {"Lavf63.1.102"}},
		},
		{
			// ID3 wins per field; INFO stays in the raw tags, and fills the
			// comment ID3 lacks.
			name: "sample_id3.wav", format: types.FormatWAV,
			audio: types.AudioInfo{Codec: codecPCM, Container: "WAV", SampleRate: 8000, BitDepth: 16, Channels: 1, Bitrate: 128000, Lossless: true, Duration: 250 * time.Millisecond},
			title: "Test Title", artist: "Test Artist", comment: "Info comment", track: 3, year: 2021,
			raw: map[string][]string{"INAM": {"Info Title"}, "IART": {"Info Artist"}, "TIT2": {"Test Title"}, "TRCK": {"3/12"}},
		},
		{
			name: "sample_ext.wav", format: types.FormatWAV,
			audio: types.AudioInfo{Codec: codecPCM, Container: "WAV", SampleRate: 8000, BitDepth: 24, Channels: 2, Bitrate: 384000, Lossless: true, Duration: 100 * time.Millisecond},
		},
		{
			name: "sample_float.wav", format: types.FormatWAV,
			audio: types.AudioInfo{Codec: codecFloat, Container: "WAV", SampleRate: 8000, BitDepth: 32, Channels: 1, Bitrate: 256000, Lossless: true, Duration: 100 * time.Millisecond},
		},
		{
			name: "sample_rf64.wav", format: types.FormatWAV,
			audio: types.AudioInfo{Codec: codecPCM, Container: "RF64", SampleRate: 8000, BitDepth: 16, Channels: 1, Bitrate: 128000, Lossless: true, Duration: 250 * time.Millisecond},
			title: "Test Title",
		},
		{
			name: "sample.aiff", format: types.FormatAIFF,
			audio: types.AudioInfo{Codec: codecPCM, Container: "AIFF", SampleRate: 8000, BitDepth: 16, Channels: 1, Bitrate: 128000, Lossless: true, Duration: 250 * time.Millisecond},
			title: "Test Title", artist: "Test Artist", comment: "A comment", track: 3, year: 2021,
			raw: map[string][]string{"NAME": {"Test Title"}, keyAIFFCopyright: {"(C) Someone"}, "ANNO": {"A comment"}, "TIT2": {"Test Title"}},
		},
		{
			name: "sample.aifc", format: types.FormatAIFF,
			audio: types.AudioInfo{Codec: codecFloat, Container: "AIFF-C", SampleRate: 8000, BitDepth: 32, Channels: 1, Bitrate: 256000, Lossless: true, Duration: 100 * time.Millisecond},
			title: "Test Title",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := parse(t, tc.format, corpus(t, tc.name))
			if f.Audio != tc.audio {
				t.Errorf("Audio = %+v\nwant    %+v", f.Audio, tc.audio)
			}
			if len(f.Warnings) > 0 {
				t.Errorf("Warnings = %v", f.Warnings)
			}
			tags := &f.Tags
			if tags.Title != tc.title || tags.Artist != tc.artist || tags.Comment != tc.comment || tags.TrackNumber != tc.track || tags.Year != tc.year {
				t.Errorf("tags = %q %q %q %d %d, want %q %q %q %d %d", tags.Title, tags.Artist, tags.Comment, tags.TrackNumber, tags.Year,
					tc.title, tc.artist, tc.comment, tc.track, tc.year)
			}
			for k, want := range tc.raw {
				if got := tags.Get(k); !slices.Equal(got, want) {
					t.Errorf("raw %q = %q, want %q", k, got, want)
				}
			}
		})
	}
}

func TestExtractArtwork_Corpus(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		format types.Format
		want   int
	}{
		{"sample.wav", types.FormatWAV, 0},
		{"sample_id3.wav", types.FormatWAV, 1},
		{"sample.aiff", types.FormatAIFF, 1},
	} {
		data := corpus(t, tc.name)
		art, err := (&parser{format: tc.format}).ExtractArtwork(t.Context(), bytes.NewReader(data), int64(len(data)), tc.name)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if len(art) != tc.want {
			t.Errorf("%s: %d pictures, want %d", tc.name, len(art), tc.want)
		}
		for _, a := range art {
			if a.MIMEType != "image/jpeg" || !bytes.HasPrefix(a.Data, []byte{0xFF, 0xD8}) {
				t.Errorf("%s: picture %s of %d bytes", tc.name, a.MIMEType, len(a.Data))
			}
		}
	}
}

// riff builds a RIFF WAVE file from chunks, given as ID and payload pairs.
func riff(order byteOrder, magic string, chunks ...string) []byte {
	var body []byte
	for i := 0; i+1 < len(chunks); i += 2 {
		body = append(body, chunks[i]...)
		body = order.AppendUint32(body, uint32(len(chunks[i+1])))
		body = append(body, chunks[i+1]...)
		if len(chunks[i+1])%2 == 1 {
			body = append(body, 0)
		}
	}
	out := []byte(magic)
	out = order.AppendUint32(out, uint32(4+len(body)))
	out = append(out, "WAVE"...)
	return append(out, body...)
}

// pcmFmt is a 16-bit mono 8 kHz PCM fmt payload.
func pcmFmt(order byteOrder) string {
	b := order.AppendUint16(nil, wavePCM)
	b = order.AppendUint16(b, 1)
	b = order.AppendUint32(b, 8000)
	b = order.AppendUint32(b, 16000)
	b = order.AppendUint16(b, 2)
	b = order.AppendUint16(b, 16)
	return string(b)
}

// info builds a LIST/INFO payload from ID and value pairs.
func info(order byteOrder, pairs ...string) string {
	b := []byte("INFO")
	for i := 0; i+1 < len(pairs); i += 2 {
		v := pairs[i+1] + "\x00"
		b = append(b, pairs[i]...)
		b = order.AppendUint32(b, uint32(len(v)))
		b = append(b, v...)
		if len(v)%2 == 1 {
			b = append(b, 0)
		}
	}
	return string(b)
}

func TestParse_RIFX(t *testing.T) {
	t.Parallel()
	be := binary.BigEndian
	data := riff(be, magicRIFX, "fmt ", pcmFmt(be), "LIST", info(be, "INAM", "Big", "IART", "Endian"), "data", strings.Repeat("\x00", 1600))
	f := parse(t, types.FormatWAV, data)
	if f.Audio.Duration != 100*time.Millisecond || f.Audio.SampleRate != 8000 {
		t.Errorf("Audio = %+v", f.Audio)
	}
	if f.Tags.Title != "Big" || f.Tags.Artist != "Endian" {
		t.Errorf("Title %q, Artist %q", f.Tags.Title, f.Tags.Artist)
	}
}

func TestParse_OddChunksArePadded(t *testing.T) {
	t.Parallel()
	le := binary.LittleEndian
	// "junk" has an odd size, so a pad byte follows it before INFO.
	data := riff(le, magicRIFF, "fmt ", pcmFmt(le), "junk", "abc", "LIST", info(le, "INAM", "Odd"), "data", "\x00\x00")
	f := parse(t, types.FormatWAV, data)
	if f.Tags.Title != "Odd" || len(f.Warnings) > 0 {
		t.Errorf("Title %q, Warnings %v", f.Tags.Title, f.Warnings)
	}
}

func TestParse_SloppyFiles(t *testing.T) {
	t.Parallel()
	le := binary.LittleEndian
	good := riff(le, magicRIFF, "fmt ", pcmFmt(le), "LIST", info(le, "INAM", "Kept"), "data", strings.Repeat("\x00", 1600))
	unset := bytes.Clone(good)
	le.PutUint32(unset[4:], 0)
	tooBig := bytes.Clone(good)
	le.PutUint32(tooBig[4:], uint32(len(good)*2))
	trailing := append(bytes.Clone(good), "TAG garbage after the form"...)
	stray := append(bytes.Clone(good), 1, 2, 3)
	le.PutUint32(stray[4:], uint32(len(stray)-8))
	cases := []struct {
		name     string
		data     []byte
		warnings int
		duration time.Duration
		broken   bool
	}{
		{"cut short", good[:len(good)-600], 2, 62500 * time.Microsecond, true}, // the form and the data chunk
		{"unset form size", unset, 1, 100 * time.Millisecond, false},
		{"form size past the end", tooBig, 1, 100 * time.Millisecond, false},
		{"junk after the form", trailing, 0, 100 * time.Millisecond, false},
		{"stray bytes", stray, 1, 100 * time.Millisecond, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := parse(t, types.FormatWAV, tc.data)
			if len(f.Warnings) != tc.warnings {
				t.Errorf("Warnings = %v, want %d", f.Warnings, tc.warnings)
			}
			if f.Audio.Duration != tc.duration || f.Tags.Title != "Kept" {
				t.Errorf("Duration %v, Title %q; want %v, Kept", f.Audio.Duration, f.Tags.Title, tc.duration)
			}
			c, err := walk(t.Context(), newReader(tc.data), int64(len(tc.data)))
			if err != nil {
				t.Fatal(err)
			}
			if c.broken != tc.broken {
				t.Errorf("broken = %v, want %v", c.broken, tc.broken)
			}
		})
	}
}

func TestParse_Refused(t *testing.T) {
	t.Parallel()
	le := binary.LittleEndian
	cases := map[string][]byte{
		"no fmt":       riff(le, magicRIFF, "data", "\x00\x00"),
		"short fmt":    riff(le, magicRIFF, "fmt ", "\x01\x00", "data", "\x00\x00"),
		"RF64 no ds64": riff(le, magicRF64, "fmt ", pcmFmt(le), "data", "\x00\x00"),
		"too short":    []byte("RIFF\x00\x00"),
	}
	for name, data := range cases {
		_, err := (&parser{format: types.FormatWAV}).Parse(t.Context(), bytes.NewReader(data), int64(len(data)), "x.wav")
		if _, ok := errors.AsType[*types.CorruptedFileError](err); !ok {
			t.Errorf("%s: err = %v, want a CorruptedFileError", name, err)
		}
	}
	aiff := corpus(t, "sample.aiff")
	if _, err := (&parser{format: types.FormatWAV}).Parse(t.Context(), bytes.NewReader(aiff), int64(len(aiff)), "x"); err == nil {
		t.Error("the WAV parser took an AIFF file")
	}
}

func TestParse_Latin1Text(t *testing.T) {
	t.Parallel()
	le := binary.LittleEndian
	data := riff(le, magicRIFF, "fmt ", pcmFmt(le), "LIST", info(le, "INAM", "Caf\xe9"), "data", "\x00\x00")
	if got := parse(t, types.FormatWAV, data).Tags.Title; got != "Café" {
		t.Errorf("Title = %q, want Café", got)
	}
}

func TestExtendedFloat(t *testing.T) {
	t.Parallel()
	for _, rate := range []float64{8000, 44100, 48000, 96000, 192000, 11025.5} {
		frac, exp := math.Frexp(rate) // rate = frac * 2^exp, frac in [0.5, 1)
		var b [extendedSize]byte
		binary.BigEndian.PutUint16(b[0:], uint16(exp-1+16383))
		binary.BigEndian.PutUint64(b[2:], uint64(math.Ldexp(frac, 64)))
		if got := extendedFloat(b); got != rate {
			t.Errorf("extendedFloat(%v) = %v", rate, got)
		}
	}
}

func TestParse_Cancel(t *testing.T) {
	t.Parallel()
	data := corpus(t, "sample_id3.wav")
	for n := 1; n <= 2; n++ {
		_, err := (&parser{format: types.FormatWAV}).Parse(testctx.CancelAfter(n), bytes.NewReader(data), int64(len(data)), "x.wav")
		if !errors.Is(err, context.Canceled) {
			t.Errorf("CancelAfter(%d): err = %v, want context.Canceled", n, err)
		}
	}
}

// newReader returns a SafeReader over data.
func newReader(data []byte) *binutil.SafeReader {
	return binutil.NewSafeReader(bytes.NewReader(data), int64(len(data)), "test")
}
