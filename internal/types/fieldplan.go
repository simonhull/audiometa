package types

import (
	"fmt"
	"slices"
	"strconv"
)

// YearValue returns year as four digits, keeping whatever followed the four-digit year in
// old: YearValue(1999, "2021-05-03") is "1999-05-03".
func YearValue(year int, old string) string {
	y := fmt.Sprintf("%04d", year)
	if len(old) > 4 && isASCIIDigits(old[:4]) {
		return y + old[4:]
	}
	return y
}

// isASCIIDigits reports whether s is all ASCII digits.
func isASCIIDigits(s string) bool {
	for _, c := range []byte(s) {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// FieldTargets returns the raw keys field edit e writes, in format's binding precedence.
// When setting, that is the highest-precedence bound key present, or canonical when none is;
// when clearing, every bound key present. A key is present when it has values after
// edits.Keys are applied to edits.Orig, which is what the writers do before field edits.
func FieldTargets(format Format, e FieldEdit, edits *EditSet, canonical string, setting bool) []string {
	var present []string
	for _, b := range bindingsFor(format) {
		if b.Field != e.Field {
			continue
		}
		for _, k := range b.Keys {
			if len(keyValues(edits, k)) > 0 {
				present = append(present, k)
			}
		}
	}
	switch {
	case !setting:
		return present
	case len(present) > 0:
		return present[:1]
	default:
		return []string{canonical}
	}
}

// FieldValues returns the values e writes to key; nil deletes it. Year keeps the rest of
// key's current date ([YearValue]), read after the key edits; Artists writes Final.Artists;
// Artist writes Final.Artist followed by Orig.Artists[1:]; other fields write a copy of
// e.Values.
func FieldValues(e FieldEdit, key string, edits *EditSet) []string {
	if e.Field == FieldYear {
		return yearFieldValues(e, key, edits)
	}
	if e.Field == FieldArtists {
		return slices.Clone(edits.Final.Artists)
	}
	if e.Field != FieldArtist {
		return slices.Clone(e.Values)
	}
	if edits.Final.Artist == "" {
		return nil
	}
	values := []string{edits.Final.Artist}
	if len(edits.Orig.Artists) > 1 {
		values = append(values, edits.Orig.Artists[1:]...)
	}
	return values
}

// yearFieldValues returns the value a Year edit writes to key, or nil to delete it: a cleared
// or invalid year deletes.
func yearFieldValues(e FieldEdit, key string, edits *EditSet) []string {
	if len(e.Values) == 0 {
		return nil
	}
	year, err := strconv.Atoi(e.Values[0])
	if err != nil || year == 0 {
		return nil
	}
	var old string
	if values := keyValues(edits, key); len(values) > 0 {
		old = values[0]
	}
	return []string{YearValue(year, old)}
}

// keyValues returns the values of raw key after edits.Keys are applied to edits.Orig. The
// result aliases the edit set and must not be modified.
func keyValues(edits *EditSet, key string) []string {
	if i := slices.IndexFunc(edits.Keys, func(k KeyEdit) bool { return k.Key == key }); i >= 0 {
		return edits.Keys[i].Values
	}
	if edits.Orig == nil {
		return nil
	}
	return edits.Orig.raw[key]
}
