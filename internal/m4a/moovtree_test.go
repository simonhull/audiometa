package m4a

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

func TestMAtom_RoundTripCorpus(t *testing.T) {
	for _, path := range []string{"../../testdata/sample.m4b", "../../testdata/sample.m4a", "../../testdata/bench/book.m4b"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		m := topAtomOf(t, data, atomMoov)
		got, err := moovTreeOf(t, data).bytes()
		if err != nil {
			t.Fatalf("%s: bytes: %v", path, err)
		}
		if !bytes.Equal(got, data[m.start:m.end]) {
			t.Errorf("%s: serialized moov differs from the source", path)
		}
	}
}

func TestMAtom_RoundTripSynthetic(t *testing.T) {
	for _, faststart := range []bool{false, true} {
		data := buildMP4(faststart, ilstText("\xA9nam", "T"), ilstFreeform("Narrator", "N"), covrItem(covrImage{13, []byte{1, 2, 3}}))
		m := topAtomOf(t, data, atomMoov)
		root := moovTreeOf(t, data)
		if findPath(root, atomUdta, atomMeta, atomIlst) == nil || len(traks(root)) != 1 {
			t.Fatal("tree is missing ilst or the trak")
		}
		got, err := root.bytes()
		if err != nil || !bytes.Equal(got, data[m.start:m.end]) {
			t.Errorf("faststart %v: round trip differs (err %v)", faststart, err)
		}
	}
}

func TestMAtom_ExtendedHeaderPreserved(t *testing.T) {
	ilst := atom("ilst", ilstText("\xA9nam", "T"))
	udta := extAtom("udta", atom("meta", []byte{0, 0, 0, 0}, mdirHdlr, ilst))
	moov := atom("moov", atom("mvhd", make([]byte, 100)), udta)
	root := moovTreeOf(t, moov)
	got, err := root.bytes()
	if err != nil || !bytes.Equal(got, moov) {
		t.Fatalf("round trip differs (err %v)", err)
	}

	item := findPath(root, atomUdta, atomMeta, atomIlst).children[0]
	item.payload = append(item.payload, make([]byte, 10)...)
	got, err = root.bytes()
	if err != nil {
		t.Fatal(err)
	}
	u := bytes.Index(got, []byte("udta")) - 4
	if binary.BigEndian.Uint32(got[u:]) != 1 {
		t.Fatal("udta lost its 64-bit header")
	}
	if size := binary.BigEndian.Uint64(got[u+8:]); size != uint64(len(udta)+10) {
		t.Errorf("udta 64-bit size = %d, want %d", size, len(udta)+10)
	}
	if !bytes.Equal(got[u:], moovTreeOf(t, got).child(atomUdta).appendTo(nil)) {
		t.Error("grown tree does not re-parse to itself")
	}
}

func TestMAtom_SizesFixedUp(t *testing.T) {
	data := buildMP4(false, ilstText("\xA9nam", "Title"))
	before := moovTreeOf(t, data)
	sizes := func(root *matom) []uint64 {
		var out []uint64
		for _, path := range [][]string{{}, {atomUdta}, {atomUdta, atomMeta}, {atomUdta, atomMeta, atomIlst}} {
			out = append(out, findPath(root, path...).size())
		}
		return out
	}
	want := sizes(before)

	root := moovTreeOf(t, data)
	item := findPath(root, atomUdta, atomMeta, atomIlst).children[0]
	item.payload = append(item.payload, make([]byte, 10)...)
	out, err := root.bytes()
	if err != nil {
		t.Fatal(err)
	}
	got := sizes(moovTreeOf(t, out))
	for i := range want {
		if got[i] != want[i]+10 {
			t.Errorf("level %d size = %d, want %d", i, got[i], want[i]+10)
		}
	}
}

func TestMAtom_MalformedUdtaKeptVerbatim(t *testing.T) {
	// A udta whose child overruns it is kept as an opaque leaf.
	bad := atom("udta", []byte{0, 0, 0, 99}, []byte("junk"))
	moov := atom("moov", atom("mvhd", make([]byte, 100)), bad)
	root := moovTreeOf(t, moov)
	if u := root.child(atomUdta); u == nil || u.container {
		t.Fatal("malformed udta should be a leaf")
	}
	if got, _ := root.bytes(); !bytes.Equal(got, moov) {
		t.Error("malformed udta not preserved")
	}
}

func TestMAtom_MalformedStblIsCorrupt(t *testing.T) {
	stbl := atom("stbl", []byte{0, 0, 0, 99}, []byte("stco"))
	moov := atom("moov", atom("trak", atom("mdia", atom("minf", stbl))))
	_, err := readMoovTree(srOf(moov), &Atom{Type: atomMoov, Size: int64(len(moov))})
	if _, ok := errors.AsType[*types.CorruptedFileError](err); !ok {
		t.Errorf("err = %v, want CorruptedFileError", err)
	}
}

func TestMAtom_UdtaTerminatorPreserved(t *testing.T) {
	udta := atom("udta", atom("meta", []byte{0, 0, 0, 0}, mdirHdlr, atom("ilst")), []byte{0, 0, 0, 0})
	moov := atom("moov", atom("mvhd", make([]byte, 100)), udta)
	root := moovTreeOf(t, moov)
	if u := root.child(atomUdta); !u.container || len(u.tail) != 4 {
		t.Fatal("udta terminator not kept as tail")
	}
	if got, _ := root.bytes(); !bytes.Equal(got, moov) {
		t.Error("udta terminator not preserved")
	}
}
