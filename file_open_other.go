//go:build !unix

package audiometa

import "os"

// openFlags opens a file for reading. Outside Unix, opening has no FIFO that
// could block; openRegular still refuses anything but a regular file.
const openFlags = os.O_RDONLY
