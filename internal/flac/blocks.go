package flac

import (
	"context"
	"errors"
	"fmt"
	"iter"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// errNoLastBlock reports a metadata block chain that stops before a block
// flagged as last: the file ends, or a block runs past its end.
var errNoLastBlock = errors.New("metadata block chain ends before its last block")

// block is one metadata block of a FLAC file.
type block struct {
	typ  uint8
	off  int64 // offset of the 4-byte block header
	n    int64 // payload length
	last bool  // the header flags this as the last metadata block
}

// payload returns the offset of the block's payload.
func (b block) payload() int64 { return b.off + 4 }

// readPayload reads b's payload, or as much of it as the file holds when the
// block runs past the end of the file (blocks reports that as a broken chain
// on the next step).
func readPayload(sr *binutil.SafeReader, b block, size int64) ([]byte, error) {
	n := min(b.n, size-b.payload())
	if n <= 0 {
		return nil, nil
	}
	if err := sr.CheckLen(b.payload(), int(n), 1, "metadata block"); err != nil {
		return nil, err
	}
	buf := make([]byte, n)
	if err := sr.ReadAt(buf, b.payload(), "metadata block"); err != nil {
		return nil, err
	}
	return buf, nil
}

// end returns the offset just past the block: the next block's header, or
// the first audio frame after the last block.
func (b block) end() int64 { return b.off + 4 + b.n }

// blocks yields the metadata block chain that follows the "fLaC" magic at
// offset start, up to
// and including the block flagged last. Payloads are not read or checked
// against the file size. When the chain stops early it yields one error, with
// a block holding only the offset where it stopped, and ends: errNoLastBlock
// when the file ends before the next header, errNoLastBlock wrapped with the
// read error (an [*binutil.OutOfBoundsError]) when the file ends inside it,
// the read error when a header cannot be read for another reason, or
// ctx.Err() when ctx is done.
func blocks(ctx context.Context, sr *binutil.SafeReader, start, size int64) iter.Seq2[block, error] {
	return func(yield func(block, error) bool) {
		off := start + int64(len(flacMagic))
		for i := 0; ; i++ {
			if err := binutil.Canceled(ctx, i); err != nil {
				yield(block{off: off}, err)
				return
			}
			if off >= size {
				yield(block{off: off}, errNoLastBlock)
				return
			}
			header, err := sr.Read[uint32](off, "metadata block header")
			if err != nil {
				if size-off < 4 { // a header cut short by the end of the file
					err = fmt.Errorf("%w: %w", errNoLastBlock, err)
				}
				yield(block{off: off}, err)
				return
			}
			b := block{
				typ:  uint8(header >> 24 & 0x7F),
				off:  off,
				n:    int64(header & maxBlockLen),
				last: header>>31 == 1,
			}
			if !yield(b, nil) || b.last {
				return
			}
			off = b.end()
		}
	}
}

// errNoMagic reports a file with no "fLaC" marker at its start or right
// after a leading ID3v2 tag.
var errNoMagic = errors.New("invalid FLAC magic bytes")

// streamStart returns the offset of the "fLaC" marker: 0 for a standard file,
// or the size of the ID3v2 tag that some taggers put in front of the stream.
// It returns errNoMagic when the marker is in neither place, and the read
// error when the first bytes cannot be read.
func streamStart(sr *binutil.SafeReader, size int64) (int64, error) {
	var magic [4]byte
	if err := sr.ReadAt(magic[:], 0, "FLAC magic bytes"); err != nil {
		return 0, err
	}
	if string(magic[:]) == flacMagic {
		return 0, nil
	}
	n := types.LeadingID3v2Size(sr)
	if n == 0 || n > size-int64(len(magic)) {
		return 0, errNoMagic
	}
	if err := sr.ReadAt(magic[:], n, "FLAC magic bytes after the ID3v2 tag"); err != nil {
		return 0, err
	}
	if string(magic[:]) != flacMagic {
		return 0, errNoMagic
	}
	return n, nil
}
