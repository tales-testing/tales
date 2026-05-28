package assertion

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/zclconf/go-cty/cty"
)

const (
	matcherKey       = "__tales_matcher"
	matcherExists    = "exists"
	matcherNotExists = "not_exists"
	matcherOptional  = "optional"
	matcherRequired  = "required"
	matcherAny       = "any"

	kindAssertion        = "assertion"
	msgValueDoesNotExist = "value does not exist"
)

type matcherHandler func(args map[string]cty.Value, actual cty.Value, path string) error

var matcherHandlers = map[string]matcherHandler{
	matcherExists:    matchExists,
	matcherNotExists: matchNotExists,
	"is_string":      matchIsString,
	"is_number":      matchIsNumber,
	"is_bool":        matchIsBool,
	"is_array":       matchIsArray,
	"is_object":      matchIsObject,
	"contains":       matchContains,
	"matches":        matchRegex,
	"one_of":         matchOneOf,
	matcherAny:       matchAny,
	"lt":             matchLt,
	"lte":            matchLte,
	"gt":             matchGt,
	"gte":            matchGte,
	"between":        matchBetween,
}

func isMatcher(value cty.Value) (string, map[string]cty.Value, bool) {
	if !value.Type().IsObjectType() || value.IsNull() {
		return "", nil, false
	}

	if !value.Type().HasAttribute(matcherKey) {
		return "", nil, false
	}

	name := value.GetAttr(matcherKey)
	if name.Type() != cty.String {
		return "", nil, false
	}

	attrs := value.AsValueMap()
	delete(attrs, matcherKey)

	return name.AsString(), attrs, true
}

func applyMatcher(name string, args map[string]cty.Value, actual cty.Value, path string) error {
	handler, ok := matcherHandlers[name]
	if !ok {
		return &Mismatch{Kind: kindAssertion, Path: path, Message: fmt.Sprintf("unknown matcher %q", name)}
	}

	return handler(args, actual, path)
}

func matchExists(args map[string]cty.Value, actual cty.Value, path string) error {
	_ = args

	if !actual.IsNull() {
		return nil
	}

	return &Mismatch{Kind: kindAssertion, Path: path, Message: msgValueDoesNotExist}
}

func matchNotExists(args map[string]cty.Value, actual cty.Value, path string) error {
	_ = args

	if actual.IsNull() {
		return nil
	}

	return &Mismatch{Kind: kindAssertion, Path: path, Message: "value exists but should not"}
}

func matchIsString(args map[string]cty.Value, actual cty.Value, path string) error {
	_ = args

	if actual.Type() == cty.String {
		return nil
	}

	return &Mismatch{Kind: kindAssertion, Path: path, Message: "value is not a string"}
}

func matchIsNumber(args map[string]cty.Value, actual cty.Value, path string) error {
	_ = args

	if actual.Type() == cty.Number {
		return nil
	}

	return &Mismatch{Kind: kindAssertion, Path: path, Message: msgValueNotNumber}
}

func matchIsBool(args map[string]cty.Value, actual cty.Value, path string) error {
	_ = args

	if actual.Type() == cty.Bool {
		return nil
	}

	return &Mismatch{Kind: kindAssertion, Path: path, Message: "value is not a bool"}
}

func matchIsArray(args map[string]cty.Value, actual cty.Value, path string) error {
	_ = args

	if actual.Type().IsListType() || actual.Type().IsTupleType() {
		return nil
	}

	return &Mismatch{Kind: kindAssertion, Path: path, Message: "value is not an array"}
}

func matchIsObject(args map[string]cty.Value, actual cty.Value, path string) error {
	_ = args

	if actual.Type().IsObjectType() || actual.Type().IsMapType() {
		return nil
	}

	return &Mismatch{Kind: kindAssertion, Path: path, Message: "value is not an object"}
}

func matchContains(args map[string]cty.Value, actual cty.Value, path string) error {
	needle, ok := args["value"]
	if !ok {
		return &Mismatch{Kind: kindAssertion, Path: path, Message: "contains matcher is missing value"}
	}

	if actual.Type() == cty.String && needle.Type() == cty.String {
		if strings.Contains(actual.AsString(), needle.AsString()) {
			return nil
		}

		return &Mismatch{Kind: kindAssertion, Path: path, Message: fmt.Sprintf("%q does not contain %q", actual.AsString(), needle.AsString())}
	}

	if actual.Type().IsListType() || actual.Type().IsTupleType() {
		for it := actual.ElementIterator(); it.Next(); {
			_, elem := it.Element()
			if elem.RawEquals(needle) {
				return nil
			}
		}

		return &Mismatch{Kind: kindAssertion, Path: path, Message: "array does not contain expected value"}
	}

	return &Mismatch{Kind: kindAssertion, Path: path, Message: "contains matcher requires string or array"}
}

func matchRegex(args map[string]cty.Value, actual cty.Value, path string) error {
	patternVal, ok := args["value"]
	if !ok || patternVal.Type() != cty.String {
		return &Mismatch{Kind: kindAssertion, Path: path, Message: "matches matcher requires regex pattern"}
	}

	if actual.Type() != cty.String {
		return &Mismatch{Kind: kindAssertion, Path: path, Message: "matches matcher requires actual string"}
	}

	re, err := regexp.Compile(patternVal.AsString())
	if err != nil {
		return &Mismatch{Kind: kindAssertion, Path: path, Message: err.Error()}
	}

	if re.MatchString(actual.AsString()) {
		return nil
	}

	return &Mismatch{Kind: kindAssertion, Path: path, Message: fmt.Sprintf("%q does not match %q", actual.AsString(), patternVal.AsString())}
}

func matchAny(args map[string]cty.Value, actual cty.Value, path string) error {
	_ = args
	_ = actual
	_ = path

	return nil
}

// fieldWrapper returns the field-level wrapper kind (optional or required)
// and its inner expectation when value is one of those matchers. Returns
// ok=false when value is not a wrapper, or when the wrapper is malformed
// (missing "value" attribute) — in that case callers should not try to
// dereference inner and instead let applyMatcher surface a clean
// "unknown matcher" error if the value is still treated as a matcher.
func fieldWrapper(value cty.Value) (kind string, inner cty.Value, ok bool) {
	name, args, isM := isMatcher(value)
	if !isM {
		return "", cty.NilVal, false
	}

	if name != matcherOptional && name != matcherRequired {
		return "", cty.NilVal, false
	}

	inner, hasValue := args["value"]
	if !hasValue {
		return "", cty.NilVal, false
	}

	return name, inner, true
}

func matchOneOf(args map[string]cty.Value, actual cty.Value, path string) error {
	values, ok := args["value"]
	if !ok {
		return &Mismatch{Kind: kindAssertion, Path: path, Message: "one_of matcher is missing values"}
	}

	if !values.Type().IsTupleType() && !values.Type().IsListType() {
		return &Mismatch{Kind: kindAssertion, Path: path, Message: "one_of matcher requires list"}
	}

	for it := values.ElementIterator(); it.Next(); {
		_, candidate := it.Element()
		if candidate.RawEquals(actual) {
			return nil
		}
	}

	return &Mismatch{Kind: kindAssertion, Path: path, Message: "value is not in allowed list"}
}
