package lang

import (
	"strings"
	"testing"
)

func TestUUIDMatcherNoArgHasNoVersion(t *testing.T) {
	t.Parallel()

	value := evalTestExpression(t, `uuid()`)

	if !value.Type().IsObjectType() {
		t.Fatalf("expected object, got %s", value.Type().FriendlyName())
	}

	if name := value.GetAttr(matcherKey); name.AsString() != "uuid" {
		t.Fatalf("expected matcher name uuid, got %s", name.AsString())
	}

	if value.Type().HasAttribute(paramVersion) {
		t.Fatalf("expected no version attribute for uuid()")
	}
}

func TestUUIDMatcherVersionNormalization(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		`uuid("v4")`:  "4",
		`uuid("4")`:   "4",
		`uuid("V7")`:  "7",
		`uuid("v1")`:  "1",
		`uuid("8")`:   "8",
		`uuid("nil")`: "nil",
		`uuid("NIL")`: "nil",
		`uuid("max")`: "max",
		`uuid("Max")`: "max",
	}

	for src, wantVersion := range cases {
		value := evalTestExpression(t, src)

		if name := value.GetAttr(matcherKey); name.AsString() != "uuid" {
			t.Fatalf("%s: expected matcher name uuid, got %s", src, name.AsString())
		}

		if !value.Type().HasAttribute(paramVersion) {
			t.Fatalf("%s: expected version attribute", src)
		}

		if got := value.GetAttr(paramVersion).AsString(); got != wantVersion {
			t.Fatalf("%s: expected version %q, got %q", src, wantVersion, got)
		}
	}
}

func TestUUIDMatcherRejectsUnknownVersion(t *testing.T) {
	t.Parallel()

	for _, src := range []string{`uuid("v9")`, `uuid("0")`, `uuid("foo")`, `uuid("v")`, `uuid("")`} {
		_, err := evalTestExpressionError(src)
		if err == nil {
			t.Fatalf("%s: expected error, got nil", src)
		}

		if !strings.Contains(err.Error(), "unsupported uuid version") {
			t.Fatalf("%s: expected unsupported uuid version error, got %v", src, err)
		}
	}
}

func TestUUIDMatcherRejectsTooManyArgs(t *testing.T) {
	t.Parallel()

	_, err := evalTestExpressionError(`uuid("v4", "v5")`)
	if err == nil {
		t.Fatalf("expected error for two arguments, got nil")
	}

	if !strings.Contains(err.Error(), "at most one version argument") {
		t.Fatalf("expected at most one version argument error, got %v", err)
	}
}
