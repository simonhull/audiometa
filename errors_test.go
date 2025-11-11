package audiometa

import (
	"strings"
	"testing"
)

func TestOutOfBoundsError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *OutOfBoundsError
		contains []string
	}{
		{
			name: "offset beyond file size",
			err: &OutOfBoundsError{
				Path:   "test.m4b",
				Offset: 1000,
				Length: 4,
				Size:   500,
				What:   "ftyp atom",
			},
			contains: []string{"test.m4b", "offset 1000 out of bounds", "file size: 500", "ftyp atom"},
		},
		{
			name: "read would exceed file size",
			err: &OutOfBoundsError{
				Path:   "audio.m4a",
				Offset: 100,
				Length: 50,
				Size:   120,
				What:   "atom header",
			},
			contains: []string{"audio.m4a", "read of 50 bytes", "offset 100", "exceed file size 120", "atom header"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.err.Error()
			for _, substr := range tt.contains {
				if !strings.Contains(msg, substr) {
					t.Errorf("error message %q should contain %q", msg, substr)
				}
			}
		})
	}
}

func TestUnsupportedFormatError_Error(t *testing.T) {
	err := &UnsupportedFormatError{
		Path:   "test.mp3",
		Reason: "not an M4B/M4A file",
	}

	msg := err.Error()
	if !strings.Contains(msg, "test.mp3") {
		t.Errorf("error should contain path, got: %s", msg)
	}
	if !strings.Contains(msg, "not an M4B/M4A file") {
		t.Errorf("error should contain reason, got: %s", msg)
	}
	if !strings.Contains(msg, "unsupported format") {
		t.Errorf("error should contain 'unsupported format', got: %s", msg)
	}
}

func TestCorruptedFileError_Error(t *testing.T) {
	err := &CorruptedFileError{
		Path:   "broken.m4b",
		Offset: 256,
		Reason: "invalid atom size",
	}

	msg := err.Error()
	if !strings.Contains(msg, "broken.m4b") {
		t.Errorf("error should contain path, got: %s", msg)
	}
	if !strings.Contains(msg, "offset 256") {
		t.Errorf("error should contain offset, got: %s", msg)
	}
	if !strings.Contains(msg, "invalid atom size") {
		t.Errorf("error should contain reason, got: %s", msg)
	}
	if !strings.Contains(msg, "corrupted file") {
		t.Errorf("error should contain 'corrupted file', got: %s", msg)
	}
}
