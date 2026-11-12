package flac

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

// vcHead returns a VORBIS_COMMENT payload start: an empty vendor and a
// comment count, with no comments.
func vcHead(count uint32) []byte {
	b := binary.LittleEndian.AppendUint32(nil, 0)
	return binary.LittleEndian.AppendUint32(b, count)
}

func TestParse_CommentPastBlockIsNotRead(t *testing.T) {
	leaked := "TITLE=Leaked"
	payload := binary.LittleEndian.AppendUint32(vcHead(1), uint32(len(leaked)))
	data := buildFLAC(
		flacBlock(false, blockTypeStreamInfo, streamInfoPayload(44100, 44100)),
		flacBlock(true, blockTypeVorbisComment, payload), // the comment's bytes are not in the block
		[]byte(leaked), // audio that happens to look like the comment
	)
	file := parseTestFLAC(t, data)
	if file.Tags.Title != "" {
		t.Errorf("Title = %q, read from past the end of the block", file.Tags.Title)
	}
	if len(file.Warnings) != 1 || !strings.Contains(file.Warnings[0].Message, "runs past the end of the data") {
		t.Errorf("warnings = %+v, want one saying the length runs past the end of the data", file.Warnings)
	}
}

func TestParse_CommentsBeforeACutAreKept(t *testing.T) {
	payload := vcHead(2)
	payload = binary.LittleEndian.AppendUint32(payload, uint32(len("TITLE=Kept")))
	payload = append(payload, "TITLE=Kept"...)
	payload = binary.LittleEndian.AppendUint32(payload, 100) // second comment: no bytes
	data := buildFLAC(
		flacBlock(false, blockTypeStreamInfo, streamInfoPayload(44100, 44100)),
		flacBlock(true, blockTypeVorbisComment, payload),
	)
	file := parseTestFLAC(t, data)
	if file.Tags.Title != "Kept" {
		t.Errorf("Title = %q, want Kept", file.Tags.Title)
	}
	if len(file.Warnings) != 1 || file.Warnings[0].Stage != types.StageMetadata {
		t.Errorf("warnings = %+v, want one StageMetadata warning", file.Warnings)
	}
}

func TestParse_EveryVorbisCommentBlockIsRead(t *testing.T) {
	data := buildFLAC(
		flacBlock(false, blockTypeStreamInfo, streamInfoPayload(44100, 44100)),
		flacBlock(false, blockTypeVorbisComment, vorbisCommentPayload("TITLE=One")),
		flacBlock(true, blockTypeVorbisComment, vorbisCommentPayload("ARTIST=Two")),
	)
	file := parseTestFLAC(t, data)
	if file.Tags.Title != "One" || file.Tags.Artist != "Two" || len(file.Warnings) != 0 {
		t.Errorf("Title %q, Artist %q, warnings %+v; want One, Two, none", file.Tags.Title, file.Tags.Artist, file.Warnings)
	}
}

func TestParse_CommentCountPastBlockDoesNotAllocate(t *testing.T) {
	// 400,000 zero bytes after the last block would be 100,000 empty comments
	// if decoding ran past the block.
	audio := make([]byte, 400_000)
	build := func(count uint32) []byte {
		return buildFLAC(
			flacBlock(false, blockTypeStreamInfo, streamInfoPayload(44100, 44100)),
			flacBlock(true, blockTypeVorbisComment, vcHead(count)),
			audio,
		)
	}
	allocs := func(data []byte) float64 {
		return testing.AllocsPerRun(3, func() { parseTestFLAC(t, data) })
	}
	base, got := allocs(build(0)), allocs(build(100_000))
	if got > base+20 {
		t.Errorf("allocs = %.0f, want at most %.0f (a file without comments + 20)", got, base+20)
	}
}

func TestExtractArtwork_PictureDataPastBlockIsNotRead(t *testing.T) {
	// Each PICTURE block is 32 bytes but claims 64 KiB of picture data, which
	// lies in the blocks after it and the audio.
	const claimed = 64 << 10
	pic := make([]byte, 32) // type, MIME length 0, description length 0, 4 x u32, data length
	binary.BigEndian.PutUint32(pic[0:], 3)
	binary.BigEndian.PutUint32(pic[28:], claimed)
	parts := make([][]byte, 1, 202)
	parts[0] = flacBlock(false, blockTypeStreamInfo, streamInfoPayload(44100, 44100))
	for i := range 200 {
		parts = append(parts, flacBlock(i == 199, blockTypePicture, pic))
	}
	parts = append(parts, make([]byte, claimed))
	if art := extractTestArtwork(t, buildFLAC(parts...)); len(art) != 0 {
		t.Errorf("got %d artworks of %d bytes, want 0", len(art), len(art[0].Data))
	}
}
