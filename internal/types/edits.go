package types

import (
	"fmt"
	"maps"
	"slices"
)

// KeyEdit replaces every value of one format-native raw key. Values == nil deletes the key.
type KeyEdit struct {
	Key    string
	Values []string
}

// FieldEdit sets one typed field that no KeyEdit already covers. Values uses the
// fieldValues encoding (nil = clear).
type FieldEdit struct {
	Field  Field
	Values []string
}

// ArtworkOpKind is the kind of an ArtworkOp.
type ArtworkOpKind uint8

// Artwork edit kinds.
const (
	ArtworkOpAdd       ArtworkOpKind = iota + 1 // append Art
	ArtworkOpRemove                             // remove every image whose Type == Type
	ArtworkOpRemoveAll                          // remove every image
)

// ArtworkOp is one artwork edit. Ops apply in order to the file's picture list.
type ArtworkOp struct {
	Kind ArtworkOpKind
	Type ArtworkType // ArtworkOpRemove
	Art  Artwork     // ArtworkOpAdd
}

// EditSet is everything a format writer needs. Orig and Final are read-only context
// (Orig: freshly re-parsed source; Final: the caller's current Tags).
type EditSet struct {
	Keys    []KeyEdit   // sorted by Key
	Fields  []FieldEdit // sorted by Field
	Artwork []ArtworkOp // in call order
	Orig    *Tags
	Final   *Tags
}

// Empty reports whether the set changes nothing.
func (e *EditSet) Empty() bool {
	return len(e.Keys) == 0 && len(e.Fields) == 0 && len(e.Artwork) == 0
}

// Diff computes the key and field edits that turn orig into cur for format.
//
// Every raw key whose values differ becomes a KeyEdit. A typed field becomes a
// FieldEdit only when its value in cur differs from what the key edits alone
// produce (applied to orig with the Set sync rule). If a key edit already
// changed a field and cur disagrees with that result, or two related fields
// (Artist/Artists, Year/Date) were edited inconsistently, Diff returns an
// error wrapping ErrConflictingEdits.
func Diff(orig, cur *Tags, format Format) ([]KeyEdit, []FieldEdit, error) {
	keys := diffKeys(orig, cur)
	fields, err := diffFields(orig, cur, format, keys)
	if err != nil {
		return nil, nil, err
	}
	if err := checkCrossField(cur, fields); err != nil {
		return nil, nil, err
	}
	return keys, fields, nil
}

// diffKeys returns a KeyEdit, sorted by key, for every raw key whose values
// differ between orig and cur.
func diffKeys(orig, cur *Tags) []KeyEdit {
	all := maps.Clone(orig.raw)
	if all == nil {
		all = make(map[string][]string, len(cur.raw))
	}
	maps.Copy(all, cur.raw)
	union := slices.Sorted(maps.Keys(all))

	var edits []KeyEdit
	for _, k := range union {
		o, c := orig.raw[k], cur.raw[k]
		if slices.Equal(o, c) {
			continue
		}
		var values []string
		if len(c) > 0 {
			values = slices.Clone(c)
		}
		edits = append(edits, KeyEdit{Key: k, Values: values})
	}
	return edits
}

// diffFields returns the field edits not already implied by keys.
func diffFields(orig, cur *Tags, format Format, keys []KeyEdit) ([]FieldEdit, error) {
	derived := orig.Clone()
	BindFormat(derived, format)
	if derived.raw == nil {
		derived.raw = make(map[string][]string)
	}
	for _, e := range keys {
		if e.Values == nil {
			delete(derived.raw, e.Key)
		} else {
			derived.raw[e.Key] = slices.Clone(e.Values)
		}
		derived.syncKey(e.Key)
	}

	var edits []FieldEdit
	for f := range AllFields() {
		c, d, o := fieldValues(cur, f), fieldValues(derived, f), fieldValues(orig, f)
		if slices.Equal(c, d) {
			continue
		}
		if !slices.Equal(d, o) {
			return nil, fmt.Errorf("%w: %s and its raw key were both edited", ErrConflictingEdits, f)
		}
		edits = append(edits, FieldEdit{Field: f, Values: c})
	}
	return edits, nil
}

// checkCrossField rejects field edits that contradict each other: an Artist
// that is not the first of Artists, or a Year that is not the year of Date.
func checkCrossField(cur *Tags, fields []FieldEdit) error {
	edited := func(f Field) bool {
		return slices.ContainsFunc(fields, func(e FieldEdit) bool { return e.Field == f })
	}
	if edited(FieldArtist) && edited(FieldArtists) &&
		len(cur.Artists) > 0 && cur.Artists[0] != cur.Artist {
		return fmt.Errorf("%w: %s and %s disagree", ErrConflictingEdits, FieldArtist, FieldArtists)
	}
	if edited(FieldYear) && edited(FieldDate) &&
		len(cur.Date) >= 4 && cur.Date[:4] != fmt.Sprintf("%04d", cur.Year) {
		return fmt.Errorf("%w: %s and %s disagree", ErrConflictingEdits, FieldYear, FieldDate)
	}
	return nil
}
