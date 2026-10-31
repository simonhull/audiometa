//go:build unix

package atomicfile

import (
	"os"
	"syscall"
)

// copyOwner gives f the owner and group recorded in st. It is best-effort:
// an unprivileged process usually cannot change the owner, and the error is
// ignored.
func copyOwner(f *os.File, st os.FileInfo) {
	if sys, ok := st.Sys().(*syscall.Stat_t); ok {
		_ = f.Chown(int(sys.Uid), int(sys.Gid))
	}
}

// syncDir fsyncs the directory so a completed rename survives a crash.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	syncErr := d.Sync()
	closeErr := d.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}
