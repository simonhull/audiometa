package flac

import (
	"bytes"
	"context"
	"encoding/binary"
	"slices"
	"strings"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

// picturePayload returns a FLAC PICTURE block body.
func picturePayload(picType uint32, mime string, data []byte) []byte {
	buf := &bytes.Buffer{}
	for _, v := range []uint32{picType, uint32(len(mime))} {
		_ = binary.Write(buf, binary.BigEndian, v)
	}
	buf.WriteString(mime)
	// description length, width, height, color depth, colors used, data length
	for _, v := range []uint32{0, 1, 1, 24, 0, uint32(len(data))} {
		_ = binary.Write(buf, binary.BigEndian, v)
	}
	buf.Write(data)
	return buf.Bytes()
}

func extractTestArtwork(t *testing.T, data []byte) []types.Artwork {
	t.Helper()
	art, err := (&parser{}).ExtractArtwork(context.Background(), bytes.NewReader(data), int64(len(data)), "t.flac")
	if err != nil {
		t.Fatal(err)
	}
	return art
}

func TestExtractArtwork_BadLastPictureStopsScan(t *testing.T) {
	bad := make([]byte, 8)
	binary.BigEndian.PutUint32(bad[0:], 3)
	binary.BigEndian.PutUint32(bad[4:], 4096) // MIME length runs past EOF, so picture.Decode fails
	data := buildFLAC(
		flacBlock(false, blockTypeStreamInfo, streamInfoPayload(44100)),
		flacBlock(true, blockTypePicture, bad),
		// "Audio" that happens to look like a valid PICTURE block:
		flacBlock(true, blockTypePicture, picturePayload(3, "image/png", []byte{1, 2, 3})),
	)
	if art := extractTestArtwork(t, data); len(art) != 0 {
		t.Errorf("got %d artworks, want 0 (scan must stop at the last metadata block)", len(art))
	}
}

func TestExtractArtwork_AllPictureTypes(t *testing.T) {
	data := buildFLAC(
		flacBlock(false, blockTypeStreamInfo, streamInfoPayload(44100)),
		flacBlock(false, blockTypePicture, picturePayload(3, "image/png", []byte{1})),
		flacBlock(true, blockTypePicture, picturePayload(8, "image/jpeg", []byte{2})),
	)
	art := extractTestArtwork(t, data)
	if len(art) != 2 {
		t.Fatalf("got %d artworks, want 2", len(art))
	}
	if art[0].Type != types.ArtworkFrontCover || art[1].Type != types.ArtworkArtist {
		t.Errorf("types = %v, %v", art[0].Type, art[1].Type)
	}
}

func TestParse_BitrateExcludesMetadata(t *testing.T) {
	data := buildFLAC(
		flacBlock(false, blockTypeStreamInfo, streamInfoPayload(441000)), // 10 s
		flacBlock(false, blockTypePadding, make([]byte, 50000)),
		flacBlock(true, blockTypeVorbisComment, vorbisCommentPayload("TITLE=x")),
		make([]byte, 10000), // "audio"
	)
	file := parseTestFLAC(t, data)
	if file.Audio.Bitrate != 8000 {
		t.Errorf("Bitrate = %d, want 8000", file.Audio.Bitrate)
	}
}

func TestParse_BitrateExcludesTrailingTags(t *testing.T) {
	ape := func() []byte {
		part := func(flags uint32) []byte {
			b := make([]byte, 32)
			copy(b, "APETAGEX")
			binary.LittleEndian.PutUint32(b[8:], 2000)
			binary.LittleEndian.PutUint32(b[12:], 3000+32)
			binary.LittleEndian.PutUint32(b[20:], flags)
			return b
		}
		return slices.Concat(part(1<<31|1<<29), make([]byte, 3000), part(1<<31))
	}()
	id3v1 := make([]byte, 128)
	copy(id3v1, "TAG")
	data := buildFLAC(
		flacBlock(false, blockTypeStreamInfo, streamInfoPayload(441000)), // 10 s
		flacBlock(false, blockTypePadding, make([]byte, 50000)),
		flacBlock(true, blockTypeVorbisComment, vorbisCommentPayload("TITLE=x")),
		make([]byte, 10000), // "audio"
		ape,
		id3v1,
	)
	file := parseTestFLAC(t, data)
	if file.Audio.Bitrate != 8000 {
		t.Errorf("Bitrate = %d, want 8000 (trailing APEv2 and ID3v1 are not audio)", file.Audio.Bitrate)
	}
}

func TestParse_UnknownTotalSamplesWarns(t *testing.T) {
	data := buildFLAC(flacBlock(true, blockTypeStreamInfo, streamInfoPayload(0)))
	file := parseTestFLAC(t, data)
	if file.Audio.Duration != 0 {
		t.Errorf("Duration = %v, want 0", file.Audio.Duration)
	}
	if len(file.Warnings) != 1 {
		t.Fatalf("warnings = %+v, want exactly 1", file.Warnings)
	}
	w := file.Warnings[0]
	if w.Stage != types.StageTechnical || !strings.Contains(w.Message, "total samples") {
		t.Errorf("warning = %+v", w)
	}

	known := createMinimalFLAC("T", "A", "B")
	if f := parseTestFLAC(t, known); len(f.Warnings) != 0 {
		t.Errorf("known-duration fixture warned: %+v", f.Warnings)
	}
}
