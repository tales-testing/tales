package lang

import (
	"strings"
	"testing"
)

func TestThresholdMatcherProducesTaggedObject(t *testing.T) {
	t.Parallel()

	value := evalTestExpression(t, `lt(100)`)
	if !value.Type().IsObjectType() {
		t.Fatalf("expected object, got %s", value.Type().FriendlyName())
	}

	if value.GetAttr(matcherKey).AsString() != "lt" {
		t.Fatalf("expected matcher name lt, got %s", value.GetAttr(matcherKey).AsString())
	}

	inner := value.GetAttr(paramValue)
	f, _ := inner.AsBigFloat().Float64()

	if f != 100 {
		t.Fatalf("expected threshold 100, got %v", f)
	}
}

func TestThresholdAcceptsDurationString(t *testing.T) {
	t.Parallel()

	for _, src := range []string{
		`lt("100ms")`,
		`lte("2.5s")`,
		`gt("1m")`,
		`gte("500us")`,
	} {
		value, err := evalTestExpressionError(src)
		if err != nil {
			t.Fatalf("%s should be valid, got %v", src, err)
		}

		if value.GetAttr(paramValue).AsString() == "" {
			t.Fatalf("%s should preserve threshold string", src)
		}
	}
}

func TestThresholdRejectsInvalidDuration(t *testing.T) {
	t.Parallel()

	_, err := evalTestExpressionError(`lt("not-a-duration")`)
	if err == nil {
		t.Fatalf("lt with invalid duration should fail at HCL eval")
	}

	if !strings.Contains(err.Error(), "lt threshold must be number or duration") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestThresholdRejectsWrongType(t *testing.T) {
	t.Parallel()

	_, err := evalTestExpressionError(`lt(true)`)
	if err == nil {
		t.Fatalf("lt(true) should fail")
	}

	if !strings.Contains(err.Error(), "lt threshold must be number or duration") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBetweenMatcherProducesTaggedObject(t *testing.T) {
	t.Parallel()

	value := evalTestExpression(t, `between(10, 100)`)
	if value.GetAttr(matcherKey).AsString() != "between" {
		t.Fatalf("unexpected matcher name: %s", value.GetAttr(matcherKey).AsString())
	}

	lo, _ := value.GetAttr(paramMin).AsBigFloat().Float64()
	hi, _ := value.GetAttr(paramMax).AsBigFloat().Float64()

	if lo != 10 || hi != 100 {
		t.Fatalf("unexpected bounds: lo=%v hi=%v", lo, hi)
	}
}

func TestBetweenRejectsReversedBounds(t *testing.T) {
	t.Parallel()

	_, err := evalTestExpressionError(`between(100, 10)`)
	if err == nil {
		t.Fatalf("between(100, 10) should fail")
	}

	if !strings.Contains(err.Error(), "between min must be <= max") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBetweenAcceptsDurations(t *testing.T) {
	t.Parallel()

	if _, err := evalTestExpressionError(`between("100ms", "1s")`); err != nil {
		t.Fatalf("between with durations should be valid, got %v", err)
	}
}

func TestBetweenRejectsInvalidDuration(t *testing.T) {
	t.Parallel()

	_, err := evalTestExpressionError(`between(10, "not-a-duration")`)
	if err == nil {
		t.Fatalf("between with invalid duration max should fail")
	}

	if !strings.Contains(err.Error(), "between threshold must be number or duration") {
		t.Fatalf("unexpected error: %v", err)
	}
}
