package ogg

import (
	"bytes"
	"context"
	ebinary "encoding/binary"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/simonhull/audiometa/internal/registry"
	"github.com/simonhull/audiometa/internal/types"
)

// testWriter returns the Opus writer.
func testWriter(t testing.TB) *writer {
	t.Helper()
	w, ok := registry.GetWriter(types.FormatOpus).(*writer)
	if !ok {
		t.Fatal("no Opus writer registered")
	}
	return w
}

func tryWrite(t testing.TB, src []byte, edits *types.EditSet) ([]byte, error) {
	t.Helper()
	var buf bytes.Buffer
	n, err := testWriter(t).WriteFile(t.Context(), &buf, bytes.NewReader(src), int64(len(src)), edits)
	if n != int64(buf.Len()) {
		t.Fatalf("WriteFile reported %d bytes, wrote %d", n, buf.Len())
	}
	return buf.Bytes(), err
}

func writeBytes(t testing.TB, src []byte, edits *types.EditSet) []byte {
	t.Helper()
	out, err := tryWrite(t, src, edits)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return out
}

// editsFor builds the EditSet that mutate (applied to src's tags) produces,
// plus the given artwork operations.
func editsFor(t *testing.T, src []byte, mutate func(*types.Tags), art ...types.ArtworkOp) *types.EditSet {
	t.Helper()
	file := parseOggBytes(t, src)
	orig := file.Tags
	types.BindFormat(&orig, file.Format)
	cur := orig.Clone()
	types.BindFormat(cur, file.Format)
	if mutate != nil {
		mutate(cur)
	}
	keys, fields, err := types.Diff(&orig, cur, file.Format)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	return &types.EditSet{Keys: keys, Fields: fields, Artwork: art, Orig: &orig, Final: cur}
}

// keyEdit is an EditSet of one raw key edit.
func keyEdit(key string, values ...string) *types.EditSet {
	return &types.EditSet{Keys: []types.KeyEdit{{Key: key, Values: values}}, Orig: &types.Tags{}, Final: &types.Tags{}}
}

// rawPagesAndTail splits data into whole pages and the bytes after the last.
func rawPagesAndTail(t testing.TB, data []byte) (pages []rawPage, tail []byte) {
	t.Helper()
	s := newPageStream(bytes.NewReader(data), int64(len(data)), "x", false)
	for s.off < int64(len(data)) {
		off := s.off
		page, next, err := s.next()
		if err != nil {
			return pages, data[off:]
		}
		raw := data[off:next]
		pages = append(pages, rawPage{raw, page, ebinary.LittleEndian.Uint32(raw[pageCRCOffset:])})
	}
	return pages, nil
}

// checkStructure is the in-tree validator: every page's CRC is right, sequence
// numbers run on from the first page's without a gap, BOS is on the first page
// only, EOS on the last only, and a page continues a packet exactly when the
// page before it ended on a lacing value of 255.
func checkStructure(t testing.TB, name string, data []byte) {
	t.Helper()
	pages, _ := rawPagesAndTail(t, data)
	if len(pages) < 3 {
		t.Fatalf("%s: %d pages", name, len(pages))
	}
	for i, p := range pages {
		zeroed := slices.Clone(p.raw)
		clear(zeroed[pageCRCOffset : pageCRCOffset+4])
		if got := crcUpdate(0, zeroed); got != p.stored {
			t.Errorf("%s page %d: crc %#08x, stored %#08x", name, i, got, p.stored)
		}
		if want := pages[0].page.SequenceNumber + uint32(i); p.page.SequenceNumber != want {
			t.Errorf("%s page %d: sequence %d, want %d", name, i, p.page.SequenceNumber, want)
		}
		if bos := p.page.HeaderType&0x02 != 0; bos != (i == 0) {
			t.Errorf("%s page %d: BOS %v", name, i, bos)
		}
		if eos := p.page.HeaderType&pageEOS != 0; eos && i != len(pages)-1 {
			t.Errorf("%s page %d: EOS before the last page", name, i)
		}
		prevOpen := i > 0 && pages[i-1].page.Lacing[len(pages[i-1].page.Lacing)-1] == 255
		if cont := p.page.HeaderType&pageContinued != 0; cont != prevOpen {
			t.Errorf("%s page %d: continued %v, previous page open %v", name, i, cont, prevOpen)
		}
	}
}

// preserved asserts that out holds the same audio as src: the audio digest
// and the preserved digest are both unchanged.
func preserved(t testing.TB, name string, src, out []byte) {
	t.Helper()
	w := testWriter(t)
	ctx := t.Context()
	for what, f := range map[string]func(context.Context, io.ReaderAt, int64) ([]byte, error){
		"AudioDigest": w.AudioDigest, "PreservedDigest": w.PreservedDigest,
	} {
		a, err := f(ctx, bytes.NewReader(src), int64(len(src)))
		if err != nil {
			t.Fatalf("%s %s of the source: %v", name, what, err)
		}
		b, err := f(ctx, bytes.NewReader(out), int64(len(out)))
		if err != nil {
			t.Fatalf("%s %s of the output: %v", name, what, err)
		}
		if !bytes.Equal(a, b) {
			t.Errorf("%s: %s changed", name, what)
		}
	}
}

// syntheticOpus is a well-formed Opus stream with correct checksums: OpusHead,
// a comment header holding comments, and audioPages pages of audio, the last
// ending the stream.
func syntheticOpus(commentComments []string, audioPages int) []byte {
	pages := []pageSpec{{headerType: 0x02, serial: 5, lacing: lacingFor(len(opusHeadPacket())), data: opusHeadPacket()}}
	pages = append(pages, splitAcrossPages(commentPacket("OpusTags", false, commentComments...), 0, 5)...)
	for i := range audioPages {
		p := pageSpec{serial: 5, granule: uint64(960 * (i + 1)), lacing: lacingFor(100 + i), data: bytes.Repeat([]byte{byte(i + 1)}, 100+i)}
		if i == audioPages-1 {
			p.headerType = pageEOS
		}
		pages = append(pages, p)
	}
	return withCRCs(buildOgg(pages...))
}

// withCRCs returns data with every page's checksum filled in.
func withCRCs(data []byte) []byte {
	out := slices.Clone(data)
	for off := 0; off < len(out); {
		n := pageFixedHeader + int(out[off+26])
		for _, l := range out[off+pageFixedHeader : off+n] {
			n += int(l)
		}
		clear(out[off+pageCRCOffset : off+pageCRCOffset+4])
		ebinary.LittleEndian.PutUint32(out[off+pageCRCOffset:], crcUpdate(0, out[off:off+n]))
		off += n
	}
	return out
}

func TestWriter_Registered(t *testing.T) {
	if _, ok := registry.GetWriter(types.FormatOpus).(*writer); !ok {
		t.Error("no Opus writer registered")
	}
	// The page-aware digester keeps precedence in the registry: AudioDigest of
	// the writer and of the digester are one implementation.
	if _, ok := registry.GetDigester(types.FormatOpus).(digester); !ok {
		t.Errorf("digester for Opus is %T", registry.GetDigester(types.FormatOpus))
	}
}

// TestWriteOpusCorpus edits every Opus corpus file in the ways that exercise
// each path: a same-size page layout (verbatim copy of the audio pages), a
// header that grows by a page (every later page renumbered) and one that
// shrinks back.
func TestWriteOpusCorpus(t *testing.T) {
	big := strings.Repeat("x", 70_000)
	for _, name := range []string{"sample.opus", "sample_retagged.opus", "bench/book.opus"} {
		src := readCorpus(t, name)
		layoutBefore, err := layoutOf(t, src)
		if err != nil {
			t.Fatal(err)
		}
		cases := []struct {
			name       string
			mutate     func(*types.Tags)
			headerWant int
			verbatim   bool
		}{
			{"title", func(tg *types.Tags) { tg.Title = "New title ✓" }, 1, true},
			{"grow across a page", func(tg *types.Tags) { tg.Set("BIG", big) }, 2, false},
			{"clear", func(tg *types.Tags) { tg.Title, tg.Artist, tg.Artists, tg.Album = "", "", nil, "" }, 1, true},
		}
		for _, tc := range cases {
			out := writeBytes(t, src, editsFor(t, src, tc.mutate))
			label := name + " " + tc.name
			checkStructure(t, label, out)
			preserved(t, label, src, out)
			l, err := layoutOf(t, out)
			if err != nil {
				t.Fatalf("%s: %v", label, err)
			}
			if l.headerPages != tc.headerWant {
				t.Errorf("%s: %d header pages, want %d", label, l.headerPages, tc.headerWant)
			}
			// The audio pages are the source's: byte for byte when the header
			// page count is unchanged, in every field but sequence and CRC otherwise.
			if tc.verbatim && !bytes.Equal(out[l.audioStart:], src[layoutBefore.audioStart:]) {
				t.Errorf("%s: audio pages are not copied verbatim", label)
			}
			if !tc.verbatim && bytes.Equal(out[l.audioStart:], src[layoutBefore.audioStart:]) {
				t.Errorf("%s: audio pages were not renumbered", label)
			}
		}
		// Shrinking the grown header back gives the original page count again.
		grown := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Set("BIG", big) }))
		shrunk := writeBytes(t, grown, editsFor(t, grown, func(tg *types.Tags) { tg.Delete("BIG") }))
		checkStructure(t, name+" shrunk", shrunk)
		preserved(t, name+" shrunk", src, shrunk)
		if !bytes.Equal(shrunk, src) {
			// Pagination can differ from the source encoder's; the tags must not.
			if got, want := parseOggBytes(t, shrunk).Tags.Title, parseOggBytes(t, src).Tags.Title; got != want {
				t.Errorf("%s: title %q after a round trip, want %q", name, got, want)
			}
		}
	}
}

func TestWriteOpusTagsReadBack(t *testing.T) {
	src := readCorpus(t, "sample.opus")
	out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) {
		tg.Title = "Nouveau titre ✓ 日本語"
		tg.Year = 1999
		tg.TrackNumber, tg.TrackTotal = 3, 12
		tg.DiscNumber = 2
		tg.Narrator = "N"
		tg.Genres = []string{"A", "B"}
		tg.Set("CUSTOM", "x", "y")
	}))
	f := parseOggBytes(t, out)
	g := f.Tags
	if g.Title != "Nouveau titre ✓ 日本語" || g.Year != 1999 || g.TrackNumber != 3 || g.TrackTotal != 12 ||
		g.DiscNumber != 2 || g.Narrator != "N" || !slices.Equal(g.Genres, []string{"A", "B"}) {
		t.Errorf("read back %+v", g)
	}
	if got := g.Get("CUSTOM"); !slices.Equal(got, []string{"x", "y"}) {
		t.Errorf("CUSTOM = %q", got)
	}
	if f.Format != types.FormatOpus {
		t.Errorf("format %s", f.Format)
	}
}

func TestWriteOpusNoOpIsByteIdentical(t *testing.T) {
	for _, name := range []string{"sample.opus", "bench/book.opus"} {
		src := readCorpus(t, name)
		for label, edits := range map[string]*types.EditSet{
			"empty":             {Orig: &types.Tags{}, Final: &types.Tags{}},
			"same value":        editsFor(t, src, nil),
			"delete the absent": keyEdit("NO_SUCH_KEY"),
		} {
			if out := writeBytes(t, src, edits); !bytes.Equal(out, src) {
				t.Errorf("%s %s: output differs from the source", name, label)
			}
		}
	}
}

func TestWriteOpusIsDeterministic(t *testing.T) {
	src := readCorpus(t, "sample.opus")
	edit := func() *types.EditSet {
		return editsFor(t, src, func(tg *types.Tags) { tg.Title = "T"; tg.Set("BIG", strings.Repeat("y", 70_000)) })
	}
	if a, b := writeBytes(t, src, edit()), writeBytes(t, src, edit()); !bytes.Equal(a, b) {
		t.Error("two writes of the same edits differ")
	}
}

func TestWriteOpusKeepsVendorAndTail(t *testing.T) {
	// OpusTags with the LSB-1 binary data RFC 7845 §5.2 says to preserve, then zero padding.
	body := commentPacket("OpusTags", false, "TITLE=Old")
	body = append(body, 0x01, 0xAA, 0xBB, 0, 0, 0)
	pages := []pageSpec{
		{headerType: 0x02, serial: 5, lacing: lacingFor(len(opusHeadPacket())), data: opusHeadPacket()},
		{serial: 5, lacing: lacingFor(len(body)), data: body},
		{headerType: pageEOS, serial: 5, granule: 960, lacing: lacingFor(100), data: make([]byte, 100)},
	}
	src := withCRCs(buildOgg(pages...))
	out := writeBytes(t, src, keyEdit("TITLE", "A much longer title than before"))
	l, err := layoutOf(t, out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(l.packets[1], []byte{0x01, 0xAA, 0xBB, 0, 0, 0}) || !bytes.Contains(l.packets[1], []byte("test")) {
		t.Errorf("vendor or tail lost: %q", l.packets[1])
	}
	checkStructure(t, "tail", out)
}

func TestWriteOpusTrailingBytesCarriedThrough(t *testing.T) {
	src := slices.Concat(readCorpus(t, "sample.opus"), []byte("TAG appended after the last page"))
	for name, mutate := range map[string]func(*types.Tags){
		"same pages": func(tg *types.Tags) { tg.Title = "T" },
		"more pages": func(tg *types.Tags) { tg.Set("BIG", strings.Repeat("z", 70_000)) },
	} {
		out := writeBytes(t, src, editsFor(t, src, mutate))
		if !bytes.HasSuffix(out, []byte("TAG appended after the last page")) {
			t.Errorf("%s: trailing bytes lost", name)
		}
		pages, tail := rawPagesAndTail(t, out)
		if string(tail) != "TAG appended after the last page" || len(pages) < 3 {
			t.Errorf("%s: %d pages, tail %q", name, len(pages), tail)
		}
		preserved(t, name, src, out)
	}
}

// withSerial returns data, which must be whole pages, with every page moved
// to the stream serial.
func withSerial(t testing.TB, data []byte, serial uint32) []byte {
	t.Helper()
	out := slices.Clone(data)
	off := 0
	for _, p := range mustPages(t, data) {
		ebinary.LittleEndian.PutUint32(out[off+14:], serial)
		off += len(p.raw)
	}
	return withCRCs(out)
}

func TestWriteOpusRefusals(t *testing.T) {
	good := readCorpus(t, "sample.opus")
	l, err := layoutOf(t, good)
	if err != nil {
		t.Fatal(err)
	}
	other := slices.Clone(good[l.audioStart:])
	ebinary.LittleEndian.PutUint32(other[14:], 99) // first audio page of another stream
	head, comment := opusHeadPacket(), commentPacket("OpusTags", false, "TITLE=x")
	idPage := pageSpec{headerType: 0x02, serial: 1, lacing: lacingFor(len(head)), data: head}

	unsupported := map[string][]byte{
		"chained":     slices.Concat(good, withSerial(t, good, 99)),
		"multiplexed": slices.Concat(good[:l.audioStart], other),
		"headers of another stream": buildOgg(idPage, pageOf(2, comment), pageOf(1, comment),
			audioTail(1, 960)),
	}
	for name, src := range unsupported {
		out, err := tryWrite(t, src, keyEdit("TITLE", "new"))
		if !errors.Is(err, types.ErrUnsupportedWrite) || len(out) != 0 {
			t.Errorf("%s: err = %v, %d bytes written, want ErrUnsupportedWrite and none", name, err, len(out))
		}
		if _, err := testWriter(t).PreservedDigest(t.Context(), bytes.NewReader(src), int64(len(src))); !errors.Is(err, types.ErrUnsupportedWrite) {
			t.Errorf("%s: PreservedDigest err = %v, want ErrUnsupportedWrite", name, err)
		}
	}

	droppedContinuation := buildOgg(idPage,
		pageSpec{serial: 1, lacing: []byte{255}, data: bytes.Repeat([]byte{1}, 255)},
		pageSpec{serial: 1, lacing: []byte{4}, data: []byte{1, 2, 3, 4}}, audioTail(1, 960))
	corrupt := map[string][]byte{
		"truncated in the headers": good[:l.idEnd+10],
		"only page 0":              good[:l.idEnd],
		"dropped continuation":     droppedContinuation,
		"audio on the header page": buildOgg(idPage, pageOf(1, comment, []byte{1, 2, 3}), audioTail(1, 960)),
	}
	for name, src := range corrupt {
		out, err := tryWrite(t, src, keyEdit("TITLE", "new"))
		var ce *types.CorruptedFileError
		if !errors.As(err, &ce) || len(out) != 0 {
			t.Errorf("%s: err = %v, %d bytes written, want *CorruptedFileError and none", name, err, len(out))
		}
	}
}

func TestWriteOpusRefusesBadEdits(t *testing.T) {
	src := readCorpus(t, "sample.opus")
	tests := map[string]*types.EditSet{
		"key with =":      keyEdit("A=B", "v"),
		"empty key":       keyEdit("", "v"),
		"chapter key":     keyEdit("CHAPTER001", "00:00:00.000"),
		"invalid UTF-8":   keyEdit("TITLE", "\xff"),
		"unknown artwork": {Artwork: []types.ArtworkOp{{Kind: 99}}, Orig: &types.Tags{}, Final: &types.Tags{}},
		"bad MIME":        {Artwork: []types.ArtworkOp{{Kind: types.ArtworkOpAdd, Art: types.Artwork{MIMEType: "image/\x01", Type: types.ArtworkFrontCover}}}, Orig: &types.Tags{}, Final: &types.Tags{}},
	}
	for name, edits := range tests {
		if out, err := tryWrite(t, src, edits); !errors.Is(err, types.ErrUnsupportedWrite) || len(out) != 0 {
			t.Errorf("%s: err = %v, %d bytes", name, err, len(out))
		}
	}
}

func TestWriteOpusRefusesAHeaderBeyondTheReadersLimit(t *testing.T) {
	src := readCorpus(t, "sample.opus")
	huge := strings.Repeat("a", maxHeaderBytes)
	if _, err := tryWrite(t, src, keyEdit("BIG", huge)); !errors.Is(err, types.ErrUnsupportedWrite) {
		t.Errorf("err = %v, want ErrUnsupportedWrite", err)
	}
}

func TestWriteOpusCanceled(t *testing.T) {
	src := readCorpus(t, "sample.opus")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var buf bytes.Buffer
	if _, err := testWriter(t).WriteFile(ctx, &buf, bytes.NewReader(src), int64(len(src)), keyEdit("TITLE", "x")); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if _, err := testWriter(t).PreservedDigest(ctx, bytes.NewReader(src), int64(len(src))); !errors.Is(err, context.Canceled) {
		t.Errorf("PreservedDigest err = %v, want context.Canceled", err)
	}
}

// TestWriteOpusRenumbersManyPages rewrites a stream whose audio takes dozens of
// pages: a long renumbering run crosses the flush size.
func TestWriteOpusRenumbersManyPages(t *testing.T) {
	src := syntheticOpus([]string{"TITLE=Old"}, 3000) // about 330 KB of audio
	out := writeBytes(t, src, keyEdit("BIG", strings.Repeat("q", 70_000)))
	checkStructure(t, "many pages", out)
	preserved(t, "many pages", src, out)
	if n, m := len(mustPages(t, src)), len(mustPages(t, out)); m != n+1 {
		t.Errorf("%d pages, want %d", m, n+1)
	}
}

func mustPages(t testing.TB, data []byte) []rawPage {
	t.Helper()
	pages, tail := rawPagesAndTail(t, data)
	if len(tail) != 0 {
		t.Fatalf("%d bytes after the last page", len(tail))
	}
	return pages
}

func TestPreservedDigest(t *testing.T) {
	src := syntheticOpus([]string{"TITLE=Old"}, 5)
	w := testWriter(t)
	digest := func(data []byte) []byte {
		t.Helper()
		d, err := w.PreservedDigest(t.Context(), bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	base := digest(src)

	// Changes that must change it: audio payload, a granule, the flags, the serial's lacing, a trailing byte.
	pages := mustPages(t, src)
	last := pages[len(pages)-1]
	mutations := map[string]func([]byte) []byte{
		"payload": func(d []byte) []byte { d[len(d)-1] ^= 1; return d },
		"granule": func(d []byte) []byte { d[len(d)-len(last.raw)+6] ^= 1; return d },
		"flags":   func(d []byte) []byte { d[len(d)-len(last.raw)+5] ^= 1; return d },
		"page 0":  func(d []byte) []byte { d[40] ^= 1; return d },
		"tail":    func(d []byte) []byte { return append(d, 'x') },
	}
	for name, mutate := range mutations {
		if bytes.Equal(digest(mutate(slices.Clone(src))), base) {
			t.Errorf("%s: digest unchanged", name)
		}
	}

	// Changes that must not: the comment header and the sequence numbers and checksums after it.
	out := writeBytes(t, src, keyEdit("BIG", strings.Repeat("q", 70_000)))
	if !bytes.Equal(digest(out), base) {
		t.Error("a rewrite changed the digest")
	}
}
