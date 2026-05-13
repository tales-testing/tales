package assertion

import (
	"strings"
	"testing"

	"github.com/zclconf/go-cty/cty"
)

func optionalMatcher(inner cty.Value) cty.Value {
	return cty.ObjectVal(map[string]cty.Value{
		matcherKey: cty.StringVal(matcherOptional),
		"value":    inner,
	})
}

func requiredMatcher(inner cty.Value) cty.Value {
	return cty.ObjectVal(map[string]cty.Value{
		matcherKey: cty.StringVal(matcherRequired),
		"value":    inner,
	})
}

func anyMatcher() cty.Value {
	return cty.ObjectVal(map[string]cty.Value{matcherKey: cty.StringVal(matcherAny)})
}

func isStringMatcher() cty.Value {
	return cty.ObjectVal(map[string]cty.Value{matcherKey: cty.StringVal("is_string")})
}

func matchesMatcher(pattern string) cty.Value {
	return cty.ObjectVal(map[string]cty.Value{
		matcherKey: cty.StringVal("matches"),
		"value":    cty.StringVal(pattern),
	})
}

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

// A: missing required field still fails.
func TestMatchJSONMissingRequiredField(t *testing.T) {
	t.Parallel()

	expected := cty.ObjectVal(map[string]cty.Value{
		"tags": cty.TupleVal([]cty.Value{}),
	})
	actual := cty.EmptyObjectVal

	err := MatchJSON(expected, actual, false, "$")
	if err == nil {
		t.Fatalf("expected error for missing required field")
	}

	if !strings.Contains(err.Error(), "missing required field") {
		t.Fatalf("expected missing required field error, got %v", err)
	}

	if !strings.Contains(err.Error(), "$.tags") {
		t.Fatalf("expected path $.tags in error, got %v", err)
	}
}

// B: missing optional field passes.
func TestMatchJSONOptionalEmptyArrayMissing(t *testing.T) {
	t.Parallel()

	expected := cty.ObjectVal(map[string]cty.Value{
		"tags": optionalMatcher(cty.TupleVal([]cty.Value{})),
	})
	actual := cty.EmptyObjectVal

	if err := MatchJSON(expected, actual, false, "$"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// C: present optional empty array passes.
func TestMatchJSONOptionalEmptyArrayPresent(t *testing.T) {
	t.Parallel()

	expected := cty.ObjectVal(map[string]cty.Value{
		"tags": optionalMatcher(cty.TupleVal([]cty.Value{})),
	})
	actual := cty.ObjectVal(map[string]cty.Value{
		"tags": cty.TupleVal([]cty.Value{}),
	})

	if err := MatchJSON(expected, actual, false, "$"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// D: present optional non-empty array fails.
func TestMatchJSONOptionalEmptyArrayPresentNonEmpty(t *testing.T) {
	t.Parallel()

	expected := cty.ObjectVal(map[string]cty.Value{
		"tags": optionalMatcher(cty.TupleVal([]cty.Value{})),
	})
	actual := cty.ObjectVal(map[string]cty.Value{
		"tags": cty.TupleVal([]cty.Value{cty.StringVal("admin")}),
	})

	if err := MatchJSON(expected, actual, false, "$"); err == nil {
		t.Fatalf("expected mismatch")
	}
}

// E: optional enum default string.
func TestMatchJSONOptionalEnumDefault(t *testing.T) {
	t.Parallel()

	expected := cty.ObjectVal(map[string]cty.Value{
		"role": optionalMatcher(cty.StringVal("ROLE_UNSPECIFIED")),
	})

	cases := []struct {
		name    string
		actual  cty.Value
		wantErr bool
	}{
		{"missing", cty.EmptyObjectVal, false},
		{"matching", cty.ObjectVal(map[string]cty.Value{"role": cty.StringVal("ROLE_UNSPECIFIED")}), false},
		{"mismatched", cty.ObjectVal(map[string]cty.Value{"role": cty.StringVal("ADMIN")}), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := MatchJSON(expected, tc.actual, false, "$")
			if tc.wantErr && err == nil {
				t.Fatalf("expected error")
			}

			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// F: optional number.
func TestMatchJSONOptionalNumber(t *testing.T) {
	t.Parallel()

	expected := cty.ObjectVal(map[string]cty.Value{
		"status": optionalMatcher(cty.NumberIntVal(0)),
	})

	cases := []struct {
		name    string
		actual  cty.Value
		wantErr bool
	}{
		{"missing", cty.EmptyObjectVal, false},
		{"matching", cty.ObjectVal(map[string]cty.Value{"status": cty.NumberIntVal(0)}), false},
		{"mismatched", cty.ObjectVal(map[string]cty.Value{"status": cty.NumberIntVal(1)}), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := MatchJSON(expected, tc.actual, false, "$")
			if tc.wantErr && err == nil {
				t.Fatalf("expected error")
			}

			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// G: optional empty string.
func TestMatchJSONOptionalEmptyString(t *testing.T) {
	t.Parallel()

	expected := cty.ObjectVal(map[string]cty.Value{
		"display_name": optionalMatcher(cty.StringVal("")),
	})

	cases := []struct {
		name    string
		actual  cty.Value
		wantErr bool
	}{
		{"missing", cty.EmptyObjectVal, false},
		{"matching", cty.ObjectVal(map[string]cty.Value{"display_name": cty.StringVal("")}), false},
		{"mismatched", cty.ObjectVal(map[string]cty.Value{"display_name": cty.StringVal("Axel")}), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := MatchJSON(expected, tc.actual, false, "$")
			if tc.wantErr && err == nil {
				t.Fatalf("expected error")
			}

			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// H: optional with existing matcher.
func TestMatchJSONOptionalWithInnerMatcher(t *testing.T) {
	t.Parallel()

	expected := cty.ObjectVal(map[string]cty.Value{
		"nickname": optionalMatcher(isStringMatcher()),
	})

	cases := []struct {
		name    string
		actual  cty.Value
		wantErr bool
	}{
		{"missing", cty.EmptyObjectVal, false},
		{"matching", cty.ObjectVal(map[string]cty.Value{"nickname": cty.StringVal("axel")}), false},
		{"mismatched", cty.ObjectVal(map[string]cty.Value{"nickname": cty.NumberIntVal(123)}), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := MatchJSON(expected, tc.actual, false, "$")
			if tc.wantErr && err == nil {
				t.Fatalf("expected error")
			}

			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// I: optional nested object.
func TestMatchJSONOptionalNestedObject(t *testing.T) {
	t.Parallel()

	expected := cty.ObjectVal(map[string]cty.Value{
		"profile": optionalMatcher(cty.ObjectVal(map[string]cty.Value{
			"avatar": optionalMatcher(cty.StringVal("")),
		})),
	})

	cases := []struct {
		name    string
		actual  cty.Value
		wantErr bool
	}{
		{"missing", cty.EmptyObjectVal, false},
		{"present_empty", cty.ObjectVal(map[string]cty.Value{"profile": cty.EmptyObjectVal}), false},
		{"present_avatar_default", cty.ObjectVal(map[string]cty.Value{"profile": cty.ObjectVal(map[string]cty.Value{"avatar": cty.StringVal("")})}), false},
		{"present_avatar_value", cty.ObjectVal(map[string]cty.Value{"profile": cty.ObjectVal(map[string]cty.Value{"avatar": cty.StringVal("x")})}), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := MatchJSON(expected, tc.actual, false, "$")
			if tc.wantErr && err == nil {
				t.Fatalf("expected error")
			}

			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// J: null behavior.
func TestMatchJSONOptionalNullBehavior(t *testing.T) {
	t.Parallel()

	t.Run("optional_empty_string_vs_null", func(t *testing.T) {
		t.Parallel()

		expected := cty.ObjectVal(map[string]cty.Value{
			"name": optionalMatcher(cty.StringVal("")),
		})
		actual := cty.ObjectVal(map[string]cty.Value{
			"name": cty.NullVal(cty.String),
		})

		if err := MatchJSON(expected, actual, false, "$"); err == nil {
			t.Fatalf("expected error for null vs empty string")
		}
	})

	t.Run("optional_null_present_null", func(t *testing.T) {
		t.Parallel()

		expected := cty.ObjectVal(map[string]cty.Value{
			"name": optionalMatcher(cty.NullVal(cty.DynamicPseudoType)),
		})
		actual := cty.ObjectVal(map[string]cty.Value{
			"name": cty.NullVal(cty.String),
		})

		if err := MatchJSON(expected, actual, false, "$"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("optional_null_missing", func(t *testing.T) {
		t.Parallel()

		expected := cty.ObjectVal(map[string]cty.Value{
			"name": optionalMatcher(cty.NullVal(cty.DynamicPseudoType)),
		})
		actual := cty.EmptyObjectVal

		if err := MatchJSON(expected, actual, false, "$"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// K: required wrapper passes when present and matching.
func TestMatchJSONRequiredPresentMatching(t *testing.T) {
	t.Parallel()

	expected := cty.ObjectVal(map[string]cty.Value{
		"email": requiredMatcher(isStringMatcher()),
	})
	actual := cty.ObjectVal(map[string]cty.Value{
		"email": cty.StringVal("a@example.com"),
	})

	if err := MatchJSON(expected, actual, false, "$"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// L: required wrapper fails when missing.
func TestMatchJSONRequiredMissing(t *testing.T) {
	t.Parallel()

	expected := cty.ObjectVal(map[string]cty.Value{
		"email": requiredMatcher(isStringMatcher()),
	})
	actual := cty.EmptyObjectVal

	err := MatchJSON(expected, actual, false, "$")
	if err == nil {
		t.Fatalf("expected error for missing required field")
	}

	if !strings.Contains(err.Error(), "missing required field") {
		t.Fatalf("expected missing required field error, got %v", err)
	}

	if !strings.Contains(err.Error(), "$.email") {
		t.Fatalf("expected path $.email in error, got %v", err)
	}
}

// M: required wrapper fails when present and wrong.
func TestMatchJSONRequiredPresentWrong(t *testing.T) {
	t.Parallel()

	expected := cty.ObjectVal(map[string]cty.Value{
		"email": requiredMatcher(isStringMatcher()),
	})
	actual := cty.ObjectVal(map[string]cty.Value{
		"email": cty.NumberIntVal(123),
	})

	if err := MatchJSON(expected, actual, false, "$"); err == nil {
		t.Fatalf("expected error")
	}
}

// N: any() matches all present values.
func TestMatchJSONAnyMatchesAllPresent(t *testing.T) {
	t.Parallel()

	expected := cty.ObjectVal(map[string]cty.Value{
		"a": anyMatcher(),
		"b": anyMatcher(),
		"c": anyMatcher(),
		"d": anyMatcher(),
		"e": anyMatcher(),
		"f": anyMatcher(),
	})
	actual := cty.ObjectVal(map[string]cty.Value{
		"a": cty.NullVal(cty.String),
		"b": cty.StringVal(""),
		"c": cty.NumberIntVal(0),
		"d": cty.False,
		"e": cty.TupleVal([]cty.Value{}),
		"f": cty.EmptyObjectVal,
	})

	if err := MatchJSON(expected, actual, false, "$"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// O: any() does not make field optional.
func TestMatchJSONAnyRequiresPresence(t *testing.T) {
	t.Parallel()

	expected := cty.ObjectVal(map[string]cty.Value{
		"metadata": anyMatcher(),
	})
	actual := cty.EmptyObjectVal

	if err := MatchJSON(expected, actual, false, "$"); err == nil {
		t.Fatalf("expected error for missing required field")
	}
}

// P: optional(any()) passes missing and present.
func TestMatchJSONOptionalAny(t *testing.T) {
	t.Parallel()

	expected := cty.ObjectVal(map[string]cty.Value{
		"metadata": optionalMatcher(anyMatcher()),
	})

	cases := []struct {
		name   string
		actual cty.Value
	}{
		{"missing", cty.EmptyObjectVal},
		{"object", cty.ObjectVal(map[string]cty.Value{"metadata": cty.ObjectVal(map[string]cty.Value{"foo": cty.StringVal("bar")})})},
		{"null", cty.ObjectVal(map[string]cty.Value{"metadata": cty.NullVal(cty.String)})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if err := MatchJSON(expected, tc.actual, false, "$"); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// Q: required(any()) requires presence.
func TestMatchJSONRequiredAny(t *testing.T) {
	t.Parallel()

	expected := cty.ObjectVal(map[string]cty.Value{
		"metadata": requiredMatcher(anyMatcher()),
	})

	t.Run("missing", func(t *testing.T) {
		t.Parallel()

		if err := MatchJSON(expected, cty.EmptyObjectVal, false, "$"); err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("null", func(t *testing.T) {
		t.Parallel()

		actual := cty.ObjectVal(map[string]cty.Value{"metadata": cty.NullVal(cty.String)})
		if err := MatchJSON(expected, actual, false, "$"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("object", func(t *testing.T) {
		t.Parallel()

		actual := cty.ObjectVal(map[string]cty.Value{"metadata": cty.ObjectVal(map[string]cty.Value{"foo": cty.StringVal("bar")})})
		if err := MatchJSON(expected, actual, false, "$"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// R: nested required and optional.
func TestMatchJSONNestedRequiredOptional(t *testing.T) {
	t.Parallel()

	expected := cty.ObjectVal(map[string]cty.Value{
		"user": requiredMatcher(cty.ObjectVal(map[string]cty.Value{
			"id":   requiredMatcher(isStringMatcher()),
			"role": optionalMatcher(cty.StringVal("ROLE_UNSPECIFIED")),
		})),
	})

	t.Run("user_with_id", func(t *testing.T) {
		t.Parallel()

		actual := cty.ObjectVal(map[string]cty.Value{
			"user": cty.ObjectVal(map[string]cty.Value{"id": cty.StringVal("u_123")}),
		})
		if err := MatchJSON(expected, actual, false, "$"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("empty_root", func(t *testing.T) {
		t.Parallel()

		if err := MatchJSON(expected, cty.EmptyObjectVal, false, "$"); err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("empty_user", func(t *testing.T) {
		t.Parallel()

		actual := cty.ObjectVal(map[string]cty.Value{"user": cty.EmptyObjectVal})

		err := MatchJSON(expected, actual, false, "$")
		if err == nil {
			t.Fatalf("expected error")
		}

		if !strings.Contains(err.Error(), "$.user.id") {
			t.Fatalf("expected path $.user.id in error, got %v", err)
		}
	})
}

// S: error formatting.
func TestMatchJSONErrorFormatting(t *testing.T) {
	t.Parallel()

	t.Run("no_cty_prefix_on_missing", func(t *testing.T) {
		t.Parallel()

		expected := cty.ObjectVal(map[string]cty.Value{
			"email": requiredMatcher(isStringMatcher()),
		})

		err := MatchJSON(expected, cty.EmptyObjectVal, false, "$")
		if err == nil {
			t.Fatalf("expected error")
		}

		if strings.Contains(err.Error(), "cty.") {
			t.Fatalf("error should not contain cty. prefix: %v", err)
		}

		if !strings.Contains(err.Error(), "$.email") {
			t.Fatalf("expected path in error, got %v", err)
		}
	})

	t.Run("no_cty_prefix_on_mismatch", func(t *testing.T) {
		t.Parallel()

		expected := cty.ObjectVal(map[string]cty.Value{
			"email": requiredMatcher(matchesMatcher("^usr_")),
		})
		actual := cty.ObjectVal(map[string]cty.Value{"email": cty.StringVal("nope")})

		err := MatchJSON(expected, actual, false, "$")
		if err == nil {
			t.Fatalf("expected error")
		}

		if strings.Contains(err.Error(), "cty.") {
			t.Fatalf("error should not contain cty. prefix: %v", err)
		}

		if !strings.Contains(err.Error(), "$.email") {
			t.Fatalf("expected path in error, got %v", err)
		}
	})
}
