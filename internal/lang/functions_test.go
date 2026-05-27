package lang

import (
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/tales-testing/tales/internal/model"
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

func TestHMACVariantsRegisteredAndLengths(t *testing.T) {
	t.Parallel()

	cases := map[string]int{
		"hmac_sha1_hex":       40,
		"hmac_sha224_hex":     56,
		"hmac_sha256_hex":     64,
		"hmac_sha384_hex":     96,
		"hmac_sha512_hex":     128,
		"hmac_sha512_224_hex": 56,
		"hmac_sha512_256_hex": 64,
	}

	for name, length := range cases {
		value := evalTestExpression(t, name+`("key", "message")`)
		got := value.AsString()

		if len(got) != length {
			t.Fatalf("%s: expected %d-char hex, got %d: %s", name, length, len(got), got)
		}

		if got != strings.ToLower(got) {
			t.Fatalf("%s: expected lowercase hex, got %s", name, got)
		}
	}
}

func TestHMACVariantsRFC4231Vectors(t *testing.T) {
	t.Parallel()

	// RFC 4231 test case 2: key="Jefe", data="what do ya want for nothing?".
	// These vectors are the canonical interoperability proof for HMAC-SHA*.
	cases := map[string]string{
		"hmac_sha224_hex": "a30e01098bc6dbbf45690f3a7e9e6d0f8bbea2a39e6148008fd05e44",
		"hmac_sha256_hex": "5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843",
		"hmac_sha384_hex": "af45d2e376484031617f78d2b58a6b1b9c7ef464f5a01b47e42ec3736322445e8e2240ca5e69e2c78b3239ecfab21649",
		"hmac_sha512_hex": "164b7a7bfcf819e2e395fbe73b56e0a387bd64222e831fd610270cd7ea2505549758bf75c05a994a6d034f65f8f0e6fdcaeab1a34d4a6b4b636e070a38bce737",
	}

	for name, expected := range cases {
		value := evalTestExpression(t, name+`("Jefe", "what do ya want for nothing?")`)
		if value.AsString() != expected {
			t.Fatalf("%s vector mismatch: got %s, want %s", name, value.AsString(), expected)
		}
	}
}

func TestHMACVariantsRejectArity(t *testing.T) {
	t.Parallel()

	if _, err := evalTestExpressionError(`hmac_sha512_hex("only-one")`); err == nil {
		t.Fatalf("expected error for single argument")
	}

	if _, err := evalTestExpressionError(`hmac_sha384_hex("a", "b", "c")`); err == nil {
		t.Fatalf("expected error for extra argument")
	}
}

func TestBase64URLEncodeKnownVectors(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		`base64url_encode("hello")`: "aGVsbG8",
		`base64url_encode("")`:      "",
		// ASCII ">>>" (0x3e 0x3e 0x3e) is "Pj4+" under standard base64 and
		// "Pj4-" under url-safe — exercises the '+' → '-' substitution.
		`base64url_encode(">>>")`: "Pj4-",
		// "???" (0x3f 0x3f 0x3f) is "Pz8/" → "Pz8_" — exercises '/' → '_'.
		`base64url_encode("???")`: "Pz8_",
	}

	for src, expected := range cases {
		got := evalTestExpression(t, src).AsString()
		if got != expected {
			t.Fatalf("%s: got %q, want %q", src, got, expected)
		}
	}
}

func TestBase64URLEncodeNoPadding(t *testing.T) {
	t.Parallel()

	// Two bytes encode to 3 url-safe chars; the standard padding would add "==".
	got := evalTestExpression(t, `base64url_encode("ab")`).AsString()
	if got != "YWI" {
		t.Fatalf("expected YWI (no padding), got %q", got)
	}

	if strings.Contains(got, "=") {
		t.Fatalf("base64url_encode must not emit padding, got %q", got)
	}
}

func TestBase64URLEncodeUnicode(t *testing.T) {
	t.Parallel()

	// UTF-8 of "café" is 63 61 66 c3 a9 — base64url(no padding) = "Y2Fmw6k".
	got := evalTestExpression(t, `base64url_encode("café")`).AsString()
	if got != "Y2Fmw6k" {
		t.Fatalf("unexpected unicode encoding: %q", got)
	}
}

func TestBase64URLEncodeRejectsArity(t *testing.T) {
	t.Parallel()

	if _, err := evalTestExpressionError(`base64url_encode()`); err == nil {
		t.Fatalf("expected error for missing argument")
	}

	if _, err := evalTestExpressionError(`base64url_encode("a", "b")`); err == nil {
		t.Fatalf("expected error for extra argument")
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
