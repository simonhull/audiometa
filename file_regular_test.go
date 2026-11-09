package audiometa

import (
	"errors"
	"testing"
)

// assertNotRegular checks that err is the UnsupportedFormatError for a path
// that is not a regular file.
func assertNotRegular(t *testing.T, err error) {
	t.Helper()
	var ue *UnsupportedFormatError
	if !errors.As(err, &ue) || ue.Reason != "not a regular file" {
		t.Errorf("err = %v, want an UnsupportedFormatError with Reason %q", err, "not a regular file")
	}
}

func TestOpen_DirectoryIsNotRegular(t *testing.T) {
	dir := t.TempDir()
	_, err := Open(dir)
	assertNotRegular(t, err)
	assertNotRegular(t, Write(dir, func(f *File) error { f.Tags.Title = "x"; return nil }))
}
