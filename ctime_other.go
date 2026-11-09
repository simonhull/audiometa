//go:build !linux && !darwin

package audiometa

import (
	"os"
	"time"
)

// changeTime reports false: this platform's FileInfo has no inode change time.
func changeTime(os.FileInfo) (time.Time, bool) { return time.Time{}, false }
