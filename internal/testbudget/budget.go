// Package testbudget measures heap allocation in tests and fuzz targets and
// holds the allocation budget of each parser and writer operation.
//
// Measurements use runtime.MemStats.TotalAlloc, which counts every byte
// allocated by every goroutine. Do not measure under t.Parallel.
package testbudget

import (
	"runtime"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

// Op is a measured operation.
type Op int

const (
	// Read is one Parse, ExtractArtwork, AudioDigest or DetectFormat call.
	Read Op = iota + 1
	// Write is one File.WriteToContext (which re-parses the source) or InPlacePatch call.
	Write
)

const (
	mib = 1 << 20

	// m4aGap is the known overrun of the M4A and M4B budgets.
	m4aGap = "plan 069: repeated ilst items allocate quadratically"
)

// Budget bounds the bytes one operation may allocate for an n-byte input:
// PerByte*n + Fixed.
type Budget struct {
	// KnownGap names the plan that fixes a known overrun, as "plan NNN: why".
	// While it is set, Check logs an overrun instead of failing.
	KnownGap string
	// PerByte is the allowed allocation per input byte.
	PerByte uint64
	// Fixed is the allowed allocation independent of input size.
	Fixed uint64
}

// Limit returns the allocation ceiling for an n-byte input; a negative n counts as 0.
func (b Budget) Limit(n int) uint64 {
	if n < 0 {
		n = 0
	}
	return b.PerByte*uint64(n) + b.Fixed
}

// For returns the budget of op on format.
func For(op Op, format types.Format) Budget {
	if op == Write {
		b := Budget{PerByte: 16, Fixed: 64 * mib}
		switch format {
		case types.FormatMP3, types.FormatWAV, types.FormatAIFF:
			// Plan 069: ID3 multi-value frames amplify about 93 B/B on read; 069 sets 64.
			b.PerByte = 256
		case types.FormatFLAC:
			// Plan 069: Vorbis comments amplify about 31 B/B on read; 069 sets 16.
			b.PerByte = 64
		case types.FormatM4A, types.FormatM4B:
			b.KnownGap = m4aGap
		case types.FormatOgg, types.FormatOpus, types.FormatUnknown:
			// The default budget applies.
		}
		return b
	}
	b := Budget{PerByte: 4, Fixed: 32 * mib}
	switch format {
	case types.FormatMP3, types.FormatWAV, types.FormatAIFF:
		// Plan 069: ID3 multi-value frames amplify about 93 B/B; 069 sets 32.
		b.PerByte = 128
	case types.FormatFLAC, types.FormatOgg, types.FormatOpus:
		// Plan 069: Vorbis comments amplify about 31 B/B; 069 sets 4.
		b.PerByte = 32
	case types.FormatM4A, types.FormatM4B:
		b.KnownGap = m4aGap
	case types.FormatUnknown:
		// The default budget applies.
	}
	return b
}

// Allocated returns the bytes fn allocated on the heap.
func Allocated(fn func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// Check runs fn and fails tb when it allocates more than b allows for an
// n-byte input, or logs the overrun when b.KnownGap is set.
func Check(tb testing.TB, b Budget, n int, what string, fn func()) {
	tb.Helper()
	got := Allocated(fn)
	limit := b.Limit(n)
	if got <= limit {
		return
	}
	if b.KnownGap != "" {
		tb.Logf("%s allocated %d bytes for a %d-byte input; budget %d (known gap, %s)", what, got, n, limit, b.KnownGap)
		return
	}
	tb.Errorf("%s allocated %d bytes for a %d-byte input; budget %d", what, got, n, limit)
}
