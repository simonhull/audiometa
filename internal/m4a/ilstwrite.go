package m4a

import (
	"encoding/binary"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/simonhull/audiometa/internal/types"
)

// ilst item types with special handling by the writer.
const (
	itemFreeform = "----"
	itemCovr     = "covr"
	itemTrkn     = "trkn"
	itemDisk     = "disk"
	itemDay      = "\xA9day" // ©day
	itemMvi      = "\xA9mvi" // ©mvi
	freeformMean = "com.apple.iTunes"
)

// covr data atom type codes.
const (
	covrJPEG = 13
	covrPNG  = 14
	covrBMP  = 27
)

// mdirHdlr is the hdlr atom of a newly created iTunes meta atom.
var mdirHdlr = []byte{
	0, 0, 0, 0x21, 'h', 'd', 'l', 'r',
	0, 0, 0, 0, 0, 0, 0, 0,
	'm', 'd', 'i', 'r', 'a', 'p', 'p', 'l',
	0, 0, 0, 0, 0, 0, 0, 0, 0,
}

// knownTextItems are the ilst item types written as text items even when
// the file has none yet. Every other 4-byte type starting with © (0xA9), and
// any type already present in the ilst, is also an item; other keys are
// freeform (----) names.
var knownTextItems = setOf("\xA9nam \xA9ART \xA9alb \xA9cmt \xA9grp \xA9lyr aART cprt ldes desc \xA9des " +
	"\xA9gen \xA9wrt \xA9day \xA9mvn \xA9too \xA9st3 \xA9wrk soal soar soaa sonm soco sosn tvsh tven tvnn " +
	"catg keyw purl")

// intSpec is the encoding of an integer ilst item: its data type (21
// signed, 22 unsigned) and width in bytes.
type intSpec struct {
	dataType uint32
	width    int
}

// knownIntItems are the integer ilst items, with the encoding a new one gets.
var knownIntItems = map[string]intSpec{
	itemMvi: {dataTypeBESigned, 2}, "\xA9mvc": {dataTypeBESigned, 2}, "tmpo": {dataTypeBESigned, 2},
	"cpil": {dataTypeBESigned, 1}, "pgap": {dataTypeBESigned, 1}, "stik": {dataTypeBESigned, 1},
	"rtng": {dataTypeBESigned, 1}, "hdvd": {dataTypeBESigned, 1}, "tves": {dataTypeBESigned, 4},
	"tvsn": {dataTypeBESigned, 4},
}

// canonicalKeys is the raw key a field is written to when the source has
// none of the field's keys. SeriesPart, track and disc are handled apart.
var canonicalKeys = map[types.Field]string{
	types.FieldTitle: "\xA9nam", types.FieldArtist: itemArtist, types.FieldArtists: itemArtist,
	types.FieldAlbum: "\xA9alb", types.FieldComment: "\xA9cmt", types.FieldGrouping: "\xA9grp",
	types.FieldLyrics: "\xA9lyr", types.FieldAlbumArtist: "aART", types.FieldCopyright: "cprt",
	types.FieldDescription: itemLongDesc, types.FieldGenres: itemGenre, types.FieldComposers: itemComposer,
	types.FieldDate: itemDay, types.FieldYear: itemDay, types.FieldSeries: "\xA9mvn",
	types.FieldSubtitle: "SUBTITLE", types.FieldNarrator: "NARRATOR", types.FieldPublisher: "PUBLISHER",
	types.FieldISBN: "ISBN", types.FieldASIN: "ASIN", types.FieldLanguage: "LANGUAGE",
}

// keySeriesPart is the freeform key for a non-integer SeriesPart.
const keySeriesPart = "SERIES PART"

// setOf builds a set of the space-separated words of s.
func setOf(s string) map[string]bool {
	m := make(map[string]bool)
	for w := range strings.FieldsSeq(s) {
		m[w] = true
	}
	return m
}

// ilstEditor applies edits to the items of one ilst.
type ilstEditor struct {
	ilst      *matom
	origTypes map[string]bool // item types present before editing (---- excluded)
	edits     *types.EditSet
}

// applyIlstEdits applies edits to root's moov/udta/meta/ilst, creating the
// path when needed: key edits, then field edits, then artwork operations. A
// path it created is removed again when the ilst ends up empty.
func applyIlstEdits(root *matom, edits *types.EditSet) error {
	ilst, undo, err := ensureIlst(root)
	if err != nil {
		return err
	}
	e := &ilstEditor{ilst: ilst, origTypes: map[string]bool{}, edits: edits}
	for _, it := range ilst.children {
		if it.typ != itemFreeform {
			e.origTypes[it.typ] = true
		}
	}
	for _, k := range edits.Keys {
		if err := e.applyKey(k.Key, k.Values); err != nil {
			return err
		}
	}
	for _, f := range edits.Fields {
		if err := e.applyField(f); err != nil {
			return err
		}
	}
	if err := applyCovr(ilst, edits.Artwork); err != nil {
		return err
	}
	if len(ilst.children) == 0 && undo != nil {
		undo()
	}
	return nil
}

// ensureIlst finds or creates moov/udta/meta/ilst and drops the free and
// skip atoms of udta and meta. undo, when not nil, removes what it created.
func ensureIlst(root *matom) (ilst *matom, undo func(), err error) {
	parent := root
	for _, typ := range []string{atomUdta, atomMeta, atomIlst} {
		a := parent.child(typ)
		if a == nil {
			a = newContainer(typ)
			if undo == nil {
				p := parent
				undo = func() { p.children = slices.DeleteFunc(p.children, func(c *matom) bool { return c == a }) }
			}
			parent.children = append(parent.children, a)
		}
		if !a.container {
			return nil, nil, fmt.Errorf("%w: malformed %s atom", types.ErrUnsupportedWrite, typ)
		}
		if typ != atomIlst {
			a.children = slices.DeleteFunc(a.children, func(c *matom) bool { return c.typ == atomFree || c.typ == atomSkip })
		}
		parent = a
	}
	return parent, undo, nil
}

// newContainer returns an empty container atom; a meta gets version and
// flags 0 and an iTunes (mdir) hdlr.
func newContainer(typ string) *matom {
	a := &matom{typ: typ, container: true}
	if typ == atomMeta {
		a.full = []byte{0, 0, 0, 0}
		a.children = []*matom{{typ: "hdlr", payload: slices.Clone(mdirHdlr[8:])}}
	}
	return a
}

// applyKey replaces every item holding raw key with one holding values, or
// removes them when values is empty.
func (e *ilstEditor) applyKey(key string, values []string) error {
	for _, v := range values {
		if !utf8.ValidString(v) {
			return fmt.Errorf("%w: value of %q is not valid UTF-8", types.ErrUnsupportedWrite, key)
		}
	}
	switch {
	case key == itemTrkn || key == itemDisk:
		return fmt.Errorf("%w: %s is written through the track and disc fields", types.ErrUnsupportedWrite, key)
	case key == itemCovr:
		return fmt.Errorf("%w: covr is written through the artwork methods", types.ErrUnsupportedWrite)
	case key == itemFreeform:
		return fmt.Errorf("%w: %q is not a tag key", types.ErrUnsupportedWrite, key)
	case key == keyGnre:
		return e.setGnre(values)
	case e.isItemKey(key):
		return e.setItem(key, values)
	default:
		return e.setFreeform(key, values)
	}
}

// isItemKey reports whether key names an ilst item type rather than a
// freeform name.
func (e *ilstEditor) isItemKey(key string) bool {
	if len(key) != 4 {
		return false
	}
	_, isInt := knownIntItems[key]
	return knownTextItems[key] || isInt || key[0] == 0xA9 || e.origTypes[key]
}

// remove drops every item match selects and returns the index the first
// dropped item had in the resulting list, or -1 when none was dropped.
func (e *ilstEditor) remove(match func(*matom) bool) int {
	at := -1
	kept := make([]*matom, 0, len(e.ilst.children))
	for _, it := range e.ilst.children {
		if match(it) {
			if at < 0 {
				at = len(kept)
			}
			continue
		}
		kept = append(kept, it)
	}
	e.ilst.children = kept
	return at
}

// insert puts item at index at, or appends it when at is negative.
func (e *ilstEditor) insert(at int, item *matom) {
	if at < 0 {
		e.ilst.children = append(e.ilst.children, item)
		return
	}
	e.ilst.children = slices.Insert(e.ilst.children, at, item)
}

// replaceItems removes every item of type typ and puts payload (when not
// nil) where the first one was, or at the end.
func (e *ilstEditor) replaceItems(typ string, payload []byte) {
	at := e.remove(func(it *matom) bool { return it.typ == typ })
	if payload != nil {
		e.insert(at, &matom{typ: typ, payload: payload})
	}
}

// setItem writes values to the item of type typ: one data atom per value,
// encoded like the existing item's first data atom (text, or an integer of
// the same type and width), or as the item type's default for a new item.
func (e *ilstEditor) setItem(typ string, values []string) error {
	if len(values) == 0 {
		e.replaceItems(typ, nil)
		return nil
	}
	spec, isInt := knownIntItems[typ]
	if i := slices.IndexFunc(e.ilst.children, func(it *matom) bool { return it.typ == typ }); i >= 0 {
		if dt, value, ok := firstData(e.ilst.children[i].payload); ok {
			switch dt {
			case dataTypeImplicit, dataTypeUTF8:
				isInt = false
			case dataTypeBESigned, dataTypeBEUnsigned:
				spec, isInt = intSpec{dt, len(value)}, true
			default:
				return fmt.Errorf("%w: %q holds data of type %d, not text or an integer", types.ErrUnsupportedWrite, typ, dt)
			}
		}
	}
	var payload []byte
	for _, v := range values {
		if !isInt {
			payload = append(payload, newDataAtom(dataTypeUTF8, []byte(v))...)
			continue
		}
		b, err := encodeInt(spec, v)
		if err != nil {
			return fmt.Errorf("%w: %q: %w", types.ErrUnsupportedWrite, typ, err)
		}
		payload = append(payload, newDataAtom(spec.dataType, b)...)
	}
	e.replaceItems(typ, payload)
	return nil
}

// encodeInt encodes decimal v as a big-endian integer of spec's width.
func encodeInt(spec intSpec, v string) ([]byte, error) {
	if !slices.Contains([]int{1, 2, 3, 4, 8}, spec.width) {
		return nil, fmt.Errorf("integer width %d", spec.width)
	}
	bits := uint(8 * spec.width)
	var u uint64
	if spec.dataType == dataTypeBESigned {
		n, err := strconv.ParseInt(v, 10, int(bits))
		if err != nil {
			return nil, fmt.Errorf("%q is not a %d-bit signed integer", v, bits)
		}
		u = uint64(n) //nolint:gosec // G115: two's-complement bits, truncated to the width below
	} else {
		n, err := strconv.ParseUint(v, 10, int(bits))
		if err != nil {
			return nil, fmt.Errorf("%q is not a %d-bit unsigned integer", v, bits)
		}
		u = n
	}
	b := make([]byte, spec.width)
	for i := range b {
		b[spec.width-1-i] = byte(u >> (8 * i)) //nolint:gosec // G115: taking one byte at a time
	}
	return b, nil
}

// setGnre writes values (ID3v1 genre indexes, as decimal text) to the gnre
// item, which stores each index plus one as a 16-bit integer.
func (e *ilstEditor) setGnre(values []string) error {
	var payload []byte
	for _, v := range values {
		n, err := strconv.ParseUint(v, 10, 16)
		if err != nil || n == 0xFFFF {
			return fmt.Errorf("%w: gnre %q is not a genre index from 0 to 65534", types.ErrUnsupportedWrite, v)
		}
		payload = append(payload, newDataAtom(dataTypeImplicit, binary.BigEndian.AppendUint16(nil, uint16(n+1)))...) //nolint:gosec // G115: n < 0xFFFF, checked above
	}
	e.replaceItems(keyGnre, payload)
	return nil
}

// setFreeform replaces every ---- item whose name matches key (ignoring
// case) with one ---- item holding values. The item keeps the first
// match's name spelling, or uses key.
func (e *ilstEditor) setFreeform(key string, values []string) error {
	if !utf8.ValidString(key) || key == "" || key != strings.ToUpper(key) {
		return fmt.Errorf("%w: %q is not an M4A item type or an upper-case freeform name", types.ErrUnsupportedWrite, key)
	}
	name, found := key, false
	at := e.remove(func(it *matom) bool {
		if it.typ != itemFreeform {
			return false
		}
		n, ok := freeformName(it.payload)
		if !ok || !strings.EqualFold(n, key) {
			return false
		}
		if !found {
			name, found = n, true
		}
		return true
	})
	if len(values) == 0 {
		return nil
	}
	payload := slices.Concat(
		newAtom("mean", []byte{0, 0, 0, 0}, []byte(freeformMean)),
		newAtom("name", []byte{0, 0, 0, 0}, []byte(name)),
	)
	for _, v := range values {
		payload = append(payload, newDataAtom(dataTypeUTF8, []byte(v))...)
	}
	e.insert(at, &matom{typ: itemFreeform, payload: payload})
	return nil
}

// applyField writes one typed-field edit to the keys chosen by fieldTargets.
func (e *ilstEditor) applyField(f types.FieldEdit) error {
	final := e.edits.Final
	if f.Field == types.FieldTrackNumber || f.Field == types.FieldTrackTotal {
		return e.setNumberPair(itemTrkn, final.TrackNumber, final.TrackTotal, true)
	}
	if f.Field == types.FieldDiscNumber || f.Field == types.FieldDiscTotal {
		return e.setNumberPair(itemDisk, final.DiscNumber, final.DiscTotal, false)
	}
	targets, err := e.fieldTargets(f)
	if err != nil {
		return err
	}
	for _, key := range targets {
		values := e.fieldKeyValues(f, key)
		if f.Field == types.FieldSeriesPart && key == itemMvi && len(values) > 0 && !isMovementIndex(values[0]) {
			return fmt.Errorf("%w: SeriesPart %q is not an integer from 1 to 32767, which \xA9mvi requires", types.ErrUnsupportedWrite, values[0])
		}
		if err := e.applyKey(key, values); err != nil {
			return err
		}
	}
	return nil
}

// isMovementIndex reports whether v fits ©mvi: an integer from 1 to 32767
// (a 16-bit signed integer; 0 means unset).
func isMovementIndex(v string) bool {
	n, err := strconv.ParseInt(v, 10, 16)
	return err == nil && n >= 1
}

// fieldTargets returns the raw keys a field edit writes: when setting, the
// highest-precedence key present in the source, or the canonical key when
// none is; when clearing, every present key.
func (e *ilstEditor) fieldTargets(f types.FieldEdit) ([]string, error) {
	canonical, ok := canonicalKeys[f.Field]
	if f.Field == types.FieldSeriesPart {
		canonical, ok = keySeriesPart, true
		if len(f.Values) > 0 && isMovementIndex(f.Values[0]) {
			canonical = itemMvi
		}
	}
	if !ok {
		return nil, fmt.Errorf("%w: %s cannot be written to M4A", types.ErrUnsupportedWrite, f.Field)
	}
	var present []string
	for _, b := range types.FieldBindings(types.FormatM4A) {
		if b.Field != f.Field {
			continue
		}
		for _, k := range b.Keys {
			if e.edits.Orig.Get(k) != nil {
				present = append(present, k)
			}
		}
	}
	switch {
	case len(f.Values) == 0:
		return present, nil
	case len(present) > 0:
		return present[:1], nil
	default:
		return []string{canonical}, nil
	}
}

// fieldKeyValues returns the values a field edit writes to key; nil deletes it.
func (e *ilstEditor) fieldKeyValues(f types.FieldEdit, key string) []string {
	final, orig := e.edits.Final, e.edits.Orig
	if f.Field == types.FieldYear {
		if len(f.Values) == 0 {
			return nil
		}
		year, err := strconv.Atoi(f.Values[0])
		if err != nil || year == 0 {
			return nil
		}
		return []string{yearValue(year, orig.GetFirst(key))}
	}
	if f.Field == types.FieldArtists {
		return slices.Clone(final.Artists)
	}
	if f.Field != types.FieldArtist {
		return slices.Clone(f.Values)
	}
	if final.Artist == "" {
		return nil
	}
	values := []string{final.Artist}
	if len(orig.Artists) > 1 {
		values = append(values, orig.Artists[1:]...)
	}
	return values
}

// yearValue writes year into the existing value old: a longer date such as
// "2021-05-03" keeps everything after its 4-digit year.
func yearValue(year int, old string) string {
	y := fmt.Sprintf("%04d", year)
	if len(old) > 4 && strings.Trim(old[:4], "0123456789") == "" {
		return y + old[4:]
	}
	return y
}

// setNumberPair writes a trkn (8-byte) or disk (6-byte) item holding n and
// total, or removes it when both are 0.
func (e *ilstEditor) setNumberPair(typ string, n, total int, trailer bool) error {
	if n == 0 && total == 0 {
		e.replaceItems(typ, nil)
		return nil
	}
	if n < 0 || total < 0 || n > 0xFFFF || total > 0xFFFF {
		return fmt.Errorf("%w: %s %d/%d does not fit in 16 bits", types.ErrUnsupportedWrite, typ, n, total)
	}
	value := []byte{0, 0}
	value = binary.BigEndian.AppendUint16(value, uint16(n))
	value = binary.BigEndian.AppendUint16(value, uint16(total))
	if trailer {
		value = append(value, 0, 0)
	}
	e.replaceItems(typ, newDataAtom(dataTypeImplicit, value))
	return nil
}

// applyCovr applies artwork operations, in order, to the images (data
// atoms) of the first covr item. Other covr children stay byte-identical.
// covr stores no picture type, so every image counts as a front cover: Add
// accepts only FrontCover or Other, and Remove of any other type is a no-op.
// A covr left without images is removed.
func applyCovr(ilst *matom, ops []types.ArtworkOp) error {
	if len(ops) == 0 {
		return nil
	}
	e := &ilstEditor{ilst: ilst}
	idx := slices.IndexFunc(ilst.children, func(it *matom) bool { return it.typ == itemCovr })
	var children []subAtom
	if idx >= 0 {
		var ok bool
		if children, ok = subAtoms(ilst.children[idx].payload); !ok {
			return fmt.Errorf("%w: malformed covr item", types.ErrUnsupportedWrite)
		}
	}
	removeAll := false
	for _, op := range ops {
		switch op.Kind {
		case types.ArtworkOpAdd:
			data, err := covrData(op.Art)
			if err != nil {
				return err
			}
			children = append(children, subAtom{typ: atomData, raw: data})
		case types.ArtworkOpRemove, types.ArtworkOpRemoveAll:
			if op.Kind == types.ArtworkOpRemove && op.Type != types.ArtworkFrontCover {
				continue
			}
			children = slices.DeleteFunc(children, func(c subAtom) bool { return c.typ == atomData })
			removeAll = true
		default:
			return fmt.Errorf("%w: unknown artwork operation %d", types.ErrUnsupportedWrite, op.Kind)
		}
	}
	var payload []byte
	if slices.ContainsFunc(children, func(c subAtom) bool { return c.typ == atomData }) {
		for _, c := range children {
			payload = append(payload, c.raw...)
		}
	}
	if removeAll || payload == nil {
		e.replaceItems(itemCovr, payload)
		return nil
	}
	if idx >= 0 {
		ilst.children[idx] = &matom{typ: itemCovr, ext: ilst.children[idx].ext, payload: payload}
		return nil
	}
	e.insert(-1, &matom{typ: itemCovr, payload: payload})
	return nil
}

// covrData encodes a as a covr data atom.
func covrData(a types.Artwork) ([]byte, error) {
	if a.Type != types.ArtworkFrontCover && a.Type != types.ArtworkOther {
		return nil, fmt.Errorf("%w: M4A cover art has no picture type; %s cannot be stored", types.ErrUnsupportedWrite, a.Type)
	}
	var code uint32
	switch a.MIMEType {
	case mimeTypeJPEG:
		code = covrJPEG
	case mimeTypePNG:
		code = covrPNG
	case mimeTypeBMP:
		code = covrBMP
	default:
		return nil, fmt.Errorf("%w: M4A cover art must be JPEG, PNG or BMP, not %q", types.ErrUnsupportedWrite, a.MIMEType)
	}
	return newDataAtom(code, a.Data), nil
}

// subAtom is one child atom inside an ilst item.
type subAtom struct {
	typ  string
	raw  []byte // the whole atom, header included
	body []byte // the atom's content after its header
}

// subAtoms splits an ilst item's payload into its child atoms. ok is false
// when the payload does not divide exactly into atoms.
func subAtoms(b []byte) ([]subAtom, bool) {
	var out []subAtom
	for off := 0; off < len(b); {
		rest := b[off:]
		if len(rest) < 8 {
			return nil, false
		}
		size, hdr := uint64(binary.BigEndian.Uint32(rest)), uint64(8)
		if size == 1 {
			if len(rest) < 16 {
				return nil, false
			}
			size, hdr = binary.BigEndian.Uint64(rest[8:]), 16
		}
		if size < hdr || size > uint64(len(rest)) {
			return nil, false
		}
		out = append(out, subAtom{typ: string(rest[4:8]), raw: rest[:size], body: rest[hdr:size]})
		off += int(size) //nolint:gosec // G115: size <= len(rest)
	}
	return out, true
}

// firstData returns the type and value of an item's first data atom.
func firstData(payload []byte) (dataType uint32, value []byte, ok bool) {
	children, ok := subAtoms(payload)
	if !ok {
		return 0, nil, false
	}
	for _, c := range children {
		if c.typ == atomData && len(c.body) >= 8 {
			return binary.BigEndian.Uint32(c.body) & 0x00FFFFFF, c.body[8:], true
		}
	}
	return 0, nil, false
}

// freeformName returns the name of a ---- item, as the reader decodes it.
func freeformName(payload []byte) (string, bool) {
	children, ok := subAtoms(payload)
	if !ok {
		return "", false
	}
	for _, c := range children {
		if c.typ == "name" && len(c.body) > 4 {
			return string(c.body[4:]), true
		}
	}
	return "", false
}

// newAtom builds an atom with an 8-byte header around the concatenated parts.
func newAtom(typ string, parts ...[]byte) []byte {
	n := 8
	for _, p := range parts {
		n += len(p)
	}
	b := binary.BigEndian.AppendUint32(make([]byte, 0, n), uint32(n))
	b = append(b, typ...)
	for _, p := range parts {
		b = append(b, p...)
	}
	return b
}

// newDataAtom builds a data atom of the given type, locale 0.
func newDataAtom(dataType uint32, value []byte) []byte {
	return newAtom(atomData, binary.BigEndian.AppendUint32(nil, dataType), []byte{0, 0, 0, 0}, value)
}
