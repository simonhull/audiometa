package types

import (
	"errors"
	"testing"
)

func TestWarn(t *testing.T) {
	t.Parallel()

	cause := errors.New("cause")
	var f File
	Warn(&f, StageArtwork, 42, cause, "bad picture")

	want := Warning{Stage: StageArtwork, Message: "bad picture", Err: cause, Offset: 42}
	if len(f.Warnings) != 1 || f.Warnings[0] != want {
		t.Errorf("Warnings = %+v, want [%+v]", f.Warnings, want)
	}
}

func TestWarn_Cap(t *testing.T) {
	t.Parallel()

	var f File
	for range MaxWarnings + 5 {
		Warn(&f, StageMetadata, 0, nil, "x")
	}
	if len(f.Warnings) != MaxWarnings+1 {
		t.Fatalf("len(Warnings) = %d, want %d", len(f.Warnings), MaxWarnings+1)
	}
	if last := f.Warnings[MaxWarnings]; last.Message != "further warnings suppressed after 100" {
		t.Errorf("last warning = %q, want the suppression note", last.Message)
	}
}
