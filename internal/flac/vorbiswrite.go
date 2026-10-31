package flac

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/simonhull/audiometa/internal/types"
)

// vcomment is a VORBIS_COMMENT block being rewritten.
type vcomment struct {
	vendor   []byte   // copied verbatim
	comments [][]byte // raw "KEY=VALUE" comments, in file order
	tail     []byte   // bytes after the last comment, copied verbatim
}

// errShortVC reports a VORBIS_COMMENT length field that runs past the block.
var errShortVC = errors.New("length runs past the end of the block")

// parseVC splits a VORBIS_COMMENT payload: a little-endian 32-bit vendor
// length and the vendor string, a little-endian 32-bit comment count, then
// each comment as a little-endian 32-bit length and its bytes.
func parseVC(payload []byte) (*vcomment, error) {
	vendor, rest, err := takeLE(payload)
	if err != nil {
		return nil, fmt.Errorf("vendor string: %w", err)
	}
	if len(rest) < 4 {
		return nil, fmt.Errorf("comment count: %w", errShortVC)
	}
	count := binary.LittleEndian.Uint32(rest)
	rest = rest[4:]
	if uint64(count) > uint64(len(rest)/4) {
		return nil, fmt.Errorf("comment count %d: %w", count, errShortVC)
	}
	v := &vcomment{vendor: vendor, comments: make([][]byte, 0, count)}
	for i := range count {
		var c []byte
		if c, rest, err = takeLE(rest); err != nil {
			return nil, fmt.Errorf("comment %d: %w", i, err)
		}
		v.comments = append(v.comments, c)
	}
	v.tail = rest
	return v, nil
}

// takeLE splits a little-endian 32-bit length and that many bytes off b.
func takeLE(b []byte) (field, rest []byte, err error) {
	if len(b) < 4 {
		return nil, nil, errShortVC
	}
	n := int64(binary.LittleEndian.Uint32(b))
	if n > int64(len(b)-4) {
		return nil, nil, errShortVC
	}
	return b[4 : 4+n], b[4+n:], nil
}

// bytes encodes the block payload. Lengths beyond the 32-bit range are not
// checked here: such a payload exceeds the block size limit, which
// serialize enforces.
func (v *vcomment) bytes() []byte {
	n := 8 + len(v.vendor) + len(v.tail)
	for _, c := range v.comments {
		n += 4 + len(c)
	}
	out := make([]byte, 0, n)
	out = appendLE(out, v.vendor)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(v.comments))) //nolint:gosec // G115: bounded by the block size limit
	for _, c := range v.comments {
		out = appendLE(out, c)
	}
	return append(out, v.tail...)
}

// appendLE appends b preceded by its little-endian 32-bit length.
func appendLE(out, b []byte) []byte {
	out = binary.LittleEndian.AppendUint32(out, uint32(len(b))) //nolint:gosec // G115: bounded by the block size limit
	return append(out, b...)
}

// commentKey returns the key of a raw comment (the text before the first
// "="); ok is false for a comment without "=", which matches no key.
func commentKey(c []byte) (key string, ok bool) {
	k, _, ok := bytes.Cut(c, []byte{'='})
	return string(k), ok
}

// has reports whether any comment has key (case-insensitively).
func (v *vcomment) has(key string) bool {
	_, ok := v.first(key)
	return ok
}

// first returns the value of the first comment with key (case-insensitively).
func (v *vcomment) first(key string) (string, bool) {
	for _, c := range v.comments {
		if k, ok := commentKey(c); ok && strings.EqualFold(k, key) {
			return string(c[len(k)+1:]), true
		}
	}
	return "", false
}

// applyKey replaces every comment whose key equals key (case-insensitively)
// by one comment per value, inserted where the first one was, or appended
// when there was none. The new comments keep the first match's key spelling.
// Empty values delete the key.
func (v *vcomment) applyKey(key string, values []string) error {
	if isChapterKey(key) {
		return fmt.Errorf("%w: %s holds chapters, which are not editable", types.ErrUnsupportedWrite, key)
	}
	if len(values) > 0 {
		if err := validKey(key); err != nil {
			return err
		}
		for _, val := range values {
			if !utf8.ValidString(val) {
				return fmt.Errorf("%w: value of %s is not valid UTF-8", types.ErrUnsupportedWrite, key)
			}
		}
	}
	spelling, at := key, -1
	kept := make([][]byte, 0, len(v.comments)+len(values))
	for _, c := range v.comments {
		if k, ok := commentKey(c); ok && strings.EqualFold(k, key) {
			if at < 0 {
				spelling, at = k, len(kept)
			}
			continue
		}
		kept = append(kept, c)
	}
	if at < 0 {
		at = len(kept)
	}
	added := make([][]byte, len(values))
	for i, val := range values {
		added[i] = []byte(spelling + "=" + val)
	}
	v.comments = slices.Insert(kept, at, added...)
	return nil
}

// validKey checks a Vorbis comment field name: non-empty, bytes 0x20-0x7D,
// no "=".
func validKey(key string) error {
	if key == "" {
		return fmt.Errorf("%w: empty Vorbis comment key", types.ErrUnsupportedWrite)
	}
	for _, c := range []byte(key) {
		if c < 0x20 || c > 0x7D || c == '=' {
			return fmt.Errorf("%w: invalid Vorbis comment key %q", types.ErrUnsupportedWrite, key)
		}
	}
	return nil
}

// isChapterKey reports whether key is a CHAPTERnnn chapter comment key.
func isChapterKey(key string) bool {
	const prefix = "CHAPTER"
	return len(key) > len(prefix) && strings.EqualFold(key[:len(prefix)], prefix) &&
		key[len(prefix)] >= '0' && key[len(prefix)] <= '9'
}

// applyField writes one typed-field edit. Setting writes the
// highest-precedence bound key present in the block, or the field's first
// bound key when none is; clearing deletes every bound key. No other key is
// touched.
func (v *vcomment) applyField(e types.FieldEdit, edits *types.EditSet) error {
	var keys []string
	for _, b := range types.FieldBindings(types.FormatFLAC) {
		if b.Field == e.Field {
			keys = append(keys, b.Keys...)
		}
	}
	if len(keys) == 0 {
		return fmt.Errorf("%w: %s cannot be written to FLAC", types.ErrUnsupportedWrite, e.Field)
	}
	target := keys[0]
	var present []string
	for _, k := range keys {
		if v.has(k) {
			present = append(present, k)
		}
	}
	if len(present) > 0 {
		target = present[0]
	}
	values := v.fieldValues(e, target, edits)
	if len(values) == 0 {
		for _, k := range present {
			if err := v.applyKey(k, nil); err != nil {
				return err
			}
		}
		return nil
	}
	return v.applyKey(target, values)
}

// fieldValues returns the values a field edit writes to key; nil deletes it.
func (v *vcomment) fieldValues(e types.FieldEdit, key string, edits *types.EditSet) []string {
	if e.Field == types.FieldYear {
		if len(e.Values) == 0 {
			return nil
		}
		year, err := strconv.Atoi(e.Values[0])
		if err != nil || year == 0 {
			return nil
		}
		old, _ := v.first(key)
		return []string{yearValue(year, old)}
	}
	if e.Field == types.FieldArtists {
		return slices.Clone(edits.Final.Artists)
	}
	if e.Field == types.FieldArtist {
		if edits.Final.Artist == "" {
			return nil
		}
		values := []string{edits.Final.Artist}
		if len(edits.Orig.Artists) > 1 {
			values = append(values, edits.Orig.Artists[1:]...)
		}
		return values
	}
	return slices.Clone(e.Values)
}

// yearValue writes year into the existing date old: a longer date such as
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
