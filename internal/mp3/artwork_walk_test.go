package mp3

import (
	"bytes"
	"context"
	"runtime"
	"slices"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

// unsync applies ID3 unsynchronisation: it puts 0x00 after every 0xFF that is
// followed by 0x00, by a byte of 0xE0 or more, or by nothing.
func unsync(b []byte) []byte {
	out := make([]byte, 0, len(b)+len(b)/8)
	for i, c := range b {
		out = append(out, c)
		if c == 0xFF && (i+1 == len(b) || b[i+1] == 0x00 || b[i+1] >= 0xE0) {
			out = append(out, 0x00)
		}
	}
	return out
}

// pictureImage holds every byte pattern unsynchronisation rewrites: FF E0,
// FF 00 and a trailing FF.
var pictureImage = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0xFF, 0x00, 0x01, 0xFF}

func extractAll(t *testing.T, data []byte) []types.Artwork {
	t.Helper()
	art, err := (&parser{}).ExtractArtwork(context.Background(), bytes.NewReader(data), int64(len(data)), "t.mp3")
	if err != nil {
		t.Fatalf("ExtractArtwork: %v", err)
	}
	return art
}

func TestExtractArtwork_DecodesFramesLikeMetadata(t *testing.T) {
	t.Parallel()
	apic := apicPayload("image/jpeg", 3, "", pictureImage)
	tests := []struct {
		name string
		tag  []byte
	}{
		{"v2.3 tag-level unsynchronisation", tagBytes(3, flagUnsync, unsync(slices.Concat(
			frameBytes(3, "TIT2", 0, latin1Text("T")), frameBytes(3, "APIC", 0, apic))))},
		{"v2.4 frame unsynchronisation", tagBytes(4, 0, frameBytes(4, "APIC", v24FrameUnsync, unsync(apic)))},
		{"v2.4 tag-level unsynchronisation", tagBytes(4, flagUnsync, frameBytes(4, "APIC", 0, unsync(apic)))},
		{"v2.4 data length indicator", tagBytes(4, 0, frameBytes(4, "APIC", v24FrameDataLength,
			slices.Concat(synchsafe(len(apic)), apic)))},
		{"empty frame before the picture", tagBytes(3, 0, slices.Concat(
			frameBytes(3, "TCOP", 0, nil), frameBytes(3, "APIC", 0, apic)))},
		{"compressed picture skipped", tagBytes(3, 0, slices.Concat(
			frameBytes(3, "APIC", v23FrameCompression, []byte{0, 0, 0, 0x40, 'x', 'x'}), frameBytes(3, "APIC", 0, apic)))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			art := extractAll(t, append(tt.tag, mpegFrame...))
			if len(art) != 1 || !bytes.Equal(art[0].Data, pictureImage) {
				var got [][]byte
				for _, a := range art {
					got = append(got, a.Data)
				}
				t.Errorf("pictures = %x, want exactly one holding %x", got, pictureImage)
			}
		})
	}
}

func TestExtractArtwork_CompressedV22TagHasNone(t *testing.T) {
	t.Parallel()
	tag := v22Tag() // holds a PIC frame
	tag[5] = flagV22Compression
	if art := extractAll(t, append(tag, mpegFrame...)); len(art) != 0 {
		t.Errorf("got %d pictures from a compressed ID3v2.2 tag, want 0", len(art))
	}
}

// Not parallel: it measures process-wide allocation.
func TestExtractArtwork_OversizedFrameAllocatesLittle(t *testing.T) {
	// A 40-byte file whose APIC header claims 0x06300000 bytes (99 MiB).
	data := tagBytes(3, 0, append([]byte{'A', 'P', 'I', 'C', 0x06, 0x30, 0x00, 0x00, 0, 0}, make([]byte, 20)...))
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	art := extractAll(t, data)
	runtime.ReadMemStats(&after)
	if len(art) != 0 {
		t.Errorf("got %d pictures, want 0", len(art))
	}
	if got := after.TotalAlloc - before.TotalAlloc; got > 1<<20 {
		t.Errorf("ExtractArtwork allocated %d bytes for a %d-byte file, want under 1 MiB", got, len(data))
	}
}
