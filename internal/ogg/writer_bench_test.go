package ogg

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

// longOpus is a well-formed Opus stream of pages audio pages of 5900 bytes
// each (an hour of 48 kb/s Opus is about 3600 of them).
func longOpus(pages int) []byte {
	specs := []pageSpec{{headerType: 0x02, serial: 5, lacing: lacingFor(len(opusHeadPacket())), data: opusHeadPacket()}}
	specs = append(specs, splitAcrossPages(commentPacket("OpusTags", false, "TITLE=Old"), 0, 5)...)
	payload := bytes.Repeat([]byte{0x5A}, 5900)
	for i := range pages {
		p := pageSpec{serial: 5, granule: uint64(960 * (i + 1)), lacing: lacingFor(len(payload)), data: payload}
		if i == pages-1 {
			p.headerType = pageEOS
		}
		specs = append(specs, p)
	}
	return withCRCs(buildOgg(specs...))
}

// BenchmarkWriteOpus rewrites a long Opus file in memory: with the header page
// count unchanged (the audio pages are copied untouched) and with one more
// header page (every audio page is renumbered and checksummed).
func BenchmarkWriteOpus(b *testing.B) {
	src := longOpus(3600)
	w := &writer{codec: opusCodec}
	for _, bc := range []struct {
		name  string
		edits *types.EditSet
	}{
		{"same page count", keyEdit("TITLE", "New title")},
		{"one more page", keyEdit("BIG", strings.Repeat("x", 70_000))},
	} {
		b.Run(bc.name, func(b *testing.B) {
			b.SetBytes(int64(len(src)))
			for b.Loop() {
				if _, err := w.WriteFile(b.Context(), io.Discard, bytes.NewReader(src), int64(len(src)), bc.edits); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkPreservedDigestOpus is the cost of the digest a Write takes before
// and after the rewrite.
func BenchmarkPreservedDigestOpus(b *testing.B) {
	src := longOpus(3600)
	w := &writer{codec: opusCodec}
	b.SetBytes(int64(len(src)))
	for b.Loop() {
		if _, err := w.PreservedDigest(b.Context(), bytes.NewReader(src), int64(len(src))); err != nil {
			b.Fatal(err)
		}
	}
}
