package lang

import (
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hyperxlab/tales/internal/model"
	"github.com/zclconf/go-cty/cty"
)

func TestRegexFindFullMatch(t *testing.T) {
	t.Parallel()

	value := evalTestExpression(t, `regex_find("Your code is A1B2C3", "[A-Z0-9]{6}")`)
	if value.AsString() != "A1B2C3" {
		t.Fatalf("unexpected match: %s", value.AsString())
	}
}

func TestRegexFindCaptureGroup(t *testing.T) {
	t.Parallel()

	value := evalTestExpression(t, `regex_find("Your code is A1B2C3", "code is ([A-Z0-9]{6})", 1)`)
	if value.AsString() != "A1B2C3" {
		t.Fatalf("unexpected capture group: %s", value.AsString())
	}
}

func TestRegexFindNoMatchError(t *testing.T) {
	t.Parallel()

	_, err := evalTestExpressionError(`regex_find("no code", "[A-Z0-9]{6}")`)
	if err == nil || !strings.Contains(err.Error(), "found no match") {
		t.Fatalf("expected no match error, got %v", err)
	}
}

func TestRegexFindInvalidRegexError(t *testing.T) {
	t.Parallel()

	_, err := evalTestExpressionError(`regex_find("value", "[")`)
	if err == nil || !strings.Contains(err.Error(), "pattern is invalid") {
		t.Fatalf("expected invalid regex error, got %v", err)
	}
}

func TestRegexFindGroupOutOfRangeError(t *testing.T) {
	t.Parallel()

	_, err := evalTestExpressionError(`regex_find("Your code is A1B2C3", "code is ([A-Z0-9]{6})", 2)`)
	if err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("expected group out of range error, got %v", err)
	}
}

func TestRegexFindRejectsMultipleGroupIndices(t *testing.T) {
	t.Parallel()

	_, err := evalTestExpressionError(`regex_find("Your code is A1B2C3", "code is ([A-Z0-9]{6})", 1, 2)`)
	if err == nil || !strings.Contains(err.Error(), "at most one capture group index") {
		t.Fatalf("expected multiple group index error, got %v", err)
	}
}

func TestRegexFindConvertsNonStringInput(t *testing.T) {
	t.Parallel()

	value := evalTestExpression(t, `regex_find(123456, "[0-9]+")`)
	if value.AsString() != "123456" {
		t.Fatalf("unexpected converted match: %s", value.AsString())
	}
}

func TestOptionalMatcherProducesTaggedObject(t *testing.T) {
	t.Parallel()

	value := evalTestExpression(t, `optional("")`)
	if !value.Type().IsObjectType() {
		t.Fatalf("expected object, got %s", value.Type().FriendlyName())
	}

	name := value.GetAttr(matcherKey)
	if name.AsString() != "optional" {
		t.Fatalf("expected matcher name optional, got %s", name.AsString())
	}

	inner := value.GetAttr("value")
	if inner.AsString() != "" {
		t.Fatalf("expected empty inner, got %q", inner.AsString())
	}
}

func TestRequiredMatcherProducesTaggedObject(t *testing.T) {
	t.Parallel()

	value := evalTestExpression(t, `required(is_string())`)
	if !value.Type().IsObjectType() {
		t.Fatalf("expected object, got %s", value.Type().FriendlyName())
	}

	if value.GetAttr(matcherKey).AsString() != "required" {
		t.Fatalf("expected matcher name required")
	}

	inner := value.GetAttr("value")
	if inner.GetAttr(matcherKey).AsString() != "is_string" {
		t.Fatalf("expected inner is_string matcher, got %v", inner)
	}
}

func TestAnyMatcherProducesTaggedObject(t *testing.T) {
	t.Parallel()

	value := evalTestExpression(t, `any()`)
	if value.GetAttr(matcherKey).AsString() != "any" {
		t.Fatalf("expected any matcher tag")
	}
}

func TestOptionalAcceptsNullArgument(t *testing.T) {
	t.Parallel()

	value := evalTestExpression(t, `optional(null)`)
	if value.GetAttr(matcherKey).AsString() != "optional" {
		t.Fatalf("expected optional matcher tag")
	}

	inner := value.GetAttr("value")
	if !inner.IsNull() {
		t.Fatalf("expected inner to be null")
	}
}

func TestOptionalRejectsArity(t *testing.T) {
	t.Parallel()

	if _, err := evalTestExpressionError(`optional()`); err == nil {
		t.Fatalf("expected error for missing argument")
	}

	if _, err := evalTestExpressionError(`optional("", "x")`); err == nil {
		t.Fatalf("expected error for too many arguments")
	}
}

func TestAnyRejectsArguments(t *testing.T) {
	t.Parallel()

	if _, err := evalTestExpressionError(`any("x")`); err == nil {
		t.Fatalf("expected error for extra argument")
	}
}

func TestURLEncode(t *testing.T) {
	t.Parallel()

	value := evalTestExpression(t, `url_encode("a&b=c +%#")`)
	if value.AsString() != "a%26b%3Dc+%2B%25%23" {
		t.Fatalf("unexpected encoded value: %s", value.AsString())
	}
}

func TestJSONEncodeSortsObjectKeys(t *testing.T) {
	t.Parallel()

	value := evalTestExpression(t, `jsonencode({ z = 1, a = 2, m = 3 })`)
	if value.AsString() != `{"a":2,"m":3,"z":1}` {
		t.Fatalf("unexpected JSON output: %s", value.AsString())
	}
}

func TestJSONEncodeNestedObjectsAreSorted(t *testing.T) {
	t.Parallel()

	value := evalTestExpression(t, `jsonencode({ outer = { z = "last", a = "first" }, top = true })`)
	if value.AsString() != `{"outer":{"a":"first","z":"last"},"top":true}` {
		t.Fatalf("unexpected nested JSON output: %s", value.AsString())
	}
}

func TestJSONEncodeIsStableAcrossCalls(t *testing.T) {
	t.Parallel()

	const expr = `jsonencode({ d = 1, c = 2, b = 3, a = 4 })`

	first := evalTestExpression(t, expr).AsString()
	for range 50 {
		again := evalTestExpression(t, expr).AsString()
		if again != first {
			t.Fatalf("jsonencode is not stable: %q vs %q", first, again)
		}
	}
}

func TestJSONEncodeListsPreserveOrder(t *testing.T) {
	t.Parallel()

	value := evalTestExpression(t, `jsonencode([3, 1, 2])`)
	if value.AsString() != `[3,1,2]` {
		t.Fatalf("expected list order preserved, got %s", value.AsString())
	}
}

func TestJSONEncodeScalars(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		`jsonencode("hello \"world\"")`: `"hello \"world\""`,
		`jsonencode(42)`:                `42`,
		`jsonencode(3.14)`:              `3.14`,
		`jsonencode(true)`:              `true`,
		`jsonencode(false)`:             `false`,
		`jsonencode(null)`:              `null`,
	}

	for expr, expected := range cases {
		value := evalTestExpression(t, expr)
		if value.AsString() != expected {
			t.Fatalf("jsonencode %s = %s, expected %s", expr, value.AsString(), expected)
		}
	}
}

func TestNowUnixCloseToWallClock(t *testing.T) {
	t.Parallel()

	before := time.Now().Unix()
	value := evalTestExpression(t, `now_unix()`)
	after := time.Now().Unix()

	parsed, _ := value.AsBigFloat().Int64()
	if parsed < before-1 || parsed > after+1 {
		t.Fatalf("now_unix() %d is outside [%d, %d]", parsed, before, after)
	}
}

func TestNowUnixRejectsArguments(t *testing.T) {
	t.Parallel()

	if _, err := evalTestExpressionError(`now_unix(1)`); err == nil {
		t.Fatalf("expected error when passing arguments to now_unix")
	}
}

func TestNowRFC3339IsParseable(t *testing.T) {
	t.Parallel()

	value := evalTestExpression(t, `now_rfc3339()`)
	parsed, err := time.Parse(time.RFC3339, value.AsString())
	if err != nil {
		t.Fatalf("now_rfc3339 output %q is not RFC3339: %v", value.AsString(), err)
	}

	if parsed.Location().String() != "UTC" {
		t.Fatalf("expected UTC timezone, got %s", parsed.Location().String())
	}
}

func TestHMACSHA256HexKnownVector(t *testing.T) {
	t.Parallel()

	value := evalTestExpression(t, `hmac_sha256_hex("key", "The quick brown fox jumps over the lazy dog")`)
	if value.AsString() != "f7bc83f430538424b13298e6aa6fb143ef4d59a14946175997479dbc2d1a3cd8" {
		t.Fatalf("unexpected HMAC: %s", value.AsString())
	}
}

func TestHMACSHA256HexEmptyMessage(t *testing.T) {
	t.Parallel()

	value := evalTestExpression(t, `hmac_sha256_hex("secret", "")`)
	if value.AsString() != "f9e66e179b6747ae54108f82f8ade8b3c25d76fd30afde6c395822c530196169" {
		t.Fatalf("unexpected HMAC for empty message: %s", value.AsString())
	}
}

func TestHMACSHA256HexUnicode(t *testing.T) {
	t.Parallel()

	value := evalTestExpression(t, `hmac_sha256_hex("clé", "café")`)
	if len(value.AsString()) != 64 {
		t.Fatalf("expected 64-char hex, got %d chars: %s", len(value.AsString()), value.AsString())
	}
}

func TestHMACSHA256HexRejectsArity(t *testing.T) {
	t.Parallel()

	if _, err := evalTestExpressionError(`hmac_sha256_hex("only-one-arg")`); err == nil {
		t.Fatalf("expected error for single argument")
	}

	if _, err := evalTestExpressionError(`hmac_sha256_hex("a", "b", "c")`); err == nil {
		t.Fatalf("expected error for too many arguments")
	}
}

func TestHMACSHA256HexErrorDoesNotLeakSecret(t *testing.T) {
	t.Parallel()

	_, err := evalTestExpressionError(`hmac_sha256_hex()`)
	if err == nil {
		t.Fatalf("expected error for missing arguments")
	}

	if strings.Contains(err.Error(), "supersecret") {
		t.Fatalf("error message should never embed user secret material: %v", err)
	}
}

func TestHMACSHA1HexKnownVector(t *testing.T) {
	t.Parallel()

	value := evalTestExpression(t, `hmac_sha1_hex("key", "The quick brown fox jumps over the lazy dog")`)
	if value.AsString() != "de7c9b85b8b78aa6bc8a7a36f70a90701c9db4d9" {
		t.Fatalf("unexpected HMAC: %s", value.AsString())
	}
}

func TestHMACSHA1HexEmptyMessage(t *testing.T) {
	t.Parallel()

	value := evalTestExpression(t, `hmac_sha1_hex("secret", "")`)
	if len(value.AsString()) != 40 {
		t.Fatalf("expected 40-char hex, got %d chars: %s", len(value.AsString()), value.AsString())
	}
}

func TestHMACSHA1HexUnicode(t *testing.T) {
	t.Parallel()

	value := evalTestExpression(t, `hmac_sha1_hex("clé", "café")`)
	if len(value.AsString()) != 40 {
		t.Fatalf("expected 40-char hex, got %d chars: %s", len(value.AsString()), value.AsString())
	}
}

func TestHMACSHA1HexRejectsArity(t *testing.T) {
	t.Parallel()

	if _, err := evalTestExpressionError(`hmac_sha1_hex("only-one-arg")`); err == nil {
		t.Fatalf("expected error for single argument")
	}

	if _, err := evalTestExpressionError(`hmac_sha1_hex("a", "b", "c")`); err == nil {
		t.Fatalf("expected error for too many arguments")
	}
}

func TestHMACSHA1HexErrorDoesNotLeakSecret(t *testing.T) {
	t.Parallel()

	_, err := evalTestExpressionError(`hmac_sha1_hex()`)
	if err == nil {
		t.Fatalf("expected error for missing arguments")
	}

	if strings.Contains(err.Error(), "supersecret") {
		t.Fatalf("error message should never embed user secret material: %v", err)
	}
}

func TestTOTPDefaultReturnsSixDigits(t *testing.T) {
	t.Parallel()

	value := evalTestExpression(t, `totp("GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ")`)
	got := value.AsString()

	if len(got) != 6 {
		t.Fatalf("expected 6-digit string, got %q (%d chars)", got, len(got))
	}
}

func TestTOTPWithExplicitTimestamp(t *testing.T) {
	t.Parallel()

	value := evalTestExpression(t, `totp("GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", {period = 30, digits = 8, algorithm = "SHA1", timestamp = 59})`)
	if value.AsString() != "94287082" {
		t.Fatalf("expected RFC 6238 vector 94287082, got %s", value.AsString())
	}
}

func TestTOTPRejectsUnknownOption(t *testing.T) {
	t.Parallel()

	_, err := evalTestExpressionError(`totp("GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", {foo = 1})`)
	if err == nil || !strings.Contains(err.Error(), "unknown option") {
		t.Fatalf("expected unknown option error, got %v", err)
	}
}

func TestTOTPRejectsUnsupportedAlgorithm(t *testing.T) {
	t.Parallel()

	_, err := evalTestExpressionError(`totp("GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", {algorithm = "SHA256"})`)
	if err == nil || !strings.Contains(err.Error(), "unsupported TOTP algorithm") {
		t.Fatalf("expected unsupported algorithm error, got %v", err)
	}
}

func TestTOTPRejectsTooManyArguments(t *testing.T) {
	t.Parallel()

	_, err := evalTestExpressionError(`totp("GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", {digits = 6}, {extra = true})`)
	if err == nil || !strings.Contains(err.Error(), "too many arguments") {
		t.Fatalf("expected too-many-arguments error, got %v", err)
	}
}

func TestTOTPRejectsNonObjectOptions(t *testing.T) {
	t.Parallel()

	_, err := evalTestExpressionError(`totp("GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", "not-an-object")`)
	if err == nil || !strings.Contains(err.Error(), "options must be an object") {
		t.Fatalf("expected options-must-be-an-object error, got %v", err)
	}
}

func TestTOTPInvalidSecretIsOpaque(t *testing.T) {
	t.Parallel()

	_, err := evalTestExpressionError(`totp("BADBADBAD!!!")`)
	if err == nil {
		t.Fatalf("expected invalid-secret error")
	}

	if strings.Contains(err.Error(), "BADBADBAD") {
		t.Fatalf("error must never echo raw secret material: %v", err)
	}
}

func evalTestExpression(t *testing.T, src string) cty.Value {
	t.Helper()

	value, err := evalTestExpressionError(src)
	if err != nil {
		t.Fatalf("eval failed: %v", err)
	}

	return value
}

func evalTestExpressionError(src string) (cty.Value, error) {
	evaluator := NewEvaluator(nil)

	return evaluator.Eval(parseLangFunctionExpr(src), ScopeData{
		Config:   map[string]cty.Value{},
		Result:   map[string]cty.Value{},
		Request:  map[string]cty.Value{},
		Response: map[string]cty.Value{},
		Input:    map[string]cty.Value{},
	}, GenerateMeta{})
}

func parseLangFunctionExpr(src string) model.Expression {
	expr, diags := hclsyntax.ParseExpression([]byte(src), "test.hcl", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		panic(diags.Error())
	}

	return model.Expression{Expr: expr, File: "test.hcl", Line: 1}
}
