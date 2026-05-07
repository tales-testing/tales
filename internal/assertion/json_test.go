package assertion

import (
	"testing"

	"github.com/zclconf/go-cty/cty"
)

func TestMatchJSONPartialObject(t *testing.T) {
	t.Parallel()
	expected := cty.ObjectVal(map[string]cty.Value{
		"id": cty.StringVal("1"),
	})
	actual := cty.ObjectVal(map[string]cty.Value{
		"id":    cty.StringVal("1"),
		"email": cty.StringVal("foo@example.com"),
	})

	if err := MatchJSON(expected, actual, false, "$"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMatchJSONStrictObject(t *testing.T) {
	t.Parallel()
	expected := cty.ObjectVal(map[string]cty.Value{"id": cty.StringVal("1")})
	actual := cty.ObjectVal(map[string]cty.Value{"id": cty.StringVal("1"), "extra": cty.StringVal("x")})

	if err := MatchJSON(expected, actual, true, "$"); err == nil {
		t.Fatalf("expected strict mismatch")
	}
}

func TestMatchers(t *testing.T) {
	t.Parallel()
	matcher := cty.ObjectVal(map[string]cty.Value{matcherKey: cty.StringVal("contains"), "value": cty.StringVal("json")})
	if err := MatchJSON(matcher, cty.StringVal("application/json"), true, "$"); err != nil {
		t.Fatalf("contains matcher failed: %v", err)
	}

	oneOf := cty.ObjectVal(map[string]cty.Value{matcherKey: cty.StringVal("one_of"), "value": cty.TupleVal([]cty.Value{cty.NumberIntVal(200), cty.NumberIntVal(204)})})
	if err := MatchJSON(oneOf, cty.NumberIntVal(204), true, "status"); err != nil {
		t.Fatalf("one_of matcher failed: %v", err)
	}
}
