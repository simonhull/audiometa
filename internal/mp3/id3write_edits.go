package mp3

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/simonhull/audiometa/internal/types"
)

// Text encodings of ID3v2 frames.
const (
	encLatin1 byte = 0
	encUTF16  byte = 1
	encUTF8   byte = 3
)

// standardTextFrameIDs are the raw keys written as their own text frame; any
// other key is written as a TXXX frame with the key as its description.
var standardTextFrameIDs = setOf(`
	TALB TBPM TCOM TCON TCOP TDAT TDEN TDLY TDOR TDRC TDRL TDTG TENC TEXT TFLT TIME TIPL TIT1 TIT2
	TIT3 TKEY TLAN TLEN TMCL TMED TMOO TOAL TOFN TOLY TOPE TORY TOWN TPE1 TPE2 TPE3 TPE4 TPOS TPRO
	TPUB TRCK TRDA TRSN TRSO TSIZ TSO2 TSOA TSOC TSOP TSOT TSRC TSSE TSST TYER TCMP GRP1 MVNM MVIN`)

// Frame IDs that exist in only one ID3v2 version.
var (
	v24OnlyIDs = setOf("TDRC TDOR TDRL TDEN TDTG TIPL TMCL TMOO TPRO TSOA TSOP TSOT TSST")
	v23OnlyIDs = setOf("TYER TDAT TIME TORY TRDA TSIZ IPLS")
)

// canonicalKeys is the raw key a field is written to when the source has
// none of the field's keys, as a whitespace-separated "Field key" table
// (keys with spaces use "_"). Year is handled apart (TDRC or TYER by version).
var canonicalKeys = func() map[types.Field]string {
	byName := pairsOf(`
		Title TIT2  Subtitle TIT3  Artist TPE1  Artists TPE1  Album TALB  AlbumArtist TPE2
		Genres TCON  Composers TCOM  TrackNumber TRCK  TrackTotal TRCK  DiscNumber TPOS
		DiscTotal TPOS  Grouping GRP1  Narrator NARRATOR  Series SERIES  SeriesPart SERIES_PART
		Publisher PUBLISHER  ISBN ISBN  ASIN ASIN  Language LANGUAGE  Description DESCRIPTION`)
	m := make(map[types.Field]string, len(byName))
	for f := range types.AllFields() {
		if key, ok := byName[f.String()]; ok {
			m[f] = strings.ReplaceAll(key, "_", " ")
		}
	}
	return m
}()

// Raw keys with special value encodings.
const (
	keyTDRC = "TDRC"
	keyTYER = "TYER"
)

// setOf builds a set of the whitespace-separated words of s.
func setOf(s string) map[string]bool {
	m := make(map[string]bool)
	for w := range strings.FieldsSeq(s) {
		m[w] = true
	}
	return m
}

// apply applies edits to the frame list: key edits, then field edits (each in
// slice order), then artwork operations.
func (t *wtag) apply(edits *types.EditSet) error {
	for _, e := range edits.Keys {
		if err := t.applyKeyEdit(e.Key, e.Values); err != nil {
			return err
		}
	}
	for _, e := range edits.Fields {
		if err := t.applyFieldEdit(e, edits); err != nil {
			return err
		}
	}
	return t.applyArtwork(edits.Artwork)
}

// drmCheck returns ErrDRMProtected when any frame is encrypted or an AENC
// frame exists. (ID3v2.2 CRA/CRM frames are refused while reading.)
func (t *wtag) drmCheck() error {
	encFlag := uint16(v24FrameEncryption)
	if t.outVersion == 3 {
		encFlag = v23FrameEncryption
	}
	for _, f := range t.frames {
		if f.id == "AENC" {
			return fmt.Errorf("%w: AENC frame", types.ErrDRMProtected)
		}
		if f.flags&encFlag != 0 {
			return fmt.Errorf("%w: encrypted %s frame", types.ErrDRMProtected, f.id)
		}
	}
	return nil
}

// normalizeKey maps a raw key read from an ID3v2.2 tag (normalized by the
// reader to ID3v2.3, or kept as a 3-character ID) to the ID3v2.4 frame it
// was converted to.
func (t *wtag) normalizeKey(key string) string {
	if t.inVersion != 2 {
		return key
	}
	if key == keyTYER {
		return keyTDRC
	}
	if id, ok := v22ToV24[key]; ok {
		return id
	}
	return key
}

// applyKeyEdit replaces every frame holding raw key with one frame holding
// values, or removes them when values is empty.
func (t *wtag) applyKeyEdit(key string, values []string) error {
	key = t.normalizeKey(key)
	if strings.EqualFold(key, mediaMarkersDescription) {
		return fmt.Errorf("%w: %s holds chapters, which are not editable", types.ErrUnsupportedWrite, key)
	}
	for _, v := range values {
		if !utf8.ValidString(v) || strings.ContainsRune(v, 0) {
			return fmt.Errorf("%w: value of %s is not valid UTF-8 or contains NUL", types.ErrUnsupportedWrite, key)
		}
	}
	if t.isTextKey(key) {
		return t.setTextFrame(key, values)
	}
	return t.setTXXX(key, values)
}

// isTextKey reports whether key is written as its own text frame: a standard
// text frame ID, or another text frame ID already present in the tag (so
// such a frame can be rewritten and deleted).
func (t *wtag) isTextKey(key string) bool {
	if standardTextFrameIDs[key] {
		return true
	}
	return len(key) == 4 && isTextFrameID(key) &&
		slices.ContainsFunc(t.frames, func(f wframe) bool { return f.id == key })
}

// setTextFrame replaces every frame with ID id by one text frame of values,
// inserted where the first one was.
func (t *wtag) setTextFrame(id string, values []string) error {
	if len(values) > 0 {
		if (t.outVersion == 3 && v24OnlyIDs[id]) || (t.outVersion == 4 && v23OnlyIDs[id]) {
			return fmt.Errorf("%w: %s cannot be written to an ID3v2.%d tag", types.ErrUnsupportedWrite, id, t.outVersion)
		}
	}
	at := t.remove(func(f wframe) (bool, error) { return f.id == id, nil })
	if len(values) == 0 {
		return nil
	}
	t.insert(at, wframe{id: id, payload: encodeText(t.outVersion, values), edited: true})
	return nil
}

// setTXXX replaces every TXXX frame whose upper-cased description is key by
// one TXXX frame of values. The new frame keeps the first match's
// description case, or uses key when nothing matched.
func (t *wtag) setTXXX(key string, values []string) error {
	desc, found := key, false
	var bodyErr error
	at := t.remove(func(f wframe) (bool, error) {
		if f.id != frameIDTXXX {
			return false, nil
		}
		d, ok := t.txxxDescription(f)
		if !ok {
			bodyErr = fmt.Errorf("%w: a compressed TXXX frame cannot be matched against %s", types.ErrUnsupportedWrite, key)
			return false, bodyErr
		}
		if !strings.EqualFold(d, key) {
			return false, nil
		}
		if !found {
			desc, found = d, true
		}
		return true, nil
	})
	if bodyErr != nil {
		return bodyErr
	}
	if len(values) == 0 {
		return nil
	}
	t.insert(at, wframe{id: frameIDTXXX, payload: encodeTXXX(t.outVersion, desc, values), edited: true})
	return nil
}

// txxxDescription decodes a TXXX frame's description the way the reader does.
// ok is false when the frame is compressed.
func (t *wtag) txxxDescription(f wframe) (string, bool) {
	if t.compressed(f) {
		return "", false
	}
	body, skip := frameBody(t.outVersion, 0, f.flags, f.payload)
	if skip != "" || len(body) < 2 {
		return "", true
	}
	enc := body[0]
	nul := findNullTerminator(body[1:], enc)
	if nul < 0 {
		return "", true
	}
	return decodeText(body[1:1+nul], enc), true
}

// compressed reports whether f has the compression flag of the output version.
func (t *wtag) compressed(f wframe) bool {
	if t.outVersion == 3 {
		return f.flags&v23FrameCompression != 0
	}
	return f.flags&v24FrameCompression != 0
}

// remove drops every frame match selects and returns the index the first
// dropped frame had in the resulting list, or -1 when none was dropped. A
// match error stops the walk and leaves the list unchanged.
func (t *wtag) remove(match func(wframe) (bool, error)) int {
	at := -1
	kept := make([]wframe, 0, len(t.frames))
	for _, f := range t.frames {
		drop, err := match(f)
		if err != nil {
			return -1
		}
		if drop {
			if at < 0 {
				at = len(kept)
			}
			continue
		}
		kept = append(kept, f)
	}
	t.frames = kept
	return at
}

// insert puts f at index at, or appends it when at is negative.
func (t *wtag) insert(at int, f wframe) {
	if at < 0 || at > len(t.frames) {
		t.frames = append(t.frames, f)
		return
	}
	t.frames = slices.Insert(t.frames, at, f)
}

// applyFieldEdit writes one typed-field edit to the keys chosen by
// fieldTargetKeys.
func (t *wtag) applyFieldEdit(e types.FieldEdit, edits *types.EditSet) error {
	if e.Field == types.FieldComment {
		return t.applyComment(e.Values)
	}
	targets, err := t.fieldTargetKeys(e, edits)
	if err != nil {
		return err
	}
	for _, key := range targets {
		if err := t.applyKeyEdit(key, fieldKeyValues(e, key, edits)); err != nil {
			return err
		}
	}
	return nil
}

// fieldTargetKeys returns the raw keys a field edit writes: when setting, the
// highest-precedence key present in the source, or the canonical key when
// none is; when clearing, every present key.
func (t *wtag) fieldTargetKeys(e types.FieldEdit, edits *types.EditSet) ([]string, error) {
	canonical, ok := canonicalKeys[e.Field]
	if e.Field == types.FieldYear {
		canonical, ok = keyTDRC, true
		if t.outVersion == 3 {
			canonical = keyTYER
		}
	}
	if !ok {
		return nil, fmt.Errorf("%w: %s cannot be written to MP3", types.ErrUnsupportedWrite, e.Field)
	}
	var present []string
	for _, b := range types.FieldBindings(types.FormatMP3) {
		if b.Field != e.Field {
			continue
		}
		for _, k := range b.Keys {
			if edits.Orig.Get(k) != nil {
				present = append(present, k)
			}
		}
	}
	if !isSetting(e, edits) {
		return present, nil
	}
	if len(present) > 0 {
		return present[:1], nil
	}
	return []string{canonical}, nil
}

// isSetting reports whether a field edit leaves a value to write. Track and
// disc numbers and totals share one frame, so clearing one of them still
// writes the frame while the other is set.
func isSetting(e types.FieldEdit, edits *types.EditSet) bool {
	if isTrackField(e.Field) {
		return edits.Final.TrackNumber != 0 || edits.Final.TrackTotal != 0
	}
	if isDiscField(e.Field) {
		return edits.Final.DiscNumber != 0 || edits.Final.DiscTotal != 0
	}
	return len(e.Values) > 0
}

// isTrackField reports whether f is stored in TRCK.
func isTrackField(f types.Field) bool {
	return f == types.FieldTrackNumber || f == types.FieldTrackTotal
}

// isDiscField reports whether f is stored in TPOS.
func isDiscField(f types.Field) bool {
	return f == types.FieldDiscNumber || f == types.FieldDiscTotal
}

// fieldKeyValues returns the values a field edit writes to key; nil deletes it.
func fieldKeyValues(e types.FieldEdit, key string, edits *types.EditSet) []string {
	final := edits.Final
	switch {
	case e.Field == types.FieldYear:
		if len(e.Values) == 0 {
			return nil
		}
		year, err := strconv.Atoi(e.Values[0])
		if err != nil || year == 0 {
			return nil
		}
		return []string{yearValue(year, edits.Orig.GetFirst(key))}
	case isTrackField(e.Field):
		return numberTotal(final.TrackNumber, final.TrackTotal)
	case isDiscField(e.Field):
		return numberTotal(final.DiscNumber, final.DiscTotal)
	case e.Field == types.FieldArtists:
		return slices.Clone(final.Artists)
	case e.Field == types.FieldArtist:
		if final.Artist == "" {
			return nil
		}
		values := []string{final.Artist}
		if len(edits.Orig.Artists) > 1 {
			values = append(values, edits.Orig.Artists[1:]...)
		}
		return values
	default:
		return slices.Clone(e.Values)
	}
}

// yearValue writes year into the existing value old: a longer date such as
// "2021-05-03" keeps everything after its 4-digit year.
func yearValue(year int, old string) string {
	y := fmt.Sprintf("%04d", year)
	if len(old) > 4 && isDigits(old[:4]) {
		return y + old[4:]
	}
	return y
}

// isDigits reports whether s is all ASCII digits.
func isDigits(s string) bool {
	for _, c := range []byte(s) {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// numberTotal encodes a TRCK/TPOS value: "n", "n/t", or nil when both are 0.
func numberTotal(n, total int) []string {
	switch {
	case n == 0 && total == 0:
		return nil
	case total == 0:
		return []string{strconv.Itoa(n)}
	default:
		return []string{strconv.Itoa(n) + "/" + strconv.Itoa(total)}
	}
}

// applyComment sets or clears the comment. Setting rewrites the text of every
// COMM frame with an empty description (keeping its language), or appends one
// in English. Clearing removes those frames and the described COMM frames
// the reader falls back to, so the comment reads back empty.
func (t *wtag) applyComment(values []string) error {
	if len(values) == 0 {
		t.remove(func(f wframe) (bool, error) {
			c, ok := t.parseCOMM(f)
			return ok && !strings.HasPrefix(c.desc, "iTun"), nil
		})
		return nil
	}
	text := values[0]
	if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return fmt.Errorf("%w: Comment is not valid UTF-8 or contains NUL", types.ErrUnsupportedWrite)
	}
	replaced := false
	for i, f := range t.frames {
		c, ok := t.parseCOMM(f)
		if !ok || c.desc != "" {
			continue
		}
		t.frames[i] = wframe{id: frameIDCOMM, payload: encodeCOMM(t.outVersion, c.lang, text), edited: true}
		replaced = true
	}
	if !replaced {
		t.frames = append(t.frames, wframe{id: frameIDCOMM, payload: encodeCOMM(t.outVersion, "eng", text), edited: true})
	}
	return nil
}

// commInfo is the language and description of a COMM frame.
type commInfo struct {
	lang, desc string
}

// parseCOMM decodes a COMM frame's language and description; ok is false for
// other frames and for COMM frames that cannot be read (compressed or short).
func (t *wtag) parseCOMM(f wframe) (commInfo, bool) {
	if f.id != frameIDCOMM {
		return commInfo{}, false
	}
	body, skip := frameBody(t.outVersion, 0, f.flags, f.payload)
	if skip != "" || len(body) < 4 {
		return commInfo{}, false
	}
	enc, data := body[0], body[4:]
	c := commInfo{lang: string(body[1:4])}
	if nul := findNullTerminator(data, enc); nul >= 0 {
		c.desc = decodeText(data[:nul], enc)
	}
	return c, true
}

// applyArtwork applies the artwork operations, in order, to the APIC frames.
func (t *wtag) applyArtwork(ops []types.ArtworkOp) error {
	for _, op := range ops {
		switch op.Kind {
		case types.ArtworkOpAdd:
			payload, err := encodeAPIC(t.outVersion, op.Art)
			if err != nil {
				return err
			}
			t.insert(t.lastAPIC()+1, wframe{id: frameIDAPIC, payload: payload, edited: true})
		case types.ArtworkOpRemove:
			var typeErr error
			t.remove(func(f wframe) (bool, error) {
				if f.id != frameIDAPIC {
					return false, nil
				}
				pt, ok := t.pictureType(f)
				if !ok {
					typeErr = fmt.Errorf("%w: cannot read the picture type of a compressed APIC frame", types.ErrUnsupportedWrite)
					return false, typeErr
				}
				return types.ArtworkTypeFromPictureType(uint32(pt)) == op.Type, nil
			})
			if typeErr != nil {
				return typeErr
			}
		case types.ArtworkOpRemoveAll:
			t.remove(func(f wframe) (bool, error) { return f.id == frameIDAPIC, nil })
		default:
			return fmt.Errorf("%w: unknown artwork operation %d", types.ErrUnsupportedWrite, op.Kind)
		}
	}
	return nil
}

// lastAPIC returns the index of the last APIC frame, or len(frames)-1 when
// there is none (so an insert after it appends).
func (t *wtag) lastAPIC() int {
	for i, f := range slices.Backward(t.frames) {
		if f.id == frameIDAPIC {
			return i
		}
	}
	return len(t.frames) - 1
}

// pictureType returns an APIC frame's picture-type byte: after the encoding
// byte and the NUL-terminated MIME type. ok is false when the frame is
// compressed. A malformed frame reads as type 0 (Other).
func (t *wtag) pictureType(f wframe) (byte, bool) {
	if t.compressed(f) {
		return 0, false
	}
	body, skip := frameBody(t.outVersion, 0, f.flags, f.payload)
	if skip != "" || len(body) < 2 {
		return 0, true
	}
	nul := strings.IndexByte(string(body[1:]), 0)
	if nul < 0 || 1+nul+1 >= len(body) {
		return 0, true
	}
	return body[1+nul+1], true
}

// textEncoding picks the encoding for strs: UTF-8 in ID3v2.4; in ID3v2.3,
// ISO-8859-1 when every string is Latin-1 and reads back unchanged, else
// UTF-16 with a BOM.
func textEncoding(version byte, strs ...string) byte {
	if version == 4 {
		return encUTF8
	}
	for _, s := range strs {
		if !latin1RoundTrips(s) {
			return encUTF16
		}
	}
	return encLatin1
}

// latin1RoundTrips reports whether s encodes to ISO-8859-1 and decodes back
// to s. The reader decodes valid UTF-8 as UTF-8, so a non-ASCII Latin-1
// encoding that happens to be valid UTF-8 does not round-trip.
func latin1RoundTrips(s string) bool {
	ascii := true
	for _, r := range s {
		if r > 0xFF {
			return false
		}
		if r >= utf8.RuneSelf {
			ascii = false
		}
	}
	return ascii || !utf8.Valid(encodeString(encLatin1, s))
}

// encodeString encodes s (no terminator) in enc. UTF-16 gets a little-endian BOM.
func encodeString(enc byte, s string) []byte {
	switch enc {
	case encLatin1:
		out := make([]byte, 0, len(s))
		for _, r := range s {
			out = append(out, byte(r)) //nolint:gosec // G115: callers pick Latin-1 only when every rune is <= 0xFF
		}
		return out
	case encUTF16:
		out := []byte{0xFF, 0xFE}
		for _, u := range utf16.Encode([]rune(s)) {
			out = append(out, byte(u), byte(u>>8)) //nolint:gosec // G115: splitting a 16-bit unit into its two bytes
		}
		return out
	default:
		return []byte(s)
	}
}

// terminator returns the NUL terminator of enc.
func terminator(enc byte) []byte {
	if enc == encUTF16 {
		return []byte{0, 0}
	}
	return []byte{0}
}

// encodeText builds a text-frame payload; multiple values are NUL-separated.
func encodeText(version byte, values []string) []byte {
	joined := strings.Join(values, "\x00")
	enc := textEncoding(version, joined)
	return append([]byte{enc}, encodeString(enc, joined)...)
}

// encodeTXXX builds a TXXX payload: [enc][desc][NUL][values, NUL-separated].
func encodeTXXX(version byte, desc string, values []string) []byte {
	joined := strings.Join(values, "\x00")
	enc := textEncoding(version, desc, joined)
	out := append([]byte{enc}, encodeString(enc, desc)...)
	out = append(out, terminator(enc)...)
	return append(out, encodeString(enc, joined)...)
}

// encodeCOMM builds a COMM payload with an empty description:
// [enc][lang(3)][NUL][text].
func encodeCOMM(version byte, lang, text string) []byte {
	enc := textEncoding(version, text)
	out := []byte{enc}
	out = append(out, (lang + "\x00\x00\x00")[:3]...)
	out = append(out, encodeString(enc, "")...)
	out = append(out, terminator(enc)...)
	return append(out, encodeString(enc, text)...)
}

// encodeAPIC builds an APIC payload: [enc][MIME][NUL][type][desc][NUL][data].
func encodeAPIC(version byte, a types.Artwork) ([]byte, error) {
	for _, r := range a.MIMEType {
		if r == 0 || r >= utf8.RuneSelf {
			return nil, fmt.Errorf("%w: MIME type %q is not ASCII", types.ErrUnsupportedWrite, a.MIMEType)
		}
	}
	if a.Type < types.ArtworkOther || a.Type > types.ArtworkPublisherLogotype {
		return nil, fmt.Errorf("%w: picture type %d", types.ErrUnsupportedWrite, a.Type)
	}
	if !utf8.ValidString(a.Description) || strings.ContainsRune(a.Description, 0) {
		return nil, fmt.Errorf("%w: artwork description is not valid UTF-8 or contains NUL", types.ErrUnsupportedWrite)
	}
	enc := textEncoding(version, a.Description)
	out := make([]byte, 0, len(a.MIMEType)+len(a.Description)*2+len(a.Data)+8)
	out = append(out, enc)
	out = append(out, a.MIMEType...)
	out = append(out, 0, byte(a.Type))
	out = append(out, encodeString(enc, a.Description)...)
	out = append(out, terminator(enc)...)
	return append(out, a.Data...), nil
}
