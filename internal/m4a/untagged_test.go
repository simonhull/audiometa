package m4a

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"
	"time"

	"github.com/simonhull/audiometa/internal/types"
)

func m4bFtyp() []byte {
	ftyp := &bytes.Buffer{}
	ftyp.WriteString("M4B ")
	binary.Write(ftyp, binary.BigEndian, uint32(0))
	ftyp.WriteString("M4B ")
	return createMockAtom("ftyp", ftyp.Bytes())
}

func parseBytes(t *testing.T, data []byte) *types.File {
	t.Helper()
	file, err := (&parser{}).Parse(context.Background(), bytes.NewReader(data), int64(len(data)), "untagged.m4b")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	return file
}

func TestParse_UntaggedStillParsesTechnicalAndChapters(t *testing.T) {
	mvhd := createMvhdAtom(0, 1000, 180_000) // 180 s
	chpl := createChplAtom([]struct {
		time  int64
		title string
	}{{0, "One"}, {600_000_000, "Two"}})
	metaNoIlst := createMockAtom("meta", []byte{0, 0, 0, 0}) // version+flags only

	tests := []struct {
		name         string
		moovChildren [][]byte
		wantChapters int
	}{
		{"no udta", [][]byte{mvhd}, 0},
		{"udta without meta", [][]byte{mvhd, createMockAtom("udta", chpl)}, 2},
		{"meta without ilst", [][]byte{mvhd, createMockAtom("udta", append(append([]byte{}, chpl...), metaNoIlst...))}, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			moov := createMockAtom("moov", bytes.Join(tt.moovChildren, nil))
			data := append(m4bFtyp(), moov...)

			file := parseBytes(t, data)

			if file.Audio.Duration != 180*time.Second {
				t.Errorf("Duration = %v, want 3m0s", file.Audio.Duration)
			}
			if len(file.Chapters) != tt.wantChapters {
				t.Errorf("len(Chapters) = %d, want %d", len(file.Chapters), tt.wantChapters)
			}
			if len(file.Warnings) != 0 {
				t.Errorf("expected no warnings for an untagged file, got %v", file.Warnings)
			}
		})
	}
}

func TestParse_CorruptUdtaWarnsButKeepsDuration(t *testing.T) {
	mvhd := createMvhdAtom(0, 1000, 180_000)
	// udta whose child claims more bytes than udta holds.
	bad := []byte{0, 0, 0xFF, 0, 'm', 'e', 't', 'a'}
	moov := createMockAtom("moov", append(append([]byte{}, mvhd...), createMockAtom("udta", bad)...))
	file := parseBytes(t, append(m4bFtyp(), moov...))

	if file.Audio.Duration != 180*time.Second {
		t.Errorf("Duration = %v, want 3m0s", file.Audio.Duration)
	}
	n := 0
	for _, w := range file.Warnings {
		if w.Stage == types.StageMetadata {
			n++
		}
	}
	if n != 1 {
		t.Errorf("expected exactly 1 StageMetadata warning, got %d (%v)", n, file.Warnings)
	}
}
