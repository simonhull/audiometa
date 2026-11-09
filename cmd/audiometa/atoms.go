package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/simonhull/audiometa"
	"github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/m4a"
)

// maxAtomDepth bounds recursion on hostile input.
const maxAtomDepth = 32

// runAtoms implements "audiometa atoms".
func runAtoms(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("atoms", "FILE...", stderr)
	if code, ok := parse(fs, args); !ok {
		return code
	}
	if !requireFiles(fs, stderr) {
		return exitUsage
	}
	out := &printer{w: stdout}
	code := exitOK
	for _, path := range fs.Args() {
		if err := dumpFile(path, out); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			code = exitFail
		}
		if out.err != nil {
			fmt.Fprintf(stderr, "audiometa atoms: %v\n", out.err)
			return exitFail
		}
	}
	return code
}

func dumpFile(path string, out *printer) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return err
	}
	format, err := audiometa.DetectFormat(f, st.Size(), path)
	if err != nil || (format != audiometa.FormatM4A && format != audiometa.FormatM4B) {
		return errors.New("not an MP4 container")
	}
	walkAtoms(binary.NewSafeReader(f, st.Size(), path), 0, st.Size(), 0, out)
	return nil
}

// walkAtoms prints the atoms in [offset, end). It never reads outside that
// range, rejects sizes that are smaller than the header or run past the end,
// and stops descending beyond maxAtomDepth.
func walkAtoms(sr *binary.SafeReader, offset, end int64, depth int, out *printer) {
	indent := strings.Repeat("  ", depth)
	if depth >= maxAtomDepth {
		out.printf("%s! max depth\n", indent)
		return
	}
	zero := m4a.SizeZeroInvalid
	if depth == 0 {
		zero = m4a.SizeZeroToEnd // only the last top-level atom may run to the end of the file
	}
	for offset < end {
		h, err := decodeHeaderAt(sr, offset, end, zero)
		if sizeErr, ok := errors.AsType[*m4a.AtomSizeError](err); ok {
			out.printf("%s! invalid atom size %d at offset %d\n", indent, sizeErr.Size, offset)
			return
		}
		if err != nil {
			out.printf("%s! invalid atom header at offset %d\n", indent, offset)
			return
		}
		out.printf("%s%s (size: %d, offset: %d)\n", indent, cleanText(h.Type), h.Size, offset)

		atomEnd := offset + h.Size
		if m4a.IsContainer(h.Type) {
			dataOffset := offset + h.HeaderLen
			if h.Type == "meta" {
				dataOffset += 4 // version and flags
			}
			walkAtoms(sr, dataOffset, atomEnd, depth+1, out)
		}
		offset = atomEnd
	}
}

// decodeHeaderAt decodes the header of the atom at offset, whose container
// ends at end, with the library's decoder.
func decodeHeaderAt(sr *binary.SafeReader, offset, end int64, zero m4a.SizeZero) (m4a.AtomHeader, error) {
	var buf [16]byte
	n := int(min(end-offset, int64(len(buf))))
	if n >= 8 { // anything shorter cannot hold a header, which the decoder reports
		if err := sr.ReadAt(buf[:n], offset, "atom header"); err != nil {
			return m4a.AtomHeader{}, err
		}
	}
	return m4a.DecodeAtomHeader(buf[:max(n, 0)], end-offset, zero)
}
