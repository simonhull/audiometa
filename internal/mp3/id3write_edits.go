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

// standardTextFrameIDs are the raw keys written as their own text frame. Other
// keys are written as a TXXX frame with the key as its description, unless
// isFalseTXXX refuses them.
var standardTextFrameIDs = setOf(`
	TALB TBPM TCOM TCON TCOP TDAT TDEN TDLY TDOR TDRC TDRL TDTG TENC TEXT TFLT TIME TIPL TIT1 TIT2
	TIT3 TKEY TLAN TLEN TMCL TMED TMOO TOAL TOFN TOLY TOPE TORY TOWN TPE1 TPE2 TPE3 TPE4 TPOS TPRO
	TPUB TRCK TRDA TRSN TRSO TSIZ TSO2 TSOA TSOC TSOP TSOT TSRC TSSE TSST TYER TCMP GRP1 MVNM MVIN`)

// unwritableFrameIDs are the registered ID3v2.3 and ID3v2.4 frames, and the
// common iTunes and legacy extensions, that a raw key cannot write: non-text
// frames, and text frames outside standardTextFrameIDs. It is a closed list and
// not a test of the key's shape, because ISBN, ASIN, LANG and MOOD also have
// four upper-case characters and are ordinary TXXX descriptions.
var unwritableFrameIDs = setOf(`
	AENC APIC ASPI ATXT CHAP COMM COMR CTOC ENCR EQU2 EQUA ETCO GEOB GRID IPLS LINK MCDI MLLT OWNE
	PCNT PCST POPM POSS PRIV RBUF RGAD RVA2 RVAD RVRB SEEK SIGN SYLT SYTC TCAT TDES TGID TKWD TXXX
	UFID USER USLT WCOM WCOP WFED WOAF WOAR WOAS WORS WPAY WPUB WXXX XDOR XRVA XSOA XSOP XSOT`)

// Frame IDs that exist in only one ID3v2 version. TSOA, TSOP and TSOT are
// ID3v2.4 by the spec but left out on purpose: iTunes and other taggers write
// them to ID3v2.3 too, and ID3v2.3 has no sort frames of its own.
var (
	v24OnlyIDs = setOf("TDRC TDOR TDRL TDEN TDTG TIPL TMCL TMOO TPRO TSST")
	v23OnlyIDs = setOf("TYER TDAT TIME TORY TRDA TSIZ IPLS")
)

// canonicalKeys is the raw key a field is written to when the source has
// none of the field's keys, as a whitespace-separated "Field key" table
// (keys with spaces use "_"). Year is handled apart by applyYear (TDRC or TYER by version).
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
	t.final = edits.Final
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
	if key == "" {
		return fmt.Errorf("%w: empty tag key", types.ErrUnsupportedWrite)
	}
	if !utf8.ValidString(key) || strings.ContainsRune(key, 0) {
		return fmt.Errorf("%w: key %q is not valid UTF-8 or contains NUL", types.ErrUnsupportedWrite, key)
	}
	if strings.EqualFold(key, mediaMarkersDescription) {
		return fmt.Errorf("%w: %s holds chapters, which are not editable", types.ErrUnsupportedWrite, key)
	}
	if key == frameIDUSLT {
		if len(values) > 0 {
			return fmt.Errorf("%w: %s is written through the Lyrics field", types.ErrUnsupportedWrite, key)
		}
		t.removeLyrics()
		return nil
	}
	for _, v := range values {
		if !utf8.ValidString(v) || strings.ContainsRune(v, 0) {
			return fmt.Errorf("%w: value of %s is not valid UTF-8 or contains NUL", types.ErrUnsupportedWrite, key)
		}
	}
	if owner, ok := strings.CutPrefix(key, ufidKeyPrefix); ok {
		return t.setUFID(owner, values)
	}
	if t.isTextKey(key) {
		return t.setTextFrame(key, values)
	}
	if t.isFalseTXXX(key) {
		return fmt.Errorf("%w: %q names an ID3v2 frame that is not a writable text frame", types.ErrUnsupportedWrite, key)
	}
	return t.setTXXX(key, values)
}

// isFalseTXXX reports whether writing key as a TXXX frame would only pretend
// to write the frame it names: key is exactly a frame ID, and no TXXX frame in
// the tag already has that description. Frame IDs are upper-case, so "text" or
// "user" stays an ordinary description. A TXXX frame that is really there stays
// editable, whatever its description.
func (t *wtag) isFalseTXXX(key string) bool {
	if !unwritableFrameIDs[key] {
		return false
	}
	return !slices.ContainsFunc(t.frames, func(f wframe) bool {
		if f.id != frameIDTXXX {
			return false
		}
		d, ok := t.txxxDescription(f)
		return ok && strings.EqualFold(d, key)
	})
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

// isShadowTXXX reports whether f is a TXXX frame whose description is id in
// any case: the reader files its values under the same raw key as the id
// frame. A compressed TXXX is never read, so it never matches.
func (t *wtag) isShadowTXXX(f wframe, id string) bool {
	if f.id != frameIDTXXX {
		return false
	}
	d, ok := t.txxxDescription(f)
	return ok && strings.EqualFold(d, id)
}

// setTextFrame replaces every frame with ID id, and every TXXX frame the
// reader files under the same key, by one text frame of values, inserted
// where the first one was.
func (t *wtag) setTextFrame(id string, values []string) error {
	if len(values) > 0 {
		if (t.outVersion == 3 && v24OnlyIDs[id]) || (t.outVersion == 4 && v23OnlyIDs[id]) {
			return fmt.Errorf("%w: %s cannot be written to an ID3v2.%d tag", types.ErrUnsupportedWrite, id, t.outVersion)
		}
	}
	at := t.remove(func(f wframe) (bool, error) { return f.id == id || t.isShadowTXXX(f, id), nil })
	if len(values) == 0 {
		return nil
	}
	t.insert(at, wframe{id: id, payload: encodeText(t.outVersion, values)})
	return nil
}

// setTXXX replaces every TXXX frame whose description matches key, ignoring
// case, by one TXXX frame of values, put where the first match was. The frame
// keeps the first match's description as it is spelled, so frames whose
// descriptions differ only in case become one, named as the first of them.
// With no match it is new, and named as the caller's tags spell key
// ([types.Tags.Name]).
func (t *wtag) setTXXX(key string, values []string) error {
	desc, found := t.newName(key), false
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
	t.insert(at, wframe{id: frameIDTXXX, payload: encodeTXXX(t.outVersion, desc, values)})
	return nil
}

// newName returns the description of a new TXXX frame for key: the spelling
// the caller's tags give it when that is key in another case, else key.
func (t *wtag) newName(key string) string {
	if t.final == nil {
		return key
	}
	if name := t.final.Name(key); strings.EqualFold(name, key) {
		return name
	}
	return key
}

// maxUFIDIdentifier is the longest identifier a UFID frame may hold, in bytes.
const maxUFIDIdentifier = 64

// setUFID replaces every UFID frame of owner by one holding values[0] as its
// identifier, put where the first one was, or removes them when values is
// empty. The owner is matched exactly, as the spec has it: it is a URL or an
// email address naming the database the identifier belongs to.
func (t *wtag) setUFID(owner string, values []string) error {
	if !validUFIDOwner(owner) {
		return fmt.Errorf("%w: UFID owner %q is not printable ASCII", types.ErrUnsupportedWrite, owner)
	}
	if len(values) > 1 {
		return fmt.Errorf("%w: a UFID frame holds one identifier, not %d", types.ErrUnsupportedWrite, len(values))
	}
	if len(values) == 1 && (values[0] == "" || len(values[0]) > maxUFIDIdentifier) {
		return fmt.Errorf("%w: a UFID identifier is 1 to %d bytes, not %d", types.ErrUnsupportedWrite, maxUFIDIdentifier, len(values[0]))
	}
	var bodyErr error
	at := t.remove(func(f wframe) (bool, error) {
		if f.id != frameIDUFID {
			return false, nil
		}
		if t.compressed(f) {
			bodyErr = fmt.Errorf("%w: a compressed UFID frame cannot be matched against %s", types.ErrUnsupportedWrite, owner)
			return false, bodyErr
		}
		body, skip := frameBody(t.outVersion, 0, f.flags, f.payload)
		if skip != "" {
			return false, nil
		}
		o, _, ok := parseUFID(body)
		return ok && o == owner, nil
	})
	if bodyErr != nil {
		return bodyErr
	}
	if len(values) == 0 {
		return nil
	}
	payload := slices.Concat([]byte(owner), []byte{0}, []byte(values[0]))
	t.insert(at, wframe{id: frameIDUFID, payload: payload})
	return nil
}

// validUFIDOwner reports whether owner can name a new UFID frame: non-empty
// printable ASCII, which ISO-8859-1 stores as it is.
func validUFIDOwner(owner string) bool {
	if owner == "" {
		return false
	}
	for _, c := range []byte(owner) {
		if c < 0x20 || c > 0x7E {
			return false
		}
	}
	return true
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
	if e.Field == types.FieldLyrics {
		return t.applyLyrics(e.Values)
	}
	if e.Field == types.FieldYear {
		return t.applyYear(e, edits)
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

// applyYear writes Year to the tag version's own frame (TDRC in ID3v2.4, TYER
// in ID3v2.3) and removes the other version's frame, which that version
// cannot hold. Clearing Year removes both. A year the reader would drop
// (outside 1900-2100) is refused, as it would read back as 0.
func (t *wtag) applyYear(e types.FieldEdit, edits *types.EditSet) error {
	if len(e.Values) > 0 {
		if year, err := strconv.Atoi(e.Values[0]); err == nil && year != 0 && !validID3Year(year) {
			return fmt.Errorf("%w: Year %d is outside 1900-2100, the years an ID3 tag can hold", types.ErrUnsupportedWrite, year)
		}
	}
	own, other := keyTDRC, keyTYER
	if t.outVersion == 3 {
		own, other = keyTYER, keyTDRC
	}
	if err := t.applyKeyEdit(other, nil); err != nil {
		return err
	}
	return t.applyKeyEdit(own, fieldKeyValues(e, own, edits))
}

// fieldTargetKeys returns the raw keys a field edit writes: when setting, the
// highest-precedence key present in the source, or the canonical key when
// none is; when clearing, every present key.
func (t *wtag) fieldTargetKeys(e types.FieldEdit, edits *types.EditSet) ([]string, error) {
	canonical, ok := canonicalKeys[e.Field]
	if !ok {
		return nil, fmt.Errorf("%w: %s cannot be written to MP3", types.ErrUnsupportedWrite, e.Field)
	}
	return types.FieldTargets(types.FormatMP3, e, edits, canonical, isSetting(e, edits)), nil
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

// fieldKeyValues returns the values a field edit writes to key; nil deletes it. A track or
// disc edit writes the number and total together; every other field goes through
// [types.FieldValues].
func fieldKeyValues(e types.FieldEdit, key string, edits *types.EditSet) []string {
	final := edits.Final
	switch {
	case isTrackField(e.Field):
		return numberTotal(final.TrackNumber, final.TrackTotal)
	case isDiscField(e.Field):
		return numberTotal(final.DiscNumber, final.DiscTotal)
	default:
		return types.FieldValues(e, key, edits)
	}
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
		t.frames[i] = wframe{id: frameIDCOMM, payload: encodeLangText(t.outVersion, c.lang, "", text)}
		replaced = true
	}
	if !replaced {
		t.frames = append(t.frames, wframe{id: frameIDCOMM, payload: encodeLangText(t.outVersion, "eng", "", text)})
	}
	return nil
}

// parseCOMM decodes a COMM frame; ok is false for other frames and for COMM
// frames that cannot be read (compressed or short).
func (t *wtag) parseCOMM(f wframe) (langText, bool) {
	if f.id != frameIDCOMM {
		return langText{}, false
	}
	return t.frameLangText(f)
}

// frameLangText decodes a COMM or USLT frame the way the reader does; ok is
// false when it cannot be read (compressed or short).
func (t *wtag) frameLangText(f wframe) (langText, bool) {
	body, skip := frameBody(t.outVersion, 0, f.flags, f.payload)
	if skip != "" {
		return langText{}, false
	}
	return parseLangText(body)
}

// undeterminedLanguage is the language of a new USLT frame: "XXX", which
// ID3v2 reserves for an unknown language. A language is never guessed.
const undeterminedLanguage = "XXX"

// applyLyrics sets or clears the lyrics. Setting rewrites the text of one USLT
// frame, keeping its language and description, and leaves any other USLT frame
// alone; with no frame to rewrite it appends one in the undetermined language.
// Clearing removes every USLT frame, so the lyrics read back empty.
func (t *wtag) applyLyrics(values []string) error {
	if len(values) == 0 {
		t.removeLyrics()
		return nil
	}
	text := values[0]
	if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return fmt.Errorf("%w: Lyrics is not valid UTF-8 or contains NUL", types.ErrUnsupportedWrite)
	}
	i, c := t.lyricsFrame()
	frame := wframe{id: frameIDUSLT, payload: encodeLangText(t.outVersion, c.lang, c.desc, text)}
	if i < 0 {
		t.frames = append(t.frames, frame)
		return nil
	}
	t.frames[i] = frame
	return nil
}

// lyricsFrame returns the index and content of the USLT frame the Lyrics field
// is read from: the first with text, else the first readable one. With neither
// it returns -1 and the undetermined language with no description.
func (t *wtag) lyricsFrame() (int, langText) {
	at, found := -1, langText{lang: undeterminedLanguage}
	for i, f := range t.frames {
		if f.id != frameIDUSLT {
			continue
		}
		c, ok := t.frameLangText(f)
		switch {
		case ok && c.text != "":
			return i, c
		case ok && at < 0:
			at, found = i, c
		}
	}
	return at, found
}

// removeLyrics drops every USLT frame.
func (t *wtag) removeLyrics() {
	t.remove(func(f wframe) (bool, error) { return f.id == frameIDUSLT, nil })
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
			t.insert(t.lastAPIC()+1, wframe{id: frameIDAPIC, payload: payload})
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

// encodeLangText builds a COMM or USLT payload:
// [enc][lang(3)][desc][NUL][text].
func encodeLangText(version byte, lang, desc, text string) []byte {
	enc := textEncoding(version, desc, text)
	out := []byte{enc}
	out = append(out, (lang + "\x00\x00\x00")[:3]...)
	out = append(out, encodeString(enc, desc)...)
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
