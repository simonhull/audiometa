package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// Useful test file to confirm what we're able to actually read from the different atoms.
func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: atom-dump <file.m4b>")
		os.Exit(1)
	}

	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	dumpAtoms(f, 0, 0, 0)
}

func dumpAtoms(r io.ReaderAt, offset int64, end int64, depth int) {
	if end == 0 {
		// Get file size
		if f, ok := r.(*os.File); ok {
			stat, _ := f.Stat()
			end = stat.Size()
		}
	}

	indent := ""
	for i := 0; i < depth; i++ {
		indent += "  "
	}

	for offset < end {
		// Read atom header
		header := make([]byte, 8)
		if _, err := r.ReadAt(header, offset); err != nil {
			return
		}

		size := binary.BigEndian.Uint32(header[0:4])
		atomType := string(header[4:8])

		// Handle extended size
		atomSize := uint64(size)
		headerSize := int64(8)
		if size == 1 {
			extSize := make([]byte, 8)
			r.ReadAt(extSize, offset+8)
			atomSize = binary.BigEndian.Uint64(extSize)
			headerSize = 16
		}

		fmt.Printf("%s%s (size: %d, offset: %d)\n", indent, atomType, atomSize, offset)

		// Recurse into container atoms
		if isContainer(atomType) {
			dataOffset := offset + headerSize

			// meta atom has 4 extra bytes
			if atomType == "meta" {
				dataOffset += 4
			}

			dataEnd := offset + int64(atomSize)
			dumpAtoms(r, dataOffset, dataEnd, depth+1)
		}

		offset += int64(atomSize)

		if atomSize == 0 {
			break
		}
	}
}

func isContainer(atomType string) bool {
	containers := map[string]bool{
		"moov": true,
		"trak": true,
		"mdia": true,
		"minf": true,
		"stbl": true,
		"udta": true,
		"meta": true,
		"ilst": true,
		"edts": true,
	}
	return containers[atomType]
}
