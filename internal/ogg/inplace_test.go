package ogg

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/simonhull/audiometa/internal/registry"
	"github.com/simonhull/audiometa/internal/types"
	"github.com/simonhull/audiometa/internal/writeutil"
)

func tryPatch(t testing.TB, src []byte, edits *types.EditSet) (*registry.Patch, error) {
	t.Helper()
	return writerFor(t, src).InPlacePatch(t.Context(), bytes.NewReader(src), int64(len(src)), edits)
}

// applied returns src with p written over it.
func applied(src []byte, p *registry.Patch) []byte {
	out := slices.Clone(src)
	copy(out[p.Off:], p.Data)
	return out
}

// sameLength is an edit that replaces a title by one of the same length.
func sameLength(t *testing.T, src []byte) *types.EditSet {
	t.Helper()
	title := parseOggBytes(t, src).Tags.GetFirst("TITLE")
	if title == "" {
		t.Fatal("no title")
	}
	return keyEdit("TITLE", strings.Repeat("Z", len(title)))
}

// TestInPlacePatchEqualsRewrite checks, for every corpus file, that an edit
// that keeps the comment packet's length patches only header pages and that
// the patched file is what a full rewrite writes.
func TestInPlacePatchEqualsRewrite(t *testing.T) {
	for _, name := range corpusFiles {
		src := readCorpus(t, name)
		l, err := layoutOf(t, src)
		if err != nil {
			t.Fatal(err)
		}
		edits := sameLength(t, src)
		p, err := tryPatch(t, src, edits)
		if err != nil || p == nil {
			t.Fatalf("%s: patch %v, err %v", name, p, err)
		}
		if p.Off < l.idEnd || p.Off+int64(len(p.Data)) > l.audioStart || len(p.Data) == 0 {
			t.Errorf("%s: patch [%d, %d) outside the header pages [%d, %d)", name, p.Off, p.Off+int64(len(p.Data)), l.idEnd, l.audioStart)
		}
		patched := applied(src, p)
		if rewritten := writeBytes(t, src, edits); !bytes.Equal(patched, rewritten) {
			t.Errorf("%s: the patched file differs from the rewrite", name)
		}
		checkStructure(t, name, patched)
		preserved(t, name, src, patched)
		if got := parseOggBytes(t, patched).Tags.GetFirst("TITLE"); got != strings.Repeat("Z", len(got)) || got == "" {
			t.Errorf("%s: title %q", name, got)
		}
		if len(patched) != len(src) {
			t.Errorf("%s: the patched file has %d bytes, want %d", name, len(patched), len(src))
		}
	}
}

func TestInPlacePatchDeclinesAChangeOfLength(t *testing.T) {
	for _, name := range corpusFiles {
		src := readCorpus(t, name)
		for label, edits := range map[string]*types.EditSet{
			"longer":  keyEdit("TITLE", "A much longer title than the sample has"),
			"shorter": keyEdit("TITLE", "S"),
			"new key": keyEdit("NEWKEY", "x"),
			"page":    keyEdit("BIG", strings.Repeat("x", 70_000)),
		} {
			if p, err := tryPatch(t, src, edits); err != nil || p != nil {
				t.Errorf("%s %s: patch %v, err %v; want nil, nil", name, label, p, err)
			}
		}
	}
}

func TestInPlacePatchOfNoChange(t *testing.T) {
	src := readCorpus(t, "sample.opus")
	p, err := tryPatch(t, src, keyEdit("NO_SUCH_KEY"))
	if err != nil || p == nil || len(p.Data) != 0 {
		t.Errorf("patch %+v, err %v; want an empty patch", p, err)
	}
}

// TestInPlacePatchCoversOnlyChangedPages builds a comment header on four small
// pages and edits bytes on the third page only.
func TestInPlacePatchCoversOnlyChangedPages(t *testing.T) {
	filler := strings.Repeat("f", 1000)
	specs := []pageSpec{{headerType: 0x02, serial: 5, lacing: lacingFor(len(opusHeadPacket())), data: opusHeadPacket()}}
	specs = append(specs, smallPages(commentPacket("OpusTags", false, "A="+filler, "B="+filler, "E="+filler, "C=x", "D="+filler, "F="+filler, "G="+filler), 5)...)
	specs = append(specs, audioTail(5, 960))
	src := withCRCs(buildOgg(specs...))
	pages := mustPages(t, src)
	if len(pages) < 6 {
		t.Fatalf("%d pages", len(pages))
	}
	// Flip the value of C, which sits on one page of the header region.
	edits := keyEdit("C", "y")
	p, err := tryPatch(t, src, edits)
	if err != nil || p == nil {
		t.Fatalf("patch %v, err %v", p, err)
	}
	var covered []int
	for i, pg := range pages {
		off := 0
		for _, q := range pages[:i] {
			off += len(q.raw)
		}
		if int64(off) < p.Off+int64(len(p.Data)) && int64(off+len(pg.raw)) > p.Off {
			covered = append(covered, i)
		}
	}
	if len(covered) == 0 || len(covered) >= len(pages)-2 {
		t.Errorf("the patch covers pages %v of %d header pages", covered, len(pages)-2)
	}
	if got := applied(src, p); !bytes.Equal(got, writeBytes(t, src, edits)) {
		t.Error("patched file differs from the rewrite")
	}
	// The layout is the old one, though paginate would lay the packet out differently.
	before, err := layoutOf(t, src)
	if err != nil {
		t.Fatal(err)
	}
	after, err := layoutOf(t, applied(src, p))
	if err != nil || len(after.pages) != len(before.pages) || len(before.pages) < 4 {
		t.Errorf("header pages %d before, %d after the patch, err %v", len(before.pages), len(after.pages), err)
	}
}

// smallPages lays one packet out on pages of at most 4 lacing values each.
func smallPages(packet []byte, serial uint32) []pageSpec {
	lacing := lacingFor(len(packet))
	var out []pageSpec
	for len(lacing) > 0 {
		n := min(len(lacing), 4)
		size := 0
		for _, l := range lacing[:n] {
			size += int(l)
		}
		ht := byte(pageContinued)
		if len(out) == 0 {
			ht = 0
		}
		out = append(out, pageSpec{headerType: ht, serial: serial, lacing: lacing[:n], data: packet[:size]})
		packet, lacing = packet[size:], lacing[n:]
	}
	return out
}

// TestOpusPaddingAbsorbsTheChange covers the data after the comments:
// padding keeps the packet's length while the comments fit in it, binary data
// is kept as it is.
func TestOpusPaddingAbsorbsTheChange(t *testing.T) {
	build := func(tail []byte) []byte {
		body := append(commentPacket("OpusTags", false, "TITLE=Old title"), tail...)
		return withCRCs(buildOgg(
			pageSpec{headerType: 0x02, serial: 5, lacing: lacingFor(len(opusHeadPacket())), data: opusHeadPacket()},
			pageSpec{serial: 5, lacing: lacingFor(len(body)), data: body},
			pageSpec{headerType: pageEOS, serial: 5, granule: 960, lacing: lacingFor(100), data: make([]byte, 100)},
		))
	}
	padded := build(make([]byte, 64))
	oldLen := func(b []byte) int {
		l, err := layoutOf(t, b)
		if err != nil {
			t.Fatal(err)
		}
		return len(l.packets[1])
	}

	for label, edits := range map[string]*types.EditSet{
		"shrinks":          keyEdit("TITLE", "New"),
		"grows in padding": keyEdit("TITLE", "A new title that is longer"),
		"same length":      keyEdit("TITLE", "Old Title"),
		"new key":          keyEdit("X", "y"),
	} {
		p, err := tryPatch(t, padded, edits)
		if err != nil || p == nil {
			t.Fatalf("%s: patch %v, err %v", label, p, err)
		}
		out := applied(padded, p)
		if oldLen(out) != oldLen(padded) {
			t.Errorf("%s: packet length %d, want %d", label, oldLen(out), oldLen(padded))
		}
		if !bytes.Equal(out, writeBytes(t, padded, edits)) {
			t.Errorf("%s: patched file differs from the rewrite", label)
		}
		checkStructure(t, label, out)
	}

	// Growing past the padding changes the length: no patch, and the rewrite drops what is left of it.
	grown := keyEdit("BIG", strings.Repeat("g", 200))
	if p, err := tryPatch(t, padded, grown); err != nil || p != nil {
		t.Errorf("growth beyond the padding: patch %v, err %v", p, err)
	}
	out := writeBytes(t, padded, grown)
	if l, err := layoutOf(t, out); err != nil || bytes.HasSuffix(l.packets[1], []byte{0}) {
		t.Errorf("rewrite beyond the padding: err %v, padding left: %v", err, err == nil)
	}

	// Binary data (lowest bit of its first byte set) is kept, so only an equal-length edit patches.
	binary := build([]byte{0x01, 0xAA, 0xBB})
	if p, err := tryPatch(t, binary, keyEdit("TITLE", "New")); err != nil || p != nil {
		t.Errorf("shrinking next to binary data: patch %v, err %v", p, err)
	}
	if p, err := tryPatch(t, binary, keyEdit("TITLE", "Old Title")); err != nil || p == nil {
		t.Errorf("equal-length edit next to binary data: patch %v, err %v", p, err)
	}
	out = writeBytes(t, binary, keyEdit("TITLE", "New"))
	if l, err := layoutOf(t, out); err != nil || !bytes.HasSuffix(l.packets[1], []byte{0x01, 0xAA, 0xBB}) {
		t.Errorf("binary data after the comments lost: %v", err)
	}
}

// TestPreservedRanges checks the contract of registry.PreservedRanger: the
// digest is the hash of the ranges, the ranges leave out the header pages and
// the sequence numbers and checksums after them, and a rewrite or an in-place
// patch moves nothing the ranges cover.
func TestPreservedRanges(t *testing.T) {
	trailing := append(readCorpus(t, "sample.opus"), "a tag after the last page"...)
	files := map[string][]byte{"trailing bytes": trailing, "truncated page": readCorpus(t, "sample.ogg")}
	files["truncated page"] = files["truncated page"][:len(files["truncated page"])-9]
	for _, name := range corpusFiles {
		files[name] = readCorpus(t, name)
	}
	for name, src := range files {
		w := writerFor(t, src)
		r := bytes.NewReader(src)
		ranges, err := w.PreservedRanges(t.Context(), r, int64(len(src)))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want, err := writeutil.DigestRanges(t.Context(), r, ranges)
		if err != nil {
			t.Fatal(err)
		}
		got, err := w.PreservedDigest(t.Context(), r, int64(len(src)))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s: PreservedDigest is not the hash of PreservedRanges (err %v)", name, err)
		}
		l, err := layoutOf(t, src)
		if err != nil {
			t.Fatal(err)
		}
		for i, rg := range ranges {
			if rg.Off >= rg.End || (i > 0 && rg.Off < ranges[i-1].End) {
				t.Errorf("%s: range %d %v is empty or out of order", name, i, rg)
			}
			if rg.Off > 0 && rg.Off < l.audioStart {
				t.Errorf("%s: range %v starts in the header pages", name, rg)
			}
		}
		if ranges[0] != (registry.Range{Off: 0, End: l.idEnd}) {
			t.Errorf("%s: first range %v, want page 0", name, ranges[0])
		}
		if bytes.HasSuffix(src, []byte("a tag after the last page")) && ranges[len(ranges)-1].End != int64(len(src)) {
			t.Errorf("%s: the trailing bytes are not covered", name)
		}
	}

	// An in-place patch and a rewrite that adds a page both leave the digest alone;
	// the patch leaves the ranges themselves where they were.
	src := readCorpus(t, "bench/book.opus")
	w := writerFor(t, src)
	r0, err := w.PreservedRanges(t.Context(), bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatal(err)
	}
	p, err := tryPatch(t, src, sameLength(t, src))
	if err != nil || p == nil {
		t.Fatalf("patch %v, err %v", p, err)
	}
	patch := registry.Range{Off: p.Off, End: p.Off + int64(len(p.Data))}
	patched := applied(src, p)
	r1, err := w.PreservedRanges(t.Context(), bytes.NewReader(patched), int64(len(patched)))
	if err != nil || !slices.Equal(r0, r1) {
		t.Errorf("ranges moved after the patch: %v", err)
	}
	for _, rg := range r0 {
		if patch.Overlaps(rg) {
			t.Errorf("patch %v overlaps preserved bytes %v", patch, rg)
		}
	}
	grown := writeBytes(t, src, keyEdit("BIG", strings.Repeat("x", 70_000)))
	d0, _ := w.PreservedDigest(t.Context(), bytes.NewReader(src), int64(len(src)))
	d1, _ := w.PreservedDigest(t.Context(), bytes.NewReader(grown), int64(len(grown)))
	if !bytes.Equal(d0, d1) {
		t.Error("a rewrite that renumbers every page changed the preserved digest")
	}
}
