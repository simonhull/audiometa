//go:build darwin

package audiometa

import (
	"os"
	"syscall"
	"time"
)

// changeTime returns fi's inode change time and true, or false when fi does
// not carry one.
func changeTime(fi os.FileInfo) (time.Time, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return time.Time{}, false
	}
	return time.Unix(st.Ctimespec.Unix()), true
}
