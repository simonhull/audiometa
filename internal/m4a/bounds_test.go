package m4a

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	audiobinary "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// hugeExtendedAtomFile is a 32-byte file: a 16-byte ftyp atom followed by a
// "free" atom with size32=1 and extended size 0xFFFFFFFFFFFFFFF0 (-16 as int64).
func hugeExtendedAtomFile() []byte {
	buf := &bytes.Buffer{}
	binary.Write(buf, binary.BigEndian, uint32(16))
	buf.WriteString("ftyp")
	buf.WriteString("isom")
	binary.Write(buf, binary.BigEndian, uint32(0))
	binary.Write(buf, binary.BigEndian, uint32(1))
	buf.WriteString("free")
	binary.Write(buf, binary.BigEndian, uint64(0xFFFFFFFFFFFFFFF0))
	return buf.Bytes()
}

func TestFindAtom_RejectsSizeBeyondParent(t *testing.T) {
	data := hugeExtendedAtomFile()
	sr := audiobinary.NewSafeReader(bytes.NewReader(data), int64(len(data)), "huge.m4a")

	done := make(chan error, 1)
	go func() {
		_, err := findAtom(sr, 0, int64(len(data)), "moov")
		done <- err
	}()

	select {
	case err := <-done:
		corrupted, ok := errors.AsType[*types.CorruptedFileError](err)
		if !ok {
			t.Fatalf("expected *types.CorruptedFileError, got %T: %v", err, err)
		}
		if corrupted.Offset != 16 {
			t.Errorf("expected offset 16, got %d", corrupted.Offset)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("findAtom did not return within 2s: atom size overflow loops forever")
	}
}

func TestParse_HugeExtendedAtomTerminates(t *testing.T) {
	data := hugeExtendedAtomFile()

	done := make(chan error, 1)
	go func() {
		_, err := (&parser{}).Parse(context.Background(), bytes.NewReader(data), int64(len(data)), "huge.m4a")
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Parse should degrade gracefully, got error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Parse did not return within 2s")
	}
}
