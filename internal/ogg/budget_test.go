package ogg

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/testbudget"
	"github.com/simonhull/audiometa/internal/types"
)

func TestReadHeaderPackets_EmptyPagesEndTheSearch(t *testing.T) {
	head := opusHeadPacket()
	pages := []pageSpec{{headerType: 2, serial: 1, lacing: lacingFor(len(head)), data: head}}
	pages = append(pages, slices.Repeat([]pageSpec{{serial: 1}}, 300_000)...)
	data := buildOgg(pages...)
	sr := binutil.NewSafeReader(bytes.NewReader(data), int64(len(data)), "x.opus")

	file := &types.File{}
	alloc := testbudget.Allocated(func() {
		if _, err := readHeaderPackets(context.Background(), sr, int64(len(data)), file, maxHeaderBytes); err != nil {
			t.Error(err)
		}
	})
	if !slices.ContainsFunc(file.Warnings, func(w types.Warning) bool {
		return w.Stage == types.StageMetadata && strings.Contains(w.Message, "header search exceeds")
	}) {
		t.Errorf("warnings = %+v, want a header search limit warning", file.Warnings)
	}
	if limit := testbudget.For(testbudget.Read, types.FormatOpus).Limit(len(data)); alloc > limit {
		t.Errorf("allocated %d bytes for a %d-byte input, budget %d", alloc, len(data), limit)
	}
	if alloc > 16<<20 {
		t.Errorf("allocated %d bytes, want under 16 MiB", alloc)
	}
}
