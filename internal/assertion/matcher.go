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
		return &Mismatch{Kind: "assertion", Path: path, Message: fmt.Sprintf("unknown matcher %q", name)}
	}

	return handler(args, actual, path)
}

func matchExists(args map[string]cty.Value, actual cty.Value, path string) error {
	_ = args

	if !actual.IsNull() {
		return nil
	}

	return &Mismatch{Kind: "assertion", Path: path, Message: "value does not exist"}
}

func matchNotExists(args map[string]cty.Value, actual cty.Value, path string) error {
	_ = args

	if actual.IsNull() {
		return nil
	}

	return &Mismatch{Kind: "assertion", Path: path, Message: "value exists but should not"}
}

func matchIsString(args map[string]cty.Value, actual cty.Value, path string) error {
	_ = args

	if actual.Type() == cty.String {
		return nil
	}

	return &Mismatch{Kind: "assertion", Path: path, Message: "value is not a string"}
}

func matchIsNumber(args map[string]cty.Value, actual cty.Value, path string) error {
	_ = args

	if actual.Type() == cty.Number {
		return nil
	}

	return &Mismatch{Kind: "assertion", Path: path, Message: "value is not a number"}
}

func matchIsBool(args map[string]cty.Value, actual cty.Value, path string) error {
	_ = args

	if actual.Type() == cty.Bool {
		return nil
	}

	return &Mismatch{Kind: "assertion", Path: path, Message: "value is not a bool"}
}

func matchIsArray(args map[string]cty.Value, actual cty.Value, path string) error {
	_ = args

	if actual.Type().IsListType() || actual.Type().IsTupleType() {
		return nil
	}

	return &Mismatch{Kind: "assertion", Path: path, Message: "value is not an array"}
}

func matchIsObject(args map[string]cty.Value, actual cty.Value, path string) error {
	_ = args

	if actual.Type().IsObjectType() || actual.Type().IsMapType() {
		return nil
	}

	return &Mismatch{Kind: "assertion", Path: path, Message: "value is not an object"}
}

func matchContains(args map[string]cty.Value, actual cty.Value, path string) error {
	needle, ok := args["value"]
	if !ok {
		return &Mismatch{Kind: "assertion", Path: path, Message: "contains matcher is missing value"}
	}

	if actual.Type() == cty.String && needle.Type() == cty.String {
		if strings.Contains(actual.AsString(), needle.AsString()) {
			return nil
		}

		return &Mismatch{Kind: "assertion", Path: path, Message: fmt.Sprintf("%q does not contain %q", actual.AsString(), needle.AsString())}
	}

	if actual.Type().IsListType() || actual.Type().IsTupleType() {
		for it := actual.ElementIterator(); it.Next(); {
			_, elem := it.Element()
			if elem.RawEquals(needle) {
				return nil
			}
		}

		return &Mismatch{Kind: "assertion", Path: path, Message: "array does not contain expected value"}
	}

	return &Mismatch{Kind: "assertion", Path: path, Message: "contains matcher requires string or array"}
}

func matchRegex(args map[string]cty.Value, actual cty.Value, path string) error {
	patternVal, ok := args["value"]
	if !ok || patternVal.Type() != cty.String {
		return &Mismatch{Kind: "assertion", Path: path, Message: "matches matcher requires regex pattern"}
	}

	if actual.Type() != cty.String {
		return &Mismatch{Kind: "assertion", Path: path, Message: "matches matcher requires actual string"}
	}

	re, err := regexp.Compile(patternVal.AsString())
	if err != nil {
		return &Mismatch{Kind: "assertion", Path: path, Message: err.Error()}
	}

	if re.MatchString(actual.AsString()) {
		return nil
	}

	return &Mismatch{Kind: "assertion", Path: path, Message: fmt.Sprintf("%q does not match %q", actual.AsString(), patternVal.AsString())}
}

func matchAny(args map[string]cty.Value, actual cty.Value, path string) error {
	_ = args
	_ = actual
	_ = path

	return nil
}

// unwrapFieldMatcher returns inner matcher value when expVal is optional/required.
// optional is reported via isOpt, required via isReq. Inner is unwrapped value.
func unwrapFieldMatcher(expVal cty.Value) (isOpt, isReq bool, inner cty.Value) {
	name, args, ok := isMatcher(expVal)
	if !ok {
		return false, false, expVal
	}

	switch name {
	case matcherOptional:
		return true, false, args["value"]
	case matcherRequired:
		return false, true, args["value"]
	}

	return false, false, expVal
}

func matchOneOf(args map[string]cty.Value, actual cty.Value, path string) error {
	values, ok := args["value"]
	if !ok {
		return &Mismatch{Kind: "assertion", Path: path, Message: "one_of matcher is missing values"}
	}

	if !values.Type().IsTupleType() && !values.Type().IsListType() {
		return &Mismatch{Kind: "assertion", Path: path, Message: "one_of matcher requires list"}
	}

	for it := values.ElementIterator(); it.Next(); {
		_, candidate := it.Element()
		if candidate.RawEquals(actual) {
			return nil
		}
	}

	return &Mismatch{Kind: "assertion", Path: path, Message: "value is not in allowed list"}
}
