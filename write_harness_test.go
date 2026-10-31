package audiometa_test

// Independent audio-payload oracles for the write round-trip harness. They
// parse just enough of each container to find the audio bytes and must not
// use the library or its internal packages: they check the writers, so they
// cannot share code with them.

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"os"
	"slices"
	"testing"

	"github.com/simonhull/audiometa"
)

// synchsafe decodes a 4-byte ID3v2 synchsafe integer.
func synchsafe(b []byte) int {
	return int(b[0]&0x7F)<<21 | int(b[1]&0x7F)<<14 | int(b[2]&0x7F)<<7 | int(b[3]&0x7F)
}

// mp3AudioDigest hashes everything after the ID3v2 tag (and its footer) and
// before an ID3v1 trailer.
func mp3AudioDigest(t *testing.T, data []byte) [32]byte {
	t.Helper()
	start, end := 0, len(data)
	if len(data) >= 10 && string(data[:3]) == "ID3" {
		start = 10 + synchsafe(data[6:10])
		if data[3] == 4 && data[5]&0x10 != 0 {
			start += 10 // ID3v2.4 footer
		}
	}
	if end-start >= 128 && string(data[end-128:end-125]) == "TAG" {
		end -= 128
	}
	if start > end {
		t.Fatalf("mp3: ID3v2 tag (%d bytes) runs past the audio end %d", start, end)
	}
	return sha256.Sum256(data[start:end])
}

// flacAudioDigest hashes everything after the last metadata block.
func flacAudioDigest(t *testing.T, data []byte) [32]byte {
	t.Helper()
	if len(data) < 4 || string(data[:4]) != "fLaC" {
		t.Fatal("flac: missing fLaC marker")
	}
	off := 4
	for {
		if off+4 > len(data) {
			t.Fatalf("flac: metadata block header at %d past EOF", off)
		}
		header := data[off]
		length := int(data[off+1])<<16 | int(data[off+2])<<8 | int(data[off+3])
		off += 4 + length
		if header&0x80 != 0 {
			break
		}
	}
	if off > len(data) {
		t.Fatalf("flac: last metadata block ends at %d, past EOF", off)
	}
	return sha256.Sum256(data[off:])
}

// mp4Atom is one top-level atom: its type and the payload bounds.
type mp4Atom struct {
	typ        string
	start, end int // payload
}

// m4aTopLevelAtoms walks the top-level atoms of an MP4 file.
func m4aTopLevelAtoms(t *testing.T, data []byte) []mp4Atom {
	t.Helper()
	var atoms []mp4Atom
	for off := 0; off < len(data); {
		if off+8 > len(data) {
			t.Fatalf("m4a: atom header at %d past EOF", off)
		}
		size := int(binary.BigEndian.Uint32(data[off:]))
		typ := string(data[off+4 : off+8])
		header := 8
		switch size {
		case 0: // extends to EOF
			size = len(data) - off
		case 1: // 64-bit size follows the type
			if off+16 > len(data) {
				t.Fatalf("m4a: 64-bit size of %q past EOF", typ)
			}
			size = int(binary.BigEndian.Uint64(data[off+8:]))
			header = 16
		}
		if size < header || off+size > len(data) {
			t.Fatalf("m4a: atom %q at %d has bad size %d", typ, off, size)
		}
		atoms = append(atoms, mp4Atom{typ: typ, start: off + header, end: off + size})
		off += size
	}
	return atoms
}

// m4aAudioDigest hashes the payloads of every top-level mdat atom, in file order.
func m4aAudioDigest(t *testing.T, data []byte) [32]byte {
	t.Helper()
	h := sha256.New()
	for _, a := range m4aTopLevelAtoms(t, data) {
		if a.typ == "mdat" {
			h.Write(data[a.start:a.end])
		}
	}
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return sum
}

// m4aTopLevelTypes lists the top-level atom types in order, except moov, free
// and skip, which a writer may rewrite, resize or drop. Writers must preserve
// this list.
func m4aTopLevelTypes(t *testing.T, data []byte) []string {
	t.Helper()
	var types []string
	for _, a := range m4aTopLevelAtoms(t, data) {
		switch a.typ {
		case "moov", "free", "skip":
		default:
			types = append(types, a.typ)
		}
	}
	return types
}

// audioDigest dispatches to the oracle for format.
func audioDigest(t *testing.T, format audiometa.Format, data []byte) [32]byte {
	t.Helper()
	switch format {
	case audiometa.FormatMP3:
		return mp3AudioDigest(t, data)
	case audiometa.FormatFLAC:
		return flacAudioDigest(t, data)
	case audiometa.FormatM4A, audiometa.FormatM4B:
		return m4aAudioDigest(t, data)
	case audiometa.FormatUnknown, audiometa.FormatOgg, audiometa.FormatOpus, audiometa.FormatWAV, audiometa.FormatAIFF:
		t.Fatalf("no audio oracle for %s", format)
		return [32]byte{}
	default:
		t.Fatalf("no audio oracle for %s", format)
		return [32]byte{}
	}
}

func readTestdata(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestAudioDigest_MP3IgnoresTag(t *testing.T) {
	data := readTestdata(t, "testdata/sample.mp3")
	if string(data[:3]) != "ID3" {
		t.Fatal("sample.mp3 has no ID3v2 tag")
	}
	tagEnd := 10 + synchsafe(data[6:10])

	// A different ID3v2.4 tag: a 10-byte header and 20 zero bytes of body.
	replaced := []byte{'I', 'D', '3', 4, 0, 0, 0, 0, 0, 20}
	replaced = append(replaced, make([]byte, 20)...)
	replaced = append(replaced, data[tagEnd:]...)

	if mp3AudioDigest(t, data) != mp3AudioDigest(t, replaced) {
		t.Error("digest changed when only the ID3v2 tag changed")
	}
	flipped := bytes.Clone(data)
	flipped[tagEnd+100] ^= 0xFF
	if mp3AudioDigest(t, data) == mp3AudioDigest(t, flipped) {
		t.Error("digest did not change when an audio byte changed")
	}
}

func TestAudioDigest_FLACIgnoresMetadata(t *testing.T) {
	data := readTestdata(t, "testdata/sample.flac")
	streamInfoLen := int(data[5])<<16 | int(data[6])<<8 | int(data[7])
	streamInfo := bytes.Clone(data[4 : 8+streamInfoLen])
	streamInfo[0] |= 0x80 // re-flag STREAMINFO as the last block

	audioStart := 4
	for {
		header := data[audioStart]
		audioStart += 4 + (int(data[audioStart+1])<<16 | int(data[audioStart+2])<<8 | int(data[audioStart+3]))
		if header&0x80 != 0 {
			break
		}
	}
	if audioStart == 8+streamInfoLen {
		t.Fatal("sample.flac has no metadata besides STREAMINFO; the test would prove nothing")
	}

	stripped := append([]byte("fLaC"), streamInfo...)
	stripped = append(stripped, data[audioStart:]...)
	if flacAudioDigest(t, data) != flacAudioDigest(t, stripped) {
		t.Error("digest changed when only metadata blocks changed")
	}
}

func TestAudioDigest_M4AIgnoresMoov(t *testing.T) {
	data := readTestdata(t, "testdata/sample.m4a")
	i := bytes.Index(data, []byte("Test Title"))
	if i < 0 {
		t.Fatal(`sample.m4a has no "Test Title"`)
	}
	edited := bytes.Clone(data)
	edited[i] = 'X'
	if m4aAudioDigest(t, data) != m4aAudioDigest(t, edited) {
		t.Error("digest changed when only moov changed")
	}
	if got := m4aTopLevelTypes(t, data); !slices.Contains(got, "ftyp") || !slices.Contains(got, "mdat") || slices.Contains(got, "moov") {
		t.Errorf("m4aTopLevelTypes = %v, want ftyp and mdat but not moov", got)
	}

	var mdat mp4Atom
	for _, a := range m4aTopLevelAtoms(t, data) {
		if a.typ == "mdat" {
			mdat = a
		}
	}
	if mdat.end <= mdat.start {
		t.Fatal("sample.m4a has no mdat payload")
	}
	flipped := bytes.Clone(data)
	flipped[mdat.start] ^= 0xFF
	if m4aAudioDigest(t, data) == m4aAudioDigest(t, flipped) {
		t.Error("digest did not change when an mdat byte changed")
	}
}
