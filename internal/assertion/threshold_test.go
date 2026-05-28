package assertion

import (
	"strings"
	"testing"

	"github.com/zclconf/go-cty/cty"
)

func ltMatcher(threshold cty.Value) cty.Value {
	return cty.ObjectVal(map[string]cty.Value{
		matcherKey: cty.StringVal("lt"),
		"value":    threshold,
	})
}

func lteMatcher(threshold cty.Value) cty.Value {
	return cty.ObjectVal(map[string]cty.Value{
		matcherKey: cty.StringVal("lte"),
		"value":    threshold,
	})
}

func gtMatcher(threshold cty.Value) cty.Value {
	return cty.ObjectVal(map[string]cty.Value{
		matcherKey: cty.StringVal("gt"),
		"value":    threshold,
	})
}

func gteMatcher(threshold cty.Value) cty.Value {
	return cty.ObjectVal(map[string]cty.Value{
		matcherKey: cty.StringVal("gte"),
		"value":    threshold,
	})
}

func betweenMatcher(lo, hi cty.Value) cty.Value {
	return cty.ObjectVal(map[string]cty.Value{
		matcherKey: cty.StringVal("between"),
		"min":      lo,
		"max":      hi,
	})
}

func TestThresholdLtPassFail(t *testing.T) {
	t.Parallel()

	if err := MatchJSON(ltMatcher(cty.NumberIntVal(100)), cty.NumberIntVal(50), true, "$"); err != nil {
		t.Fatalf("lt should pass when 50 < 100, got %v", err)
	}

	err := MatchJSON(ltMatcher(cty.NumberIntVal(100)), cty.NumberIntVal(150), true, "$")
	if err == nil {
		t.Fatalf("lt should fail when 150 < 100")
	}

	if !strings.Contains(err.Error(), "value must be < 100") {
		t.Fatalf("unexpected error message: %v", err)
	}

	if !strings.Contains(err.Error(), "got 150") {
		t.Fatalf("error must report actual value, got %v", err)
	}
}

func TestThresholdLtBoundaryFails(t *testing.T) {
	t.Parallel()

	if err := MatchJSON(ltMatcher(cty.NumberIntVal(100)), cty.NumberIntVal(100), true, "$"); err == nil {
		t.Fatalf("lt should fail at boundary value")
	}
}

func TestThresholdLteBoundaryPasses(t *testing.T) {
	t.Parallel()

	if err := MatchJSON(lteMatcher(cty.NumberIntVal(100)), cty.NumberIntVal(100), true, "$"); err != nil {
		t.Fatalf("lte should pass at boundary value, got %v", err)
	}

	if err := MatchJSON(lteMatcher(cty.NumberIntVal(100)), cty.NumberIntVal(101), true, "$"); err == nil {
		t.Fatalf("lte should fail when actual > threshold")
	}
}

func TestThresholdGtPassFail(t *testing.T) {
	t.Parallel()

	if err := MatchJSON(gtMatcher(cty.NumberIntVal(40)), cty.NumberFloatVal(40.1), true, "$"); err != nil {
		t.Fatalf("gt should pass when 40.1 > 40, got %v", err)
	}

	if err := MatchJSON(gtMatcher(cty.NumberIntVal(40)), cty.NumberIntVal(40), true, "$"); err == nil {
		t.Fatalf("gt should fail at boundary value")
	}
}

func TestThresholdGteBoundaryPasses(t *testing.T) {
	t.Parallel()

	if err := MatchJSON(gteMatcher(cty.NumberFloatVal(0.95)), cty.NumberFloatVal(0.95), true, "$"); err != nil {
		t.Fatalf("gte should pass at boundary value, got %v", err)
	}
}

func TestThresholdDurationString(t *testing.T) {
	t.Parallel()

	// lt("200ms") should compare actual (interpreted as ms) against 200.
	if err := MatchJSON(ltMatcher(cty.StringVal("200ms")), cty.NumberIntVal(150), true, "$"); err != nil {
		t.Fatalf("lt(200ms) should pass when actual=150ms, got %v", err)
	}

	if err := MatchJSON(ltMatcher(cty.StringVal("200ms")), cty.NumberIntVal(250), true, "$"); err == nil {
		t.Fatalf("lt(200ms) should fail when actual=250ms")
	}

	// Duration string in actual position is supported too.
	if err := MatchJSON(ltMatcher(cty.StringVal("2s")), cty.StringVal("1500ms"), true, "$"); err != nil {
		t.Fatalf("lt(2s) should pass when actual=1500ms, got %v", err)
	}
}

func TestThresholdActualNotNumber(t *testing.T) {
	t.Parallel()

	err := MatchJSON(ltMatcher(cty.NumberIntVal(100)), cty.StringVal("not-a-number"), true, "$")
	if err == nil {
		t.Fatalf("lt should fail when actual is not numeric")
	}

	if !strings.Contains(err.Error(), "value is not a number") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestThresholdInvalidThreshold(t *testing.T) {
	t.Parallel()

	// Defensive path: the lang factory would normally reject this,
	// but the assertion engine must also be safe if a hand-crafted
	// matcher object slips through.
	err := MatchJSON(ltMatcher(cty.StringVal("nope")), cty.NumberIntVal(50), true, "$")
	if err == nil {
		t.Fatalf("lt with invalid threshold should fail")
	}

	if !strings.Contains(err.Error(), "lt threshold must be number or duration") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestThresholdMissingValue(t *testing.T) {
	t.Parallel()

	matcher := cty.ObjectVal(map[string]cty.Value{
		matcherKey: cty.StringVal("lt"),
	})

	err := MatchJSON(matcher, cty.NumberIntVal(50), true, "$")
	if err == nil {
		t.Fatalf("lt without value should fail")
	}

	if !strings.Contains(err.Error(), "missing value") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestBetweenInclusiveBoundaries(t *testing.T) {
	t.Parallel()

	m := betweenMatcher(cty.NumberIntVal(10), cty.NumberIntVal(100))

	for _, actual := range []cty.Value{
		cty.NumberIntVal(10),
		cty.NumberIntVal(50),
		cty.NumberIntVal(100),
	} {
		if err := MatchJSON(m, actual, true, "$"); err != nil {
			t.Fatalf("between(10,100) should pass for %v, got %v", actual.GoString(), err)
		}
	}
}

func TestBetweenOutOfRange(t *testing.T) {
	t.Parallel()

	m := betweenMatcher(cty.NumberIntVal(10), cty.NumberIntVal(100))

	err := MatchJSON(m, cty.NumberIntVal(9), true, "$")
	if err == nil {
		t.Fatalf("between(10,100) should fail for 9")
	}

	if !strings.Contains(err.Error(), "between 10 and 100") {
		t.Fatalf("unexpected error message: %v", err)
	}

	err = MatchJSON(m, cty.NumberIntVal(101), true, "$")
	if err == nil {
		t.Fatalf("between(10,100) should fail for 101")
	}
}

func TestBetweenWithDurations(t *testing.T) {
	t.Parallel()

	m := betweenMatcher(cty.StringVal("100ms"), cty.StringVal("1s"))

	if err := MatchJSON(m, cty.NumberIntVal(500), true, "$"); err != nil {
		t.Fatalf("between(100ms,1s) should pass for 500ms, got %v", err)
	}

	if err := MatchJSON(m, cty.NumberIntVal(50), true, "$"); err == nil {
		t.Fatalf("between(100ms,1s) should fail for 50ms")
	}
}

func TestBetweenInvalidBounds(t *testing.T) {
	t.Parallel()

	m := betweenMatcher(cty.NumberIntVal(100), cty.NumberIntVal(10))

	err := MatchJSON(m, cty.NumberIntVal(50), true, "$")
	if err == nil {
		t.Fatalf("between with min > max should fail")
	}

	if !strings.Contains(err.Error(), "between min must be <= max") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestThresholdInsideJSONExpectation(t *testing.T) {
	t.Parallel()

	expected := cty.ObjectVal(map[string]cty.Value{
		"latency_ms": ltMatcher(cty.NumberIntVal(200)),
		"score":      gteMatcher(cty.NumberFloatVal(0.95)),
		"size":       betweenMatcher(cty.NumberIntVal(100), cty.NumberIntVal(500)),
	})

	actual := cty.ObjectVal(map[string]cty.Value{
		"latency_ms": cty.NumberIntVal(120),
		"score":      cty.NumberFloatVal(0.99),
		"size":       cty.NumberIntVal(250),
	})

	if err := MatchJSON(expected, actual, true, "$"); err != nil {
		t.Fatalf("composite threshold expectation should pass, got %v", err)
	}

	failingActual := cty.ObjectVal(map[string]cty.Value{
		"latency_ms": cty.NumberIntVal(120),
		"score":      cty.NumberFloatVal(0.99),
		"size":       cty.NumberIntVal(600),
	})

	err := MatchJSON(expected, failingActual, true, "$")
	if err == nil {
		t.Fatalf("composite expectation should fail when size out of range")
	}

	if !strings.Contains(err.Error(), "$.size") {
		t.Fatalf("error must report path $.size, got %v", err)
	}
}
