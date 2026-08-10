package sql

import (
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/zclconf/go-cty/cty"
)

func TestConvertArgScalars(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value cty.Value
		want  any
	}{
		{name: "string", value: cty.StringVal("hello"), want: "hello"},
		{name: "bool true", value: cty.True, want: true},
		{name: "bool false", value: cty.False, want: false},
		{name: "int", value: cty.NumberIntVal(42), want: int64(42)},
		{name: "negative int", value: cty.NumberIntVal(-7), want: int64(-7)},
		{name: "bigint", value: cty.NumberIntVal(9_000_000_000_000), want: int64(9_000_000_000_000)},
		{name: "float", value: cty.NumberFloatVal(3.14), want: 3.14},
		{name: "null", value: cty.NullVal(cty.String), want: nil},
	}

	for _, tc := range tests {
		got, err := ConvertArg(tc.value)
		if err != nil {
			t.Errorf("%s: unexpected error %v", tc.name, err)

			continue
		}

		if got != tc.want {
			t.Errorf("%s: want %v (%T) got %v (%T)", tc.name, tc.want, tc.want, got, got)
		}
	}
}

func TestConvertArgRejectsObjects(t *testing.T) {
	t.Parallel()

	obj := cty.ObjectVal(map[string]cty.Value{"x": cty.NumberIntVal(1)})
	if _, err := ConvertArg(obj); err == nil {
		t.Errorf("ConvertArg(object): want error, got nil")
	}

	m := cty.MapVal(map[string]cty.Value{"x": cty.StringVal("a")})
	if _, err := ConvertArg(m); err == nil {
		t.Errorf("ConvertArg(map): want error, got nil")
	}
}

// A list becomes a ListArg, which the provider later expands into a
// placeholder run.
func TestConvertArgLowersListsToListArg(t *testing.T) {
	t.Parallel()

	got, err := ConvertArg(cty.TupleVal([]cty.Value{
		cty.NumberIntVal(1),
		cty.StringVal("a"),
		cty.NullVal(cty.String),
	}))
	if err != nil {
		t.Fatalf("ConvertArg(tuple): %v", err)
	}

	list, ok := got.(ListArg)
	if !ok {
		t.Fatalf("ConvertArg(tuple) = %T, want ListArg", got)
	}

	if len(list) != 3 || list[0] != int64(1) || list[1] != "a" || list[2] != nil {
		t.Fatalf("ListArg = %#v, want [1 \"a\" <nil>]", list)
	}

	empty, err := ConvertArg(cty.EmptyTupleVal)
	if err != nil {
		t.Fatalf("ConvertArg(empty tuple): %v", err)
	}

	if l, ok := empty.(ListArg); !ok || len(l) != 0 {
		t.Fatalf("ConvertArg(empty tuple) = %#v, want an empty ListArg", empty)
	}
}

// A placeholder expands into a flat run of values, so a list of lists has no
// meaning; and a set has no source order, so its expansion would not be
// reproducible.
func TestConvertArgRejectsNestedListsAndSets(t *testing.T) {
	t.Parallel()

	nested := cty.TupleVal([]cty.Value{cty.TupleVal([]cty.Value{cty.NumberIntVal(1)})})
	if _, err := ConvertArg(nested); err == nil || !strings.Contains(err.Error(), "nested list") {
		t.Fatalf("ConvertArg(nested list) error = %v, want a nested-list rejection", err)
	}

	set := cty.SetVal([]cty.Value{cty.StringVal("a"), cty.StringVal("b")})
	if _, err := ConvertArg(set); err == nil || !strings.Contains(err.Error(), "sets are not supported") {
		t.Fatalf("ConvertArg(set) error = %v, want a set rejection", err)
	}
}

func TestConvertArgsReportsIndex(t *testing.T) {
	t.Parallel()

	_, err := ConvertArgs([]cty.Value{
		cty.StringVal("ok"),
		cty.ObjectVal(map[string]cty.Value{"x": cty.NumberIntVal(1)}),
	})

	if err == nil || !strings.Contains(err.Error(), "args[1]") {
		t.Fatalf("want args[1] error, got %v", err)
	}
}

func TestConvertArgNumberPreservesBigIntPrecision(t *testing.T) {
	t.Parallel()

	bf := big.NewFloat(0)
	bf.SetInt64(1234567890123456)

	got, err := ConvertArg(cty.NumberVal(bf))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != int64(1234567890123456) {
		t.Fatalf("want int64(1234567890123456) got %v (%T)", got, got)
	}
}

func TestConvertRowValue(t *testing.T) {
	t.Parallel()

	got, err := ConvertRowValue([]byte("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !got.RawEquals(cty.StringVal("hello")) {
		t.Errorf("UTF-8 []byte: want StringVal(hello) got %#v", got)
	}

	if _, err := ConvertRowValue([]byte{0xff, 0xfe, 0xfd}); err == nil {
		t.Errorf("non-UTF-8 []byte should produce an error")
	}

	ts := time.Date(2026, 5, 26, 12, 30, 45, 0, time.UTC)

	got, err = ConvertRowValue(ts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !got.RawEquals(cty.StringVal(ts.Format(time.RFC3339Nano))) {
		t.Errorf("time.Time: want %q got %#v", ts.Format(time.RFC3339Nano), got)
	}

	got, err = ConvertRowValue(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !got.IsNull() {
		t.Errorf("nil should map to a null cty value")
	}
}
