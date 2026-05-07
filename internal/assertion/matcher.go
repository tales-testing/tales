package assertion

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/zclconf/go-cty/cty"
)

const matcherKey = "__tales_matcher"

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
	switch name {
	case "exists":
		if !actual.IsNull() {
			return nil
		}
		return &Mismatch{Kind: "assertion", Path: path, Message: "value does not exist"}
	case "not_exists":
		if actual.IsNull() {
			return nil
		}
		return &Mismatch{Kind: "assertion", Path: path, Message: "value exists but should not"}
	case "is_string":
		if actual.Type() == cty.String {
			return nil
		}
		return &Mismatch{Kind: "assertion", Path: path, Message: "value is not a string"}
	case "is_number":
		if actual.Type() == cty.Number {
			return nil
		}
		return &Mismatch{Kind: "assertion", Path: path, Message: "value is not a number"}
	case "is_bool":
		if actual.Type() == cty.Bool {
			return nil
		}
		return &Mismatch{Kind: "assertion", Path: path, Message: "value is not a bool"}
	case "is_array":
		if actual.Type().IsListType() || actual.Type().IsTupleType() {
			return nil
		}
		return &Mismatch{Kind: "assertion", Path: path, Message: "value is not an array"}
	case "is_object":
		if actual.Type().IsObjectType() || actual.Type().IsMapType() {
			return nil
		}
		return &Mismatch{Kind: "assertion", Path: path, Message: "value is not an object"}
	case "contains":
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
	case "matches":
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
	case "one_of":
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
	default:
		return &Mismatch{Kind: "assertion", Path: path, Message: fmt.Sprintf("unknown matcher %q", name)}
	}
}
