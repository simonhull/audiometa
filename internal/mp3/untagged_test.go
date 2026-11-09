package mp3

import "testing"

func TestParse_NoID3v2TagNoWarning(t *testing.T) {
	tests := map[string][]byte{
		"frames only": audioBytes(16000),
		"ID3v1 only":  append(audioBytes(16000), id3v1Trailer("Title", "Artist", "", "", "", 0)...),
		"short file":  {0xFF, 0xFB, 0x90, 0x00, 0, 0, 0, 0}, // too short for an ID3v2 header
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			f := parseBytes(t, data)
			if len(f.Warnings) != 0 {
				t.Errorf("warnings = %v, want none", f.Warnings)
			}
			if f.Audio.Bitrate != 128000 {
				t.Errorf("bitrate = %d, want 128000", f.Audio.Bitrate)
			}
		})
	}
}

func TestParse_BrokenID3v2TagStillWarns(t *testing.T) {
	tests := map[string][]byte{
		"truncated header":    {'I', 'D', '3', 3, 0},
		"unsupported version": append(id3Tag(5, make([]byte, 20)), mpegFrame...),
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			if f := parseBytes(t, data); !hasWarning(f, "ID3v2 parsing failed") {
				t.Errorf("warnings = %v, want an 'ID3v2 parsing failed' warning", f.Warnings)
			}
		})
	}
}
