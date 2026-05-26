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

func TestConvertArgRejectsCollections(t *testing.T) {
	t.Parallel()

	list := cty.ListVal([]cty.Value{cty.StringVal("a"), cty.StringVal("b")})
	if _, err := ConvertArg(list); err == nil {
		t.Errorf("ConvertArg(list): want error, got nil")
	}

	obj := cty.ObjectVal(map[string]cty.Value{"x": cty.NumberIntVal(1)})
	if _, err := ConvertArg(obj); err == nil {
		t.Errorf("ConvertArg(object): want error, got nil")
	}
}

func TestConvertArgsReportsIndex(t *testing.T) {
	t.Parallel()

	_, err := ConvertArgs([]cty.Value{
		cty.StringVal("ok"),
		cty.ListVal([]cty.Value{cty.NumberIntVal(1)}),
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
