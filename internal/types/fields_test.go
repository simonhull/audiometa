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
		if got := valuesOf(tags, f); !slices.Equal(got, sample) {
			t.Errorf("%s: valuesOf after set %v = %v", f, sample, got)
		}

		setFieldValues(tags, f, nil)
		if got := valuesOf(tags, f); got != nil {
			t.Errorf("%s: valuesOf after clear = %v, want nil", f, got)
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

// TestCloneEqual_CoverEveryField sets every field, so a field that Clone
// forgets to copy or Equal forgets to compare fails here.
func TestCloneEqual_CoverEveryField(t *testing.T) {
	sample := func(f Field, alt bool) []string {
		switch {
		case intField(&Tags{}, f) != nil:
			return []string{map[bool]string{false: "7", true: "8"}[alt]}
		case sliceField(&Tags{}, f) != nil:
			return []string{"a", map[bool]string{false: "b", true: "c"}[alt]}
		default:
			return []string{map[bool]string{false: "x", true: "y"}[alt]}
		}
	}
	full := &Tags{}
	for f := range AllFields() {
		setFieldValues(full, f, sample(f, false))
	}
	full.Set("CUSTOM", "v")
	full.SetNamed("Mixed Case", "w") // fills names, which Clone must copy and Equal compare
	BindFormat(full, FormatFLAC)

	c := full.Clone()
	if !reflect.DeepEqual(c, full) {
		t.Fatalf("Clone differs:\n got  %+v\n want %+v", c, full)
	}
	if !c.Equal(full) {
		t.Fatal("Equal(Clone) = false")
	}
	if renamed := full.Clone(); true {
		renamed.setName("MIXED CASE", "MIXED case")
		if renamed.Equal(full) {
			t.Error("Equal ignores the spelling of names")
		}
		if full.Name("MIXED CASE") != "Mixed Case" {
			t.Error("Clone shares the names map")
		}
	}
	for f := range AllFields() {
		d := full.Clone()
		setFieldValues(d, f, sample(f, true))
		if d.Equal(full) {
			t.Errorf("Equal ignores %s", f)
		}
		if p := sliceField(c, f); p != nil {
			(*p)[0] = "mutated"
			if sliceField(full, f) != nil && (*sliceField(full, f))[0] == "mutated" {
				t.Errorf("Clone shares the %s slice", f)
			}
		}
	}
}
