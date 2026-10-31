package types

import "testing"

func TestArtworkTypeFromPictureType(t *testing.T) {
	tests := []struct {
		code uint32
		want ArtworkType
	}{
		{0, ArtworkOther},
		{3, ArtworkFrontCover},
		{4, ArtworkBackCover},
		{20, ArtworkPublisherLogotype},
		{21, ArtworkOther},
		{0xFFFFFFFF, ArtworkOther},
	}
	for _, tt := range tests {
		if got := ArtworkTypeFromPictureType(tt.code); got != tt.want {
			t.Errorf("ArtworkTypeFromPictureType(%d) = %v, want %v", tt.code, got, tt.want)
		}
	}
}
