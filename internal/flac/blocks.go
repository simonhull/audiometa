package flac

import (
	"context"
	"errors"
	"fmt"
	"iter"

	binutil "github.com/simonhull/audiometa/internal/binary"
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

// end returns the offset just past the block: the next block's header, or
// the first audio frame after the last block.
func (b block) end() int64 { return b.off + 4 + b.n }

// blocks yields the metadata block chain that follows the "fLaC" magic, up to
// and including the block flagged last. Payloads are not read or checked
// against the file size. When the chain stops early it yields one error, with
// a block holding only the offset where it stopped, and ends: errNoLastBlock
// when the file ends before the next header, errNoLastBlock wrapped with the
// read error (an [*binutil.OutOfBoundsError]) when the file ends inside it,
// the read error when a header cannot be read for another reason, or
// ctx.Err() when ctx is done.
func blocks(ctx context.Context, sr *binutil.SafeReader, size int64) iter.Seq2[block, error] {
	return func(yield func(block, error) bool) {
		off := int64(len(flacMagic))
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
