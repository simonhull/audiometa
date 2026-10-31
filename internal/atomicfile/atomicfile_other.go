//go:build !unix

package atomicfile

import "os"

// copyOwner is a no-op: ownership is not carried over outside Unix.
func copyOwner(*os.File, os.FileInfo) {}

// syncDir is a no-op: directories cannot be fsynced outside Unix.
func syncDir(string) error { return nil }
