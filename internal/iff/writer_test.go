package iff

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/simonhull/audiometa/internal/types"
)

// rewriteWith parses data as format, applies edit to its tags and returns what
// the writer makes of it.
func rewriteWith(t *testing.T, format types.Format, data []byte, edit func(*types.Tags)) ([]byte, error) {
	t.Helper()
	orig := parse(t, format, data)
	cur := orig.Tags.Clone()
	edit(cur)
	keys, fields, err := types.Diff(&orig.Tags, cur, format)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	edits := &types.EditSet{Keys: keys, Fields: fields, Orig: &orig.Tags, Final: cur}
	var out bytes.Buffer
	_, err = (&writer{format: format}).WriteFile(t.Context(), &out, bytes.NewReader(data), int64(len(data)), edits)
	return out.Bytes(), err
}

// preserved returns the writer's PreservedDigest of data.
func preserved(t *testing.T, format types.Format, data []byte) []byte {
	t.Helper()
	sum, err := (&writer{format: format}).PreservedDigest(t.Context(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("PreservedDigest: %v", err)
	}
	return sum
}

func TestWriteFile_ManyChunksIsLinear(t *testing.T) {
	t.Parallel()
	le := binary.LittleEndian
	chunks := slices.Grow([]string{"fmt ", pcmFmt(le)}, 2*(maxChunks-2)+2)
	for range maxChunks - 2 {
		chunks = append(chunks, "JUNK", "")
	}
	data := riff(le, magicRIFF, append(chunks, "data", "\x00\x00")...)
	start := time.Now()
	if _, err := rewriteWith(t, types.FormatWAV, data, func(tags *types.Tags) { tags.Title = "x" }); err != nil {
		t.Fatal(err)
	}
	preserved(t, types.FormatWAV, data)
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("writing and digesting %d chunks took %v, want well under 10s", maxChunks, d)
	}
}

// rf64 builds an RF64 WAVE file: a ds64 chunk with payload ds, then chunks
// as for riff. It fills in the ds64 RIFF and data sizes.
func rf64(ds []byte, chunks ...string) []byte {
	le := binary.LittleEndian
	data := riff(le, magicRF64, append([]string{idDS64, string(ds)}, chunks...)...)
	le.PutUint32(data[4:], unsetSize)
	le.PutUint64(data[20:], uint64(len(data)-8))
	for i := 0; i+1 < len(chunks); i += 2 {
		if chunks[i] == idData {
			le.PutUint64(data[28:], uint64(len(chunks[i+1])))
		}
	}
	return data
}

func TestWalk_DS64Table(t *testing.T) {
	t.Parallel()
	le := binary.LittleEndian
	ds := make([]byte, ds64MinSize+ds64EntrySize)
	le.PutUint32(ds[24:], 1) // one table entry
	copy(ds[28:], "junk")
	le.PutUint64(ds[32:], 3)
	data := rf64(ds, "fmt ", pcmFmt(le), "junk", "abc", "data", "\x00\x00")
	at := 20 + len(ds) // past the ds64 chunk, whose table also holds "junk"
	at += bytes.Index(data[at:], []byte("junk"))
	le.PutUint32(data[at+4:], unsetSize) // the junk chunk's size now comes from ds64
	c, err := walk(t.Context(), newReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	junk, ok := c.first("junk")
	if !ok || junk.size != 3 || c.broken || len(c.warnings) > 0 {
		t.Errorf("junk = %+v (found %v), broken %v, warnings %v; want size 3 from ds64", junk, ok, c.broken, c.warnings)
	}
}

func TestWriteFile_OddDS64KeepsItsPad(t *testing.T) {
	t.Parallel()
	le := binary.LittleEndian
	data := rf64(make([]byte, ds64MinSize+1), "fmt ", pcmFmt(le), "data", strings.Repeat("\x00", 1600))
	out, err := rewriteWith(t, types.FormatWAV, data, func(tags *types.Tags) { tags.Title = "Odd" })
	if err != nil {
		t.Fatal(err)
	}
	f, err := (&parser{format: types.FormatWAV}).Parse(t.Context(), bytes.NewReader(out), int64(len(out)), "out")
	if err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
	if f.Tags.Title != "Odd" || f.Audio.Duration != 100*time.Millisecond || len(f.Warnings) > 0 {
		t.Errorf("Title %q, Duration %v, Warnings %v", f.Tags.Title, f.Audio.Duration, f.Warnings)
	}
	if !bytes.Equal(preserved(t, types.FormatWAV, out), preserved(t, types.FormatWAV, data)) {
		t.Error("preserved digest changed")
	}
}

// fmtChunk is a fmt payload with the given fields.
func fmtChunk(tag, channels uint16, rate, byteRate uint32, align, bits uint16) string {
	le := binary.LittleEndian
	b := le.AppendUint16(nil, tag)
	b = le.AppendUint16(b, channels)
	b = le.AppendUint32(b, rate)
	b = le.AppendUint32(b, byteRate)
	b = le.AppendUint16(b, align)
	b = le.AppendUint16(b, bits)
	return string(b)
}

func TestParse_AudioValuesOutOfRange(t *testing.T) {
	t.Parallel()
	le := binary.LittleEndian
	cases := []struct {
		name          string
		fmt           string
		rate, bitrate int
		warning       bool
	}{
		{"ordinary", fmtChunk(wavePCM, 2, 48000, 192000, 4, 16), 48000, 1536000, false},
		{"sample rate past int32", fmtChunk(wavePCM, 1, 0xFFFFFFFF, 0, 2, 16), 0, 0, true},
		{"bit rate past int32", fmtChunk(wavePCM, 0xFFFF, 192000, 0, 2, 0xFFFF), 192000, 0, true},
		{"byte rate past int32", fmtChunk(waveMP3, 1, 8000, 0xFFFFFFFF, 1, 0), 8000, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := parse(t, types.FormatWAV, riff(le, magicRIFF, "fmt ", tc.fmt, "data", strings.Repeat("\x00", 4000)))
			if a := f.Audio; a.SampleRate != tc.rate || a.Bitrate != tc.bitrate || a.Duration < 0 {
				t.Errorf("SampleRate %d, Bitrate %d, Duration %v; want %d, %d and no negative duration", a.SampleRate, a.Bitrate, a.Duration, tc.rate, tc.bitrate)
			}
			warned := len(f.Warnings) == 1 && f.Warnings[0].Stage == types.StageTechnical
			if warned != tc.warning {
				t.Errorf("Warnings = %v, want one StageTechnical warning: %v", f.Warnings, tc.warning)
			}
		})
	}
}

// aiffFile builds an AIFF file whose COMM chunk has the given fields.
func aiffFile(channels, bits uint16, frames uint32, rate float64) []byte {
	be := binary.BigEndian
	comm := be.AppendUint16(nil, channels)
	comm = be.AppendUint32(comm, frames)
	comm = be.AppendUint16(comm, bits)
	frac, exp := math.Frexp(rate) // as TestExtendedFloat encodes it
	comm = be.AppendUint16(comm, uint16(exp-1+16383))
	comm = be.AppendUint64(comm, uint64(math.Ldexp(frac, 64)))
	data := riff(be, magicFORM, "COMM", string(comm), "SSND", strings.Repeat("\x00", 8+16))
	copy(data[8:], formAIFF) // riff writes WAVE as the form type
	return data
}

func TestParse_AIFFBitrateOutOfRange(t *testing.T) {
	t.Parallel()
	f := parse(t, types.FormatAIFF, aiffFile(0xFFFF, 0xFFFF, 192000, 192000))
	if a := f.Audio; a.SampleRate != 192000 || a.Bitrate != 0 || a.Duration != time.Second {
		t.Errorf("SampleRate %d, Bitrate %d, Duration %v; want 192000, 0, 1s", a.SampleRate, a.Bitrate, a.Duration)
	}
	if len(f.Warnings) != 1 || f.Warnings[0].Stage != types.StageTechnical {
		t.Errorf("Warnings = %v, want one StageTechnical warning", f.Warnings)
	}
}

// sparse reads as head followed by zeros, up to size.
type sparse struct {
	head []byte
	size int64
}

func (s sparse) ReadAt(p []byte, off int64) (int, error) {
	if off >= s.size {
		return 0, io.EOF
	}
	n := copy(p, s.head[min(off, int64(len(s.head))):])
	clear(p[n:])
	if rest := s.size - off; int64(len(p)) > rest {
		return int(rest), io.EOF
	}
	return len(p), nil
}

func TestParse_DurationOutOfRange(t *testing.T) {
	t.Parallel()
	le := binary.LittleEndian
	head := rf64(make([]byte, ds64MinSize), "fmt ", fmtChunk(wavePCM, 1, 1, 1, 1, 8), "data", "")
	const audio = 1 << 40 // bytes: 2^40 one-byte frames at 1 Hz, past what time.Duration holds
	le.PutUint64(head[28:], audio)
	le.PutUint32(head[len(head)-4:], unsetSize) // the data chunk's size comes from ds64
	size := int64(len(head)) + audio
	le.PutUint64(head[20:], uint64(size-8))
	f, err := (&parser{format: types.FormatWAV}).Parse(t.Context(), sparse{head, size}, size, "big")
	if err != nil {
		t.Fatal(err)
	}
	if f.Audio.Duration != 0 || len(f.Warnings) != 1 || f.Warnings[0].Stage != types.StageTechnical {
		t.Errorf("Duration %v, Warnings %v; want 0 and one StageTechnical warning", f.Audio.Duration, f.Warnings)
	}
}

// Not parallel: testing.AllocsPerRun refuses to run in a parallel test.
func TestWalk_DS64TableTooLong(t *testing.T) {
	le := binary.LittleEndian
	data := rf64(make([]byte, ds64MinSize), "fmt ", pcmFmt(le), "data", "\x00\x00")
	data = append(data, make([]byte, 1<<20)...)
	le.PutUint32(data[16:], 0xFFFFFFF0) // ds64 chunk size
	le.PutUint32(data[44:], 0xFFFFFFFF) // ds64 table length
	_, err := walk(t.Context(), newReader(data), int64(len(data)))
	if _, ok := errors.AsType[*types.CorruptedFileError](err); !ok {
		t.Fatalf("err = %v, want a CorruptedFileError", err)
	}
	if allocs := testing.AllocsPerRun(5, func() { _, _ = walk(t.Context(), newReader(data), int64(len(data))) }); allocs > 50 {
		t.Errorf("walk made %v allocations, want a handful", allocs)
	}
}

// infoChunk returns the LIST/INFO chunk of a WAV file, header included.
func infoChunk(t *testing.T, data []byte) []byte {
	t.Helper()
	c, err := walk(t.Context(), newReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	i := c.infoIndex()
	if i < 0 {
		t.Fatal("no LIST/INFO chunk")
	}
	ch := c.chunks[i]
	return data[ch.off : ch.data()+ch.size]
}

// subchunk is one raw INFO subchunk, padded.
func subchunk(id, payload string) string {
	b := append([]byte(id), binary.LittleEndian.AppendUint32(nil, uint32(len(payload)))...)
	b = append(b, payload...)
	if len(payload)%2 == 1 {
		b = append(b, 0)
	}
	return string(b)
}

// lossyList is INFO text a re-encode would damage: Latin-1, and text after a NUL.
var lossyList = "INFO" + subchunk("IART", "Bj\xf6rk\x00") + subchunk("ICMT", "a\x00hidden") + subchunk("INAM", "Title\x00")

func TestWriteFile_UneditedNativeTextIsCopied(t *testing.T) {
	t.Parallel()
	le := binary.LittleEndian
	data := riff(le, magicRIFF, "fmt ", pcmFmt(le), "LIST", lossyList, "data", "\x00\x00")
	cases := map[string]func(*types.Tags){
		"ID3-only key":              func(tags *types.Tags) { tags.Set("TBPM", "120") },
		"field without native text": func(tags *types.Tags) { tags.Composers = []string{"X"} },
	}
	for name, edit := range cases {
		out, err := rewriteWith(t, types.FormatWAV, data, edit)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got, want := infoChunk(t, out), infoChunk(t, data); !bytes.Equal(got, want) {
			t.Errorf("%s: LIST/INFO = %q, want it copied as %q", name, got, want)
		}
	}
}

func TestWriteFile_EditedNativeTextKeepsOtherEntries(t *testing.T) {
	t.Parallel()
	le := binary.LittleEndian
	data := riff(le, magicRIFF, "fmt ", pcmFmt(le), "LIST", lossyList, "data", "\x00\x00")
	out, err := rewriteWith(t, types.FormatWAV, data, func(tags *types.Tags) { tags.Title = "New" })
	if err != nil {
		t.Fatal(err)
	}
	want := "INFO" + subchunk("IART", "Bj\xf6rk\x00") + subchunk("ICMT", "a\x00hidden") + subchunk("INAM", "New\x00")
	if got := infoChunk(t, out)[chunkHeaderSize:]; string(got) != want {
		t.Errorf("LIST/INFO payload = %q\nwant %q", got, want)
	}
}

func TestWriteFile_UnreadableInfoSubchunk(t *testing.T) {
	t.Parallel()
	le := binary.LittleEndian
	list := "INFO" + subchunk("INAM", "Title\x00") + "\x01\x02\x03\x04\x05\x00\x00\x00" + subchunk("ICOP", "kept\x00")
	data := riff(le, magicRIFF, "fmt ", pcmFmt(le), "LIST", list, "data", "\x00\x00")

	out, err := rewriteWith(t, types.FormatWAV, data, func(tags *types.Tags) { tags.Set("TBPM", "120") })
	if err != nil {
		t.Fatalf("ID3-only edit: %v", err)
	}
	if !bytes.Equal(infoChunk(t, out), infoChunk(t, data)) {
		t.Error("ID3-only edit changed the LIST/INFO chunk")
	}
	for name, edit := range map[string]func(*types.Tags){
		"mirrored field": func(tags *types.Tags) { tags.Title = "New" },
		"native key":     func(tags *types.Tags) { tags.Set("ISFT", "x") },
	} {
		if _, err := rewriteWith(t, types.FormatWAV, data, edit); !errors.As(err, new(*types.CorruptedFileError)) {
			t.Errorf("%s: err = %v, want a CorruptedFileError", name, err)
		}
	}
}

func TestWriteFile_UneditedAIFFTextStaysInPlace(t *testing.T) {
	t.Parallel()
	be := binary.BigEndian
	comm := make([]byte, commSize)
	be.PutUint16(comm[0:], 1)                                  // channels
	be.PutUint16(comm[6:], 16)                                 // bits
	copy(comm[8:], "\x40\x0B\xFA\x00\x00\x00\x00\x00\x00\x00") // 8000 Hz as an 80-bit float
	data := riff(be, magicFORM, "NAME", "Caf\xe9", "COMM", string(comm), "ANNO", "a\x00b", "SSND", strings.Repeat("\x00", 10))
	copy(data[8:], formAIFF) // riff writes WAVE as the form type
	out, err := rewriteWith(t, types.FormatAIFF, data, func(tags *types.Tags) { tags.Set("TBPM", "120") })
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(out[12:], data[12:]) {
		t.Errorf("chunks before the new ID3 chunk changed:\n got %q\nwant %q", out[12:], data[12:])
	}
}

func TestSetEntries_ReportsChange(t *testing.T) {
	t.Parallel()
	entries := []entry{{id: "INAM", value: "a", raw: []byte("a\x00")}, {id: "IART", value: "b"}}
	if got, changed := setEntries(entries, "INAM", []string{"a"}); changed || got[0].raw == nil {
		t.Errorf("same values: changed %v, entry %+v; want unchanged with its raw bytes", changed, got[0])
	}
	if _, changed := setEntries(entries, "INAM", []string{"c"}); !changed {
		t.Error("new value: changed = false")
	}
	if _, changed := setEntries(entries, "ICOP", nil); changed {
		t.Error("deleting an absent id: changed = true")
	}
}

func TestMirrors_FollowNativeBindings(t *testing.T) {
	t.Parallel()
	for format, want := range map[types.Format]struct {
		mirrors []mirror
		comment string
	}{
		types.FormatWAV:  {wavMirrors, keyICMT},
		types.FormatAIFF: {aiffMirrors, keyANNO},
	} {
		native := types.NativeTextBindings(format)
		if len(want.mirrors) != len(native)+1 {
			t.Fatalf("%s: %d mirrors, want one per native binding (%d) and the comment", format, len(want.mirrors), len(native))
		}
		for i, b := range native {
			if m := want.mirrors[i]; m.field != b.Field || !slices.Equal(m.ids, b.Keys) || m.value == nil {
				t.Errorf("%s: mirror %d = %v %v, want %v %v", format, i, m.field, m.ids, b.Field, b.Keys)
			}
		}
		if m := want.mirrors[len(native)]; m.field != types.FieldComment || !slices.Equal(m.ids, []string{want.comment}) {
			t.Errorf("%s: last mirror = %v %v, want the comment %q", format, m.field, m.ids, want.comment)
		}
	}
}
