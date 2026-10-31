package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/simonhull/audiometa"
	"github.com/simonhull/audiometa/internal/types"
)

const (
	mp3Fixture = "../../testdata/sample.mp3"
	m4bFixture = "../../testdata/sample.m4b"
)

func runCLI(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func ndjson(t *testing.T, s string) []map[string]any {
	t.Helper()
	var objs []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(s), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("invalid JSON line %q: %v", line, err)
		}
		objs = append(objs, m)
	}
	return objs
}

func TestVersion(t *testing.T) {
	code, out, _ := runCLI(t, "version")
	if code != exitOK || !strings.HasPrefix(out, "audiometa ") || !strings.Contains(out, audiometa.GetVersion()) {
		t.Fatalf("code=%d out=%q", code, out)
	}
	code, out, _ = runCLI(t, "version", "--json")
	if code != exitOK {
		t.Fatalf("code=%d", code)
	}
	if got := ndjson(t, out)[0]["version"]; got != audiometa.GetVersion() {
		t.Fatalf("version = %v", got)
	}
	if code, out, _ = runCLI(t, "--version"); code != exitOK || !strings.HasPrefix(out, "audiometa ") {
		t.Fatalf("--version: code=%d out=%q", code, out)
	}
}

func TestNoArgsAndHelp(t *testing.T) {
	code, out, errs := runCLI(t)
	if code != exitUsage || out != "" || !strings.Contains(errs, "Usage:") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errs)
	}
	code, out, _ = runCLI(t, "help")
	if code != exitOK || !strings.Contains(out, "Exit codes:") {
		t.Fatalf("help: code=%d", code)
	}
}

func TestUnknownFlag(t *testing.T) {
	for _, args := range [][]string{{"inspect", "--bogus", mp3Fixture}, {"--bogus"}, {"chapters", "-x"}} {
		code, _, errs := runCLI(t, args...)
		if code != exitUsage || !strings.Contains(errs, "Usage:") {
			t.Errorf("%v: code=%d err=%q", args, code, errs)
		}
	}
}

func TestInspectNoFiles(t *testing.T) {
	if code, _, _ := runCLI(t, "inspect"); code != exitUsage {
		t.Fatalf("code=%d", code)
	}
}

func TestBareFileIsInspect(t *testing.T) {
	code, bare, _ := runCLI(t, mp3Fixture)
	_, explicit, _ := runCLI(t, "inspect", mp3Fixture)
	if code != exitOK || bare != explicit || !strings.Contains(bare, "Title:") {
		t.Fatalf("code=%d bare=%q explicit=%q", code, bare, explicit)
	}
	// Flags first also works.
	if code, out, _ := runCLI(t, "--json", mp3Fixture); code != exitOK || !strings.HasPrefix(out, "{") {
		t.Fatalf("--json first: code=%d out=%q", code, out)
	}
}

func TestInspectJSONGoodAndMissing(t *testing.T) {
	code, out, errs := runCLI(t, "inspect", "--json", mp3Fixture, filepath.Join(t.TempDir(), "missing.mp3"))
	if code != exitFail {
		t.Fatalf("code=%d", code)
	}
	if errs != "" {
		t.Fatalf("JSON errors belong on stdout, stderr=%q", errs)
	}
	objs := ndjson(t, out)
	if len(objs) != 2 {
		t.Fatalf("got %d lines, want 2: %q", len(objs), out)
	}
	good, bad := objs[0], objs[1]
	if good["format"] != "MP3" || good["schema_version"] != 1.0 {
		t.Errorf("good = %v", good)
	}
	if tags, _ := good["tags"].(map[string]any); tags["title"] != "Test Title" {
		t.Errorf("tags = %v", good["tags"])
	}
	if _, ok := good["warnings"].([]any); !ok {
		t.Errorf("warnings must be an array: %v", good["warnings"])
	}
	if _, ok := bad["error"].(string); !ok {
		t.Errorf("bad has no error: %v", bad)
	}
	if arr, ok := bad["chapters"].([]any); !ok || len(arr) != 0 {
		t.Errorf("bad chapters = %v", bad["chapters"])
	}
}

func TestInspectMissingHumanGoesToStderr(t *testing.T) {
	code, out, errs := runCLI(t, "inspect", "nope.mp3")
	if code != exitFail || out != "" || !strings.HasPrefix(errs, "nope.mp3: ") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errs)
	}
}

func TestChapters(t *testing.T) {
	code, out, _ := runCLI(t, "chapters", m4bFixture)
	if code != exitOK || !strings.Contains(out, "[1] 00:00:00.000 → 00:00:01.000  Chapter One") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	_, out, _ = runCLI(t, "chapters", "--json", m4bFixture)
	chs, _ := ndjson(t, out)[0]["chapters"].([]any)
	if len(chs) != 2 {
		t.Fatalf("chapters = %v", chs)
	}
	if ch, _ := chs[1].(map[string]any); ch["start_ms"] != 1000.0 || ch["end_ms"] != 2000.0 {
		t.Errorf("chapter = %v", ch)
	}
}

func TestChaptersNone(t *testing.T) {
	flac := "../../testdata/sample.flac"
	_, out, _ := runCLI(t, "chapters", flac)
	if out != "" {
		t.Errorf("human out = %q, want empty", out)
	}
	_, out, _ = runCLI(t, "chapters", "--json", flac)
	if !strings.Contains(out, `"chapters":[]`) {
		t.Errorf("out = %q", out)
	}
}

func TestDTOGolden(t *testing.T) {
	var f audiometa.File
	f.Path = "book.m4b"
	f.Format = audiometa.FormatOgg
	f.Size = 1234
	f.Tags.Title = "T"
	f.Tags.Genres = []string{"A", "B"}
	f.Tags.TrackNumber = 3
	f.Tags.Set("ZETA", "z")
	f.Tags.Set("ALPHA", "a1", "a2")
	f.Audio = audiometa.AudioInfo{Codec: "vorbis", Duration: 90*time.Second + 500*time.Millisecond, SampleRate: 44100, Channels: 2}
	f.Warnings = []types.Warning{{Stage: types.StageMetadata, Message: "m", Offset: 7}}

	got, err := marshal(toFileDTO(&f))
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"schema_version":1,"path":"book.m4b","format":"Ogg Vorbis","size":1234,` +
		`"tags":{"title":"T","genres":["A","B"],"track_number":3,"raw":{"ALPHA":["a1","a2"],"ZETA":["z"]}},` +
		`"audio":{"codec":"vorbis","duration_ms":90500,"sample_rate":44100,"channels":2,"lossless":false,"vbr":false},` +
		`"chapters":[],"warnings":[{"stage":"metadata","message":"m","offset":7}]}`
	if string(got) != want {
		t.Errorf("golden mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestDTOInvalidUTF8(t *testing.T) {
	bad := "bad\xffvalue"
	var f audiometa.File
	f.Path = "p\xfe"
	f.Tags.Title = bad
	f.Tags.Genres = []string{bad}
	f.Tags.Set("K\xff", bad)
	f.Chapters = []types.Chapter{{Title: bad, Index: 1}}
	f.Warnings = []types.Warning{{Stage: "s", Message: bad, Err: os.ErrNotExist}}

	var buf bytes.Buffer
	if err := writeJSONLine(&printer{w: &buf}, toFileDTO(&f)); err != nil {
		t.Fatalf("invalid UTF-8 must not fail encoding: %v", err)
	}
	if !utf8.Valid(buf.Bytes()) || !strings.Contains(buf.String(), "bad�value") {
		t.Fatalf("output = %q", buf.String())
	}
	var round map[string]any
	if err := json.Unmarshal(buf.Bytes(), &round); err != nil {
		t.Fatal(err)
	}

	// Error records carry arbitrary paths and messages too.
	buf.Reset()
	if err := writeJSONLine(&printer{w: &buf}, toErrorDTO("x\xff", os.ErrPermission)); err != nil || !utf8.Valid(buf.Bytes()) {
		t.Fatalf("error DTO: err=%v out=%q", err, buf.Bytes())
	}
}

func TestCleanTextStripsControls(t *testing.T) {
	if got := cleanText("a\x1b[31mb\nc\xff"); strings.ContainsAny(got, "\x1b\n") || !utf8.ValidString(got) {
		t.Errorf("cleanText = %q", got)
	}
}

func TestArtworkRequiresOut(t *testing.T) {
	if code, _, errs := runCLI(t, "artwork", mp3Fixture); code != exitUsage || !strings.Contains(errs, "--out") {
		t.Fatalf("code=%d err=%q", code, errs)
	}
}

func TestArtworkExtAllowlist(t *testing.T) {
	cases := map[string]string{
		"image/jpeg": ".jpg", "image/jpg": ".jpg", "IMAGE/PNG": ".png", "image/gif": ".gif",
		"image/bmp": ".bmp", "image/webp": ".webp", "image/tiff": ".tiff",
		"image/../../evil": ".bin", "": ".bin", "image/svg+xml": ".bin",
	}
	for mime, want := range cases {
		if got := artworkExt(mime); got != want {
			t.Errorf("artworkExt(%q) = %q, want %q", mime, got, want)
		}
	}
}

func TestArtworkBase(t *testing.T) {
	cases := map[string]string{
		"dir/song.flac": "song", "..": "artwork", "": "artwork", "a/..": "artwork", "x.tar.gz": "x.tar",
	}
	for in, want := range cases {
		if got := artworkBase(in); got != want {
			t.Errorf("artworkBase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWriteNewRefusesOverwrite(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "a.bin")
	if err := writeNew(dest, []byte("one"), false); err != nil {
		t.Fatal(err)
	}
	if err := writeNew(dest, []byte("two"), false); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(dest); string(b) != "one" {
		t.Fatalf("file was overwritten: %q", b)
	}
	if err := writeNew(dest, []byte("tw"), true); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dest); string(b) != "tw" {
		t.Fatalf("force did not truncate+write: %q", b)
	}
}

func TestArtworkNoArtworkFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "out")
	code, out, _ := runCLI(t, "artwork", "--out", dir, mp3Fixture)
	if code != exitOK {
		t.Fatalf("code=%d", code)
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Fatalf("out dir not created: %v", err)
	}
	if strings.Contains(out, "wrote") {
		t.Logf("fixture has artwork: %s", out)
	}
}

// atom builds a 32-bit-size atom.
func atom(typ string, payload ...[]byte) []byte {
	body := bytes.Join(payload, nil)
	b := binary.BigEndian.AppendUint32(nil, uint32(8+len(body)))
	return append(append(b, typ...), body...)
}

func writeTemp(t *testing.T, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "x.m4a")
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func ftyp() []byte { return atom("ftyp", []byte("M4A \x00\x00\x00\x00M4A mp42isom")) }

func TestAtomsValidTree(t *testing.T) {
	meta := atom("meta", []byte{0, 0, 0, 0}, atom("ilst", atom("free")))
	data := append(ftyp(), atom("moov", atom("udta", meta))...)
	code, out, errs := runCLI(t, "atoms", writeTemp(t, data))
	if code != exitOK {
		t.Fatalf("code=%d err=%q", code, errs)
	}
	for _, want := range []string{
		"ftyp (size: 28, offset: 0)\n",
		"moov (size: 44, offset: 28)\n",
		"  udta (size: 36, offset: 36)\n",
		"    meta (size: 28, offset: 44)\n",
		"      ilst (size: 16, offset: 56)\n",
		"        free (size: 8, offset: 64)\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestAtomsExtendedSizeOverflow(t *testing.T) {
	hostile := binary.BigEndian.AppendUint32(nil, 1)
	hostile = append(hostile, "free"...)
	hostile = binary.BigEndian.AppendUint64(hostile, ^uint64(0))
	code, out, _ := runCLI(t, "atoms", writeTemp(t, append(ftyp(), hostile...)))
	if code != exitOK || !strings.Contains(out, "! invalid atom size 18446744073709551615 at offset 28") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestAtomsTooSmallSize(t *testing.T) {
	small := binary.BigEndian.AppendUint32(nil, 4)
	small = append(small, "free"...)
	_, out, _ := runCLI(t, "atoms", writeTemp(t, append(ftyp(), small...)))
	if !strings.Contains(out, "! invalid atom size 4 at offset 28") {
		t.Fatalf("out=%q", out)
	}
}

func TestAtomsExtendsToEOF(t *testing.T) {
	zero := binary.BigEndian.AppendUint32(nil, 0)
	zero = append(zero, "mdat"...)
	zero = append(zero, make([]byte, 10)...)
	_, out, _ := runCLI(t, "atoms", writeTemp(t, append(ftyp(), zero...)))
	if !strings.Contains(out, "mdat (size: 18, offset: 28)") {
		t.Fatalf("out=%q", out)
	}
}

func TestAtomsDepthLimit(t *testing.T) {
	inner := atom("free")
	for range 40 {
		inner = atom("moov", inner)
	}
	code, out, _ := runCLI(t, "atoms", writeTemp(t, append(ftyp(), inner...)))
	if code != exitOK {
		t.Fatalf("code=%d", code)
	}
	if n := strings.Count(out, "moov ("); n != maxAtomDepth {
		t.Errorf("printed %d moov levels, want %d", n, maxAtomDepth)
	}
	if !strings.Contains(out, "! max depth") {
		t.Errorf("no max depth marker")
	}
}

func TestAtomsNotMP4(t *testing.T) {
	code, out, errs := runCLI(t, "atoms", mp3Fixture)
	if code != exitFail || out != "" || !strings.Contains(errs, "not an MP4 container") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errs)
	}
}

func TestAtomsRealFile(t *testing.T) {
	code, out, _ := runCLI(t, "atoms", m4bFixture)
	if code != exitOK || !strings.Contains(out, "moov") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}
