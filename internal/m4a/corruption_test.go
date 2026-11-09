package m4a

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	audiobinary "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// corruptAtom is an atom whose size field (4) is smaller than its own header.
var corruptAtom = []byte{0, 0, 0, 4, 'f', 'r', 'e', 'e'}

// wantOneWarning fails unless file carries exactly one warning, of stage.
func wantOneWarning(t *testing.T, file *types.File, stage string) types.Warning {
	t.Helper()
	if len(file.Warnings) != 1 || file.Warnings[0].Stage != stage {
		t.Fatalf("warnings = %+v, want exactly one of stage %v", file.Warnings, stage)
	}
	return file.Warnings[0]
}

func TestParse_CorruptMovieHeaderKeepsTrackInfo(t *testing.T) {
	badMvhd := createMockAtom("mvhd", []byte{0, 0, 0, 0, 0, 0})
	audio := trakWith("soun", stsdFor("mp4a", sampleEntryV01(0, 2, 44100)))
	moov := createMockAtom("moov", append(badMvhd, audio...))

	file := parseBytes(t, append(m4bFtyp(), moov...))

	if file.Audio.Duration != 0 {
		t.Errorf("duration = %v, want 0", file.Audio.Duration)
	}
	if file.Audio.Codec != "mp4a" || file.Audio.SampleRate != 44100 {
		t.Errorf("codec %q rate %d, want mp4a 44100", file.Audio.Codec, file.Audio.SampleRate)
	}
	wantOneWarning(t, file, types.StageTechnical)
}

func TestParse_CorruptSampleTableKeepsDuration(t *testing.T) {
	moov := createMockAtom("moov", append(createMvhdAtom(0, 1000, 10000), trakWithStbl("soun", corruptAtom)...))

	file := parseBytes(t, append(m4bFtyp(), moov...))

	if file.Audio.Duration != 10*time.Second {
		t.Errorf("duration = %v, want 10s", file.Audio.Duration)
	}
	if file.Audio.Codec != "" {
		t.Errorf("codec = %q, want none", file.Audio.Codec)
	}
	w := wantOneWarning(t, file, types.StageTechnical)
	if !errors.As(w.Err, new(*types.CorruptedFileError)) {
		t.Errorf("warning error = %v, want a *types.CorruptedFileError", w.Err)
	}
}

func TestParse_AbsentTechnicalAtomsAreSilent(t *testing.T) {
	file := parseBytes(t, append(m4bFtyp(), createMockAtom("moov", nil)...))
	if len(file.Warnings) != 0 {
		t.Errorf("warnings = %+v, want none", file.Warnings)
	}
}

// ilstFile wraps ilst children in a minimal M4B: ftyp, then moov, udta, meta, ilst.
func ilstFile(children ...[]byte) []byte {
	ilst := createMockAtom("ilst", bytes.Join(children, nil))
	meta := fullAtom("meta", ilst)
	return append(m4bFtyp(), createMockAtom("moov", createMockAtom("udta", meta))...)
}

// extractArtworkFrom runs extractArtwork over data.
func extractArtworkFrom(t *testing.T, data []byte) ([]types.Artwork, error) {
	t.Helper()
	sr := audiobinary.NewSafeReader(bytes.NewReader(data), int64(len(data)), "t.m4b")
	return extractArtwork(t.Context(), sr, int64(len(data)))
}

func coverData(image []byte) []byte {
	return createMockAtom("data", append(u32be(0x0D, 0), image...))
}

func TestExtractArtwork_CorruptPathIsAnError(t *testing.T) {
	moov := createMockAtom("moov", createMockAtom("udta", corruptAtom))
	_, err := extractArtworkFrom(t, append(m4bFtyp(), moov...))
	if !errors.As(err, new(*types.CorruptedFileError)) {
		t.Errorf("err = %v, want a *types.CorruptedFileError", err)
	}
}

func TestExtractArtwork_AbsentCovrIsNotAnError(t *testing.T) {
	art, err := extractArtworkFrom(t, ilstFile(createMetadataItem([]byte("\xa9nam"), "Title")))
	if err != nil || art != nil {
		t.Errorf("artwork = %v, err = %v, want nil, nil", art, err)
	}
}

func TestExtractArtwork_CorruptCovrChildKeepsEarlierImages(t *testing.T) {
	covr := createMockAtom("covr", append(coverData([]byte("\xff\xd8\xff\xd9")), corruptAtom...))
	art, err := extractArtworkFrom(t, ilstFile(covr))
	if err != nil {
		t.Fatalf("err = %v, want none", err)
	}
	if len(art) != 1 {
		t.Errorf("got %d images, want 1", len(art))
	}
}

func TestIlstItem_CorruptSecondValueWarnsAndKeepsFirst(t *testing.T) {
	item := createMockAtom("\xa9nam", append(createDataAtom("First"), corruptAtom...))
	file := runIlst(t, item)
	if file.Tags.Title != "First" {
		t.Errorf("Title = %q, want First", file.Tags.Title)
	}
	wantOneWarning(t, file, types.StageMetadata)
}

func TestParse_UnreadableEsdsWarns(t *testing.T) {
	entry := mp4aSampleEntry(esdsPayload(0, []byte{0x12, 0x10}))
	binary.BigEndian.PutUint32(entry, 1000) // the entry claims to run past the end of the file
	stsd := fullAtom("stsd", append(u32be(1), entry...))
	moov := createMockAtom("moov", append(createMvhdAtom(0, 1000, 10000), trakWithStbl("soun", stsd)...))

	file := parseBytes(t, append(m4bFtyp(), moov...))

	if file.Audio.Codec != "mp4a" {
		t.Errorf("codec = %q, want mp4a", file.Audio.Codec)
	}
	wantOneWarning(t, file, types.StageTechnical)
}

func TestParseAudiobookTags_CorruptSiblingAfterFreeformIsReported(t *testing.T) {
	ilst := createMockAtom("ilst", append(createCustomAtom("com.apple.iTunes", "Narrator", "Wil Wheaton"), corruptAtom...))
	sr := audiobinary.NewSafeReader(bytes.NewReader(ilst), int64(len(ilst)), "t.m4b")
	ilstAtom, err := readAtomHeader(sr, 0, int64(len(ilst)))
	if err != nil {
		t.Fatal(err)
	}
	file := &types.File{}
	err = parseAudiobookTags(t.Context(), sr, ilstAtom, file)
	if !errors.As(err, new(*types.CorruptedFileError)) {
		t.Errorf("err = %v, want a *types.CorruptedFileError", err)
	}
	if file.Tags.Narrator != "Wil Wheaton" {
		t.Errorf("Narrator = %q, want Wil Wheaton", file.Tags.Narrator)
	}
}

func TestParseAudiobookTags_CorruptChildOfFreeformIsReported(t *testing.T) {
	freeform := createMockAtom("----", append(createCustomAtom("com.apple.iTunes", "Narrator", "Wil Wheaton")[8:], corruptAtom...))
	ilst := createMockAtom("ilst", freeform)
	sr := audiobinary.NewSafeReader(bytes.NewReader(ilst), int64(len(ilst)), "t.m4b")
	ilstAtom, err := readAtomHeader(sr, 0, int64(len(ilst)))
	if err != nil {
		t.Fatal(err)
	}
	file := &types.File{}
	err = parseAudiobookTags(t.Context(), sr, ilstAtom, file)
	if !errors.As(err, new(*types.CorruptedFileError)) {
		t.Errorf("err = %v, want a *types.CorruptedFileError", err)
	}
	if file.Tags.Narrator != "Wil Wheaton" {
		t.Errorf("Narrator = %q, want Wil Wheaton", file.Tags.Narrator)
	}
}
