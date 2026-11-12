package mp3

import (
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

func TestParseAPICFrame_LinkPictureKeepsItsMarker(t *testing.T) {
	t.Parallel()
	art, err := parseAPICFrame(apicPayload("-->", 3, "", []byte("http://example.com/a.jpg")))
	if err != nil {
		t.Fatalf("parseAPICFrame: %v", err)
	}
	if art.MIMEType != "-->" {
		t.Errorf("MIMEType = %q, want %q", art.MIMEType, "-->")
	}
}

func TestParsePICFrame_LinkPictureKeepsItsMarker(t *testing.T) {
	t.Parallel()
	payload := append([]byte{0, '-', '-', '>', 3, 0}, "http://example.com/a.jpg"...)
	art, err := parsePICFrame(payload)
	if err != nil {
		t.Fatalf("parsePICFrame: %v", err)
	}
	if art.MIMEType != "-->" {
		t.Errorf("MIMEType = %q, want %q", art.MIMEType, "-->")
	}
}

func TestParseAPICFrame_UnknownPictureTypeIsOther(t *testing.T) {
	t.Parallel()
	art, err := parseAPICFrame(apicPayload("image/jpeg", 0xFF, "", pictureImage))
	if err != nil {
		t.Fatalf("parseAPICFrame: %v", err)
	}
	if art.Type != types.ArtworkOther {
		t.Errorf("Type = %d, want ArtworkOther", art.Type)
	}
}
