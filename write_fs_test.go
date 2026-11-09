package audiometa

// Tests in this file swap the package-level lookupWriter (useWriter), so none
// of them may call t.Parallel.

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func TestWrite_ReadOnlyDirReturnsPathError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs Unix permissions and a non-root user")
	}
	useWriter(t, &fakeWriter{})
	path, data := copySample(t, sampleFLAC)
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	err := Write(path, func(f *File) error { f.Tags.Title = "New"; return nil })
	if _, ok := errors.AsType[*os.PathError](err); !ok || !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("err = %v, want an *os.PathError for permission", err)
	}
	assertFileBytes(t, path, data)
	assertNoTempFiles(t, dir)
}

func TestWrite_Symlinks(t *testing.T) {
	edit := func(f *File) error { f.Tags.Title = "New"; return nil }
	symlink := func(t *testing.T, target, link string) {
		t.Helper()
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	t.Run("relative link in another directory", func(t *testing.T) {
		useWriter(t, &fakeWriter{})
		path, data := copySample(t, sampleFLAC)
		linkDir := t.TempDir()
		rel, err := filepath.Rel(linkDir, path)
		if err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(linkDir, "link.flac")
		symlink(t, rel, link)
		if err := Write(link, edit); err != nil {
			t.Fatal(err)
		}
		assertFileBytes(t, path, append(slices.Clone(data), fakeTrailer...))
		if st, err := os.Lstat(link); err != nil || st.Mode()&os.ModeSymlink == 0 {
			t.Error("the symlink was replaced")
		}
		assertNoTempFiles(t, filepath.Dir(path))
		assertNoTempFiles(t, linkDir)
	})
	t.Run("chain", func(t *testing.T) {
		useWriter(t, &fakeWriter{})
		path, data := copySample(t, sampleFLAC)
		dir := filepath.Dir(path)
		first, second := filepath.Join(dir, "first.flac"), filepath.Join(dir, "second.flac")
		symlink(t, path, first)
		symlink(t, first, second)
		if err := Write(second, edit); err != nil {
			t.Fatal(err)
		}
		assertFileBytes(t, path, append(slices.Clone(data), fakeTrailer...))
		for _, l := range []string{first, second} {
			if st, err := os.Lstat(l); err != nil || st.Mode()&os.ModeSymlink == 0 {
				t.Errorf("%s is no longer a symlink", l)
			}
		}
	})
	t.Run("dangling", func(t *testing.T) {
		useWriter(t, &fakeWriter{})
		dir := t.TempDir()
		missing, link := filepath.Join(dir, "missing.flac"), filepath.Join(dir, "link.flac")
		symlink(t, missing, link)
		if err := Write(link, edit); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("err = %v, want fs.ErrNotExist", err)
		}
		if _, err := os.Lstat(missing); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("Write created the link target: %v", err)
		}
		assertNoTempFiles(t, dir)
	})
}
