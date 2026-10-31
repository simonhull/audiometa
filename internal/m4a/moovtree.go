package m4a

import (
	"encoding/binary"
	"fmt"
	"math"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// Atom types the writer navigates.
const (
	atomMoov = "moov"
	atomUdta = "udta"
	atomMeta = "meta"
	atomIlst = "ilst"
	atomFree = "free"
	atomSkip = "skip"
	atomMdat = "mdat"
	atomStco = "stco"
	atomCo64 = "co64"
)

// maxMoovDepth bounds container nesting in readMoovTree. Real files nest
// about six levels deep (moov/trak/mdia/minf/stbl/stsd).
const maxMoovDepth = 32

// structuralContainers hold the sample tables, and so the chunk offsets. A
// malformed one is a corrupt file: the writer must see every stco and co64.
var structuralContainers = map[string]bool{atomMoov: true, atomTrak: true, "mdia": true, "minf": true, "stbl": true}

// otherContainers are descended into when their children parse. A malformed
// one is kept as an opaque leaf, so its bytes are preserved verbatim.
var otherContainers = map[string]bool{atomUdta: true, "edts": true, "dinf": true, "tref": true, atomMeta: true, atomIlst: true}

// matom is one atom of an in-memory moov tree. A container holds its
// children; a leaf holds its payload byte for byte. Serializing recomputes
// every size, so only edited leaves and their ancestors change.
type matom struct {
	typ       string
	ext       bool     // the source used a 16-byte (64-bit size) header
	container bool     // children (and full, tail) are used instead of payload
	full      []byte   // a full container's version and flags (meta: 4 bytes)
	children  []*matom // container children, in order
	tail      []byte   // trailing bytes too short to be an atom (a QuickTime udta's 0 terminator)
	payload   []byte   // leaf payload, after the header
}

// readMoovTree reads moovAtom into memory and parses it into a tree.
func readMoovTree(sr *binutil.SafeReader, moovAtom *Atom) (*matom, error) {
	if moovAtom.Size > binutil.MaxAllocBytes {
		return nil, fmt.Errorf("moov atom of %d bytes exceeds %d", moovAtom.Size, binutil.MaxAllocBytes)
	}
	if err := sr.CheckLen(moovAtom.Offset, int(moovAtom.Size), 1, "moov atom"); err != nil {
		return nil, err
	}
	buf := make([]byte, moovAtom.Size)
	if err := sr.ReadAt(buf, moovAtom.Offset, "moov atom"); err != nil {
		return nil, err
	}
	atoms, tail, err := parseAtoms(sr, buf, moovAtom.Offset, "", 0)
	if err != nil {
		return nil, err
	}
	if len(atoms) != 1 || len(tail) != 0 || atoms[0].typ != atomMoov {
		return nil, corrupted(sr, moovAtom.Offset, "moov atom does not parse as one atom")
	}
	return atoms[0], nil
}

// parseAtoms splits b (which starts at file offset base) into atoms, parsing
// containers recursively. parent is the enclosing atom's type: the children
// of ilst (its items) are always leaves. Bytes after the last atom that are
// too short to hold an atom header are returned as tail.
func parseAtoms(sr *binutil.SafeReader, b []byte, base int64, parent string, depth int) ([]*matom, []byte, error) {
	if depth > maxMoovDepth {
		return nil, nil, corrupted(sr, base, "moov atoms nest too deeply")
	}
	var atoms []*matom
	off := 0
	for len(b)-off >= 8 {
		a, size, err := parseAtom(sr, b[off:], base+int64(off), parent, depth)
		if err != nil {
			return nil, nil, err
		}
		atoms = append(atoms, a)
		off += size
	}
	return atoms, b[off:], nil
}

// parseAtom parses the atom at the start of b and returns it with its size.
func parseAtom(sr *binutil.SafeReader, b []byte, at int64, parent string, depth int) (*matom, int, error) {
	size, hdr := uint64(binary.BigEndian.Uint32(b)), 8
	a := &matom{typ: string(b[4:8])}
	switch size {
	case 0:
		return nil, 0, corrupted(sr, at, fmt.Sprintf("atom %q inside moov has size 0", a.typ))
	case 1:
		if len(b) < 16 {
			return nil, 0, corrupted(sr, at, fmt.Sprintf("atom %q: truncated extended size", a.typ))
		}
		size, hdr, a.ext = binary.BigEndian.Uint64(b[8:16]), 16, true
	}
	if size < uint64(hdr) || size > uint64(len(b)) {
		return nil, 0, corrupted(sr, at, fmt.Sprintf("atom %q size %d exceeds its container (%d bytes remain)", a.typ, size, len(b)))
	}
	n := int(size) //nolint:gosec // G115: size <= len(b)
	a.payload = b[hdr:n]
	if parent == atomIlst || (!structuralContainers[a.typ] && !otherContainers[a.typ]) {
		return a, n, nil
	}
	if err := a.parseChildren(sr, at+int64(hdr), depth); err != nil {
		if structuralContainers[a.typ] {
			return nil, 0, err
		}
		return a, n, nil // kept as an opaque leaf
	}
	return a, n, nil
}

// parseChildren turns the leaf a into a container by parsing its payload.
func (a *matom) parseChildren(sr *binutil.SafeReader, at int64, depth int) error {
	body := a.payload
	var full []byte
	if a.typ == atomMeta {
		if len(body) < 4 {
			return corrupted(sr, at, "meta atom too short for its version and flags")
		}
		full, body = body[:4], body[4:]
		at += 4
	}
	children, tail, err := parseAtoms(sr, body, at, a.typ, depth+1)
	if err != nil {
		return err
	}
	a.container, a.full, a.children, a.tail, a.payload = true, full, children, tail, nil
	return nil
}

// bodySize returns the size of a's content after its header.
func (a *matom) bodySize() uint64 {
	if !a.container {
		return uint64(len(a.payload))
	}
	n := uint64(len(a.full) + len(a.tail))
	for _, c := range a.children {
		n += c.size()
	}
	return n
}

// extHeader reports whether a is serialized with a 16-byte header: when the
// source had one, or when its size does not fit in 32 bits.
func (a *matom) extHeader(body uint64) bool {
	return a.ext || body+8 > math.MaxUint32
}

// size returns a's serialized size, header included.
func (a *matom) size() uint64 {
	body := a.bodySize()
	if a.extHeader(body) {
		return body + 16
	}
	return body + 8
}

// bytes serializes a, recomputing every size.
func (a *matom) bytes() ([]byte, error) {
	if n := a.size(); n > binutil.MaxAllocBytes {
		return nil, fmt.Errorf("%w: %s atom of %d bytes exceeds %d", types.ErrTagTooLarge, a.typ, n, binutil.MaxAllocBytes)
	}
	return a.appendTo(make([]byte, 0, a.size())), nil
}

// appendTo appends a's serialization to b.
func (a *matom) appendTo(b []byte) []byte {
	body := a.bodySize()
	if a.extHeader(body) {
		b = binary.BigEndian.AppendUint32(b, 1)
		b = append(b, a.typ...)
		b = binary.BigEndian.AppendUint64(b, body+16)
	} else {
		b = binary.BigEndian.AppendUint32(b, uint32(body+8)) //nolint:gosec // G115: extHeader is false, so body+8 fits in 32 bits
		b = append(b, a.typ...)
	}
	if !a.container {
		return append(b, a.payload...)
	}
	b = append(b, a.full...)
	for _, c := range a.children {
		b = c.appendTo(b)
	}
	return append(b, a.tail...)
}

// child returns a's first container child of type typ, or nil.
func (a *matom) child(typ string) *matom {
	for _, c := range a.children {
		if c.typ == typ {
			return c
		}
	}
	return nil
}

// findPath follows the first child of each type in path from root and
// returns the atom reached, or nil when a level is missing. Every level but
// the last must be a container.
func findPath(root *matom, path ...string) *matom {
	a := root
	for _, typ := range path {
		if a == nil || !a.container {
			return nil
		}
		a = a.child(typ)
	}
	return a
}

// traks returns the trak children of moov.
func traks(root *matom) []*matom {
	var out []*matom
	for _, c := range root.children {
		if c.typ == atomTrak && c.container {
			out = append(out, c)
		}
	}
	return out
}

// walk calls fn for a and every atom below it, depth first.
func (a *matom) walk(fn func(*matom) error) error {
	if err := fn(a); err != nil {
		return err
	}
	for _, c := range a.children {
		if err := c.walk(fn); err != nil {
			return err
		}
	}
	return nil
}

// corrupted returns a CorruptedFileError for the file behind sr.
func corrupted(sr *binutil.SafeReader, off int64, reason string) error {
	return &types.CorruptedFileError{Path: sr.Path(), Offset: off, Reason: reason}
}
