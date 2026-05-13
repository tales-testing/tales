package assertion

import (
	"fmt"

	"github.com/zclconf/go-cty/cty"
)

// Equal checks exact value equality with matcher support.
func Equal(path string, expected, actual cty.Value) error {
	if name, args, ok := isMatcher(expected); ok {
		if name == matcherOptional || name == matcherRequired {
			return Equal(path, args["value"], actual)
		}

		return applyMatcher(name, args, actual, path)
	}

	if expected.RawEquals(actual) {
		return nil
	}

	return &Mismatch{Kind: "assertion", Path: path, Want: expected, Got: actual}
}

// MatchJSON performs JSON assertion with partial object semantics by default.
func MatchJSON(expected, actual cty.Value, strict bool, path string) error {
	if name, args, ok := isMatcher(expected); ok {
		if name == matcherOptional || name == matcherRequired {
			return MatchJSON(args["value"], actual, strict, path)
		}

		return applyMatcher(name, args, actual, path)
	}

	if expected.IsNull() {
		if actual.IsNull() {
			return nil
		}

		return &Mismatch{Kind: "assertion", Path: path, Message: "expected null"}
	}

	if actual.IsNull() {
		return &Mismatch{Kind: "assertion", Path: path, Message: "actual value is null"}
	}

	if expected.Type().IsObjectType() {
		return matchJSONObject(expected, actual, strict, path)
	}

	if expected.Type().IsTupleType() || expected.Type().IsListType() {
		return matchJSONArray(expected, actual, strict, path)
	}

	if expected.RawEquals(actual) {
		return nil
	}

	return &Mismatch{Kind: "assertion", Path: path, Want: expected, Got: actual}
}

func matchJSONObject(expected, actual cty.Value, strict bool, path string) error {
	if !actual.Type().IsObjectType() && !actual.Type().IsMapType() {
		return &Mismatch{Kind: "assertion", Path: path, Message: "expected object"}
	}

	expectedMap := expected.AsValueMap()

	actualMap := actual.AsValueMap()
	for key, expVal := range expectedMap {
		actVal, ok := actualMap[key]
		isOpt, isReq, inner := unwrapFieldMatcher(expVal)

		if !ok {
			if isOpt {
				continue
			}

			if name, _, is := isMatcher(expVal); is && name == matcherNotExists {
				continue
			}

			if name, _, is := isMatcher(expVal); is && name == matcherExists {
				return &Mismatch{Kind: "assertion", Path: path + "." + key, Message: "value does not exist"}
			}

			return &Mismatch{Kind: "assertion", Path: path + "." + key, Message: "missing required field"}
		}

		target := expVal
		if isOpt || isReq {
			target = inner
		}

		if err := MatchJSON(target, actVal, strict, path+"."+key); err != nil {
			return err
		}
	}

	if strict && len(actualMap) != len(expectedMap) {
		return &Mismatch{Kind: "assertion", Path: path, Message: "object has extra fields"}
	}

	return nil
}

func matchJSONArray(expected, actual cty.Value, strict bool, path string) error {
	if !actual.Type().IsTupleType() && !actual.Type().IsListType() {
		return &Mismatch{Kind: "assertion", Path: path, Message: "expected array"}
	}

	expLen := expected.LengthInt()

	actLen := actual.LengthInt()
	if expLen != actLen {
		return &Mismatch{Kind: "assertion", Path: path, Message: fmt.Sprintf("array length mismatch want=%d got=%d", expLen, actLen)}
	}

	for i := 0; i < expLen; i++ {
		expItem := expected.Index(cty.NumberIntVal(int64(i)))

		actItem := actual.Index(cty.NumberIntVal(int64(i)))
		if err := MatchJSON(expItem, actItem, strict, fmt.Sprintf("%s[%d]", path, i)); err != nil {
			return err
		}
	}

	return nil
}
