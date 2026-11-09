//go:build unix

package audiometa

import (
	"os"
	"syscall"
)

// openFlags opens a file for reading without blocking on a FIFO that has no
// writer (or a device that would wait); openRegular refuses such files right
// after. O_NONBLOCK does not change reads of a regular file.
const openFlags = os.O_RDONLY | syscall.O_NONBLOCK
