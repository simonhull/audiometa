package types

import (
	"reflect"
	"slices"
	"testing"
)

func TestFieldsCoverEveryTagsField(t *testing.T) {
	typ := reflect.TypeFor[Tags]()
	exported := map[string]bool{}
	for sf := range typ.Fields() {
		if sf.IsExported() {
			exported[sf.Name] = true
		}
	}
	if got, want := len(exported), int(fieldCount)-1; got != want {
		t.Fatalf("Tags has %d exported fields, Field constants cover %d; add a Field for the new Tags field", got, want)
	}

	seen := map[string]bool{}
	for f := range AllFields() {
		name := f.String()
		if !exported[name] {
			t.Errorf("Field(%d).String() = %q, not an exported Tags field", f, name)
		}
		if seen[name] {
			t.Errorf("Field name %q used twice", name)
		}
		seen[name] = true
	}
}

func TestFieldString_Invalid(t *testing.T) {
	if got := Field(0).String(); got != "Field(0)" {
		t.Errorf("Field(0).String() = %q", got)
	}
	if got := fieldCount.String(); got != "Field(36)" {
		t.Errorf("fieldCount.String() = %q", got)
	}
}

func TestFieldValuesRoundTrip(t *testing.T) {
	for f := range AllFields() {
		var sample []string
		switch {
		case stringField(&Tags{}, f) != nil:
			sample = []string{"x"}
		case sliceField(&Tags{}, f) != nil:
			sample = []string{"a", "b"}
		case intField(&Tags{}, f) != nil:
			sample = []string{"7"}
		default:
			t.Fatalf("%s has no storage kind", f)
		}

		tags := &Tags{}
		setFieldValues(tags, f, sample)
		if got := fieldValues(tags, f); !slices.Equal(got, sample) {
			t.Errorf("%s: fieldValues after set %v = %v", f, sample, got)
		}

		setFieldValues(tags, f, nil)
		if got := fieldValues(tags, f); got != nil {
			t.Errorf("%s: fieldValues after clear = %v, want nil", f, got)
		}
	}
}

func TestSetFieldValues_IntParseError(t *testing.T) {
	tags := &Tags{Year: 2000}
	setFieldValues(tags, FieldYear, []string{"abc"})
	if tags.Year != 0 {
		t.Errorf("Year = %d, want 0 for unparsable value", tags.Year)
	}
}
