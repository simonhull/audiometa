//go:build unix

package audiometa

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// mkfifo creates a FIFO in a temp dir, or skips the test.
func mkfifo(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "pipe.mp3")
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	return p
}

// noHang runs fn and fails the test if it has not returned after 5 seconds.
// A call stuck opening fifo is released by opening the FIFO's write end.
func noHang(t *testing.T, fifo string, fn func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		if w, err := os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			_ = w.Close()
		}
		_ = <-done
		t.Fatal("blocked opening a FIFO")
		return nil
	}
}

func TestOpen_RefusesNonRegularFiles(t *testing.T) {
	t.Parallel()
	fifo := mkfifo(t)
	link := filepath.Join(t.TempDir(), "link.mp3")
	if err := os.Symlink(fifo, link); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{
		"FIFO":             fifo,
		"symlink to FIFO":  link,
		"character device": os.DevNull,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertNotRegular(t, noHang(t, fifo, func() error {
				f, err := OpenContext(t.Context(), path)
				if err == nil {
					_ = f.Close()
				}
				return err
			}))
			assertNotRegular(t, noHang(t, fifo, func() error {
				_, err := OpenMany(t.Context(), path)
				return err
			}))
			assertNotRegular(t, noHang(t, fifo, func() error {
				return Write(path, func(f *File) error { f.Tags.Title = "x"; return nil })
			}))
		})
	}
}

// TestOpenFlags_DoNotBlockOnFIFO covers a path that becomes a FIFO between
// the stat and the open: the open itself must return at once.
func TestOpenFlags_DoNotBlockOnFIFO(t *testing.T) {
	t.Parallel()
	fifo := mkfifo(t)
	err := noHang(t, fifo, func() error {
		f, err := os.OpenFile(fifo, openFlags, 0)
		if err == nil {
			_ = f.Close()
		}
		return err //nolint:wrapcheck // test helper returns the error for the test to report
	})
	if err != nil {
		t.Errorf("open with openFlags: %v", err)
	}
}
