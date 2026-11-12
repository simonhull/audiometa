package atomicfile

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
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
	t.Parallel()
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
	if runtime.GOOS != "windows" { // Windows maps only the read-only bit
		if st.Mode().Perm() != 0o640 {
			t.Errorf("mode = %v, want 0640", st.Mode().Perm())
		}
	}
	assertNoTemp(t, filepath.Dir(path))
}

func TestReplace_ErrorsLeaveOriginal(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	if err := Replace(t.TempDir(), writeBytes(nil), Options{}); err == nil {
		t.Error("want error for a directory")
	}
}

func TestReplace_Missing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := Replace(filepath.Join(dir, "missing"), writeBytes(nil), Options{}); err == nil {
		t.Error("want error for a missing file")
	}
	assertNoTemp(t, dir)
}

func TestTempPattern(t *testing.T) {
	t.Parallel()
	for name, base := range map[string]string{
		"short":           "song.mp3",
		"exactly the cap": strings.Repeat("a", maxTempBase),
		"long ASCII":      strings.Repeat("a", 250),
		"long multibyte":  strings.Repeat("日", 84),                           // 252 bytes, 3 per rune
		"long mixed":      strings.Repeat("é", 49) + strings.Repeat("日", 30), // 188 bytes; byte 100 is mid-rune
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := tempPattern(filepath.Join("dir", base))
			if !strings.HasPrefix(p, ".") || !strings.HasSuffix(p, ".audiometa-*.tmp") {
				t.Fatalf("pattern %q lacks the . prefix or .audiometa-*.tmp suffix", p)
			}
			kept := strings.TrimSuffix(strings.TrimPrefix(p, "."), ".audiometa-*.tmp")
			if !strings.HasPrefix(base, kept) || len(kept) > maxTempBase || !utf8.ValidString(kept) {
				t.Errorf("kept %q (%d bytes): want a valid UTF-8 prefix of the name, at most %d bytes", kept, len(kept), maxTempBase)
			}
			if len(base) <= maxTempBase && kept != base {
				t.Errorf("kept %q, want the whole name %q", kept, base)
			}
			if len(base) > maxTempBase && len(kept) < maxTempBase-3 {
				t.Errorf("kept %d bytes, want the longest prefix up to %d", len(kept), maxTempBase)
			}
		})
	}
}

func TestReplace_LongName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, strings.Repeat("a", 246)+".mp3") // 250 bytes
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Skipf("file system rejects a 250-byte name: %v", err)
	}
	if err := Replace(path, writeBytes([]byte("new")), Options{}); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	assertContent(t, path, []byte("new"))
	assertNoTemp(t, dir)
}

// skipUnlessUnixUser skips tests that rely on Unix permission bits, which
// Windows ignores for directories and root bypasses.
func skipUnlessUnixUser(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs Unix permissions and a non-root user")
	}
}

func TestReplace_CreateTempFails(t *testing.T) {
	t.Parallel()
	skipUnlessUnixUser(t)
	path := writeOriginal(t, 0o644)
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	err := Replace(path, writeBytes([]byte("new")), Options{})
	if _, ok := errors.AsType[*os.PathError](err); !ok || !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("err = %v, want an *os.PathError for permission", err)
	}
	assertContent(t, path, original)
	assertNoTemp(t, dir)
}

// TestReplace_RenameFails puts a non-empty directory where the target was, so
// the rename fails after the temp file is complete.
func TestReplace_RenameFails(t *testing.T) {
	t.Parallel()
	path := writeOriginal(t, 0o644)
	err := Replace(path, writeBytes([]byte("new")), Options{BeforeRename: func() error {
		if err := os.Remove(path); err != nil {
			return err //nolint:wrapcheck // test helper returns the error for the test to report
		}
		if err := os.Mkdir(path, 0o755); err != nil {
			return err //nolint:wrapcheck // test helper returns the error for the test to report
		}
		return os.WriteFile(filepath.Join(path, "x"), nil, 0o644)
	}})
	if _, ok := errors.AsType[*os.LinkError](err); !ok {
		t.Fatalf("err = %v, want an *os.LinkError", err)
	}
	assertNoTemp(t, filepath.Dir(path))
}

// TestReplace_SyncDirFails: the new contents are in place, and the error says
// the directory sync failed.
func TestReplace_SyncDirFails(t *testing.T) {
	boom := errors.New("boom")
	prev := syncDirectory
	syncDirectory = func(string) error { return boom }
	t.Cleanup(func() { syncDirectory = prev })
	path := writeOriginal(t, 0o644)
	err := Replace(path, writeBytes([]byte("new")), Options{})
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "sync directory") {
		t.Fatalf("err = %v, want the sync error", err)
	}
	assertContent(t, path, []byte("new"))
	assertNoTemp(t, filepath.Dir(path))
}

// TestReplace_RemoveTempFails: when the temp file cannot be removed after an
// error, both errors are reported and the original is untouched.
func TestReplace_RemoveTempFails(t *testing.T) {
	t.Parallel()
	skipUnlessUnixUser(t)
	path := writeOriginal(t, 0o644)
	dir := filepath.Dir(path)
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) }) // lets TempDir remove the leftover temp file
	boom := errors.New("boom")
	err := Replace(path, func(io.Writer) error {
		if err := os.Chmod(dir, 0o555); err != nil {
			return err //nolint:wrapcheck // test helper returns the error for the test to report
		}
		return boom
	}, Options{})
	if !errors.Is(err, boom) || !errors.Is(err, fs.ErrPermission) || !strings.Contains(err.Error(), "remove temp file") {
		t.Fatalf("err = %v, want boom joined with the removal error", err)
	}
	assertContent(t, path, original)
}

// TestReplace_PreserveModTimeFails removes the temp file before seal sets its
// mtime.
func TestReplace_PreserveModTimeFails(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot remove an open file")
	}
	path := writeOriginal(t, 0o644)
	err := Replace(path, writeBytes([]byte("new")), Options{
		PreserveModTime: true,
		Verify:          func(tmp *os.File) error { return os.Remove(tmp.Name()) },
	})
	if !errors.Is(err, fs.ErrNotExist) || !strings.Contains(err.Error(), "preserve mtime") {
		t.Fatalf("err = %v, want the mtime error", err)
	}
	assertContent(t, path, original)
	assertNoTemp(t, filepath.Dir(path))
}
