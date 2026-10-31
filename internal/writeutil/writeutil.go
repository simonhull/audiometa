// Package writeutil holds helpers shared by format writers.
package writeutil

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
)

// chunkSize is the copy buffer size for CopyRange.
const chunkSize = 1 << 20

// CopyRange copies n bytes of src, starting at off, to w in 1 MiB chunks. It
// checks ctx before every chunk and returns ctx.Err() once ctx is done. A
// source shorter than off+n returns an error wrapping io.ErrUnexpectedEOF. It
// returns the number of bytes written.
func CopyRange(ctx context.Context, w io.Writer, src io.ReaderAt, off, n int64) (int64, error) {
	if off < 0 || n < 0 {
		return 0, fmt.Errorf("copy range: invalid range off=%d n=%d", off, n)
	}
	buf := make([]byte, min(n, chunkSize))
	var written int64
	for written < n {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		chunk := buf[:min(n-written, chunkSize)]
		read, err := src.ReadAt(chunk, off+written)
		if read < len(chunk) {
			if err == nil || errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF
			}
			return written, fmt.Errorf("copy range at %d: %w", off+written, err)
		}
		m, err := w.Write(chunk)
		written += int64(m)
		if err != nil {
			return written, err
		}
		if m != len(chunk) {
			return written, io.ErrShortWrite
		}
	}
	return written, nil
}

// DigestRange returns the SHA-256 of n bytes of src starting at off.
func DigestRange(ctx context.Context, src io.ReaderAt, off, n int64) ([]byte, error) {
	h := sha256.New()
	if _, err := CopyRange(ctx, h, src, off, n); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// CountingWriter wraps W and counts the bytes written through it in N.
type CountingWriter struct {
	W io.Writer
	N int64
}

// Write writes p to the underlying writer and adds the bytes written to N.
func (c *CountingWriter) Write(p []byte) (int, error) {
	n, err := c.W.Write(p)
	c.N += int64(n)
	return n, err
}
