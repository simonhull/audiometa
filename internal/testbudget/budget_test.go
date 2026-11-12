package testbudget

import (
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

var sink []byte

func TestAllocated(t *testing.T) {
	got := Allocated(func() { sink = make([]byte, 1<<20) })
	if got < 1<<20 || got > 2<<20 {
		t.Errorf("Allocated = %d, want about 1 MiB", got)
	}
}

func TestLimit(t *testing.T) {
	b := Budget{PerByte: 4, Fixed: 100}
	if got := b.Limit(10); got != 140 {
		t.Errorf("Limit(10) = %d, want 140", got)
	}
	if got := b.Limit(-1); got != 100 {
		t.Errorf("Limit(-1) = %d, want 100", got)
	}
}

func TestForCoversEveryFormat(t *testing.T) {
	for _, op := range []Op{Read, Write} {
		for _, f := range []types.Format{
			types.FormatMP3, types.FormatFLAC, types.FormatM4A, types.FormatM4B,
			types.FormatOgg, types.FormatOpus, types.FormatWAV, types.FormatAIFF, types.FormatUnknown,
		} {
			if b := For(op, f); b.PerByte == 0 || b.Fixed == 0 {
				t.Errorf("For(%v, %v) = %+v, want a non-zero budget", op, f, b)
			}
		}
	}
}

func TestCheckPassesWithinBudget(t *testing.T) {
	Check(t, Budget{PerByte: 1, Fixed: 2 << 20}, 0, "small", func() { sink = make([]byte, 1<<20) })
}
