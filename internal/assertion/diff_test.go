package assertion

import (
	"testing"

	"github.com/zclconf/go-cty/cty"
)

// TestMismatchErrorRendersValuesAsJSON pins the user-visible format of a
// scalar mismatch (issue #81). Both sides used to go through `%v`, which
// printed the internals of a cty.Value (`want={{{{} 83}} abc}`) instead
// of the value itself.
func TestMismatchErrorRendersValuesAsJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want interface{}
		got  interface{}
		msg  string
	}{
		{
			name: "string keeps its quotes",
			want: cty.StringVal("submitted with abc"),
			got:  cty.StringVal("submitted with abcEnter"),
			msg:  `assertion at #out: want="submitted with abc" got="submitted with abcEnter"`,
		},
		{
			name: "trailing whitespace stays visible",
			want: cty.StringVal("abc"),
			got:  cty.StringVal("abc "),
			msg:  `assertion at #out: want="abc" got="abc "`,
		},
		{
			name: "string is not HTML-escaped",
			want: cty.StringVal("https://example.test/?a=1&b=<2>"),
			got:  cty.StringVal(`say "hi"`),
			msg:  `assertion at #out: want="https://example.test/?a=1&b=<2>" got="say \"hi\""`,
		},
		{
			name: "integer",
			want: cty.NumberIntVal(200),
			got:  cty.NumberIntVal(404),
			msg:  `assertion at #out: want=200 got=404`,
		},
		{
			name: "fractional number",
			want: cty.NumberFloatVal(1.5),
			got:  cty.NumberFloatVal(0.25),
			msg:  `assertion at #out: want=1.5 got=0.25`,
		},
		{
			name: "number beyond int64 keeps every digit",
			want: cty.MustParseNumberVal("123456789012345678901234567890"),
			got:  cty.NumberIntVal(-7),
			msg:  `assertion at #out: want=123456789012345678901234567890 got=-7`,
		},
		{
			name: "bool",
			want: cty.True,
			got:  cty.False,
			msg:  `assertion at #out: want=true got=false`,
		},
		{
			name: "string against number",
			want: cty.StringVal("1"),
			got:  cty.NumberIntVal(1),
			msg:  `assertion at #out: want="1" got=1`,
		},
		{
			name: "null",
			want: cty.NullVal(cty.String),
			got:  cty.StringVal("x"),
			msg:  `assertion at #out: want=null got="x"`,
		},
		{
			name: "nil cty value",
			want: cty.NilVal,
			got:  cty.NullVal(cty.DynamicPseudoType),
			msg:  `assertion at #out: want=null got=null`,
		},
		{
			name: "unknown",
			want: cty.UnknownVal(cty.String),
			got:  cty.DynamicVal,
			msg:  `assertion at #out: want=(unknown) got=(unknown)`,
		},
		{
			name: "object keys are sorted",
			want: cty.StringVal("x"),
			got: cty.ObjectVal(map[string]cty.Value{
				"name": cty.StringVal("Ada"),
				"id":   cty.NumberIntVal(1),
				"tags": cty.ListVal([]cty.Value{cty.StringVal("a"), cty.StringVal("b")}),
			}),
			msg: `assertion at #out: want="x" got={"id":1,"name":"Ada","tags":["a","b"]}`,
		},
		{
			name: "object masks secret fields like the report does",
			want: cty.StringVal("x"),
			got: cty.ObjectVal(map[string]cty.Value{
				"id":    cty.StringVal("u_1"),
				"token": cty.StringVal("s3cr3t"),
			}),
			msg: `assertion at #out: want="x" got={"id":"u_1","token":"***"}`,
		},
		{
			name: "map",
			want: cty.MapVal(map[string]cty.Value{"b": cty.NumberIntVal(2), "a": cty.NumberIntVal(1)}),
			got:  cty.MapValEmpty(cty.Number),
			msg:  `assertion at #out: want={"a":1,"b":2} got={}`,
		},
		{
			name: "tuple, list and set",
			want: cty.TupleVal([]cty.Value{cty.NumberIntVal(1), cty.StringVal("a"), cty.NullVal(cty.Bool)}),
			got:  cty.SetVal([]cty.Value{cty.StringVal("z")}),
			msg:  `assertion at #out: want=[1,"a",null] got=["z"]`,
		},
		{
			name: "empty list",
			want: cty.ListValEmpty(cty.String),
			got:  cty.EmptyTupleVal,
			msg:  `assertion at #out: want=[] got=[]`,
		},
		{
			name: "matcher object",
			want: cty.ObjectVal(map[string]cty.Value{
				matcherKey: cty.StringVal("contains"),
				"value":    cty.StringVal("abc"),
			}),
			got: cty.StringVal("xyz"),
			msg: `assertion at #out: want={"__tales_matcher":"contains","value":"abc"} got="xyz"`,
		},
		{
			name: "infinity falls back to the Go syntax",
			want: cty.PositiveInfinity,
			got:  cty.NumberIntVal(1),
			msg:  `assertion at #out: want=cty.NumberFloatVal(+Inf) got=1`,
		},
		{
			name: "marked value falls back to the Go syntax instead of panicking",
			want: cty.StringVal("abc").Mark("note"),
			got:  cty.ListVal([]cty.Value{cty.StringVal("abc").Mark("note")}),
			msg:  `assertion at #out: want=cty.StringVal("abc").Mark("note") got=cty.ListVal([]cty.Value{cty.StringVal("abc").Mark("note")})`,
		},
		{
			name: "plain Go values",
			want: "abc",
			got:  42,
			msg:  `assertion at #out: want="abc" got=42`,
		},
		{
			name: "plain Go nil and collections",
			want: nil,
			got:  []string{"a", "b"},
			msg:  `assertion at #out: want=null got=["a","b"]`,
		},
		{
			name: "plain Go value JSON cannot encode",
			want: complex(1, 2),
			got:  true,
			msg:  `assertion at #out: want=(1+2i) got=true`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := &Mismatch{Kind: kindAssertion, Path: "#out", Want: tt.want, Got: tt.got}

			if got := m.Error(); got != tt.msg {
				t.Fatalf("Error() mismatch\n got: %s\nwant: %s", got, tt.msg)
			}
		})
	}
}

func TestMismatchErrorPrefersMessage(t *testing.T) {
	t.Parallel()

	m := &Mismatch{
		Kind:    kindAssertion,
		Path:    "$.id",
		Want:    cty.StringVal("a"),
		Got:     cty.StringVal("b"),
		Message: "missing required field",
	}

	if got, want := m.Error(), "assertion at $.id: missing required field"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

// TestMatchJSONScalarMismatchMessage covers the path the issue was
// reported on: MatchJSON hands its operands to Mismatch unchanged.
func TestMatchJSONScalarMismatchMessage(t *testing.T) {
	t.Parallel()

	err := MatchJSON(cty.StringVal("submitted with abc"), cty.StringVal("submitted with abcEnter"), false, "#out")
	if err == nil {
		t.Fatal("expected a mismatch")
	}

	want := `assertion at #out: want="submitted with abc" got="submitted with abcEnter"`
	if got := err.Error(); got != want {
		t.Fatalf("Error() mismatch\n got: %s\nwant: %s", got, want)
	}
}
