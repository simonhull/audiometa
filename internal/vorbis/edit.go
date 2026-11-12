package vorbis

import (
	"bytes"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/simonhull/audiometa/internal/types"
)

// Editor applies tag edits to a Vorbis comment header being rewritten: a FLAC
// VORBIS_COMMENT block, an Ogg Vorbis comment packet or an OpusTags packet.
// The vendor string and tail are copied verbatim; the entries are the raw
// "KEY=VALUE" comments in file order. Typed-field edits follow the binding
// table of the Editor's format.
type Editor struct {
	Comments

	format types.Format
}

// NewEditor returns an Editor that rewrites c for a file of format. The
// vendor and tail bytes alias c.
func NewEditor(format types.Format, c Comments) *Editor {
	return &Editor{Comments: c, format: format}
}

// commentKey returns the key of a raw comment (the text before the first
// "="); ok is false for a comment without "=", which matches no key.
func commentKey(c []byte) (key string, ok bool) { //nolint:nonamedreturns // the names document the results
	k, _, ok := bytes.Cut(c, []byte{'='})
	return string(k), ok
}

// Has reports whether any comment has key (case-insensitively).
func (v *Editor) Has(key string) bool {
	_, ok := v.First(key)
	return ok
}

// First returns the value of the first comment with key (case-insensitively).
func (v *Editor) First(key string) (string, bool) {
	for _, c := range v.Entries {
		if k, ok := commentKey(c); ok && strings.EqualFold(k, key) {
			return string(c[len(k)+1:]), true
		}
	}
	return "", false
}

// ApplyKey replaces every comment whose key equals key (case-insensitively)
// by one comment per value, inserted where the first one was, or appended
// when there was none. The new comments keep the first match's key spelling.
// Empty values delete the key.
func (v *Editor) ApplyKey(key string, values []string) error {
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
	kept := make([][]byte, 0, len(v.Entries)+len(values))
	for _, c := range v.Entries {
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
	v.Entries = slices.Insert(kept, at, added...)
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

// ApplyField writes one typed-field edit. Setting writes the
// highest-precedence bound key present in the block, or the field's first
// bound key when none is; clearing deletes every bound key. No other key is
// touched. The track and disc number and total fields go through
// [Editor.applyNumberField], because TRACKNUMBER and DISCNUMBER can also
// hold the total.
func (v *Editor) ApplyField(e types.FieldEdit, edits *types.EditSet) error {
	if keys, _, ok := numberKeysFor(e.Field); ok {
		return v.applyNumberField(e, edits, keys)
	}
	var keys []string
	for _, b := range types.FieldBindings(v.format) {
		if b.Field == e.Field {
			keys = append(keys, b.Keys...)
		}
	}
	if len(keys) == 0 {
		return fmt.Errorf("%w: %s cannot be written to %s", types.ErrUnsupportedWrite, e.Field, v.format)
	}
	target := types.FieldTargets(v.format, e, edits, keys[0], true)[0]
	values := types.FieldValues(e, target, edits)
	if len(values) == 0 {
		for _, k := range types.FieldTargets(v.format, e, edits, keys[0], false) {
			if err := v.ApplyKey(k, nil); err != nil {
				return err
			}
		}
		return nil
	}
	return v.ApplyKey(target, values)
}

// numberKeys describes where a track or disc number and total live.
type numberKeys struct {
	number string   // TRACKNUMBER or DISCNUMBER; may hold "N/Total"
	totals []string // explicit total keys, in precedence order
	final  func(*types.Tags) (number, total int)
}

var (
	trackKeys = numberKeys{
		"TRACKNUMBER",
		[]string{"TRACKTOTAL", "TOTALTRACKS"},
		func(t *types.Tags) (int, int) { return t.TrackNumber, t.TrackTotal },
	}
	discKeys = numberKeys{
		"DISCNUMBER",
		[]string{"DISCTOTAL", "TOTALDISCS"},
		func(t *types.Tags) (int, int) { return t.DiscNumber, t.DiscTotal },
	}
)

// numberKeysFor returns the keys of a track or disc field and whether f is
// the total (rather than the number). ok is false for any other field.
func numberKeysFor(f types.Field) (keys numberKeys, isTotal, ok bool) { //nolint:nonamedreturns // the names document the results
	if f == types.FieldTrackNumber || f == types.FieldTrackTotal {
		return trackKeys, f == types.FieldTrackTotal, true
	}
	if f == types.FieldDiscNumber || f == types.FieldDiscTotal {
		return discKeys, f == types.FieldDiscTotal, true
	}
	return numberKeys{}, false, false
}

// applyNumberField writes a track or disc number or total edit.
//
// A file may store the total as a "/Total" suffix of TRACKNUMBER (resp.
// DISCNUMBER) with no explicit total key; that is suffix mode, and both
// fields rewrite the number key, so the total stays where the file kept it.
// Otherwise the number goes to the number key and the total to the first
// present explicit total key, or the first one when none is present. Clearing
// a total deletes every explicit total key and strips a stale "/Total" suffix
// from the number key; it never deletes the number.
func (v *Editor) applyNumberField(e types.FieldEdit, edits *types.EditSet, keys numberKeys) error {
	_, isTotal, _ := numberKeysFor(e.Field)
	number, total := keys.final(edits.Final)
	edited := 0 // no values: the field is being cleared
	if len(e.Values) > 0 {
		edited, _ = strconv.Atoi(e.Values[0])
	}
	if isTotal {
		total = edited
	} else {
		number = edited
	}

	var presentTotals []string
	for _, k := range keys.totals {
		if v.Has(k) {
			presentTotals = append(presentTotals, k)
		}
	}
	old, _ := v.First(keys.number)
	hasSuffix := strings.Contains(old, "/")

	switch {
	case hasSuffix && len(presentTotals) == 0:
		return v.ApplyKey(keys.number, numberTotal(number, total))
	case !isTotal:
		return v.ApplyKey(keys.number, slices.Clone(e.Values))
	case len(e.Values) > 0:
		target := keys.totals[0]
		if len(presentTotals) > 0 {
			target = presentTotals[0]
		}
		return v.ApplyKey(target, slices.Clone(e.Values))
	}
	for _, k := range presentTotals {
		if err := v.ApplyKey(k, nil); err != nil {
			return err
		}
	}
	if hasSuffix {
		return v.ApplyKey(keys.number, numberTotal(number, 0))
	}
	return nil
}

// numberTotal encodes a "N/Total" value: "n", "n/t", or nil when both are 0.
// internal/mp3 applies the same rule to TRCK and TPOS.
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
