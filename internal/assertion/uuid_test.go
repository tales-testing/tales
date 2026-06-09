package assertion

import (
	"strings"
	"testing"

	"github.com/zclconf/go-cty/cty"
)

func uuidMatcher(version string) cty.Value {
	attrs := map[string]cty.Value{matcherKey: cty.StringVal("uuid")}
	if version != "" {
		attrs[uuidVersionArg] = cty.StringVal(version)
	}

	return cty.ObjectVal(attrs)
}

const (
	sampleUUIDv1  = "c232ab00-9414-11ec-b3c8-9e6bdeced846"
	sampleUUIDv4  = "f47ac10b-58cc-4372-a567-0e02b2c3d479"
	sampleUUIDv7  = "017f22e2-79b0-7cc3-98c4-dc0c0c07398f"
	sampleUUIDup  = "F47AC10B-58CC-4372-A567-0E02B2C3D479" // v4 uppercase
	sampleUUIDnil = "00000000-0000-0000-0000-000000000000"
	sampleUUIDmax = "ffffffff-ffff-ffff-ffff-ffffffffffff"
	badVariantv4  = "f47ac10b-58cc-4372-c567-0e02b2c3d479" // variant nibble c
)

func TestUUIDMatcherPass(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		version string
		actual  string
	}{
		{"any accepts v1", "", sampleUUIDv1},
		{"any accepts v4", "", sampleUUIDv4},
		{"any accepts v7", "", sampleUUIDv7},
		{"any accepts nil", "", sampleUUIDnil},
		{"any accepts max", "", sampleUUIDmax},
		{"any accepts uppercase", "", sampleUUIDup},
		{"v1 matches", "1", sampleUUIDv1},
		{"v4 matches", "4", sampleUUIDv4},
		{"v7 matches", "7", sampleUUIDv7},
		{"v4 matches uppercase", "4", sampleUUIDup},
		{"nil token", "nil", sampleUUIDnil},
		{"max token", "max", sampleUUIDmax},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if err := MatchJSON(uuidMatcher(tc.version), cty.StringVal(tc.actual), true, "$"); err != nil {
				t.Fatalf("expected pass, got %v", err)
			}
		})
	}
}

func TestUUIDMatcherFail(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		version string
		actual  cty.Value
	}{
		{"wrong version", "1", cty.StringVal(sampleUUIDv4)},
		{"nil is not v4", "4", cty.StringVal(sampleUUIDnil)},
		{"max is not v4", "4", cty.StringVal(sampleUUIDmax)},
		{"v4 token rejects nil", "nil", cty.StringVal(sampleUUIDv4)},
		{"nil token rejects max", "nil", cty.StringVal(sampleUUIDmax)},
		{"max token rejects nil", "max", cty.StringVal(sampleUUIDnil)},
		{"any rejects bad variant", "", cty.StringVal(badVariantv4)},
		{"versioned rejects bad variant", "4", cty.StringVal(badVariantv4)},
		{"not a uuid", "", cty.StringVal("not-a-uuid")},
		{"unhyphenated", "", cty.StringVal("f47ac10b58cc4372a5670e02b2c3d479")},
		{"any rejects non-string", "", cty.NumberIntVal(5)},
		{"any rejects null", "", cty.NullVal(cty.String)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if err := MatchJSON(uuidMatcher(tc.version), tc.actual, true, "$"); err == nil {
				t.Fatalf("expected failure, got pass")
			}
		})
	}
}

func TestUUIDMatcherMessages(t *testing.T) {
	t.Parallel()

	err := MatchJSON(uuidMatcher("4"), cty.StringVal(sampleUUIDv1), true, "$")
	if err == nil || !strings.Contains(err.Error(), "is not a UUID v4") {
		t.Fatalf("expected v4 mismatch message, got %v", err)
	}

	err = MatchJSON(uuidMatcher("nil"), cty.StringVal(sampleUUIDv4), true, "$")
	if err == nil || !strings.Contains(err.Error(), "is not the nil UUID") {
		t.Fatalf("expected nil mismatch message, got %v", err)
	}

	err = MatchJSON(uuidMatcher(""), cty.StringVal("not-a-uuid"), true, "$")
	if err == nil || !strings.Contains(err.Error(), "is not a valid UUID") {
		t.Fatalf("expected invalid uuid message, got %v", err)
	}
}
