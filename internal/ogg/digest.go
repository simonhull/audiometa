package ogg

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"

	"github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/registry"
	"github.com/simonhull/audiometa/internal/types"
)

// headerPackets is how many header packets precede the audio in each codec's
// logical stream: identification, comment and setup for Vorbis (Vorbis I
// §4.2); OpusHead and OpusTags for Opus (RFC 7845 §3).
var headerPackets = map[string]int{codecVorbis: 3, codecOpus: 2}

// digester computes audio digests for Ogg Vorbis and Opus files.
type digester struct{}

// AudioDigest returns the SHA-256 of the first logical stream's audio: the
// page payload bytes after its header packets. Page headers are left out: a
// retag renumbers the pages after the comment header, changing their sequence
// numbers and checksums, while the packet bytes stay the same. Hashing
// payloads alone also makes the digest independent of pagination.
func (digester) AudioDigest(ctx context.Context, src io.ReaderAt, size int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sr := binary.NewSafeReader(src, size, "")
	h := sha256.New()
	var serial uint32
	var skip headerSkip
	for i, off := 0, int64(0); off < size; i++ {
		if err := binary.Canceled(ctx, i); err != nil {
			return nil, err
		}
		page, next, err := readPage(sr, off)
		if err != nil {
			return nil, &types.CorruptedFileError{Offset: off, Reason: fmt.Sprintf("Ogg page %d: %v", i, err)}
		}
		if i == 0 {
			n, ok := headerPackets[detectOggCodec(page.Data)]
			if !ok {
				return nil, &types.CorruptedFileError{Reason: "unknown Ogg codec"}
			}
			serial, skip.remaining = page.SerialNumber, n
		}
		if page.SerialNumber == serial {
			h.Write(skip.audio(page))
		}
		off = next
	}
	if skip.remaining > 0 {
		return nil, &types.CorruptedFileError{Offset: size, Reason: "Ogg stream ends inside its header packets"}
	}
	return h.Sum(nil), nil
}

// headerSkip separates a logical stream's header packets from its audio.
type headerSkip struct {
	remaining int // header packets not yet ended
}

// audio returns the part of page's payload after the stream's header packets,
// counting down each header packet that ends on the page. It returns nil while
// header packets are still incomplete.
func (s *headerSkip) audio(page *Page) []byte {
	pos := 0
	for _, lace := range page.laceValues() {
		if s.remaining == 0 {
			break
		}
		pos += int(lace)
		if lace < 255 {
			s.remaining--
		}
	}
	if s.remaining > 0 {
		return nil
	}
	return page.Data[pos:]
}

func init() {
	registry.RegisterDigester(types.FormatOgg, digester{})
	registry.RegisterDigester(types.FormatOpus, digester{})
}
