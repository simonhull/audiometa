package audiometa

import "testing"

// An MP3 without an ID3v2 tag is valid: strict parsing must accept it.
func TestOpen_StrictAcceptsMP3WithoutID3v2(t *testing.T) {
	_, data := copySample(t, "testdata/sample_v24.mp3") // ID3v2.4 tag, audio, ID3v1 trailer
	tagLen := 10 + (int(data[6])<<21 | int(data[7])<<14 | int(data[8])<<7 | int(data[9]))
	path := writeTemp(t, "v1only.mp3", data[tagLen:])
	f, err := Open(path, WithStrictParsing())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()
	if f.Tags.Title != "Test Title" {
		t.Errorf("Title = %q, want the ID3v1 title %q", f.Tags.Title, "Test Title")
	}
}
