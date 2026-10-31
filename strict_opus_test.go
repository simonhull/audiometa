package audiometa_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/simonhull/audiometa"
)

// minimalOpus builds an Ogg Opus stream whose OpusHead records a 44.1 kHz source and a
// non-zero output gain, as most transcoded real-world files do.
func minimalOpus() []byte {
	buf := &bytes.Buffer{}
	page := func(headerType byte, granule uint64, seq uint32, data []byte) {
		buf.WriteString("OggS")
		buf.WriteByte(0)
		buf.WriteByte(headerType)
		_ = binary.Write(buf, binary.LittleEndian, granule)
		_ = binary.Write(buf, binary.LittleEndian, uint32(777)) // serial
		_ = binary.Write(buf, binary.LittleEndian, seq)
		_ = binary.Write(buf, binary.LittleEndian, uint32(0)) // checksum (unchecked)
		var segs []byte
		for rem := len(data); rem > 0; rem -= 255 {
			segs = append(segs, byte(min(rem, 255)))
		}
		buf.WriteByte(byte(len(segs)))
		buf.Write(segs)
		buf.Write(data)
	}

	head := &bytes.Buffer{}
	head.WriteString("OpusHead")
	head.WriteByte(1) // version
	head.WriteByte(2) // channels
	_ = binary.Write(head, binary.LittleEndian, uint16(312))
	_ = binary.Write(head, binary.LittleEndian, uint32(44100)) // input sample rate
	_ = binary.Write(head, binary.LittleEndian, int16(256))    // output gain
	head.WriteByte(0)                                          // mapping family
	page(0x02, 0, 0, head.Bytes())

	tags := &bytes.Buffer{}
	tags.WriteString("OpusTags")
	vendor := "audiometa-test"
	_ = binary.Write(tags, binary.LittleEndian, uint32(len(vendor)))
	tags.WriteString(vendor)
	_ = binary.Write(tags, binary.LittleEndian, uint32(1))
	title := "TITLE=Strict"
	_ = binary.Write(tags, binary.LittleEndian, uint32(len(title)))
	tags.WriteString(title)
	page(0x00, 0, 1, tags.Bytes())

	page(0x04, 48000, 2, make([]byte, 100)) // last page: 1 second
	return buf.Bytes()
}

func TestStrictParsing_ValidOpus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.opus")
	if err := os.WriteFile(path, minimalOpus(), 0o600); err != nil {
		t.Fatal(err)
	}

	f, err := audiometa.Open(path, audiometa.WithStrictParsing())
	if err != nil {
		t.Fatalf("strict mode rejected a valid Opus file: %v", err)
	}
	defer f.Close()
	if len(f.Warnings) != 0 {
		t.Errorf("unexpected warnings on valid Opus: %v", f.Warnings)
	}
	if f.Tags.Title != "Strict" {
		t.Errorf("Title = %q", f.Tags.Title)
	}
}
