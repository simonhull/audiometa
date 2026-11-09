package m4a

import (
	"errors"
	"testing"
	"time"

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
