package m4a

import (
	"testing"
)

func TestMapCodecName(t *testing.T) {
	tests := []struct {
		fourCC   string
		expected string
	}{
		{"mhm1", "xHE-AAC"},
		{"mhm2", "xHE-AAC v2"},
		{"ec-3", "E-AC-3"},
		{"ac-4", "AC-4"},
		{"mp4a", "AAC"},
		{"alac", "Apple Lossless"},
		{"UNKN", "UNKN"},
	}

	for _, tt := range tests {
		t.Run(tt.fourCC, func(t *testing.T) {
			result := mapCodecName(tt.fourCC)
			if result != tt.expected {
				t.Errorf("mapCodecName(%q) = %q, want %q", tt.fourCC, result, tt.expected)
			}
		})
	}
}

func TestAACProfiles(t *testing.T) {
	tests := []struct {
		audioObjectType uint8
		expected        string
		found           bool
	}{
		{2, "AAC-LC", true},
		{5, "HE-AAC", true},
		{29, "HE-AAC v2", true},
		{42, "xHE-AAC", true},
		{99, "", false},
	}

	for _, tt := range tests {
		result, found := aacProfiles[tt.audioObjectType]
		if found != tt.found {
			t.Errorf("aacProfiles[%d] found = %v, want %v", tt.audioObjectType, found, tt.found)
		}
		if found && result != tt.expected {
			t.Errorf("aacProfiles[%d] = %q, want %q", tt.audioObjectType, result, tt.expected)
		}
	}
}
