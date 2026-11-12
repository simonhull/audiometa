package m4a

import (
	"bytes"
	"encoding/binary"
	"testing"

	audiobinary "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// esdsPayload builds the descriptor bytes that follow an esds atom's
// version+flags: ES_Descriptor(0x03) → DecoderConfigDescriptor(0x04, OTI 0x40)
// → DecoderSpecificInfo(0x05) holding asc, then SLConfigDescriptor(0x06).
func esdsPayload(esFlags byte, asc []byte) []byte {
	dsi := append([]byte{0x05, byte(len(asc))}, asc...)
	dcd := append([]byte{
		0x04, byte(13 + len(dsi)),
		0x40,             // objectTypeIndication: MPEG-4 Audio
		0x15,             // streamType audio, upStream 0, reserved 1
		0x00, 0x00, 0x00, // bufferSizeDB
		0x00, 0x01, 0xF4, 0x00, // maxBitrate
		0x00, 0x01, 0xF4, 0x00, // avgBitrate
	}, dsi...)
	esBody := []byte{0x00, 0x01, esFlags} // ES_ID, flags
	if esFlags&0x80 != 0 {
		esBody = append(esBody, 0x00, 0x02) // dependsOn_ES_ID
	}
	esBody = append(esBody, dcd...)
	esBody = append(esBody, 0x06, 0x01, 0x02) // SLConfigDescriptor
	return append([]byte{0x03, byte(len(esBody))}, esBody...)
}

// mp4aSampleEntry builds a version-0 mp4a sample entry with a child esds atom.
func mp4aSampleEntry(esds []byte) []byte {
	body := &bytes.Buffer{}
	body.Write(make([]byte, 6))                      // reserved
	binary.Write(body, binary.BigEndian, uint16(1))  // data reference index
	body.Write(make([]byte, 8))                      // version, revision, vendor
	binary.Write(body, binary.BigEndian, uint16(2))  // channels
	binary.Write(body, binary.BigEndian, uint16(16)) // sample size
	body.Write(make([]byte, 4))                      // compression ID, packet size
	binary.Write(body, binary.BigEndian, uint32(44100<<16))
	body.Write(createMockAtom("esds", append([]byte{0, 0, 0, 0}, esds...)))
	return createMockAtom("mp4a", body.Bytes())
}

func TestParseESDescriptors(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		wantType uint8
		wantAvg  uint32
	}{
		{"AAC-LC", esdsPayload(0, []byte{0x12, 0x10}), 2, 128000},
		{"HE-AAC explicit", esdsPayload(0, []byte{0x2B, 0x92, 0x08, 0x00}), 5, 128000},
		{"escaped AOT 42", esdsPayload(0, []byte{0xF9, 0x40}), 42, 128000},
		{"streamDependenceFlag", esdsPayload(0x80, []byte{0x12, 0x10}), 2, 128000},
		{"truncated", esdsPayload(0, []byte{0x12, 0x10})[:10], 0, 0},
		{"empty", nil, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseESDescriptors(tt.data)
			if got.objectType != tt.wantType || got.avgBitrate != tt.wantAvg {
				t.Errorf("parseESDescriptors() = %+v, want type %d avgBitrate %d", got, tt.wantType, tt.wantAvg)
			}
		})
	}
}

func TestParseCodecDetails_AACProfile(t *testing.T) {
	tests := []struct {
		name        string
		asc         []byte
		wantProfile string
		wantDesc    string
	}{
		{"AAC-LC", []byte{0x12, 0x10}, "AAC-LC", "AAC"},
		{"HE-AAC", []byte{0x2B, 0x92, 0x08, 0x00}, "HE-AAC", "HE-AAC"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The entry is the whole file, so fewer than 256 bytes follow it.
			entry := mp4aSampleEntry(esdsPayload(0, tt.asc))
			sr := audiobinary.NewSafeReader(bytes.NewReader(entry), int64(len(entry)), "test.m4a")
			file := &types.File{}
			parseCodecDetails(sr, 0, uint32(len(entry)), "mp4a", file)
			if len(file.Warnings) != 0 {
				t.Fatalf("parseCodecDetails warned: %+v", file.Warnings)
			}
			if file.Audio.CodecProfile != tt.wantProfile {
				t.Errorf("CodecProfile = %q, want %q", file.Audio.CodecProfile, tt.wantProfile)
			}
			if file.Audio.CodecDescription != tt.wantDesc {
				t.Errorf("CodecDescription = %q, want %q", file.Audio.CodecDescription, tt.wantDesc)
			}
			if file.Audio.Bitrate != 128000 {
				t.Errorf("Bitrate = %d, want the esds avgBitrate 128000", file.Audio.Bitrate)
			}
		})
	}
}
