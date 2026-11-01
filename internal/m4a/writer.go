package m4a

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/registry"
	"github.com/simonhull/audiometa/internal/types"
	"github.com/simonhull/audiometa/internal/writeutil"
)

// growFree is the size of the free atom written after a moov that outgrew
// its space, so later edits can grow in place.
const growFree = 2048

// drmSampleEntries are the sample entry types of encrypted tracks: Apple
// FairPlay, ISO Common Encryption and Audible AAX.
var drmSampleEntries = setOf("drms drmi enca encv enct encs aavd")

// writer rewrites the iTunes metadata (moov/udta/meta/ilst) of an MP4 file
// (M4A or M4B). It implements registry.TagWriter and registry.InPlaceWriter.
//
// Only the ilst items for the keys and fields the caller edited change, and
// covr for artwork edits. Every other atom of moov, including the chapter
// carriers (chpl, the chapter text track and its tref), is re-emitted byte
// for byte, except that free and skip atoms inside udta and meta are dropped.
// Deleting every raw key leaves an empty ilst.
//
// The new moov takes the place of the old one. When it fits in the old moov
// plus the free or skip atoms directly after it, the rest is padded with one
// free atom and nothing else in the file moves. When moov is the last atom
// it simply grows. Otherwise it is followed by a 2048-byte free atom and
// everything after it moves by one delta: every stco and co64 chunk offset
// past the old moov is shifted by that delta. A chunk offset that points
// into the old moov or its free space, or an stco offset that would exceed
// 32 bits, makes the write fail with ErrUnsupportedWrite (stco is not
// upgraded to co64). Fragmented files are refused with ErrUnsupportedWrite
// and encrypted tracks with ErrDRMProtected.
type writer struct{}

// topLevel lists the top-level atoms of a file and the index of its moov.
type topLevel struct {
	atoms []*Atom
	moov  int
}

// layout is one planned rewrite of moov.
type layout struct {
	moovStart int64  // offset of the old moov
	rEnd      int64  // end of the old moov and the free/skip run after it
	moov      []byte // the new moov
	free      int64  // size of the free atom after the new moov; 0 for none
	delta     int64  // how far everything from rEnd moves
}

// WriteFile streams src with edits applied to out.
func (w *writer) WriteFile(ctx context.Context, out io.Writer, src io.ReaderAt, size int64, edits *types.EditSet) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	l, err := planWrite(ctx, binutil.NewSafeReader(src, size, ""), size, edits)
	if err != nil {
		return 0, err
	}
	cw := &writeutil.CountingWriter{W: out}
	if _, err := writeutil.CopyRange(ctx, cw, src, 0, l.moovStart); err != nil {
		return cw.N, err
	}
	if _, err := cw.Write(l.moov); err != nil {
		return cw.N, err
	}
	if err := writeFree(ctx, cw, l.free); err != nil {
		return cw.N, err
	}
	if _, err := writeutil.CopyRange(ctx, cw, src, l.rEnd, size-l.rEnd); err != nil {
		return cw.N, err
	}
	return cw.N, nil
}

// PreservedDigest is the AudioDigest, as M4A write verification has always
// been: the atoms around the edits are re-emitted rather than copied as one
// region, and verifyFile checks the chapters they carry separately.
func (w *writer) PreservedDigest(ctx context.Context, src io.ReaderAt, size int64) ([]byte, error) {
	return w.AudioDigest(ctx, src, size)
}

// AudioDigest returns the SHA-256 of the payloads of every top-level mdat
// atom, in file order.
func (w *writer) AudioDigest(ctx context.Context, src io.ReaderAt, size int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	top, err := readTopLevel(ctx, binutil.NewSafeReader(src, size, ""), size)
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	for _, a := range top.atoms {
		if a.Type != atomMdat {
			continue
		}
		if _, err := writeutil.CopyRange(ctx, h, src, a.DataOffset(), a.DataSize()); err != nil {
			return nil, err
		}
	}
	return h.Sum(nil), nil
}

// InPlacePatch returns the new moov and free atom to write over the old
// moov and the free space after it, when the edited moov fits there.
func (w *writer) InPlacePatch(ctx context.Context, src io.ReaderAt, size int64, edits *types.EditSet) (int64, []byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return 0, nil, false, err
	}
	l, err := planWrite(ctx, binutil.NewSafeReader(src, size, ""), size, edits)
	if err != nil {
		return 0, nil, false, err
	}
	region := l.rEnd - l.moovStart
	if l.delta != 0 || int64(len(l.moov))+l.free != region || region > binutil.MaxAllocBytes {
		return 0, nil, false, nil
	}
	patch := make([]byte, 0, region)
	patch = append(patch, l.moov...)
	if l.free > 0 {
		patch = appendFreeHeader(patch, l.free)
		patch = append(patch, make([]byte, region-int64(len(patch)))...)
	}
	return l.moovStart, patch, true, nil
}

// planWrite reads the file's structure, refuses what cannot be written,
// applies edits to moov and lays out the result.
func planWrite(ctx context.Context, sr *binutil.SafeReader, size int64, edits *types.EditSet) (*layout, error) {
	top, err := readTopLevel(ctx, sr, size)
	if err != nil {
		return nil, err
	}
	if err := top.structureCheck(sr); err != nil {
		return nil, err
	}
	moovAtom := top.atoms[top.moov]
	root, err := readMoovTree(sr, moovAtom)
	if err != nil {
		return nil, err
	}
	if err := checkTracks(sr, root); err != nil {
		return nil, err
	}
	if err := applyIlstEdits(root, edits); err != nil {
		return nil, err
	}
	l := &layout{moovStart: moovAtom.Offset, rEnd: moovAtom.Offset + moovAtom.Size}
	for _, a := range top.atoms[top.moov+1:] {
		if a.Type != atomFree && a.Type != atomSkip {
			break
		}
		l.rEnd += a.Size
	}
	if l.moov, err = root.bytes(); err != nil {
		return nil, err
	}
	if err := l.place(root, size); err != nil {
		return nil, err
	}
	return l, nil
}

// place chooses the layout case for the new moov (M5) and, when the data
// after it moves, shifts the chunk offsets and re-serializes moov.
func (l *layout) place(root *matom, size int64) error {
	n, region := int64(len(l.moov)), l.rEnd-l.moovStart
	switch {
	case n == region || region-n >= 8: // fits: pad with one free atom
		l.free = region - n
		return nil
	case l.rEnd == size: // moov is last: grow into the end of the file
		return nil
	}
	l.free = growFree
	l.delta = n + growFree - region
	if err := shiftChunkOffsets(root, l.moovStart, l.rEnd, l.delta); err != nil {
		return err
	}
	moov, err := root.bytes()
	if err != nil {
		return err
	}
	if int64(len(moov)) != n {
		return fmt.Errorf("%w: moov size changed while shifting chunk offsets", types.ErrUnsupportedWrite)
	}
	l.moov = moov
	return nil
}

// shiftChunkOffsets adds delta to every stco/co64 chunk offset at or past
// rEnd. Offsets before moovStart stay. An offset inside [moovStart, rEnd)
// points into the old moov or its free space, and an stco offset that would
// not fit in 32 bits cannot be stored: both return ErrUnsupportedWrite.
func shiftChunkOffsets(root *matom, moovStart, rEnd, delta int64) error {
	return root.walk(func(a *matom) error {
		if a.container || (a.typ != atomStco && a.typ != atomCo64) {
			return nil
		}
		width := 4
		if a.typ == atomCo64 {
			width = 8
		}
		if len(a.payload) < 8 {
			return fmt.Errorf("%w: %s atom too short", types.ErrUnsupportedWrite, a.typ)
		}
		count := uint64(binary.BigEndian.Uint32(a.payload[4:8]))
		if count*uint64(width) > uint64(len(a.payload)-8) { //nolint:gosec // G115: len(a.payload) >= 8, checked above
			return fmt.Errorf("%w: %s holds %d entries but has room for %d", types.ErrUnsupportedWrite, a.typ, count, (len(a.payload)-8)/width)
		}
		for i := range int(count) { //nolint:gosec // G115: count*width <= len(payload)
			entry := a.payload[8+i*width : 8+(i+1)*width]
			if err := shiftEntry(a.typ, entry, moovStart, rEnd, delta); err != nil {
				return err
			}
		}
		return nil
	})
}

// shiftEntry shifts one big-endian chunk offset (4 bytes for stco, 8 for co64).
func shiftEntry(typ string, entry []byte, moovStart, rEnd, delta int64) error {
	var o uint64
	if typ == atomStco {
		o = uint64(binary.BigEndian.Uint32(entry))
	} else {
		o = binary.BigEndian.Uint64(entry)
	}
	switch {
	case o < uint64(moovStart): //nolint:gosec // G115: moovStart is a file offset, never negative
		return nil
	case o < uint64(rEnd): //nolint:gosec // G115: rEnd is a file offset, never negative
		return fmt.Errorf("%w: %s chunk offset %d points into moov or its free space [%d, %d)", types.ErrUnsupportedWrite, typ, o, moovStart, rEnd)
	}
	shifted := o + uint64(delta) //nolint:gosec // G115: the grow case only, where delta > 0
	if typ == atomCo64 {
		if shifted < o {
			return fmt.Errorf("%w: co64 chunk offset overflow", types.ErrUnsupportedWrite)
		}
		binary.BigEndian.PutUint64(entry, shifted)
		return nil
	}
	if shifted > math.MaxUint32 {
		return fmt.Errorf("%w: chunk offset overflow; co64 upgrade not supported", types.ErrUnsupportedWrite)
	}
	binary.BigEndian.PutUint32(entry, uint32(shifted))
	return nil
}

// readTopLevel lists the file's top-level atoms. A final atom with size 0
// extends to the end of the file.
func readTopLevel(ctx context.Context, sr *binutil.SafeReader, size int64) (*topLevel, error) {
	top := &topLevel{moov: -1}
	for i, off := 0, int64(0); off < size; i++ {
		if err := binutil.Canceled(ctx, i); err != nil {
			return nil, err
		}
		var a *Atom
		size32, err := sr.Read[uint32](off, "atom size")
		if err != nil {
			return nil, err
		}
		if size32 == 0 {
			a = &Atom{Offset: off, Size: size - off}
			if a.Size < 8 {
				return nil, corrupted(sr, off, "truncated atom header")
			}
			var typ [4]byte
			if err := sr.ReadAt(typ[:], off+4, "atom type"); err != nil {
				return nil, err
			}
			a.Type = string(typ[:])
		} else if a, err = readAtomHeader(sr, off, size); err != nil {
			return nil, err
		}
		top.atoms = append(top.atoms, a)
		off += a.Size
	}
	return top, nil
}

// structureCheck refuses files the writer cannot rewrite (M1): it needs
// exactly one moov and no fragments (moof).
func (top *topLevel) structureCheck(sr *binutil.SafeReader) error {
	for i, a := range top.atoms {
		switch a.Type {
		case "moof":
			return fmt.Errorf("%w: fragmented MP4", types.ErrUnsupportedWrite)
		case atomMoov:
			if top.moov >= 0 {
				return corrupted(sr, a.Offset, "more than one moov atom")
			}
			top.moov = i
		}
	}
	if top.moov < 0 {
		return corrupted(sr, 0, "no moov atom")
	}
	return nil
}

// checkTracks refuses fragmented movies (moov/mvex) and encrypted tracks:
// any stsd sample entry of a DRM type.
func checkTracks(sr *binutil.SafeReader, root *matom) error {
	if root.child("mvex") != nil {
		return fmt.Errorf("%w: fragmented MP4", types.ErrUnsupportedWrite)
	}
	for _, trak := range traks(root) {
		stsd := findPath(trak, "mdia", "minf", "stbl", "stsd")
		if stsd == nil || stsd.container {
			continue
		}
		entries, err := sampleEntryTypes(stsd.payload)
		if err != nil {
			return corrupted(sr, 0, err.Error())
		}
		for _, e := range entries {
			if drmSampleEntries[e] {
				return fmt.Errorf("%w: %q sample entry", types.ErrDRMProtected, e)
			}
		}
	}
	return nil
}

// sampleEntryTypes returns the fourcc of every sample entry of an stsd payload.
func sampleEntryTypes(p []byte) ([]string, error) {
	if len(p) < 8 {
		return nil, errors.New("stsd atom too short")
	}
	count := binary.BigEndian.Uint32(p[4:8])
	var out []string
	off := 8
	for i := range count {
		if len(p)-off < 8 {
			return nil, fmt.Errorf("stsd entry %d overruns the atom", i)
		}
		size := binary.BigEndian.Uint32(p[off:])
		if size < 8 || uint64(size) > uint64(len(p)-off) { //nolint:gosec // G115: off <= len(p) by the check above
			return nil, fmt.Errorf("stsd entry %d has invalid size %d", i, size)
		}
		out = append(out, string(p[off+4:off+8]))
		off += int(size)
	}
	return out, nil
}

// writeFree writes a free atom of n bytes (none when n is 0), zero-filled.
func writeFree(ctx context.Context, w io.Writer, n int64) error {
	if n == 0 {
		return nil
	}
	hdr := appendFreeHeader(nil, n)
	if _, err := w.Write(hdr); err != nil {
		return err
	}
	_, err := writeutil.CopyRange(ctx, w, zeroReader{}, 0, n-int64(len(hdr)))
	return err
}

// appendFreeHeader appends the header of a free atom of n (>= 8) bytes,
// using a 64-bit size when n does not fit in 32 bits.
func appendFreeHeader(b []byte, n int64) []byte {
	if n > math.MaxUint32 {
		b = binary.BigEndian.AppendUint32(b, 1)
		b = append(b, atomFree...)
		return binary.BigEndian.AppendUint64(b, uint64(n))
	}
	b = binary.BigEndian.AppendUint32(b, uint32(n)) //nolint:gosec // G115: n <= MaxUint32
	return append(b, atomFree...)
}

// zeroReader reads zeros at any offset.
type zeroReader struct{}

// ReadAt fills p with zeros.
func (zeroReader) ReadAt(p []byte, _ int64) (int, error) {
	clear(p)
	return len(p), nil
}

func init() {
	w := &writer{}
	registry.RegisterWriter(types.FormatM4A, w)
	registry.RegisterWriter(types.FormatM4B, w)
}
