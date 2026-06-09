package assertion

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/zclconf/go-cty/cty"
)

const (
	uuidVersionArg = "version"
	uuidNilValue   = "00000000-0000-0000-0000-000000000000"
	uuidMaxValue   = "ffffffff-ffff-ffff-ffff-ffffffffffff"
)

// uuidCanonicalRE matches the canonical hyphenated 8-4-4-4-12 form, case
// insensitive. Braced ({...}), URN (urn:uuid:) and unhyphenated forms are
// intentionally rejected: JSON API responses use the canonical form.
var uuidCanonicalRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// matchUUID validates that actual is a UUID string. The optional "version"
// arg (canonical "1".."8", "nil" or "max", set by the lang factory) pins a
// specific RFC 9562 form; absent, any official UUID is accepted.
func matchUUID(args map[string]cty.Value, actual cty.Value, path string) error {
	if actual.IsNull() {
		return &Mismatch{Kind: kindAssertion, Path: path, Message: msgValueDoesNotExist}
	}

	if actual.Type() != cty.String {
		return &Mismatch{Kind: kindAssertion, Path: path, Message: "uuid matcher requires a string value"}
	}

	value := actual.AsString()
	if !uuidCanonicalRE.MatchString(value) {
		return &Mismatch{Kind: kindAssertion, Path: path, Message: fmt.Sprintf("%q is not a valid UUID", value)}
	}

	lower := strings.ToLower(value)

	want := ""
	if v, ok := args[uuidVersionArg]; ok && v.Type() == cty.String {
		want = v.AsString()
	}

	switch want {
	case "nil":
		return expectExactUUID(lower, uuidNilValue, value, "nil", path)
	case "max":
		return expectExactUUID(lower, uuidMaxValue, value, "max", path)
	case "":
		return validateAnyUUID(lower, value, path)
	default:
		return validateVersionedUUID(lower, value, want, path)
	}
}

func expectExactUUID(lower, want, original, label, path string) error {
	if lower == want {
		return nil
	}

	return &Mismatch{Kind: kindAssertion, Path: path, Message: fmt.Sprintf("%q is not the %s UUID", original, label)}
}

func validateAnyUUID(lower, original, path string) error {
	if lower == uuidNilValue || lower == uuidMaxValue {
		return nil
	}

	if v := lower[14]; v < '1' || v > '8' || !uuidVariantOK(lower) {
		return &Mismatch{Kind: kindAssertion, Path: path, Message: fmt.Sprintf("%q is not an official UUID", original)}
	}

	return nil
}

func validateVersionedUUID(lower, original, want, path string) error {
	if string(lower[14]) != want {
		return &Mismatch{Kind: kindAssertion, Path: path, Message: fmt.Sprintf("%q is not a UUID v%s", original, want)}
	}

	if !uuidVariantOK(lower) {
		return &Mismatch{Kind: kindAssertion, Path: path, Message: fmt.Sprintf("%q has an invalid UUID variant", original)}
	}

	return nil
}

// uuidVariantOK reports whether the variant nibble (index 19) is the RFC 9562
// variant (binary 10xx, i.e. one of 8, 9, a, b).
func uuidVariantOK(lower string) bool {
	switch lower[19] {
	case '8', '9', 'a', 'b':
		return true
	default:
		return false
	}
}
