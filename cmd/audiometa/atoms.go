package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/simonhull/audiometa"
	"github.com/simonhull/audiometa/internal/binary"
)

// maxAtomDepth bounds recursion on hostile input.
const maxAtomDepth = 32

// atomContainers are the atoms whose payload is a sequence of child atoms.
var atomContainers = map[string]bool{
	"moov": true, "trak": true, "mdia": true, "minf": true, "stbl": true,
	"udta": true, "meta": true, "ilst": true, "edts": true,
}

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
	for offset < end {
		atomType, size, headerSize, ok := readAtomHeader(sr, offset, end)
		if !ok {
			out.printf("%s! invalid atom header at offset %d\n", indent, offset)
			return
		}
		if size < uint64(headerSize) || size > uint64(end-offset) {
			out.printf("%s! invalid atom size %d at offset %d\n", indent, size, offset)
			return
		}
		out.printf("%s%s (size: %d, offset: %d)\n", indent, cleanText(atomType), size, offset)

		atomEnd := offset + int64(size)
		if atomContainers[atomType] {
			dataOffset := offset + headerSize
			if atomType == "meta" {
				dataOffset += 4 // version and flags
			}
			walkAtoms(sr, dataOffset, atomEnd, depth+1, out)
		}
		offset = atomEnd
	}
}

// readAtomHeader reads the type, total size and header length of the atom at
// offset. A size field of 0 means the atom extends to end.
func readAtomHeader(sr *binary.SafeReader, offset, end int64) (atomType string, size uint64, headerSize int64, ok bool) {
	size32, err := sr.Read[uint32](offset, "atom size")
	if err != nil {
		return "", 0, 0, false
	}
	typ := make([]byte, 4)
	if err := sr.ReadAt(typ, offset+4, "atom type"); err != nil {
		return "", 0, 0, false
	}
	atomType = string(typ)
	headerSize = 8
	size = uint64(size32)
	switch size32 {
	case 0:
		size = uint64(end - offset)
	case 1:
		size, err = sr.Read[uint64](offset+8, "extended atom size")
		if err != nil {
			return "", 0, 0, false
		}
		headerSize = 16
	}
	return atomType, size, headerSize, true
}
