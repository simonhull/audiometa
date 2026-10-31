// Package atomicfile replaces a file's contents crash-safely: write a temp file in
// the same directory, fsync it, verify it, rename it over the original, fsync the
// directory. The original is never opened for writing.
//
// Windows notes: os.Rename maps to MoveFileEx(MOVEFILE_REPLACE_EXISTING), which fails
// with a sharing violation while another process holds the target open without
// FILE_SHARE_DELETE (and Go's os.Open does not request it); callers must close their
// own handles before Replace renames. Directory fsync is not available and is skipped.
// Only the read-only attribute maps from Unix mode bits; ACLs are not copied.
package atomicfile

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// bufSize is the write buffer in front of the temp file.
const bufSize = 1 << 20

// Options tunes Replace.
type Options struct {
	// PreserveModTime sets the new file's mtime to the original's.
	PreserveModTime bool
	// Verify runs after the temp file is synced and before the rename; it may
	// ReadAt the temp file. An error aborts the replacement.
	Verify func(*os.File) error
	// BeforeRename is the last chance to abort (concurrent-modification
	// check, closing handles). It runs after the temp file is closed.
	BeforeRename func() error
}

// Replace atomically replaces the regular file at path (symlinks are resolved and
// the target is replaced; the link is kept) with the bytes produced by write.
// On any error the temp file is removed and the original is untouched.
//
// The new file gets the original's permission bits and, on Unix, best-effort
// its owner and group. The returned error may report a failure to sync the
// directory after the rename; the new contents are then in place but their
// durability across a crash is not guaranteed.
func Replace(path string, write func(io.Writer) error, opts Options) (err error) {
	target, st, err := resolveRegular(path)
	if err != nil {
		return err
	}

	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(target)+".audiometa-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tmp.Close()
			if rmErr := os.Remove(tmp.Name()); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
				err = errors.Join(err, fmt.Errorf("remove temp file: %w", rmErr))
			}
		}
	}()

	if err := fill(tmp, st, write, opts.Verify); err != nil {
		return err
	}
	if err := seal(tmp, st, opts); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	committed = true
	if err := syncDir(dir); err != nil {
		return fmt.Errorf("sync directory %s: %w", dir, err)
	}
	return nil
}

// resolveRegular follows symlinks in path and returns the target, which must
// be an existing regular file, with its FileInfo.
func resolveRegular(path string) (string, os.FileInfo, error) {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", nil, fmt.Errorf("resolve %s: %w", path, err)
	}
	st, err := os.Stat(target)
	if err != nil {
		return "", nil, fmt.Errorf("stat %s: %w", target, err)
	}
	if !st.Mode().IsRegular() {
		return "", nil, fmt.Errorf("replace %s: not a regular file", target)
	}
	return target, st, nil
}

// seal applies PreserveModTime, closes tmp and runs BeforeRename.
func seal(tmp *os.File, st os.FileInfo, opts Options) error {
	if opts.PreserveModTime {
		if err := os.Chtimes(tmp.Name(), time.Time{}, st.ModTime()); err != nil {
			return fmt.Errorf("preserve mtime: %w", err)
		}
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if opts.BeforeRename != nil {
		return opts.BeforeRename()
	}
	return nil
}

// fill copies st's permissions and owner to tmp, writes the new contents,
// syncs them and runs verify.
func fill(tmp *os.File, st os.FileInfo, write func(io.Writer) error, verify func(*os.File) error) error {
	if err := tmp.Chmod(st.Mode().Perm()); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	copyOwner(tmp, st)

	bw := bufio.NewWriterSize(tmp, bufSize)
	if err := write(bw); err != nil {
		return err
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync temp file: %w", err)
	}
	if verify != nil {
		if err := verify(tmp); err != nil {
			return err
		}
	}
	return nil
}
