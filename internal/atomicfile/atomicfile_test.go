package atomicfile

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var original = []byte("original contents")

func writeOriginal(t *testing.T, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "file.bin")
	if err := os.WriteFile(path, original, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil { // umask-proof
		t.Fatal(err)
	}
	return path
}

func writeBytes(b []byte) func(io.Writer) error {
	return func(w io.Writer) error {
		_, err := w.Write(b)
		return err
	}
}

func assertNoTemp(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".*.audiometa-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) > 0 {
		t.Errorf("temp files left behind: %v", matches)
	}
}

func assertContent(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s = %q, want %q", path, got, want)
	}
}

func TestReplace_HappyPath(t *testing.T) {
	path := writeOriginal(t, 0o640)
	var verified bool
	err := Replace(path, writeBytes([]byte("new")), Options{
		Verify: func(f *os.File) error {
			buf := make([]byte, 3)
			if _, err := f.ReadAt(buf, 0); err != nil {
				return err
			}
			verified = string(buf) == "new"
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !verified {
		t.Error("Verify did not see the new contents")
	}
	assertContent(t, path, []byte("new"))
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v, want 0640", st.Mode().Perm())
	}
	assertNoTemp(t, filepath.Dir(path))
}

func TestReplace_ErrorsLeaveOriginal(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name  string
		write func(io.Writer) error
		opts  Options
	}{
		{"write error", func(w io.Writer) error { _, _ = w.Write([]byte("partial")); return boom }, Options{}},
		{"verify error", writeBytes([]byte("new")), Options{Verify: func(*os.File) error { return boom }}},
		{"before rename error", writeBytes([]byte("new")), Options{BeforeRename: func() error { return boom }}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := writeOriginal(t, 0o644)
			err := Replace(path, tc.write, tc.opts)
			if !errors.Is(err, boom) {
				t.Fatalf("err = %v, want boom", err)
			}
			assertContent(t, path, original)
			assertNoTemp(t, filepath.Dir(path))
		})
	}
}

func TestReplace_Symlink(t *testing.T) {
	target := writeOriginal(t, 0o644)
	link := filepath.Join(filepath.Dir(target), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := Replace(link, writeBytes([]byte("new")), Options{}); err != nil {
		t.Fatal(err)
	}
	assertContent(t, target, []byte("new"))
	st, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode()&os.ModeSymlink == 0 {
		t.Error("link is no longer a symlink")
	}
}

func TestReplace_PreserveModTime(t *testing.T) {
	path := writeOriginal(t, 0o644)
	mtime := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if err := Replace(path, writeBytes([]byte("new")), Options{PreserveModTime: true}); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !st.ModTime().Equal(mtime) {
		t.Errorf("mtime = %v, want %v", st.ModTime(), mtime)
	}
}

func TestReplace_NewModTimeByDefault(t *testing.T) {
	path := writeOriginal(t, 0o644)
	mtime := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if err := Replace(path, writeBytes([]byte("new")), Options{}); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.ModTime().Equal(mtime) {
		t.Error("mtime unchanged without PreserveModTime")
	}
}

func TestReplace_Directory(t *testing.T) {
	if err := Replace(t.TempDir(), writeBytes(nil), Options{}); err == nil {
		t.Error("want error for a directory")
	}
}

func TestReplace_Missing(t *testing.T) {
	dir := t.TempDir()
	if err := Replace(filepath.Join(dir, "missing"), writeBytes(nil), Options{}); err == nil {
		t.Error("want error for a missing file")
	}
	assertNoTemp(t, dir)
}
