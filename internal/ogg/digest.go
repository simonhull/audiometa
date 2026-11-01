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

// pageEOS marks the last page of a logical stream (RFC 3533 §6).
const pageEOS = 0x04

// digester computes audio digests for Ogg Vorbis and Opus files.
type digester struct{}

// AudioDigest returns the SHA-256 of the audio in the file's first logical
// stream: the page payload bytes after its header packets, up to the stream's
// last page. Page headers are left out: a retag renumbers the pages after the
// comment header, changing their sequence numbers and checksums, while the
// packet bytes stay the same. Hashing payloads alone also makes the digest
// independent of pagination and of packet boundaries.
//
// Pages of other multiplexed or chained streams are ignored. Once the audio
// has begun, unreadable trailing data (a truncated last page, an appended tag)
// ends the stream instead of failing the digest, as it does for the parser.
func (digester) AudioDigest(ctx context.Context, src io.ReaderAt, size int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if size <= 0 {
		return nil, &types.CorruptedFileError{Reason: "empty Ogg stream"}
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
			if i > 0 && skip.remaining == 0 {
				break // trailing data after the audio began
			}
			return nil, fmt.Errorf("read Ogg page %d at offset %d: %w", i, off, err)
		}
		if i == 0 {
			n, ok := headerPacketCount(detectOggCodec(page.Data))
			if !ok {
				return nil, &types.CorruptedFileError{Reason: "unknown Ogg codec"}
			}
			serial, skip.remaining = page.SerialNumber, n
		}
		off = next
		if page.SerialNumber != serial {
			continue
		}
		h.Write(skip.audio(page))
		if page.HeaderType&pageEOS != 0 {
			break
		}
	}
	if skip.remaining > 0 {
		return nil, &types.CorruptedFileError{Offset: size, Reason: "Ogg stream ends inside its header packets"}
	}
	return h.Sum(nil), nil
}

// headerPacketCount returns how many header packets precede the audio in a
// codec's logical stream: identification, comment and setup for Vorbis
// (Vorbis I §4.2); OpusHead and OpusTags for Opus (RFC 7845 §3).
func headerPacketCount(codec string) (int, bool) {
	switch codec {
	case codecVorbis:
		return 3, true
	case codecOpus:
		return 2, true
	default:
		return 0, false
	}
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
